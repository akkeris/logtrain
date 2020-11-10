package syslogudp

import (
	"errors"
	server "github.com/mcuadros/go-syslog"
	syslog "github.com/trevorlinton/remote_syslog2/syslog"
	"time"
)

type HandlerSyslogUdp struct {
	errors  chan error
	packets chan syslog.Packet
	stop    chan struct{}
	channel server.LogPartsChannel
	server  *server.Server
	address string
}

func (handler *HandlerSyslogUdp) Close() error {
	handler.stop <- struct{}{}
	handler.server.Kill()
	close(handler.packets)
	close(handler.errors)
	close(handler.channel)
	close(handler.stop)
	return nil
}

func (handler *HandlerSyslogUdp) Dial() error {
	if handler.server != nil {
		return errors.New("Dial may only be called once.")
	}

	handler.server = server.NewServer()
	handler.server.SetFormat(server.RFC6587)
	handler.server.SetHandler(server.NewChannelHandler(handler.channel))
	if err := handler.server.ListenUDP(handler.address); err != nil {
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

func (handler *HandlerSyslogUdp) Errors() chan error {
	return handler.errors
}

func (handler *HandlerSyslogUdp) Packets() chan syslog.Packet {
	return handler.packets
}

func (handler *HandlerSyslogUdp) Pools() bool {
	return true
}

func Create(address string) (*HandlerSyslogUdp, error) {
	return &HandlerSyslogUdp{
		errors:  make(chan error, 1),
		packets: make(chan syslog.Packet, 100),
		stop:    make(chan struct{}, 1),
		channel: make(server.LogPartsChannel),
		server:  nil,
		address: address,
	}, nil
}
