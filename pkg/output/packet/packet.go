package packet

import (
	"encoding/json"
	"errors"
	"github.com/papertrail/remote_syslog2/syslog"
	"strconv"
	"time"
)

type Packet struct {
	syslog.Packet
}

const Rfc5424time = "2006-01-02T15:04:05.999999Z07:00"

func (p Packet) MarshalJSON() ([]byte, error) {
	return ([]byte)("{" +
		"\"severity\":" + strconv.Itoa(int(p.Severity)) + "," +
		"\"facility\":" + strconv.Itoa(int(p.Facility)) + "," +
		"\"hostname\":\"" + p.Hostname + "\"," +
		"\"tag\":\"" + p.Tag + "\"," +
		"\"time\":\"" + p.Time.Format(Rfc5424time) + "\"," +
		"\"message\":\"" + p.Message + "\"" +
		"}"), nil
}

func (p *Packet) UnmarshalJSON(b []byte) error {
	var payload map[string]interface{}
	if err := json.Unmarshal(b, &payload); err != nil {
		return err
	}

	sev, ok := payload["severity"].(float64)
	if ok == false {
		sev = 0
	}
	fac, ok := payload["facility"].(float64)
	if ok == false {
		fac = 0
	}
	host, ok := payload["hostname"].(string)
	if ok == false {
		return errors.New("Hostname could not be cast to a string.")
	}
	tag, ok := payload["tag"].(string)
	if ok == false {
		return errors.New("Tag could not be cast to a string.")
	}

	ti, ok := payload["time"].(string)
	if ok == false {
		ti = time.Now().Format(Rfc5424time)
	}
	msg, ok := payload["message"].(string)
	if ok == false {
		return errors.New("Message could not be cast to a string.")
	}
	p.Severity = syslog.Priority(sev)
	p.Facility = syslog.Priority(fac)
	p.Hostname = host
	p.Tag = tag
	tim, err := time.Parse(Rfc5424time, ti)
	if err != nil {
		return err
	}
	p.Time = tim
	p.Message = msg
	return nil
}
