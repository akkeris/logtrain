package syslogudp

import (
	"errors"
	syslog "github.com/trevorlinton/remote_syslog2/syslog"
	"net/url"
	"strings"
	"time"
)

// Syslog udp output structure
type Syslog struct {
	url      url.URL
	endpoint string
	logger   *syslog.Logger
	errors   chan<- error
}

var syslogSchemas = []string{"syslog+udp://", "syslog://"}

const syslogNetwork = "udp"
const MaxLogSize int = 99990

// Test for a syslog udp schema
func Test(endpoint string) bool {
	for _, schema := range syslogSchemas {
		if strings.HasPrefix(strings.ToLower(endpoint), schema) == true {
			return true
		}
	}
	return false
}

// Create a syslog tcp output
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
		errors:   errorsCh,
	}, nil
}

// Connect to the syslog output
func (log *Syslog) Dial() error {
	dest, err := syslog.Dial("logtrain.akkeris-system.svc.cluster.local", syslogNetwork, log.url.Host, nil, time.Second*4, time.Second*4, MaxLogSize)
	if err != nil {
		return err
	}
	log.logger = dest
	return nil
}

// Close the syslog output
func (log *Syslog) Close() error {
	return log.logger.Close()
}

// See if the syslog output pools
func (log *Syslog) Pools() bool {
	return false
}

// Send packets to the syslog endpoint
func (log *Syslog) Packets() chan syslog.Packet {
	return log.logger.Packets
}
