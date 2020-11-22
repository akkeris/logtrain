package router

import (
	"github.com/akkeris/logtrain/internal/storage"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/trevorlinton/remote_syslog2/syslog"
	"log"
	"testing"
	"time"
)

type FakeInput struct {
	errors  chan error
	packets chan syslog.Packet
}

func (fi *FakeInput) Close() error {
	return nil
}

func (fi *FakeInput) Dial() error {
	fi.errors = make(chan error, 1)
	fi.packets = make(chan syslog.Packet, 1)
	return nil
}

func (fi *FakeInput) Errors() chan error {
	return fi.errors
}

func (fi *FakeInput) Packets() chan syslog.Packet {
	return fi.packets
}

func (fi *FakeInput) Pools() bool {
	return true
}

func TestRouter(t *testing.T) {
	server, err := CreateSudoSyslogServer("10513")
	if err != nil {
		log.Fatal(err)
	}
	go server.Listen()
	route := storage.LogRoute{
		Endpoint: "syslog+tcp://localhost:10513/",
		Hostname: "test-host",
		Tag:      "test-tag",
	}
	ds := storage.CreateMemoryDataSource()
	ds.EmitNewRoute(route)
	input := FakeInput{
		errors:  make(chan error, 1),
		packets: make(chan syslog.Packet, 1),
	}

	router, err := NewRouter([]storage.DataSource{ds}, true, 40)
	if err != nil {
		log.Fatal(err)
	}
	Convey("Ensure router routes to syslog system", t, func() {
		So(router.AddInput(&input, "someid"), ShouldBeNil)
		So(router.AddInput(&input, "someid"), ShouldNotBeNil)
		So(router.Dial(), ShouldBeNil)
		So(router.Dial(), ShouldNotBeNil)

		// send ignored packet
		input.Packets() <- syslog.Packet{
			Severity: 0,
			Facility: 0,
			Message:  "Oh hello",
			Tag:      "test-tag",
			Hostname: "non-existant",
			Time:     time.Now(),
		}

		// valid packets
		input.Packets() <- syslog.Packet{
			Severity: 0,
			Facility: 0,
			Message:  "Oh hello",
			Tag:      "test-tag",
			Hostname: "test-host",
			Time:     time.Now(),
		}
		select {
		case message := <-server.Received:
			So(message.Message, ShouldContainSubstring, "Oh hello")
		case <-time.NewTimer(time.Second * 2).C:
			So(false, ShouldEqual, true)
		}
		input.Packets() <- syslog.Packet{
			Severity: 0,
			Facility: 0,
			Message:  "Pow Pow Meow",
			Tag:      "test-tag",
			Hostname: "test-host",
			Time:     time.Now(),
		}
		select {
		case message := <-server.Received:
			So(message.Message, ShouldContainSubstring, "Pow Pow Meow")
		case <-time.NewTimer(time.Second * 2).C:
			So(false, ShouldEqual, true)
		}
		ds.EmitRemoveRoute(route)
		input.Packets() <- syslog.Packet{
			Severity: 0,
			Facility: 0,
			Message:  "Pow Pow Meow",
			Tag:      "test-tag",
			Hostname: "test-host",
			Time:     time.Now(),
		}
		select {
		case message := <-server.Received:
			log.Fatal(message)
		default:
		}

		ds.EmitNewRoute(route)

		input.Packets() <- syslog.Packet{
			Severity: 0,
			Facility: 0,
			Message:  "Pow Pow Wow",
			Tag:      "test-tag",
			Hostname: "test-host",
			Time:     time.Now(),
		}
		select {
		case message := <-server.Received:
			So(message.Message, ShouldContainSubstring, "Pow Pow")
		case <-time.NewTimer(time.Second * 2).C:
			So(false, ShouldEqual, true)
		}
		select {
		case message := <-server.Received:
			So(message.Message, ShouldContainSubstring, "Pow Pow")
		case <-time.NewTimer(time.Second * 2).C:
			So(false, ShouldEqual, true)
		}

		So(router.RemoveInput("someid"), ShouldBeNil)
	})
	Convey("Ensure we can get metrics", t, func() {
		metrics := router.Metrics()
		So(metrics, ShouldNotBeNil)
		So(router.DeadPackets(), ShouldEqual, 1)
		router.ResetMetrics()
	})
	Convey("Ensure we clean up.", t, func() {
		So(router.Close(), ShouldBeNil)
		server.Close()
	})
}
