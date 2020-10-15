package syslogtls

import (
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"time"
	server "github.com/mcuadros/go-syslog"
	syslog "github.com/papertrail/remote_syslog2/syslog"
)

type HandlerSyslogTls struct {
	errors chan error
	packets chan syslog.Packet
	stop chan struct{}
	channel server.LogPartsChannel
	server *server.Server
	server_name string
	key_pem string
	cert_pem string
	ca_pem string
	address string
}

func (handler *HandlerSyslogTls) getServerConfig() (*tls.Config, error) {
	capool, err := x509.SystemCertPool()
	if err != nil {
		capool = x509.NewCertPool()
	}
	if handler.ca_pem != "" {
		if ok := capool.AppendCertsFromPEM([]byte(handler.ca_pem)); !ok {
			return nil, errors.New("Unable to parse pem.")
		}
	}

	cert, err := tls.X509KeyPair([]byte(handler.cert_pem), []byte(handler.key_pem))
	if err != nil {
		return nil, err
	}

	config := tls.Config{
		Certificates: []tls.Certificate{cert},
		ServerName: handler.server_name,
		RootCAs: capool,
	}
	config.Rand = rand.Reader
	return &config, nil
}

func (handler *HandlerSyslogTls) Close() error {
	handler.stop <-struct{}{}
	handler.server.Kill()
	close(handler.packets)
	close(handler.errors)
	close(handler.channel)
	close(handler.stop)
	return nil
}

func defaultTlsPeerName(tlsConn *tls.Conn) (tlsPeer string, ok bool) {
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) <= 0 {
		return "", true
	}
	cn := state.PeerCertificates[0].Subject.CommonName
	return cn, true
}

func (handler *HandlerSyslogTls) Dial() error {
	if handler.server != nil {
		return errors.New("Dial may only be called once.")
	}
	config, err := handler.getServerConfig()
	if err != nil {
		return err
	}
	handler.server = server.NewServer()
	handler.server.SetTlsPeerNameFunc(defaultTlsPeerName)
	handler.server.SetFormat(server.RFC6587)
	handler.server.SetHandler(server.NewChannelHandler(handler.channel))
	if err := handler.server.ListenTCPTLS(handler.address, config); err != nil {
		return err
	}
	if err := handler.server.Boot(); err != nil {
		return err
	}
	go func() {
		for {
			select {
			case message := <-handler.channel:
				var severity int = 0
				var facility int = 0
				var hostname string = ""
				var tag string = ""
				var timestamp time.Time = time.Now()
				var msg string = ""
				if s, ok := message["severity"].(int); ok {
					severity = s
				}
				if f, ok := message["facility"].(int); ok {
					facility = f
				}
				if h, ok := message["hostname"].(string); ok {
					hostname = h
				}
				if m, ok := message["message"].(string); ok {
					msg = m
				}
				if t, ok := message["app_name"].(string); ok {
					tag = t
				}
				if i, ok := message["time"].(time.Time); ok {
					timestamp = i
				}
				handler.Packets() <- syslog.Packet{
					Severity: syslog.Priority(severity),
					Facility: syslog.Priority(facility),
					Hostname: hostname,
					Tag: tag,
					Time: timestamp,
					Message: msg,
				}
			case <-handler.stop:
				return
			}
		}
	}()
	return nil
}

func (handler *HandlerSyslogTls) Errors() (chan error) {
	return handler.errors
}

func (handler *HandlerSyslogTls) Packets() (chan syslog.Packet) {
	return handler.packets
}

func (handler *HandlerSyslogTls) Pools() bool {
	return true
}

func Create(server_name string, key_pem string, cert_pem string, ca_pem string, address string) (*HandlerSyslogTls, error) {
	return &HandlerSyslogTls{
		errors: make(chan error, 1),
		packets: make(chan syslog.Packet, 100),
		stop: make(chan struct{}, 1),
		channel: make(server.LogPartsChannel),
		server: nil,
		server_name: server_name,
		key_pem: key_pem,
		cert_pem: cert_pem,
		ca_pem: ca_pem,
		address: address,
	}, nil
}