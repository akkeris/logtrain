package sysloghttp

import (
	"errors"
	syslog "github.com/papertrail/remote_syslog2/syslog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Syslog struct {
	url      url.URL
	endpoint string
	client   *http.Client
	packets  chan syslog.Packet
	errors   chan error
	stop     chan struct{}
}

var syslogSchemas = []string{"syslog+http://", "syslog+https://"}

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
	u, err := url.Parse(strings.Replace(endpoint, "syslog+", "", 1))
	if err != nil {
		return nil, err
	}
	return &Syslog{
		endpoint: endpoint,
		url:      *u,
		client:   &http.Client{},
		packets:  make(chan syslog.Packet, 10),
		errors:   make(chan error, 1),
		stop:     make(chan struct{}, 1),
	}, nil
}

func (log *Syslog) Dial() error {
	go log.loop()
	return nil
}
func (log *Syslog) Close() error {
	log.stop <- struct{}{}
	close(log.packets)
	close(log.errors)
	return nil
}
func (log *Syslog) Pools() bool {
	return true
}
func (log *Syslog) Packets() chan syslog.Packet {
	return log.packets
}
func (log *Syslog) Errors() chan error {
	return log.errors
}

func (log *Syslog) loop() {
	timer := time.NewTicker(time.Second)
	var payload string = ""
	for {
		select {
		case p := <-log.packets:
			payload = payload + p.Generate(MaxLogSize) + "\n"
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
			close(log.stop)
			return
		}
	}
}
