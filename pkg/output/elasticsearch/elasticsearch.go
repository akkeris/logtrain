package elasticsearch

import (
	"encoding/base64"
	"errors"
	"github.com/trevorlinton/remote_syslog2/syslog"
	"net/http"
	"net/url"
	"os"
	"io/ioutil"
	"strconv"
	"strings"
	"time"
)

type Syslog struct {
	node     string
	index    string
	url      url.URL
	endpoint string
	client   *http.Client
	packets  chan syslog.Packet
	errors   chan<- error
	stop     chan struct{}
}

const rfc5424time = "2006-01-02T15:04:05.999999Z07:00"

var syslogSchemas = []string{"elasticsearch://", "es://", "elasticsearch+https://", "elasticsearch+http://", "es+https://", "es+http://"}

const MaxLogSize int = 99990

// https://www.elastic.co/guide/en/elasticsearch/reference/current/docs-bulk.html

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

func Create(endpoint string, errorsCh chan<- error) (*Syslog, error) {
	if Test(endpoint) == false {
		return nil, errors.New("Invalid endpoint")
	}
	u, err := url.Parse(toUrl(endpoint))
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(u.Path, "/_bulk") == false {
		u.Path = u.Path + "/_bulk"
	}
	node := os.Getenv("NODE")
	if node == "" {
		node = "logtrain"
	}
	return &Syslog{
		node:     node,
		index:    u.Query().Get("index"),
		endpoint: endpoint,
		url:      *u,
		client:   &http.Client{},
		packets:  make(chan syslog.Packet, 10),
		errors:   errorsCh,
		stop:     make(chan struct{}, 1),
	}, nil
}

func (log *Syslog) Dial() error {
	go log.loop()
	return nil
}
func (log *Syslog) Close() error {
	log.stop <- struct{}{}
	close(log.packets)
	return nil
}
func (log *Syslog) Pools() bool {
	return true
}
func (log *Syslog) Packets() chan syslog.Packet {
	return log.packets
}

func (log *Syslog) loop() {
	timer := time.NewTicker(time.Second)
	var payload string = ""
	for {
		select {
		case p := <-log.packets:
			var index = log.index
			if index == "" {
				index = p.Hostname
			}
			payload = payload + "{\"create\":{ \"_id\": \"" + strconv.Itoa(int(time.Now().Unix())) + "\", \"_index\": \"" + strings.ReplaceAll(index, "\"", "\\\"") + "\" }}\n{ \"@timestamp\":\"" + p.Time.Format(rfc5424time) + "\", \"hostname\":\"" + strings.ReplaceAll(p.Hostname, "\"", "\\\"") + "\", \"tag\":\"" + strings.ReplaceAll(p.Tag, "\"", "\\\"") + "\", \"message\":\"" + strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(p.Message, "\"", "\\\""), "\n", "\\n"), "\r", "\\r") + "\", \"severity\":" + strconv.Itoa(int(p.Severity)) + ", \"facility\":" + strconv.Itoa(int(p.Facility)) + " }\n"
		case <-timer.C:
			if payload != "" {
				req, err := http.NewRequest(http.MethodPost, log.url.String(), strings.NewReader(string(payload)))
				if err != nil {
					log.errors <- err
				} else {
					req.Header.Set("content-type", "application/json")
					if pwd, ok := log.url.User.Password(); ok {
						if strings.ToLower(log.url.Query().Get("auth")) == "bearer" {
							req.Header.Set("Authorization", "Bearer "+pwd)
						} else if strings.ToLower(log.url.Query().Get("auth")) == "apikey" {
							req.Header.Set("Authorization", "ApiKey "+base64.StdEncoding.EncodeToString([]byte(log.url.User.Username()+":"+string(pwd))))
						} else {
							req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(log.url.User.Username()+":"+string(pwd))))
						}
					}
					resp, err := log.client.Do(req)
					if err != nil {
						log.errors <- err
					} else {
						body, err := ioutil.ReadAll(resp.Body)
						if err != nil {
							body = []byte{}
						}
						resp.Body.Close()
						if resp.StatusCode >= http.StatusMultipleChoices || resp.StatusCode < http.StatusOK {
							log.errors <- errors.New("invalid response from endpoint: " + resp.Status + " " + string(body) + "sent: [[ " + payload + " ]]")
						}
					}
				}
				payload = ""
			}
		case <-log.stop:
			close(log.stop)
			return
		}
	}
}
