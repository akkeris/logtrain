package syslogtcp

import (
	"net/url"
	"time"
	"strings"
	"errors"
	syslog "github.com/papertrail/remote_syslog2/syslog"
)

type Syslog struct {
	url url.URL
	endpoint string
	logger *syslog.Logger
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
	}, nil
}

func (log *Syslog) Dial() error {
	dest, err := syslog.Dial("logtrain.akkeris-system.svc.cluster.local", syslogNetwork, log.url.Host, nil, time.Second * 4, time.Second * 4, MaxLogSize)
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
func (log *Syslog) Packets() (chan syslog.Packet) {
	return log.logger.Packets
}
func (log *Syslog) Errors() (chan error) {
	return log.logger.Errors
}