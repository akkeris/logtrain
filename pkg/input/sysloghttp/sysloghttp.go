package sysloghttp

import (
	newline "github.com/mitchellh/go-linereader"
	syslog "github.com/papertrail/remote_syslog2/syslog"
	"net/http"
)

type HandlerHttpSyslog struct {
	errors  chan error
	packets chan syslog.Packet
}

func (handler *HandlerHttpSyslog) httpError(response http.ResponseWriter, status int, err error) {
	response.WriteHeader(status)
	response.Write([]byte(http.StatusText(status)))
	select {
	case handler.errors <- err:
	default:
	}
}

func (handler *HandlerHttpSyslog) HandlerFunc(response http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	lr := newline.New(req.Body)
	for line := range lr.Ch {
		p, err := syslog.Parse(line)
		if err != nil {
			handler.httpError(response, http.StatusBadRequest, err)
			return
		}
		select {
		case handler.packets <- p:
		default:
		}
	}
	response.WriteHeader(http.StatusOK)
	response.Write([]byte("ok"))
}

func (handler *HandlerHttpSyslog) Close() error {
	close(handler.packets)
	close(handler.errors)
	return nil
}

func (handler *HandlerHttpSyslog) Dial() error {
	return nil
}

func (handler *HandlerHttpSyslog) Errors() chan error {
	return handler.errors
}

func (handler *HandlerHttpSyslog) Packets() chan syslog.Packet {
	return handler.packets
}

func (handler *HandlerHttpSyslog) Pools() bool {
	return true
}

func Create() (*HandlerHttpSyslog, error) {
	return &HandlerHttpSyslog{
		errors:  make(chan error, 1),
		packets: make(chan syslog.Packet, 100),
	}, nil
}
