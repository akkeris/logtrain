package router

import (
	"errors"
	"github.com/akkeris/logtrain/internal/debug"
	"github.com/akkeris/logtrain/pkg/output"
	"github.com/trevorlinton/remote_syslog2/syslog"
	"hash/crc32"
	"sync"
	"time"
)

/*
 * Responsibilities:
 * - Determines if the destination is misbehaving and temporarily pauses traffic to it.
 * - Pooling connections and distributing incoming messages over pools
 * - Detecting back pressure and increasing pools
 * - Decreasing pools if output disconnects or if pressure is normal
 * - Reporting information (metrics) or errors to upstream
 *
 * Principals:
 * - Only create one drain per endpoint.  If it can be pooled, it will
 * - Propogate up all errors, assume we're still good to go unless explicitly closed.
 */

const increasePercentTrigger = 0.50 // > 50% full.
const decreasePercentTrigger = 0.02 // 2% full.
const decreaseTrendTrigger = 0.00   // pressure has been decreasing on average.
const bufferSize = 1024             // amount of records to keep in memory until upstream fails.
const drainErrorThreshold = 100     // the threshold for when we should pause the drain.
const drainErrorPercentage = 0.5	// the threshold for the percentage of the sent message resulted in errors.
const drainErrorTimeoutInMins = 5	// The amount of minutes to pause the stream if there are errors.

type Drain struct {
	Input          chan syslog.Packet
	Info           chan string
	Error          chan error
	Endpoint       string
	maxconnections uint32
	errors         uint32
	connections    []output.Output
	mutex          *sync.Mutex
	sent           uint32
	stop           chan struct{}
	pressure       float64
	open           uint32
	sticky         bool
	transportPools bool
	pressureTrend  float64
	scaling        bool
}

func Create(endpoint string, maxconnections uint32, sticky bool) (*Drain, error) {
	if maxconnections > 1024 {
		return nil, errors.New("Max connections must not be more than 1024.")
	}
	if maxconnections == 0 {
		return nil, errors.New("Max connections must not be 0.")
	}
	drain := Drain{
		Endpoint:       endpoint,
		maxconnections: maxconnections,
		errors:         0,
		sent:           0,
		pressure:       0,
		open:           0,
		sticky:         sticky,
		transportPools: false,
		Input:          make(chan syslog.Packet, bufferSize),
		Info:           make(chan string, 1),
		Error:          make(chan error, 1),
		connections:    make([]output.Output, 0),
		mutex:          &sync.Mutex{},
		stop:           make(chan struct{}, 1),
		pressureTrend:  0,
		scaling:        false,
	}

	if err := output.TestEndpoint(endpoint); err != nil {
		return nil, err
	}
	return &drain, nil
}

func (drain *Drain) MaxConnections() uint32 {
	return drain.maxconnections
}

func (drain *Drain) OpenConnections() uint32 {
	return drain.open
}

func (drain *Drain) Pressure() float64 {
	return drain.pressure
}

func (drain *Drain) Sent() uint32 {
	return drain.sent
}

func (drain *Drain) Errors() uint32 {
	return drain.errors
}

func (drain *Drain) ResetMetrics() {
	drain.sent = 0
	drain.errors = 0
}

func (drain *Drain) Dial() error {
	debug.Infof("[drains] Dailing drain %s...\n", drain.Endpoint)
	if drain.open != 0 {
		return errors.New("Dial should not be called twice.")
	}
	if err := drain.connect(); err != nil {
		debug.Debugf("[drains] a call to connect resulted in an error: %s\n", err.Error())
		return err
	}

	if drain.transportPools == true {
		go drain.loopTransportPools()
	} else if drain.sticky == true {
		go drain.loopSticky()
	} else {
		go drain.loopRoundRobin()
	}
	return nil
}

func (drain *Drain) Close() error {
	debug.Infof("[drains] Closing all connections in drain to %s\n", drain.Endpoint)
	drain.stop <- struct{}{}
	drain.mutex.Lock()
	defer drain.mutex.Unlock()
	var err error = nil
	for _, conn := range drain.connections {
		debug.Debugf("[drains] Closing connection to %s\n", drain.Endpoint)
		if err = conn.Close(); err != nil {
			debug.Debugf("[drains] Received error trying to close connection to %s: %s\n", drain.Endpoint, err.Error())
		}
		drain.open--
	}
	drain.connections = make([]output.Output, 0)
	return err
}

func (drain *Drain) connect() error {
	drain.mutex.Lock()
	defer drain.mutex.Unlock()
	defer func() { drain.scaling = false }()
	if drain.open >= drain.maxconnections {
		return nil
	}
	debug.Debugf("[drains] Drain %s connecting...\n", drain.Endpoint)
	conn, err := output.Create(drain.Endpoint, drain.Error)
	if err != nil {
		debug.Errorf("[drains] Received an error attempting to create endpoint %s: %s\n", drain.Endpoint, err.Error())
		return err
	}
	if err := conn.Dial(); err != nil {
		debug.Errorf("[drains] Received an error attempting to dial %s: %s\n", drain.Endpoint, err.Error())
		return err
	}

	drain.transportPools = conn.Pools()
	drain.connections = append(drain.connections, conn)
	drain.open++
	debug.Infof("[drains] Increasing pool size on %s to %d because back pressure was %f%% (trending by %f%%)\n", drain.Endpoint, drain.open, drain.pressure*100, drain.pressureTrend*100)
	return nil
}

func (drain *Drain) disconnect(force bool) error {
	drain.mutex.Lock()
	defer drain.mutex.Unlock()
	defer func() { drain.scaling = false }()
	if drain.open < 2 && force == false {
		// disconnects are fired as go routines by the loop writer, depending on how
		// quickly the loop writer executes and when the next mutex lock releases we
		// may have been called but shouldn't actually execute. Unless were forced.
		return nil
	} else if drain.open == 0 {
		debug.Errorf("[drains] ERROR: disconnect(true) called with no open connections available to disconnect.\n")
		return nil
	}
	conn := drain.connections[len(drain.connections)-1]
	drain.connections = drain.connections[:len(drain.connections)-1]
	if err := conn.Close(); err != nil {
		debug.Errorf("[drains] Received error trying to close connection to %s during scale down period: %s\n", drain.Endpoint, err.Error())
	}
	drain.open--
	debug.Infof("[drains] Decreased pool size on %s to %d because back pressure was below %f%% at %f%% (trending by %f%%)\n", drain.Endpoint, drain.open, decreasePercentTrigger*100, drain.pressure*100, drain.pressureTrend*100)
	return nil
}

/*
 * The write loop functions below are critical paths, removing as much operations in these as possible
 * is important to performance. An if statement to use a sticky or round robin
 * strategy in the drain is therefore pushed up to the Dail function, unfortuntely
 * this does mean there's some repetitive code.
 */

func (drain *Drain) loopRoundRobin() {
	var maxPackets = cap(drain.Input)
	for {
		select {
		case packet := <-drain.Input:
			drain.mutex.Lock()
			drain.sent++
			drain.connections[drain.sent%drain.open].Packets() <- packet
			newPressure := (drain.pressure + (float64(len(drain.Input)) / float64(maxPackets))) / float64(2)
			drain.pressureTrend = ((newPressure - drain.pressure) + drain.pressureTrend) / float64(2)
			drain.pressure = newPressure
			if drain.scaling == false && drain.pressure > increasePercentTrigger && drain.open <= drain.maxconnections {
				drain.scaling = true
				go drain.connect()
			} else if drain.scaling == false && drain.pressure < decreasePercentTrigger && drain.open > 1 && drain.pressureTrend < decreaseTrendTrigger {
				drain.scaling = true
				go drain.disconnect(false)
			}
			drain.mutex.Unlock()
		case <-drain.stop:
			return
		}
	}
}

// Potentially look at the previous pressure see if its a downward trend.
func (drain *Drain) loopSticky() {
	var maxPackets = cap(drain.Input)
	for {
		select {
		case packet := <-drain.Input:
			drain.mutex.Lock()
			drain.sent++
			drain.connections[uint32(crc32.ChecksumIEEE([]byte(packet.Hostname+packet.Tag))%drain.open)].Packets() <- packet
			newPressure := (drain.pressure + (float64(len(drain.Input)) / float64(maxPackets))) / float64(2)
			drain.pressureTrend = ((newPressure - drain.pressure) + drain.pressureTrend) / float64(2)
			drain.pressure = newPressure
			if drain.scaling == false && drain.pressure > increasePercentTrigger && drain.open <= drain.maxconnections {
				drain.scaling = true
				go drain.connect()
			} else if drain.scaling == false && drain.pressure < decreasePercentTrigger && drain.open > 1 && drain.pressureTrend < decreaseTrendTrigger {
				drain.scaling = true
				go drain.disconnect(false)
			}
			drain.mutex.Unlock()
		case err := <-drain.Error:
			if err != nil && drain.errors < drainErrorThreshold {
				debug.Errorf("[drains] An error occured on endpoint %s: %s", drain.Endpoint, err.Error())
			}
			if float64(drain.errors) > (float64(drain.sent) * drainErrorPercentage) && drain.errors < drainErrorThreshold {
				debug.Errorf("[drains] Pausing drain %s as it has incurred too many errors.\n", drain.Endpoint)
				<-time.NewTimer(time.Minute * drainErrorTimeoutInMins).C
			}
			drain.errors++
		case <-drain.stop:
			return
		}
	}
}

func (drain *Drain) loopTransportPools() {
	var maxPackets = cap(drain.Input)
	for {
		select {
		case packet := <-drain.Input:
			drain.mutex.Lock()
			drain.sent++
			drain.connections[0].Packets() <- packet
			drain.pressure = (drain.pressure + (float64(len(drain.Input)) / float64(maxPackets))) / float64(2)
			drain.mutex.Unlock()
		case err := <-drain.Error:
			if err != nil && drain.errors < drainErrorThreshold {
				debug.Errorf("[drains] An error occured on endpoint %s: %s", drain.Endpoint, err.Error())
			}
			if float64(drain.errors) > (float64(drain.sent) * drainErrorPercentage) && drain.errors < drainErrorThreshold {
				debug.Errorf("[drains] Pausing drain %s as it has incurred too many errors.\n", drain.Endpoint)
				<-time.NewTimer(time.Minute * drainErrorTimeoutInMins).C
			}
			drain.errors++
		case <-drain.stop:
			return
		}
	}
}
