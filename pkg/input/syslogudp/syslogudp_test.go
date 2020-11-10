package syslogudp

import (
	syslog "github.com/trevorlinton/remote_syslog2/syslog"
	. "github.com/smartystreets/goconvey/convey"
	"log"
	"testing"
	"time"
)

func TestSyslogTcpInput(t *testing.T) {
	server, err := Create("0.0.0.0:9004")
	if err != nil {
		log.Fatal(err)
	}

	Convey("Ensure nothing blows up on the stubs", t, func() {
		So(server.Dial(), ShouldBeNil)
		So(server.Dial(), ShouldNotBeNil)
		So(server.Pools(), ShouldEqual, true)
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
		logger, err := syslog.Dial("test", "udp", "0.0.0.0:9004", nil, time.Second, time.Second, 1024)
		if err != nil {
			log.Fatal(err)
		}
		go func() {
			select {
			case err := <-logger.Errors:
				if err != nil {
					log.Fatal(err)
				}
			}
		}()
		logger.Write(p)
		select {
		case message := <-server.Packets():
			So(p.Message, ShouldEqual, message.Message)
			So(p.Tag, ShouldEqual, message.Tag)
			So(p.Hostname, ShouldEqual, message.Hostname)
			So(p.Severity, ShouldEqual, message.Severity)
			So(p.Facility, ShouldEqual, message.Facility)
		case error := <-server.Errors():
			log.Fatal(error.Error())
		}
		logger.Close()
	})
	Convey("Ensure we can close the connection", t, func() {
		So(server.Close(), ShouldBeNil)
	})
}
