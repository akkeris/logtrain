package storage

import (
	. "github.com/smartystreets/goconvey/convey"
	"testing"
)

func TestMemoryDataSource(t *testing.T) {
	mds := CreateMemoryDataSource()

	Convey("test memory datasource", t, func() {
		So(mds.Writable(), ShouldEqual, true)
		So(mds.EmitNewRoute(LogRoute{Endpoint: "endpoint", Hostname: "hostname", Tag: "tag"}), ShouldBeNil)
		routes, err := mds.GetAllRoutes()
		So(err, ShouldBeNil)
		So(len(routes), ShouldEqual, 1)
		So(mds.EmitRemoveRoute(LogRoute{Endpoint: "endpoint", Hostname: "hostname", Tag: "tag"}), ShouldBeNil)
		routes, err = mds.GetAllRoutes()
		So(err, ShouldBeNil)
		So(len(routes), ShouldEqual, 0)
	})

	Convey("test memory closable", t, func() {
		So(mds.Close(), ShouldBeNil)
	})
}
