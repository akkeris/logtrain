package elasticsearch

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	syslog "github.com/papertrail/remote_syslog2/syslog"
)

type Syslog struct {
	url url.URL
	endpoint string
	client *http.Client
	packets chan syslog.Packet
	errors chan error
	stop chan struct{}
}

const rfc5424time = "2006-01-02T15:04:05.999999Z07:00"
var syslogSchemas = []string{"elasticsearch://", "es://", "elasticsearch+https://", "elasticsearch+http://", "es+https://", "es+http://"}
const MaxLogSize int = 99990

func Test(endpoint string) bool {
	for _, schema := range syslogSchemas {
		if strings.HasPrefix(strings.ToLower(endpoint), schema) == true {
			return true
		}
	}
	return false
}

func toUrl(endpoint string) string {
	if strings.Contains(endpoint, "+https://") == true {
		return strings.Replace(strings.Replace(endpoint, "elasticsearch+https://", "https://", 1), "es+https://", "https://", 1)
	}
	if strings.Contains(endpoint, "+http://") == true {
		return strings.Replace(strings.Replace(endpoint, "elasticsearch+http://", "http://", 1), "es+http://", "http://", 1)
	}
	return strings.Replace(strings.Replace(endpoint, "elasticsearch://", "https://", 1), "es://", "https://", 1)
}

func Create(endpoint string) (*Syslog, error) {
	if Test(endpoint) == false {
		return nil, errors.New("Invalid endpoint")
	}
	u, err := url.Parse(toUrl(endpoint))
	if err != nil {
		return nil, err;
	}
	return &Syslog {
		endpoint: endpoint,
		url: *u,
		client: &http.Client{},
		packets: make(chan syslog.Packet, 10),
		errors: make(chan error, 1),
		stop: make(chan struct{}, 1),
	}, nil
}

func (log *Syslog) Dial() error {
	go log.loop()
	return nil
}
func (log *Syslog) Close() error {
	log.stop <- struct{}{}
	close(log.packets)
	close(log.errors)
	return nil
}
func (log *Syslog) Pools() bool {
	return true
}
func (log *Syslog) Packets() (chan syslog.Packet) {
	return log.packets
}
func (log *Syslog) Errors() (chan error) {
	return log.errors
}

func (log *Syslog) loop() {
	timer := time.NewTicker(time.Second)
	var payload string = ""
	for {
		select {
		case p := <-log.packets:
			payload = payload + "{\"create\":{ }}\n{ \"@timestamp\":" + p.Time.Format(rfc5424time) + ", \"message\":\"" + p.Generate(MaxLogSize) + "\" }\n" 
		case <-timer.C:
			if payload != "" {
				resp, err := log.client.Post(log.url.String(), "application/json", strings.NewReader(string(payload)))
				payload = ""
				if err != nil {
					log.errors <- err
				} else {
					resp.Body.Close()
					if (resp.StatusCode >= http.StatusMultipleChoices || resp.StatusCode < http.StatusOK) {
						log.errors <- errors.New("invalid response from endpoint: " + resp.Status)
					}
				}
			}
		case <-log.stop:
			close(log.stop)
			return
		}
	}
}