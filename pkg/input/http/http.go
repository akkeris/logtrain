package http

import (
	"encoding/json"
	syslog "github.com/trevorlinton/remote_syslog2/syslog"
	"io/ioutil"
	"net/http"
)

// HandlerHTTPJSON is a HTTP/JSON Input handler
type HandlerHTTPJSON struct {
	errors  chan error
	packets chan syslog.Packet
}

func (handler *HandlerHTTPJSON) httpError(response http.ResponseWriter, status int, err error) {
	response.WriteHeader(status)
	response.Write([]byte(http.StatusText(status)))
	select {
	case handler.errors <- err:
	default:
	}
}

// HandlerFunc handles http input
func (handler *HandlerHTTPJSON) HandlerFunc(response http.ResponseWriter, req *http.Request) {
	data, err := ioutil.ReadAll(req.Body)
	if err != nil {
		handler.httpError(response, http.StatusBadRequest, err)
		return
	}
	defer req.Body.Close()
	var p syslog.Packet
	if err := json.Unmarshal(data, &p); err != nil {
		handler.httpError(response, http.StatusBadRequest, err)
		return
	}
	// do not block, if we cannot send to the packets channel
	// assume its closed or full and the message is lost.
	select {
	case handler.packets <- p:
	default:
	}
	response.WriteHeader(http.StatusOK)
	response.Write([]byte("ok"))
}

// Close closes the handler.
func (handler *HandlerHTTPJSON) Close() error {
	close(handler.packets)
	close(handler.errors)
	return nil
}

// Dial opens the handler.
func (handler *HandlerHTTPJSON) Dial() error {
	return nil
}

// Errors returns a channel to receive errors from the handler
func (handler *HandlerHTTPJSON) Errors() chan error {
	return handler.errors
}

// Packets returns a channel to receive packets from the input handler
func (handler *HandlerHTTPJSON) Packets() chan syslog.Packet {
	return handler.packets
}

// Pools returns whether the input pools 
func (handler *HandlerHTTPJSON) Pools() bool {
	return true
}

// Create will create a new HandlerHTTPJSON
func Create() (*HandlerHTTPJSON, error) {
	return &HandlerHTTPJSON{
		errors:  make(chan error, 1),
		packets: make(chan syslog.Packet, 100),
	}, nil
}
