package http

import (
	. "github.com/smartystreets/goconvey/convey"
	syslog2 "github.com/trevorlinton/remote_syslog2/syslog"
	"io/ioutil"
	"log"
	"net/http"
	"testing"
	"time"
)

type TestHttpServer struct {
	Incoming    chan string
	ReturnError bool
}

func (hts *TestHttpServer) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	bytes, err := ioutil.ReadAll(req.Body)
	if err != nil {
		log.Fatalln(err)
	}
	req.Body.Close()
	if hts.ReturnError == true {
		res.WriteHeader(http.StatusInternalServerError)
		res.Write(([]byte)("ERROR"))
	} else {
		hts.Incoming <- string(bytes)
		res.Write(([]byte)("OK"))
	}
}

func TestJSONHttpOutput(t *testing.T) {
	testHttpServer := TestHttpServer{
		Incoming:    make(chan string, 1),
		ReturnError: false,
	}
	s := &http.Server{
		Addr:           ":8084",
		Handler:        &testHttpServer,
		ReadTimeout:    1 * time.Second,
		WriteTimeout:   1 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	syslog, err := Create("http://localhost:8084/tests")
	go s.ListenAndServe()
	Convey("Ensure syslog is created", t, func() {
		So(err, ShouldBeNil)
	})

	Convey("Ensure we can start the http (application/json) syslog end point.", t, func() {
		So(syslog.Dial(), ShouldBeNil)
	})
	Convey("Ensure that an http transport explicitly pools connections.", t, func() {
		So(syslog.Pools(), ShouldEqual, true)
	})
	Convey("Ensure we can send syslog packets", t, func() {
		syslog.Packets() <- syslog2.Packet{
			Severity: 0,
			Facility: 0,
			Hostname: "localhost",
			Tag:      "HttpChannelTest",
			Message:  "Test Message",
		}
		select {
		case message := <-testHttpServer.Incoming:
			So(message, ShouldContainSubstring, "\"severity\":0")
			So(message, ShouldContainSubstring, "\"facility\":0")
			So(message, ShouldContainSubstring, "\"hostname\":\"localhost\"")
			So(message, ShouldContainSubstring, "\"tag\":\"HttpChannelTest\"")
			So(message, ShouldContainSubstring, "\"message\":\"Test Message\"")
		case error := <-syslog.Errors():
			log.Fatal(error.Error())
		}

	})
	Convey("Ensure we receive an error sending to a erroring endpoint", t, func() {
		testHttpServer.ReturnError = true
		syslog.Packets() <- syslog2.Packet{
			Severity: 0,
			Facility: 0,
			Hostname: "localhost",
			Tag:      "HttpChannelTest",
			Message:  "Failed Message That Shouldn't Happen",
		}
		select {
		case <-testHttpServer.Incoming:
			log.Fatal("No message should have been received from incoming...")
		case error := <-syslog.Errors():
			So(error, ShouldNotBeNil)
		}
	})
	Convey("Ensure we can close a syslog end point...", t, func() {
		s.Close()
		So(syslog.Close(), ShouldBeNil)
	})
}
