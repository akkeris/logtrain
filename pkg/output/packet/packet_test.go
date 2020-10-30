package packet

import (
	"encoding/json"
	syslog2 "github.com/papertrail/remote_syslog2/syslog"
	. "github.com/smartystreets/goconvey/convey"
	"testing"
	"time"
)

func TestPacketEncoding(t *testing.T) {
	now := time.Now()
	p := Packet{
		syslog2.Packet{
			Severity: 2,
			Facility: 3,
			Time:     now,
			Hostname: "localhost",
			Tag:      "HttpChannelTest",
			Message:  "Test Message",
		},
	}
	encoded := "{\"severity\":2,\"facility\":3,\"hostname\":\"localhost\",\"tag\":\"HttpChannelTest\",\"time\":\"" + now.Format(Rfc5424time) + "\",\"message\":\"Test Message\"}"
	encodedOptional := "{\"hostname\":\"localhost2\",\"tag\":\"HttpChannelTest2\",\"time\":\"" + now.Format(Rfc5424time) + "\",\"message\":\"Test Message 2\"}"

	Convey("Ensure JSON encoding works.", t, func() {
		payload, err := json.Marshal(p)
		So(err, ShouldBeNil)
		So(string(payload), ShouldEqual, encoded)
	})

	Convey("Ensure JSON decoding works.", t, func() {
		p2 := Packet{}
		err := json.Unmarshal([]byte(encoded), &p2)
		So(err, ShouldBeNil)
		So(p2.Severity, ShouldEqual, p.Severity)
		So(p2.Facility, ShouldEqual, p.Facility)
		// So(p2.Time, ShouldEqual, p.Time) TODO, this is oddly different depending on platforms.
		So(p2.Hostname, ShouldEqual, p.Hostname)
		So(p2.Tag, ShouldEqual, p.Tag)
		So(p2.Message, ShouldEqual, p.Message)
	})

	Convey("Ensure JSON decoding works with optional severity/facility.", t, func() {
		p2 := Packet{}
		err := json.Unmarshal([]byte(encodedOptional), &p2)
		So(err, ShouldBeNil)
		So(p2.Severity, ShouldEqual, 0)
		So(p2.Facility, ShouldEqual, 0)
		So(p2.Hostname, ShouldEqual, "localhost2")
		So(p2.Tag, ShouldEqual, "HttpChannelTest2")
		So(p2.Message, ShouldEqual, "Test Message 2")
	})

	Convey("Ensure JSON hostname field is required", t, func() {
		p2 := Packet{}
		So(json.Unmarshal([]byte("{\"tag\":\"HttpChannelTest2\",\"time\":\""+now.Format(Rfc5424time)+"\",\"message\":\"Test Message 2\"}"), &p2), ShouldNotBeNil)
	})

	Convey("Ensure JSON tag field is required", t, func() {
		p2 := Packet{}
		So(json.Unmarshal([]byte("{\"hostname\":\"localhost2\",\"time\":\""+now.Format(Rfc5424time)+"\",\"message\":\"Test Message 2\"}"), &p2), ShouldNotBeNil)
	})

	Convey("Ensure JSON time field is not required", t, func() {
		p2 := Packet{}
		So(json.Unmarshal([]byte("{\"hostname\":\"localhost2\",\"tag\":\"HttpChannelTest2\",\"message\":\"Test Message 2\"}"), &p2), ShouldBeNil)
	})

	Convey("Ensure JSON message field is required", t, func() {
		p2 := Packet{}
		So(json.Unmarshal([]byte("{\"hostname\":\"localhost2\",\"tag\":\"HttpChannelTest2\",\"time\":\""+now.Format(Rfc5424time)+"\"}"), &p2), ShouldNotBeNil)
	})

	Convey("Ensure broken JSON does not decode.", t, func() {
		p2 := Packet{}
		So(json.Unmarshal([]byte("[]"), &p2), ShouldNotBeNil)
	})

	Convey("Ensure broken timestamps do not work.", t, func() {
		p2 := Packet{}
		So(json.Unmarshal([]byte("{\"hostname\":\"localhost2\",\"tag\":\"HttpChannelTest2\",\"time\":\"invalid\",\"message\":\"Test Message 2\"}"), &p2), ShouldNotBeNil)
	})
}
