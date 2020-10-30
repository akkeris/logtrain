package http

import (
	"encoding/json"
	packet "github.com/akkeris/logtrain/pkg/output/packet"
	syslog "github.com/papertrail/remote_syslog2/syslog"
	"io/ioutil"
	"net/http"
)

type HandlerHttpJson struct {
	errors  chan error
	packets chan syslog.Packet
}

func (handler *HandlerHttpJson) httpError(response http.ResponseWriter, status int, err error) {
	response.WriteHeader(status)
	response.Write([]byte(http.StatusText(status)))
	select {
	case handler.errors <- err:
	default:
	}
}

func (handler *HandlerHttpJson) HandlerFunc(response http.ResponseWriter, req *http.Request) {
	data, err := ioutil.ReadAll(req.Body)
	if err != nil {
		handler.httpError(response, http.StatusBadRequest, err)
		return
	}
	defer req.Body.Close()
	var p packet.Packet
	if err := json.Unmarshal(data, &p); err != nil {
		handler.httpError(response, http.StatusBadRequest, err)
		return
	}
	// if only golang allowed down casting, alas.
	sp := syslog.Packet{
		Severity: p.Severity,
		Facility: p.Facility,
		Message:  p.Message,
		Tag:      p.Tag,
		Hostname: p.Hostname,
		Time:     p.Time,
	}
	// do not block, if we cannot send to the packets channel
	// assume its closed or full and the message is lost.
	select {
	case handler.packets <- sp:
	default:
	}
	response.WriteHeader(http.StatusOK)
	response.Write([]byte("ok"))
}

func (handler *HandlerHttpJson) Close() error {
	close(handler.packets)
	close(handler.errors)
	return nil
}

func (handler *HandlerHttpJson) Dial() error {
	return nil
}

func (handler *HandlerHttpJson) Errors() chan error {
	return handler.errors
}

func (handler *HandlerHttpJson) Packets() chan syslog.Packet {
	return handler.packets
}

func (handler *HandlerHttpJson) Pools() bool {
	return true
}

func Create() (*HandlerHttpJson, error) {
	return &HandlerHttpJson{
		errors:  make(chan error, 1),
		packets: make(chan syslog.Packet, 100),
	}, nil
}
