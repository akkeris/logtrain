package http

import (
	"encoding/json"
	"github.com/akkeris/logtrain/pkg/output/packet"
	. "github.com/smartystreets/goconvey/convey"
	syslog "github.com/trevorlinton/remote_syslog2/syslog"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestJsonHttpOutput(t *testing.T) {
	handler, err := Create()
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/stream-test", handler.HandlerFunc)
	s := &http.Server{
		Addr:           ":8089",
		Handler:        mux,
		ReadTimeout:    1 * time.Second,
		WriteTimeout:   1 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	go s.ListenAndServe()
	time.Sleep(time.Second)

	Convey("Ensure nothing blows up on the http handler stubs", t, func() {
		So(handler.Dial(), ShouldBeNil)
		So(handler.Pools(), ShouldEqual, true)
	})

	Convey("Ensure we can receive messages via the http JSON payload", t, func() {
		p := syslog.Packet{
			Severity: 0,
			Facility: 0,
			Message:  "Oh hello",
			Tag:      "web",
			Hostname: "name-namespace",
			Time:     time.Now(),
		}
		data, err := json.Marshal(packet.Packet{p})
		So(err, ShouldBeNil)
		resp, err := http.Post("http://localhost:8089/stream-test", "application/json", strings.NewReader(string(data)))
		So(err, ShouldBeNil)
		resp.Body.Close()
		select {
		case message := <-handler.Packets():
			So(p.Message, ShouldEqual, message.Message)
			So(p.Tag, ShouldEqual, message.Tag)
			//So(p.Time, ShouldEqual, message.Time) TODO, this is oddly different depending on platforms.
			So(p.Hostname, ShouldEqual, message.Hostname)
			So(p.Severity, ShouldEqual, message.Severity)
			So(p.Facility, ShouldEqual, message.Facility)
		case err := <-handler.Errors():
			log.Fatal(err)
		}
	})

	Convey("Test sending a malformed request", t, func() {
		p := syslog.Packet{
			Severity: 0,
			Facility: 0,
			Message:  "Oh hello",
			Tag:      "web",
			Hostname: "name-namespace",
			Time:     time.Now(),
		}
		data, err := json.Marshal(p) // syslog.Packet (p) is not serializable, therefore it'll generate a bad payload below.
		So(err, ShouldBeNil)
		resp, err := http.Post("http://localhost:8089/stream-test", "application/json", strings.NewReader(string(data)))
		So(err, ShouldBeNil)
		So(resp.StatusCode, ShouldEqual, http.StatusBadRequest)
		resp.Body.Close()
		select {
		case <-handler.Packets():
			log.Fatal("Received a message when it should have errored out.")
		case err := <-handler.Errors():
			So(err, ShouldNotBeNil)
		}
	})

	Convey("Ensure we can close the connection", t, func() {
		So(handler.Close(), ShouldBeNil)
		s.Close()
	})
}
