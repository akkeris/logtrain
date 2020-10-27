package router

import (
	"log"
	"testing"
	"time"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/papertrail/remote_syslog2/syslog"
)

type FakeDataSource struct {
	add chan LogRoute
	remove chan LogRoute
	routes []LogRoute
}

func (dataSource FakeDataSource) AddRoute() chan LogRoute {
	return dataSource.add
}

func (dataSource FakeDataSource) RemoveRoute() chan LogRoute {
	return dataSource.remove
}

func (dataSource FakeDataSource) GetAllRoutes() ([]LogRoute, error) {
	return dataSource.routes, nil
}


type FakeInput struct {
	errors chan error
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

func (fi *FakeInput) EmitError(err error) {
	fi.errors <- err
}

func (fi *FakeInput) EmitPacket(p syslog.Packet) {
	fi.packets <- p
}

func TestRouter(t *testing.T) {
	server, err := CreateSudoSyslogServer("10513")
	if err != nil {
		log.Fatal(err)
	}
	go server.Listen()
	route := LogRoute{
		Endpoint: "syslog+tcp://localhost:10513/",
		Hostname: "test-host",
		Tag: "test-tag",
	}
	ds := FakeDataSource{
		routes:[]LogRoute{route},
	}
	input := FakeInput{
		errors: make(chan error, 1),
		packets: make(chan syslog.Packet, 1),
	}

	router, err := NewRouter([]DataSource{ds}, true, 40)
	if err != nil {
		log.Fatal(err)
	}
	Convey("Ensure router routes to syslog system", t, func() {
		So(router.AddInput(&input, "someid"), ShouldBeNil)
		So(router.Dial(), ShouldBeNil)
		input.Packets() <- syslog.Packet{
			Severity:0,
			Facility:0,
			Message:"Oh hello",
			Tag:"test-tag",
			Hostname:"test-host", 
			Time:time.Now(),
		}
		select {
		case message := <-server.Received:
			So(message.Message, ShouldContainSubstring, "Oh hello")
		}
	})
	Convey("Ensure we clean up.", t, func() {
		server.Close()
	})
}