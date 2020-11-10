package syslogtls

import (
	"crypto/x509"
	"encoding/base64"
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
	roots    *x509.CertPool
}

var syslogSchemas = []string{"syslog+tls://"}

const syslogNetwork = "tls"
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
	}, nil
}

func (log *Syslog) Dial() error {
	dest, err := syslog.Dial("logtrain.akkeris-system.svc.cluster.local", syslogNetwork, log.url.Host, log.roots, time.Second*4, time.Second*4, MaxLogSize)
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
func (log *Syslog) Errors() chan error {
	return log.logger.Errors
}
