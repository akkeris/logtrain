package http

import (
	"encoding/json"
	syslog "github.com/trevorlinton/remote_syslog2/syslog"
	"io/ioutil"
	"net/http"
)

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

func (handler *HandlerHTTPJSON) Close() error {
	close(handler.packets)
	close(handler.errors)
	return nil
}

func (handler *HandlerHTTPJSON) Dial() error {
	return nil
}

func (handler *HandlerHTTPJSON) Errors() chan error {
	return handler.errors
}

func (handler *HandlerHTTPJSON) Packets() chan syslog.Packet {
	return handler.packets
}

func (handler *HandlerHTTPJSON) Pools() bool {
	return true
}

func Create() (*HandlerHTTPJSON, error) {
	return &HandlerHTTPJSON{
		errors:  make(chan error, 1),
		packets: make(chan syslog.Packet, 100),
	}, nil
}
