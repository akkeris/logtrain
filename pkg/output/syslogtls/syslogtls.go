package syslogtls

import (
	"crypto/x509"
	"encoding/base64"
	"errors"
	"github.com/akkeris/logtrain/internal/debug"
	syslog "github.com/trevorlinton/remote_syslog2/syslog"
	"net/url"
	"strings"
	"time"
)

// Syslog tls output structure
type Syslog struct {
	url      url.URL
	endpoint string
	logger   *syslog.Logger
	roots    *x509.CertPool
	errors   chan<- error
}

var syslogSchemas = []string{"syslog+tls://"}

const syslogNetwork = "tls"
const maxLogSize int = 99990

// Test for a syslog tls schema
func Test(endpoint string) bool {
	for _, schema := range syslogSchemas {
		if strings.HasPrefix(strings.ToLower(endpoint), schema) == true {
			return true
		}
	}
	return false
}

// Create a syslog tls output
func Create(endpoint string, errorsCh chan<- error) (*Syslog, error) {
	if Test(endpoint) == false {
		return nil, errors.New("Invalid endpoint")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if u.Query().Get("ca") != "" {
		decoded, err := base64.StdEncoding.DecodeString(u.Query().Get("ca"))
		if err != nil {
			return nil, err
		}
		if ok := roots.AppendCertsFromPEM([]byte(decoded)); ok == false {
			return nil, errors.New("The ca provided was invalid.")
		}
	}
	return &Syslog{
		endpoint: endpoint,
		url:      *u,
		roots:    roots,
		errors:   errorsCh,
	}, nil
}

// Connect to the syslog output
func (log *Syslog) Dial() error {
	debug.Debugf("[syslog+tls/output]: Dial called for %s\n", log.endpoint)
	dest, err := syslog.Dial("logtrain.akkeris-system.svc.cluster.local", syslogNetwork, log.url.Host, log.roots, time.Second*4, time.Second*4, maxLogSize)
	if err != nil {
		debug.Debugf("[syslog+tls/output]: Dial encountered an error on %s: %s\n", log.endpoint, err.Error())
		return err
	}
	log.logger = dest
	return nil
}

// Close the syslog output
func (log *Syslog) Close() error {
	debug.Debugf("[syslog+tls/output]: Close called for %s\n", log.endpoint)
	return log.logger.Close()
}

// Pools returns whether the syslog endpoint pools connections
func (log *Syslog) Pools() bool {
	return false
}

// Packets returns a channel to send packets to the syslog endpoint
func (log *Syslog) Packets() chan syslog.Packet {
	return log.logger.Packets
}
