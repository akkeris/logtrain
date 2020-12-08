package elasticsearch

import (
	"crypto/tls"
	"encoding/json"
	"encoding/base64"
	"errors"
	"github.com/trevorlinton/remote_syslog2/syslog"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// For more information on elastic search bulk API, see: 
// https://www.elastic.co/guide/en/elasticsearch/reference/current/docs-bulk.html

// Syslog creates a new syslog output to elasticsearch
type Syslog struct {
	auth     int
	akkeris  bool
	node     string
	index    string
	url      url.URL
	esurl    url.URL
	endpoint string
	client   *http.Client
	packets  chan syslog.Packet
	errors   chan<- error
	stop     chan struct{}
}

const (
	AuthNone int = iota
	AuthApiKey
	AuthBearer
	AuthBasic
)

type elasticSearchHeaderCreate struct {
	Source string `json:"_source"`
	Id string `json:"_id"`
	Index string `json:"_index"`
}
type elasticSearchHeader struct {
	Create elasticSearchHeaderCreate `json:"create"`
}
type elasticSearchBody struct {
	Timestamp string `json:"@timestamp"`
	Hostname string `json:"hostname"`
	Tag string `json:"tag"`
	Message string `json:"message"`
	Severity int `json:"severity"`
	Facility int `json:"facility"`
}

var syslogSchemas = []string{"elasticsearch://", "es://", "elasticsearch+https://", "elasticsearch+http://", "es+https://", "es+http://"}

// Test the schema to see if its an elasticsearch schema
func Test(endpoint string) bool {
	for _, schema := range syslogSchemas {
		if strings.HasPrefix(strings.ToLower(endpoint), schema) == true {
			return true
		}
	}
	return false
}

func toURL(endpoint string) string {
	if strings.Contains(endpoint, "+https://") == true {
		return strings.Replace(strings.Replace(endpoint, "elasticsearch+https://", "https://", 1), "es+https://", "https://", 1)
	}
	if strings.Contains(endpoint, "+http://") == true {
		return strings.Replace(strings.Replace(endpoint, "elasticsearch+http://", "http://", 1), "es+http://", "http://", 1)
	}
	return strings.Replace(strings.Replace(endpoint, "elasticsearch://", "https://", 1), "es://", "https://", 1)
}

// Create a new elasticsearch endpoint
func Create(endpoint string, errorsCh chan<- error) (*Syslog, error) {
	if Test(endpoint) == false {
		return nil, errors.New("Invalid endpoint")
	}
	u, err := url.Parse(toURL(endpoint))
	if err != nil {
		return nil, err
	}
	esurl, err := url.Parse(u.String())
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(esurl.Path, "/_bulk") == false {
		if strings.HasSuffix(esurl.Path, "/") == true {
			esurl.Path = esurl.Path + "_bulk"
		} else {
			esurl.Path = esurl.Path + "/_bulk"
		}
	}
	auth := AuthNone
	if _, ok := esurl.User.Password(); ok {
		if strings.ToLower(u.Query().Get("auth")) == "bearer" {
			auth = AuthBearer
		} else if strings.ToLower(u.Query().Get("auth")) == "apikey" {
			auth = AuthApiKey
		} else {
			auth = AuthBasic
		}
	}
	client := http.Client{}
	if u.Query().Get("insecure") == "true" {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	q := esurl.Query()
	q.Del("auth")
	q.Del("index")
	q.Del("insecure")
	esurl.RawQuery = q.Encode()
	esurl.User = nil
	node := os.Getenv("NODE") // TODO: pass this into create
	if node == "" {
		node = "logtrain"
	}
	return &Syslog{
		auth:     auth,
		node:     node,
		index:    u.Query().Get("index"),
		endpoint: endpoint,
		url:      *u,
		esurl:    *esurl,
		client:   &client,
		packets:  make(chan syslog.Packet, 1024),
		errors:   errorsCh,
		stop:     make(chan struct{}, 1),
		akkeris:  os.Getenv("AKKERIS") == "true", // TODO: pass this in to Create for all outputs.
	}, nil
}

// Dial connects to an elasticsearch
func (log *Syslog) Dial() error {
	go log.loop()
	return nil
}

// Close closes the connection to elasticsearch
func (log *Syslog) Close() error {
	log.stop <- struct{}{}
	close(log.packets)
	return nil
}

// Pools returns whether the elasticsearch end point pools connections
func (log *Syslog) Pools() bool {
	return true
}

// Packets returns a channel to send syslog packets on
func (log *Syslog) Packets() chan syslog.Packet {
	return log.packets
}

func (log *Syslog) loop() {
	timer := time.NewTicker(time.Second)
	var payload string = ""
	for {
		select {
		case p, ok := <-log.packets:
			if !ok {
				return
			}
			var index = log.index
			if index == "" {
				index = p.Hostname
			}
			
			header := elasticSearchHeader{
				Create: elasticSearchHeaderCreate{
					Source: "logtrain",
					Id: strconv.Itoa(int(time.Now().Unix())),
					Index: index,
				},
			}

			body := elasticSearchBody{
				Timestamp: p.Time.Format(syslog.Rfc5424time),
				Hostname: p.Hostname,
				Tag: p.Tag,
				Message: p.Message,
				Severity: int(p.Severity),
				Facility: int(p.Facility),
			}

			if h, err := json.Marshal(header); err == nil {
				if b, err := json.Marshal(body); err == nil {
					payload += string(h) + "\n" + string(b) + "\n"
				}
			}
		case <-timer.C:
			if payload != "" {
				req, err := http.NewRequest(http.MethodPost, log.esurl.String(), strings.NewReader(string(payload)))
				if err != nil {
					log.errors <- err
				} else {
					req.Header.Set("content-type", "application/json")
					if pwd, ok := log.url.User.Password(); ok {
						if log.auth == AuthBearer {
							req.Header.Set("Authorization", "Bearer "+pwd)
						} else if log.auth == AuthApiKey {
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
			return
		}
	}
}
