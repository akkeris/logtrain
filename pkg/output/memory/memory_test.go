package memory

import (
	. "github.com/smartystreets/goconvey/convey"
	syslog2 "github.com/trevorlinton/remote_syslog2/syslog"
	"log"
	"testing"
	"time"
)

func TestMemoryOutput(t *testing.T) {
	commCh := make(chan syslog2.Packet, 10)
	errorCh := make(chan error, 1)
	var syslog *Syslog
	var err error
	Convey("Ensure in memory output cannot be created for a bad id", t, func() {
		So(NewMemoryChannel("foo"), ShouldNotBeNil)
		syslog, err = Create("memory://localhost:8085/myid", errorCh)
		So(err, ShouldNotBeNil)
	})

	Convey("Ensure we can create a memory output", t, func() {
		GlobalInternalOutputs["myid"] = commCh
		syslog, err = Create("memory://localhost:8085/myid", errorCh)
		So(err, ShouldBeNil)
	})
	Convey("Ensure we can start the memory output.", t, func() {
		So(syslog.Dial(), ShouldBeNil)
	})
	Convey("Ensure that an http transport explicitly pools connections.", t, func() {
		So(syslog.Pools(), ShouldEqual, true)
	})
	Convey("Ensure we can send syslog packets", t, func() {
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
		case message := <-commCh:
			So(message.Generate(9990)+"\n", ShouldEqual, p.Generate(9990)+"\n")
		case error := <-errorCh:
			log.Fatal(error.Error())
		}

	})
	Convey("Ensure we can close a syslog end point...", t, func() {
		So(syslog.Close(), ShouldBeNil)
	})
}
