package storage

import (
	. "github.com/smartystreets/goconvey/convey"
	apps "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes/fake"
	"log"
	"os"
	"testing"
	"time"
)

func TestKubernetesDataSource(t *testing.T) {
	kube := fake.NewSimpleClientset()
	deplExp := apps.Deployment{}
	deplExp.SetName("alamotest2118")
	deplExp.SetNamespace("default")
	statExp := apps.StatefulSet{}
	statExp.SetName("alamotest2118s")
	statExp.SetNamespace("default")
	deaExp := apps.DaemonSet{}
	deaExp.SetName("alamotest2118de")
	deaExp.SetNamespace("default")
	if err := kube.Tracker().Add(deplExp.DeepCopyObject()); err != nil {
		log.Fatal(err.Error())
	}
	if err := kube.Tracker().Add(statExp.DeepCopyObject()); err != nil {
		log.Fatal(err.Error())
	}
	if err := kube.Tracker().Add(deaExp.DeepCopyObject()); err != nil {
		log.Fatal(err.Error())
	}
	/*
	 * Do not use the fake client Tracker to emit object changes,
	 * we must manually call private methods in the data source
	 * to mimick these callbacks as the fake client does not fully
	 * support all of the watcher functionality.
	 */
	ds, err := CreateKubernetesDataSource(kube, false)
	if err != nil {
		log.Fatal(err.Error())
	}

	Convey("test creating client", t, func() {
		So(ds.Dial(), ShouldBeNil)
		f, err := os.Create("/tmp/kubeconfigtest")
		So(err, ShouldBeNil)
		f.WriteString(`
apiVersion: v1
clusters:
- cluster:
    server: https://example.com/test/cluster
  name: test
contexts:
- context:
    cluster: test
    user: test
  name: test
current-context: test
kind: Config
preferences: {}
users:
- name: test
  user:
    token: abcdefg
`)
		So(f.Close(), ShouldBeNil)
		_, err = GetKubernetesClient("/tmp/kubeconfigtest")
		So(err, ShouldBeNil)
		_, err = GetKubernetesClient("")
		So(err, ShouldNotBeNil)
	})
	Convey("Test kubeObjectFromHost", t, func() {
		part1, part2 := kubeObjectFromHost("test-foo-fee", true)
		So(part1, ShouldEqual, "test")
		So(part2, ShouldEqual, "foo-fee")

		part1, part2 = kubeObjectFromHost("test.foo-fee", false)
		So(part1, ShouldEqual, "test")
		So(part2, ShouldEqual, "foo-fee")
	})
	Convey("Test Get hostname from TLO", t, func() {
		e := apps.Deployment{}
		e.SetName("alamotest2112")
		e.SetNamespace("default")
		e.Annotations = make(map[string]string)
		e.Annotations[DrainAnnotationKey] = "syslog://localhost:123"
		e.Annotations[HostnameAnnotationKey] = "example.com"
		So(GetHostNameFromTLO(kube, &e, true), ShouldEqual, "example.com")
		e = apps.Deployment{}
		e.SetName("alamotest2112")
		e.SetNamespace("default")
		e.Annotations = make(map[string]string)
		e.Annotations[DrainAnnotationKey] = "syslog://localhost:123"
		So(GetHostNameFromTLO(kube, &e, false), ShouldEqual, "alamotest2112.default")
		e = apps.Deployment{}
		e.SetName("alamotest2112")
		e.SetNamespace("default")
		e.Annotations = make(map[string]string)
		e.Annotations[DrainAnnotationKey] = "syslog://localhost:123"
		So(GetHostNameFromTLO(kube, &e, true), ShouldEqual, "alamotest2112-default")
	})
	Convey("Test a new route being added and removed because of a new deployment being created and then destroyed.", t, func() {
		d := apps.Deployment{}
		d.SetName("alamotest2112")
		d.SetNamespace("default")
		d.Annotations = make(map[string]string)
		d.Annotations[DrainAnnotationKey] = "syslog://localhost:123"

		// test route adds
		ds.addRouteFromObj(&d)
		select {
		case route := <-ds.AddRoute():
			So(route, ShouldNotBeNil)
			So(route.Endpoint, ShouldEqual, "syslog://localhost:123")
			So(route.Hostname, ShouldEqual, "alamotest2112.default")
		case <-time.NewTimer(time.Second * 5).C:
			log.Fatal("This should not have been called (add).")
		}
		So(GetHostNameFromTLO(kube, &d, true), ShouldEqual, "alamotest2112-default")

		// test route removals
		ds.removeRouteFromObj(&d)
		select {
		case route := <-ds.RemoveRoute():
			So(route, ShouldNotBeNil)
			So(route.Endpoint, ShouldEqual, "syslog://localhost:123")
			So(route.Hostname, ShouldEqual, "alamotest2112.default")
		case <-time.NewTimer(time.Second * 5).C:
			log.Fatal("This should not have been called (remove).")
		}

		// Test route updates
		d.Annotations = make(map[string]string)
		e := apps.Deployment{}
		e.SetName("alamotest2112")
		e.SetNamespace("default")
		e.Annotations = make(map[string]string)
		e.Annotations[DrainAnnotationKey] = "syslog://localhost:124"

		ds.reviewUpdateFromObj(&d, &e)
		select {
		case route := <-ds.AddRoute():
			So(route, ShouldNotBeNil)
			So(route.Endpoint, ShouldEqual, "syslog://localhost:124")
			So(route.Hostname, ShouldEqual, "alamotest2112.default")
		case <-time.NewTimer(time.Second * 5).C:
			log.Fatal("This should not have been called (update).")
		}

		// Test route updates (adds to annotation)
		d.Annotations[DrainAnnotationKey] = "syslog://localhost:124"
		e.Annotations[DrainAnnotationKey] = "syslog://localhost:124; syslog://localhost:125"

		ds.reviewUpdateFromObj(&d, &e)
		select {
		case route := <-ds.AddRoute():
			So(route, ShouldNotBeNil)
			So(route.Endpoint, ShouldEqual, "syslog://localhost:125")
			So(route.Hostname, ShouldEqual, "alamotest2112.default")
		case <-ds.RemoveRoute():
			log.Fatal("This should not have been called (update remove #2).")
		case <-time.NewTimer(time.Second * 5).C:
			log.Fatal("This should not have been called (update #2).")
		}

		// Test route updates (remove from annotation)
		d.Annotations[DrainAnnotationKey] = "syslog://localhost:124; syslog://localhost:125"
		e.Annotations[DrainAnnotationKey] = "syslog://localhost:125"

		ds.reviewUpdateFromObj(&d, &e)
		select {
		case route := <-ds.RemoveRoute():
			So(route, ShouldNotBeNil)
			So(route.Endpoint, ShouldEqual, "syslog://localhost:124")
			So(route.Hostname, ShouldEqual, "alamotest2112.default")
		case <-ds.AddRoute():
			log.Fatal("This should not have been called (update add #3).")
		case <-time.NewTimer(time.Second * 5).C:
			log.Fatal("This should not have been called (update #3).")
		}

		// Test route updates (remove all)
		d.Annotations[DrainAnnotationKey] = "syslog://localhost:124; syslog://localhost:125"
		e.Annotations[DrainAnnotationKey] = ""

		ds.reviewUpdateFromObj(&d, &e)
		select {
		case route := <-ds.RemoveRoute():
			So(route, ShouldNotBeNil)
		case <-ds.AddRoute():
			log.Fatal("This should not have been called (update add #4).")
		case <-time.NewTimer(time.Second * 5).C:
			log.Fatal("This should not have been called (update #4).")
		}
	})
	Convey("Ensure we can get all routes", t, func() {
		routes, err := ds.GetAllRoutes()
		So(err, ShouldBeNil)
		So(len(routes), ShouldEqual, 0)
	})
	Convey("Test emitting routes dont have errors", t, func() {
		ds.writable = true // for it to be writable.
		So(ds.EmitNewRoute(LogRoute{
			Endpoint: "syslog://example.com:1234",
			Hostname: "alamotest2118.default",
			Tag:      "sometag",
		}), ShouldBeNil)
		So(ds.EmitRemoveRoute(LogRoute{
			Endpoint: "syslog://example.com:1234",
			Hostname: "alamotest2118.default",
			Tag:      "sometag",
		}), ShouldBeNil)
		So(ds.EmitNewRoute(LogRoute{
			Endpoint: "syslog://example1.com:1234",
			Hostname: "alamotest2118s.default",
			Tag:      "sometag",
		}), ShouldBeNil)
		So(ds.EmitRemoveRoute(LogRoute{
			Endpoint: "syslog://example1.com:1234",
			Hostname: "alamotest2118s.default",
			Tag:      "sometag",
		}), ShouldBeNil)
		So(ds.EmitNewRoute(LogRoute{
			Endpoint: "syslog://example2.com:1234",
			Hostname: "alamotest2118de.default",
			Tag:      "sometag",
		}), ShouldBeNil)
		So(ds.EmitRemoveRoute(LogRoute{
			Endpoint: "syslog://example2.com:1234",
			Hostname: "alamotest2118de.default",
			Tag:      "sometag",
		}), ShouldBeNil)
	})
	Convey("Test shutting down", t, func() {
		So(ds.Close(), ShouldBeNil)
	})
}
