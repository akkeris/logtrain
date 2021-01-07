package persistent

import (
	. "github.com/smartystreets/goconvey/convey"
	syslog2 "github.com/trevorlinton/remote_syslog2/syslog"
	"github.com/google/uuid"
	"log"
	"testing"
	"time"
	"os"
	_ "github.com/mattn/go-sqlite3"
)

func TestPersistentOutput(t *testing.T) {
	// override postgres driver for sqllite for tests.
	createStatement = "create table logs (id varchar(128) primary key not null, data text not null default '')"
	sqlDriver = "sqlite3"
	if err := os.Setenv("PERSISTENT_DATABASE_URL","file::memory:"); err != nil {
		panic(err)
	}
	errorCh := make(chan error, 1)

	u := uuid.New()
	key := "keybyname" + u.String()

	output, err := Create("persistent://" + key, errorCh)

	Convey("Ensure persistent output is created", t, func() {
		So(err, ShouldBeNil)
	})
	Convey("Ensure we can start the persistent end point.", t, func() {
		So(output.Dial(), ShouldBeNil)
	})
	Convey("Ensure that an persistent end point pools connections.", t, func() {
		So(output.Pools(), ShouldEqual, true)
	})
	Convey("Ensure we can send messages to persistent channel", t, func() {
		now := time.Now()
		p := syslog2.Packet{
			Severity: 0,
			Facility: 0,
			Time:     now,
			Hostname: "localhost",
			Tag:      "HttpSyslogChannelTest",
			Message:  "Test Message",
		}
		output.Packets() <- p
		select {
		case error := <-errorCh:
			log.Fatal(error.Error())
		case <-time.NewTicker(time.Second * 2).C:
		}

		data, err := Get(key, output.db)
		So(err, ShouldBeNil)

		So(*data, ShouldEqual, p.Generate(maxLogSize) + "\n")
	})
	Convey("Ensure we can close the end point...", t, func() {
		So(output.Close(), ShouldBeNil)
	})
}
