package storage

import (
	"log"
	"time"
	"testing"
	. "github.com/smartystreets/goconvey/convey"
	"k8s.io/client-go/kubernetes/fake"
	apps "k8s.io/api/apps/v1"
)


func TestKubernetesDataSource(t *testing.T) {
	kube := fake.NewSimpleClientset()
	ds, err := CreateKubernetesDataSource(kube)
	if err != nil {
		log.Fatal(err.Error())
	}
	Convey("Test Get hostname from TLO", t, func() {
		e := apps.Deployment{}
		e.SetName("alamotest2112")
		e.SetNamespace("default")
		e.Annotations = make(map[string]string)
		e.Annotations[DrainAnnotationKey] = "syslog://localhost:123"
		e.Annotations[HostnameAnnotationKey] = "example.com"
		So(GetHostNameFromTLO(kube, &e, true), ShouldEqual, "example.com")
	})
	Convey("Test a new route being added and removed because of a new deployment being created and then destroyed.", t, func() {
		d := apps.Deployment{}
		d.SetName("alamotest2112")
		d.SetNamespace("default")
		d.Annotations = make(map[string]string)
		d.Annotations[DrainAnnotationKey] = "syslog://localhost:123"
		kube.Tracker().Add(d.DeepCopyObject())
		select {
		case route := <-ds.AddRoute():
			So(route, ShouldNotBeNil)
			So(route.Endpoint, ShouldEqual, "syslog://localhost:123")
			So(route.Hostname, ShouldEqual, "alamotest2112.default")
		case <-time.NewTimer(time.Second * 5).C:
			log.Fatal("This should not have been called.")
		}
		So(GetHostNameFromTLO(kube, &d, true), ShouldEqual, "alamotest2112-default")
	})
	Convey("Test shutting down", t, func() {
		So(ds.Close(), ShouldBeNil)
	})
}