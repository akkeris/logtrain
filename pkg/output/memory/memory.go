package memory

import (
	"errors"
	"github.com/trevorlinton/remote_syslog2/syslog"
	"net/url"
	"strings"
)

// GlobalInternalOutputs is a map of unique ids to channels
// that are used to push routed logs back to a pre-created channel.
var GlobalInternalOutputs map[string]chan syslog.Packet

// Syslog http ouput struct
type Syslog struct {
	url      url.URL
	endpoint string
	packets  chan syslog.Packet
	errors   chan<- error
	stop     chan struct{}
	closed   bool
}

var syslogSchemas = []string{"memory://"}

func NewMemoryChannel(uid string) <- chan syslog.Packet {
	GlobalInternalOutputs[uid] = make(chan syslog.Packet, 1)
	return GlobalInternalOutputs[uid]
}

// Test the schema to see if its memory://
func Test(endpoint string) bool {
	for _, schema := range syslogSchemas {
		if strings.HasPrefix(strings.ToLower(endpoint), schema) == true {
			return true
		}
	}
	return false
}

// Create a new memory output
func Create(endpoint string, errorsCh chan<- error) (*Syslog, error) {
	if Test(endpoint) == false {
		return nil, errors.New("Invalid endpoint")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	id := strings.ReplaceAll(u.Path, "/", "")
	packets, ok := GlobalInternalOutputs[id]
	if !ok {
		return nil, errors.New("id not found in global register")
	}

	return &Syslog{
		endpoint: endpoint,
		url:      *u,
		packets:  packets,
		errors:   errorsCh,
		closed:   false,
	}, nil
}

// Dial connects to the specified memory point
func (log *Syslog) Dial() error {
	return nil
}

// Close closes the memory output
func (log *Syslog) Close() error {
	if log.closed {
		return nil
	}
	log.closed = true
	close(log.packets)
	return nil
}

// Pools returns whether the output pools connections
func (log *Syslog) Pools() bool {
	return true
}

// Packets returns a channel where packets can be sent to the memory output handler
func (log *Syslog) Packets() chan syslog.Packet {
	return log.packets
}

func init() {
	GlobalInternalOutputs = make(map[string]chan syslog.Packet, 0)
}
