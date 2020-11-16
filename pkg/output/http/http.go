package http

import (
	"encoding/json"
	"errors"
	syslog "github.com/trevorlinton/remote_syslog2/syslog"
	"net/http"
	"net/url"
	"strings"
)

// Syslog http ouput struct
type Syslog struct {
	url      url.URL
	endpoint string
	client   *http.Client
	packets  chan syslog.Packet
	errors   chan<- error
	stop     chan struct{}
	closed   bool
}

var syslogSchemas = []string{"https://", "http://"}

// Test the schema to see if its https/http
func Test(endpoint string) bool {
	for _, schema := range syslogSchemas {
		if strings.HasPrefix(strings.ToLower(endpoint), schema) == true {
			return true
		}
	}
	return false
}

// Create a new http json output.
func Create(endpoint string, errorsCh chan<- error) (*Syslog, error) {
	if Test(endpoint) == false {
		return nil, errors.New("Invalid endpoint")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	return &Syslog{
		endpoint: endpoint,
		url:      *u,
		client:   &http.Client{},
		packets:  make(chan syslog.Packet, 10),
		errors:   errorsCh,
		stop:     make(chan struct{}, 1),
		closed:   false,
	}, nil
}

// Dial connects to the specified http point
func (log *Syslog) Dial() error {
	go log.loop()
	return nil
}

// Close closes the http output
func (log *Syslog) Close() error {
	log.closed = true
	log.stop <- struct{}{}
	close(log.packets)
	return nil
}

// Pools returns whether the output pools connections
func (log *Syslog) Pools() bool {
	return true
}

// Packets returns a channel where packets can be sent to the http output handler
func (log *Syslog) Packets() chan syslog.Packet {
	return log.packets
}

func (log *Syslog) loop() {
	for {
		select {
		case p := <-log.packets:
			payload, err := json.Marshal(p)
			if err == nil {
				resp, err := log.client.Post(log.url.String(), "application/json", strings.NewReader(string(payload)))
				if log.closed == true {
					return
				}
				if err != nil {
					log.errors <- err
				} else {
					resp.Body.Close()
					if resp.StatusCode >= http.StatusMultipleChoices || resp.StatusCode < http.StatusOK {
						log.errors <- errors.New("invalid response from endpoint: " + resp.Status)
					}
				}
			} else {
				log.errors <- err
			}
		case <-log.stop:
			close(log.stop)
			return
		}
	}
}
