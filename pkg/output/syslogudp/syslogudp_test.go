package syslogudp

import (
	. "github.com/smartystreets/goconvey/convey"
	syslog2 "github.com/trevorlinton/remote_syslog2/syslog"
	syslogserver "gopkg.in/mcuadros/go-syslog.v2"
	"log"
	"testing"
	"time"
)

func CreateUDPSyslogServer(port string) (syslogserver.LogPartsChannel, *syslogserver.Server) {
	channel := make(syslogserver.LogPartsChannel)
	server := syslogserver.NewServer()
	server.SetFormat(syslogserver.RFC5424)
	server.SetHandler(syslogserver.NewChannelHandler(channel))
	if err := server.ListenUDP("0.0.0.0:" + port); err != nil {
		log.Fatal(err.Error())
	}

	if err := server.Boot(); err != nil {
		log.Fatal(err.Error())
	}
	return channel, server
}

func TestSyslogUdpOutput(t *testing.T) {
	syslog, err := Create("syslog+udp://localhost:8513/")
	Convey("Ensure syslog is created", t, func() {
		So(err, ShouldBeNil)
	})
	Convey("Ensure that an syslog+tcp explicitly does not pool connections.", t, func() {
		So(syslog.Pools(), ShouldEqual, false)
	})
	Convey("Ensure we can send syslog packets", t, func() {
		channel, server := CreateUDPSyslogServer("8513")
		go server.Wait()

		Convey("Ensure we can start the tcp syslog end point on port 8513.", func() {
			So(syslog.Dial(), ShouldBeNil)
		})
		now := time.Now()
		p := syslog2.Packet{
			Severity: 0,
			Facility: 0,
			Time:     now,
			Hostname: "localhost",
			Tag:      "HttpSyslogChannelTest",
			Message:  "Test Message",
		}
		syslog.Packets() <- p
		select {
		case message := <-channel:
			So(message["app_name"], ShouldEqual, p.Tag)
			So(message["severity"], ShouldEqual, p.Severity)
			So(message["facility"], ShouldEqual, p.Facility)
			So(message["hostname"], ShouldEqual, p.Hostname)
			So(message["message"], ShouldEqual, p.Message)
		case error := <-syslog.Errors():
			log.Fatal(error.Error())
		}

		So(server.Kill(), ShouldBeNil)
	})
	/*
		 * This does not yet work, the test implementation (gopkg.in/mcuadros/go-syslog.v2)
		 * does not shutdown when we issue a server.Kill(), the logging drain can still
		 * forward to it, until we get a different reference impl this will be broken.
		Convey("Ensure we receive an error sending to a erroring endpoint", t, func() {
			var testError error = nil
			go func() {
				select {
				case error := <-syslog.Errors():
					testError = error
				}
			}()

			<-time.NewTimer(time.Second).C
			syslog.Packets() <- syslog2.Packet{
				Severity: 0,
				Facility: 0,
				Time: time.Now(),
				Hostname: "localhost",
				Tag: "HttpSyslogChannelTest",
				Message: "Failed Message That Shouldn't Happen",
			}

			<-time.NewTimer(time.Second).C
			So(testError, ShouldNotBeNil)
		})
	*/
	Convey("Ensure we can close a syslog end point...", t, func() {
		So(syslog.Close(), ShouldBeNil)
	})
}
