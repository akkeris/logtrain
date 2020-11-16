package sysloghttp

import (
	newline "github.com/mitchellh/go-linereader"
	syslog "github.com/trevorlinton/remote_syslog2/syslog"
	"net/http"
)

/* HandlerHTTPSyslog handles Syslog HTTP inputs */
type HandlerHTTPSyslog struct {
	errors  chan error
	packets chan syslog.Packet
}

func (handler *HandlerHTTPSyslog) httpError(response http.ResponseWriter, status int, err error) {
	response.WriteHeader(status)
	response.Write([]byte(http.StatusText(status)))
	select {
	case handler.errors <- err:
	default:
	}
}

// HandlerFunc is a HTTP Handler Function for input
func (handler *HandlerHTTPSyslog) HandlerFunc(response http.ResponseWriter, req *http.Request) {
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

// Close input handler.
func (handler *HandlerHTTPSyslog) Close() error {
	close(handler.packets)
	close(handler.errors)
	return nil
}

// Dial input handler.
func (handler *HandlerHTTPSyslog) Dial() error {
	return nil
}

// Errors returns a channel that sends errors occuring from input
func (handler *HandlerHTTPSyslog) Errors() chan error {
	return handler.errors
}

// Packets returns a channel that sends incoming packets from input
func (handler *HandlerHTTPSyslog) Packets() chan syslog.Packet {
	return handler.packets
}

// Pools returns whether this input pools connections or not.
func (handler *HandlerHTTPSyslog) Pools() bool {
	return true
}

// Create a new syslog http input
func Create() (*HandlerHTTPSyslog, error) {
	return &HandlerHTTPSyslog{
		errors:  make(chan error, 1),
		packets: make(chan syslog.Packet, 100),
	}, nil
}
