package syslogtcp

import (
	"errors"
	server "github.com/mcuadros/go-syslog"
	syslog "github.com/trevorlinton/remote_syslog2/syslog"
	"time"
)

/* HandlerSyslogTCP handles Syslog TCP inputs */
type HandlerSyslogTCP struct {
	errors  chan error
	packets chan syslog.Packet
	stop    chan struct{}
	channel server.LogPartsChannel
	server  *server.Server
	address string
}

// Close input handler.
func (handler *HandlerSyslogTCP) Close() error {
	handler.stop <- struct{}{}
	handler.server.Kill()
	close(handler.packets)
	close(handler.errors)
	close(handler.channel)
	close(handler.stop)
	return nil
}

// Dial input handler.
func (handler *HandlerSyslogTCP) Dial() error {
	if handler.server != nil {
		return errors.New("dial may only be called once")
	}

	handler.server = server.NewServer()
	handler.server.SetFormat(server.RFC5424)
	handler.server.SetHandler(server.NewChannelHandler(handler.channel))
	if err := handler.server.ListenTCP(handler.address); err != nil {
		return err
	}
	if err := handler.server.Boot(); err != nil {
		return err
	}
	go func() {
		for {
			select {
			case message, ok := <-handler.channel:
				if !ok {
					return
				}
				var severity int
				var facility int
				var hostname string
				var tag string
				var timestamp time.Time = time.Now()
				var msg string
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
					Tag:      tag,
					Time:     timestamp,
					Message:  msg,
				}
			case <-handler.stop:
				return
			}
		}
	}()
	return nil
}

// Errors returns a channel that sends errors occuring from input
func (handler *HandlerSyslogTCP) Errors() chan error {
	return handler.errors
}

// Packets channel that sends incoming packets from input
func (handler *HandlerSyslogTCP) Packets() chan syslog.Packet {
	return handler.packets
}

// Pools returns whether this input pools or not.
func (handler *HandlerSyslogTCP) Pools() bool {
	return true
}

// Create a new syslog tcp input
func Create(address string) (*HandlerSyslogTCP, error) {
	return &HandlerSyslogTCP{
		errors:  make(chan error, 1),
		packets: make(chan syslog.Packet, 100),
		stop:    make(chan struct{}, 1),
		channel: make(server.LogPartsChannel),
		server:  nil,
		address: address,
	}, nil
}
