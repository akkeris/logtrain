package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	syslog "github.com/papertrail/remote_syslog2/syslog"
	"github.com/akkeris/logtrain/pkg/output/packet"
)

type Syslog struct {
	url url.URL
	endpoint string
	client *http.Client
	packets chan syslog.Packet
	errors chan error
	stop chan struct{}
	closed bool
}

var syslogSchemas = []string{"https://", "http://"}
const MaxLogSize int = 99990

func Test(endpoint string) bool {
	for _, schema := range syslogSchemas {
		if strings.HasPrefix(strings.ToLower(endpoint), schema) == true {
			return true
		}
	}
	return false
}

func Create(endpoint string) (*Syslog, error) {
	if Test(endpoint) == false {
		return nil, errors.New("Invalid endpoint")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err;
	}
	return &Syslog {
		endpoint: endpoint,
		url: *u,
		client: &http.Client{},
		packets: make(chan syslog.Packet, 10),
		errors: make(chan error, 1),
		stop: make(chan struct{}, 1),
		closed: false,
	}, nil
}

func (log *Syslog) Dial() error {
	go log.loop()
	return nil
}
func (log *Syslog) Close() error {
	log.closed = true
	log.stop <- struct{}{}
	close(log.packets)
	close(log.errors)
	return nil
}
func (log *Syslog) Pools() bool {
	return true
}
func (log *Syslog) Packets() (chan syslog.Packet) {
	return log.packets
}
func (log *Syslog) Errors() (chan error) {
	return log.errors
}
func (log *Syslog) loop() {
	for {
		select {
		case p := <-log.packets:
			payload, err := json.Marshal(packet.Packet{p})
			if err == nil {
				resp, err := log.client.Post(log.url.String(), "application/json", strings.NewReader(string(payload)))
				if log.closed == true {
					return
				}
				if err != nil {
					log.errors <- err
				} else {
					resp.Body.Close()
					if (resp.StatusCode >= http.StatusMultipleChoices || resp.StatusCode < http.StatusOK) {
						log.errors <- errors.New("invalid response from endpoint: " + resp.Status)
					}
				}
			} else {
				log.errors <- err
			}
		case <-log.stop:
			close(log.stop)
			return;
		}
	}
}