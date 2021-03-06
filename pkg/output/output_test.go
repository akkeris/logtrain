package output

import (
	. "github.com/smartystreets/goconvey/convey"
	"testing"
)

func TestOutput(t *testing.T) {
	errorCh := make(chan error, 1)
	Convey("Ensure testing elasticsearch endpoint returns an item", t, func() {
		So(TestEndpoint("elasticsearch://localhost"), ShouldBeNil)
		So(TestEndpoint("elasticsearch+http://localhost"), ShouldBeNil)
		So(TestEndpoint("elasticsearch+https://localhost"), ShouldBeNil)
		So(TestEndpoint("es://localhost"), ShouldBeNil)
		So(TestEndpoint("es+http://localhost"), ShouldBeNil)
		So(TestEndpoint("es+https://localhost"), ShouldBeNil)
		out, err := Create("elasticsearch://localhost", errorCh)
		So(err, ShouldBeNil)
		So(out, ShouldNotBeNil)
		out, err = Create("elasticsearch+https://localhost", errorCh)
		So(err, ShouldBeNil)
		So(out, ShouldNotBeNil)
		out, err = Create("elasticsearch+http://localhost", errorCh)
		So(err, ShouldBeNil)
		So(out, ShouldNotBeNil)
		out, err = Create("es://localhost", errorCh)
		So(err, ShouldBeNil)
		So(out, ShouldNotBeNil)
		out, err = Create("es+https://localhost", errorCh)
		So(err, ShouldBeNil)
		So(out, ShouldNotBeNil)
		out, err = Create("es+http://localhost", errorCh)
		So(err, ShouldBeNil)
		So(out, ShouldNotBeNil)
	})
	Convey("Ensure testing syslog+http(s) endpoint returns an item", t, func() {
		So(TestEndpoint("syslog+http://localhost"), ShouldBeNil)
		So(TestEndpoint("syslog+https://localhost"), ShouldBeNil)
		out, err := Create("syslog+https://localhost", errorCh)
		So(err, ShouldBeNil)
		So(out, ShouldNotBeNil)
		out, err = Create("syslog+http://localhost", errorCh)
		So(err, ShouldBeNil)
		So(out, ShouldNotBeNil)
	})
	Convey("Ensure testing http(s) endpoint returns an item", t, func() {
		So(TestEndpoint("http://localhost"), ShouldBeNil)
		So(TestEndpoint("https://localhost"), ShouldBeNil)
		out, err := Create("https://localhost", errorCh)
		So(err, ShouldBeNil)
		So(out, ShouldNotBeNil)
		out, err = Create("http://localhost", errorCh)
		So(err, ShouldBeNil)
		So(out, ShouldNotBeNil)
	})
	Convey("Ensure testing syslog, syslog+udp endpoint returns an item", t, func() {
		So(TestEndpoint("syslog://localhost"), ShouldBeNil)
		So(TestEndpoint("syslog+udp://localhost"), ShouldBeNil)
		out, err := Create("syslog://localhost", errorCh)
		So(err, ShouldBeNil)
		So(out, ShouldNotBeNil)
		out, err = Create("syslog+udp://localhost", errorCh)
		So(err, ShouldBeNil)
		So(out, ShouldNotBeNil)
	})
	Convey("Ensure testing syslog+tcp endpoint returns an item", t, func() {
		So(TestEndpoint("syslog+tcp://localhost"), ShouldBeNil)
		out, err := Create("syslog+tcp://localhost", errorCh)
		So(err, ShouldBeNil)
		So(out, ShouldNotBeNil)
	})
	Convey("Ensure testing syslog+tls endpoint returns an item", t, func() {
		So(TestEndpoint("syslog+tls://localhost"), ShouldBeNil)
		out, err := Create("syslog+tls://localhost", errorCh)
		So(err, ShouldBeNil)
		So(out, ShouldNotBeNil)
	})
	Convey("Ensure unrecognized schemas are not allowed", t, func() {
		So(TestEndpoint("foobar://fee"), ShouldNotBeNil)
		_, err := Create("foobar://localhost", errorCh)
		So(err, ShouldNotBeNil)
	})
}
