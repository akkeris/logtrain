package storage

import (
	"database/sql"
	"github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
	. "github.com/smartystreets/goconvey/convey"
	"log"
	"testing"
	"time"
)

type fakeListener struct {
	notification chan *pq.Notification
}

func (fake *fakeListener) Close() error {
	return nil
}
func (fake *fakeListener) Listen(string) error {
	return nil
}
func (fake *fakeListener) NotificationChannel() <-chan *pq.Notification {
	return fake.notification
}
func (fake *fakeListener) Ping() error {
	return nil
}

func TestPostgresDataSource(t *testing.T) {
	db, err := sql.Open("sqlite3", "drains")
	if err != nil {
		log.Fatal(err)
	}
	listener := fakeListener{
		notification: make(chan *pq.Notification, 1),
	}
	ds, err := CreatePostgresDataSource(db, &listener, false)

	Convey("Ensure we can create a pg datasource", t, func() {
		So(err, ShouldBeNil)
		So(ds, ShouldNotBeNil)
	})

	Convey("testing adding a new route", t, func() {
		listener.notification <- &pq.Notification{
			BePid:   0,
			Channel: "drains.insert",
			Extra:   `{"drain":"ff6aca6c-2c5e-4220-a961-261bca5ff1e4","hostname":"alamotest2115.default","endpoint":"syslog://localhost:123","created":"2020-11-03T11:35:50.330592-07:00","updated":"2020-11-03T11:35:50.330592-07:00"}`,
		}
		select {
		case route := <-ds.AddRoute():
			So(route, ShouldNotBeNil)
			So(route.Endpoint, ShouldEqual, "syslog://localhost:123")
			So(route.Hostname, ShouldEqual, "alamotest2115.default")
		case <-time.NewTimer(time.Second * 5).C:
			log.Fatal("This should not have been called (add).")
		}
	})
	Convey("testing updating a route", t, func() {
		listener.notification <- &pq.Notification{
			BePid:   0,
			Channel: "drains.update",
			Extra:   `{"old":{"drain":"ff6aca6c-2c5e-4220-a961-261bca5ff1e4","hostname":"alamotest2115.default","endpoint":"syslog://localhost:123","created":"2020-11-03T11:35:50.330592-07:00","updated":"2020-11-03T11:35:50.330592-07:00"}, "new":{"drain":"ff6aca6c-2c5e-4220-a961-261bca5ff1e4","hostname":"alamotest2115.default","endpoint":"syslog://localhost:124","created":"2020-11-03T11:35:50.330592-07:00","updated":"2020-11-03T11:35:50.330592-07:00"}}`,
		}
		select {
		case route := <-ds.RemoveRoute():
			So(route, ShouldNotBeNil)
			So(route.Endpoint, ShouldEqual, "syslog://localhost:123")
			So(route.Hostname, ShouldEqual, "alamotest2115.default")
		case <-time.NewTimer(time.Second * 5).C:
			log.Fatal("This should not have been called (remove).")
		}
		select {
		case route := <-ds.AddRoute():
			So(route, ShouldNotBeNil)
			So(route.Endpoint, ShouldEqual, "syslog://localhost:124")
			So(route.Hostname, ShouldEqual, "alamotest2115.default")
		case <-time.NewTimer(time.Second * 5).C:
			log.Fatal("This should not have been called (add).")
		}
	})
	Convey("testing removing a route", t, func() {
		listener.notification <- &pq.Notification{
			BePid:   0,
			Channel: "drains.delete",
			Extra:   `{"drain":"ff6aca6c-2c5e-4220-a961-261bca5ff1e4","hostname":"alamotest2115.default","endpoint":"syslog://localhost:123","created":"2020-11-03T11:35:50.330592-07:00","updated":"2020-11-03T11:35:50.330592-07:00"}`,
		}
		select {
		case route := <-ds.RemoveRoute():
			So(route, ShouldNotBeNil)
			So(route.Endpoint, ShouldEqual, "syslog://localhost:123")
			So(route.Hostname, ShouldEqual, "alamotest2115.default")
		case <-time.NewTimer(time.Second * 5).C:
			log.Fatal("This should not have been called (remove on removing).")
		}
	})
	Convey("testing no-op on a route and test GetAllRoutes", t, func() {
		listener.notification <- &pq.Notification{
			BePid:   0,
			Channel: "drains.update",
			Extra:   `{"old":{"drain":"ff6aca6c-2c5e-4220-a961-261bca5ff1e4","hostname":"alamotest2115.default","endpoint":"syslog://localhost:123","created":"2020-11-03T11:35:50.330592-07:00","updated":"2020-11-03T11:35:50.330592-07:00"}, "new":{"drain":"ff6aca6c-2c5e-4220-a961-261bca5ff1e4","hostname":"alamotest2115.default","endpoint":"syslog://localhost:123","created":"2020-11-03T11:35:50.330592-07:00","updated":"2020-11-03T11:35:50.330592-07:00"}}`,
		}
		routes, err := ds.GetAllRoutes()
		So(err, ShouldBeNil)
		rlen := len(routes)
		select {
		case <-ds.RemoveRoute():
			log.Fatal("This should not have been called (remove, no-op).")
		case <-ds.AddRoute():
			log.Fatal("This should not have been called (add, no-op).")
		case <-time.NewTimer(time.Second * 2).C:
			routes, err := ds.GetAllRoutes()
			So(err, ShouldBeNil)
			So(rlen, ShouldEqual, len(routes))
		}
	})
}
