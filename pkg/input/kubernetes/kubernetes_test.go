package kubernetes

import (
	"log"
	"os"
	"testing"
	"time"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/papertrail/remote_syslog2/syslog"
)

func write(f *os.File, content string) error {
	if _, err := f.Write([]byte(content)); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return nil
}

func TestKubernetesInput(t *testing.T) {
	if err := os.RemoveAll("/tmp/kubernetes_test"); err != nil {
		log.Fatal(err)
	}
	if err := os.Mkdir("/tmp/kubernetes_test", 0755); err != nil {
		log.Fatal(err)
	}
	handler, err := Create("/tmp/kubernetes_test")
	if err != nil {
		log.Fatal(err)
	}
	Convey("Ensure nothing blows up on the handler stubs", t, func() {
		So(handler.Dial(), ShouldBeNil)
		So(handler.Dial(), ShouldNotBeNil)
		So(handler.Pools(), ShouldEqual, true)
	})

	Convey("Ensure we can receive messages", t, func() {
		p := syslog.Packet{
			Severity:0,
			Facility:0,
			Message:"line",
			Tag:"alamotest2112",
			Hostname:"alamotest2112-64cd4f4ff7-6bqb8.default", 
			Time:time.Now(),
		}
		f, err := os.Create("/tmp/kubernetes_test/alamotest2112-64cd4f4ff7-6bqb8_default_alamotest2112-a54517ce9ceb1e1d87fc41c263a3d7b95fd177a01b9acea61c643727a92306b1.log")
		So(err, ShouldBeNil)
		So(write(f, "{\"log\":\"line\",\"stream\":\"stdout\",\"time\":\"2006-01-02T15:04:05.000000000Z\"}\n"), ShouldBeNil)
		So(f.Close(), ShouldBeNil)
		select {
		case message := <-handler.Packets():
			So(p.Message, ShouldEqual, message.Message)
			So(p.Tag, ShouldEqual, message.Tag)
			So(p.Hostname, ShouldEqual, message.Hostname)
			So(p.Severity, ShouldEqual, message.Severity)
			So(p.Facility, ShouldEqual, message.Facility)
		case err := <-handler.Errors():
			log.Fatal(err)
		}

	})
	Convey("Ensure we can close the connection", t, func() {
		os.RemoveAll("/tmp/kubernetes_test")
		So(handler.Close(), ShouldBeNil)
	})
}