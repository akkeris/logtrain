package sysloghttp

import (
	syslog "github.com/trevorlinton/remote_syslog2/syslog"
	. "github.com/smartystreets/goconvey/convey"
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
		Addr:           ":8090",
		Handler:        mux,
		ReadTimeout:    1 * time.Second,
		WriteTimeout:   1 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	go s.ListenAndServe()

	Convey("Ensure nothing blows up on the http handler stubs", t, func() {
		So(handler.Dial(), ShouldBeNil)
		So(handler.Pools(), ShouldEqual, true)
	})

	Convey("Ensure we can receive messages via the http syslog stream payload", t, func() {
		p := syslog.Packet{
			Severity: 0,
			Facility: 0,
			Message:  "Oh hello",
			Tag:      "web",
			Hostname: "name-namespace",
			Time:     time.Now(),
		}
		p2 := syslog.Packet{
			Severity: 0,
			Facility: 0,
			Message:  "Oh hello2",
			Tag:      "web2",
			Hostname: "name-namespace2",
			Time:     time.Now(),
		}
		data := p.Generate(1024) + "\n" + p2.Generate(1024)
		resp, err := http.Post("http://localhost:8090/stream-test", "application/syslog", strings.NewReader(string(data)))
		So(err, ShouldBeNil)
		resp.Body.Close()
		select {
		case message := <-handler.Packets():
			So(p.Message, ShouldEqual, message.Message)
			So(p.Tag, ShouldEqual, message.Tag)
			// So(p.Time, ShouldEqual, message.Time) TODO, this is oddly different depending on platforms.
			So(p.Hostname, ShouldEqual, message.Hostname)
			So(p.Severity, ShouldEqual, message.Severity)
			So(p.Facility, ShouldEqual, message.Facility)
		case err := <-handler.Errors():
			log.Fatal(err)
		}
		select {
		case message := <-handler.Packets():
			So(p2.Message, ShouldEqual, message.Message)
			So(p2.Tag, ShouldEqual, message.Tag)
			// So(p2.Time, ShouldEqual, message.Time)  TODO, this is oddly different depending on platforms.
			So(p2.Hostname, ShouldEqual, message.Hostname)
			So(p2.Severity, ShouldEqual, message.Severity)
			So(p2.Facility, ShouldEqual, message.Facility)
		case err := <-handler.Errors():
			log.Fatal(err)
		}
	})
	Convey("Ensure we handle failures", t, func() {
		resp, err := http.Post("http://localhost:8090/stream-test", "application/syslog", strings.NewReader(string("foobar!\n")))
		So(err, ShouldBeNil)
		resp.Body.Close()
		select {
		case <-handler.Packets():
			log.Fatal("This should never be reached")
		case err := <-handler.Errors():
			So(err, ShouldNotBeNil)
		}
	})
	Convey("Ensure we can close the connection", t, func() {
		So(handler.Close(), ShouldBeNil)
		s.Close()
	})
}
