package sysloghttp

import (
	"crypto/tls"
	"errors"
	syslog "github.com/trevorlinton/remote_syslog2/syslog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Syslog http output structure
type Syslog struct {
	url      url.URL
	endpoint string
	client   *http.Client
	packets  chan syslog.Packet
	errors   chan<- error
	stop     chan struct{}
}

var syslogSchemas = []string{"syslog+http://", "syslog+https://"}

const maxLogSize int = 99990

// Test for a syslog http schema
func Test(endpoint string) bool {
	for _, schema := range syslogSchemas {
		if strings.HasPrefix(strings.ToLower(endpoint), schema) == true {
			return true
		}
	}
	return false
}

// Create a syslog http output
func Create(endpoint string, errorsCh chan<- error) (*Syslog, error) {
	if Test(endpoint) == false {
		return nil, errors.New("Invalid endpoint")
	}
	u, err := url.Parse(strings.Replace(endpoint, "syslog+", "", 1))
	if err != nil {
		return nil, err
	}
	client := http.Client{}
	if u.Query().Get("insecure") == "true" {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		q := u.Query()
		q.Del("insecure")
		u.RawQuery = q.Encode()
	}
	return &Syslog{
		endpoint: endpoint,
		url:      *u,
		client:   &client,
		packets:  make(chan syslog.Packet, 10),
		errors:   errorsCh,
		stop:     make(chan struct{}, 1),
	}, nil
}

// Dial connects to the syslog output
func (log *Syslog) Dial() error {
	go log.loop()
	return nil
}

// Close the syslog output
func (log *Syslog) Close() error {
	close(log.stop)
	close(log.packets)
	return nil
}

// Pools checks to see if the output pools
func (log *Syslog) Pools() bool {
	return true
}

// Packets returns a channel to send syslog packets to the endpoint
func (log *Syslog) Packets() chan syslog.Packet {
	return log.packets
}

func (log *Syslog) loop() {
	timer := time.NewTicker(time.Second)
	var payload string
	for {
		select {
		case p, ok := <-log.packets:
			if !ok {
				return
			}
			payload = payload + p.Generate(maxLogSize) + "\n"
		case <-timer.C:
			if payload != "" {
				resp, err := log.client.Post(log.url.String(), "application/syslog", strings.NewReader(string(payload)))
				payload = ""
				if err != nil {
					log.errors <- err
				} else {
					resp.Body.Close()
					if resp.StatusCode >= http.StatusMultipleChoices || resp.StatusCode < http.StatusOK {
						log.errors <- errors.New("invalid response from endpoint: " + resp.Status)
					}
				}
			}
		case <-log.stop:
			return
		}
	}
}
