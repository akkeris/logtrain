package syslogtcp

import (
	"errors"
	syslog "github.com/trevorlinton/remote_syslog2/syslog"
	"net/url"
	"strings"
	"time"
)

type Syslog struct {
	url      url.URL
	endpoint string
	logger   *syslog.Logger
	errors   chan <-error
}

var syslogSchemas = []string{"syslog+tcp://"}

const syslogNetwork = "tcp"
const MaxLogSize int = 99990

func Test(endpoint string) bool {
	for _, schema := range syslogSchemas {
		if strings.HasPrefix(strings.ToLower(endpoint), schema) == true {
			return true
		}
	}
	return false
}

func Create(endpoint string, errorsCh chan <-error) (*Syslog, error) {
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

func (log *Syslog) Dial() error {
	dest, err := syslog.Dial("logtrain.akkeris-system.svc.cluster.local", syslogNetwork, log.url.Host, nil, time.Second*4, time.Second*4, MaxLogSize)
	if err != nil {
		return err
	}
	log.logger = dest
	return nil
}
func (log *Syslog) Close() error {
	return log.logger.Close()
}
func (log *Syslog) Pools() bool {
	return false
}
func (log *Syslog) Packets() chan syslog.Packet {
	return log.logger.Packets
}
