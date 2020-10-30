package storage

import (
	"log"
	"time"
	"testing"
	. "github.com/smartystreets/goconvey/convey"
	"k8s.io/client-go/kubernetes/fake"
	core "k8s.io/api/core/v1"
	apps "k8s.io/api/apps/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)


func TestKubernetesDataSource(t *testing.T) {
	kube := fake.NewSimpleClientset()
	statefulset := apps.StatefulSet{}
	statefulset.SetName("alamotest2112")
	statefulset.SetNamespace("default")
	daemonset := apps.DaemonSet{}
	daemonset.SetName("alamotest2112")
	daemonset.SetNamespace("default")
	daemonset.Annotations = make(map[string]string)
	daemonset.Annotations[akkerisAppAnnotationKey] = "alamotest2112"
	daemonset.Annotations[akkerisDynoTypeAnnotationKey] = "worker"
	deployment := apps.Deployment{}
	deployment.SetName("alamotest2112")
	deployment.SetNamespace("default")
	replicaset := apps.ReplicaSet{}
	replicaset.SetName("alamotest2112-64cd4f4ff7")
	replicaset.SetNamespace("default")
	replicaset.SetOwnerReferences([]meta.OwnerReference{meta.OwnerReference{Kind:"deployment", Name:"alamotest2112"}})
	ds, err := CreateKubernetesDataSource(kube)
	if err != nil {
		log.Fatal(err.Error())
	}
	if err := kube.Tracker().Add(deployment.DeepCopyObject()); err != nil {
		log.Fatal(err.Error())
	}
	if err := kube.Tracker().Add(statefulset.DeepCopyObject()); err != nil {
		log.Fatal(err.Error())
	}
	if err := kube.Tracker().Add(daemonset.DeepCopyObject()); err != nil {
		log.Fatal(err.Error())
	}
	if err := kube.Tracker().Add(replicaset.DeepCopyObject()); err != nil {
		log.Fatal(err.Error())
	}
	
	Convey("Test deriving hostname from pods", t, func() {
		hostAndTag := DeriveHostnameFromPod("alamotest2112-64cd4f4ff7-6bqb8", "default", false)
		So(hostAndTag.Hostname, ShouldEqual, "alamotest2112.default")
		So(hostAndTag.Tag, ShouldEqual, "alamotest2112-64cd4f4ff7-6bqb8")
		hostAndTag = DeriveHostnameFromPod("alamotest2112-64cd4f4ff7-6bqb8", "default", true)
		So(hostAndTag.Hostname, ShouldEqual, "alamotest2112-default")
		So(hostAndTag.Tag, ShouldEqual, "web.64cd4f4ff7-6bqb8")
		hostAndTag = DeriveHostnameFromPod("alamotest2111--worker-64cd4f4ff7-6bqb8", "default", true)
		So(hostAndTag.Hostname, ShouldEqual, "alamotest2111-default")
		So(hostAndTag.Tag, ShouldEqual, "worker.64cd4f4ff7-6bqb8")
	})
	Convey("Test getting hostname from pods", t, func() {
		pod := core.Pod{}
		pod.SetName("alamotest2111-64cd4f4ff7-6bqb8") // use a different pod name to tell if it fell back to deriving the hostname.
		pod.SetNamespace("default")
		pod.Annotations = make(map[string]string)
		pod.Annotations[drainAnnotationKey] = "syslog://localhost:129"
		pod.SetOwnerReferences([]meta.OwnerReference{meta.OwnerReference{Kind:"replicaset", Name:"alamotest2112-64cd4f4ff7"}})
		hostAndTag := GetHostNameAndTagFromPod(kube, &pod, false)
		So(hostAndTag.Hostname, ShouldEqual, "alamotest2112.default")
		So(hostAndTag.Tag, ShouldEqual, "alamotest2111-64cd4f4ff7-6bqb8")
		
		hostAndTag = GetHostNameAndTagFromPod(kube, &pod, true)
		So(hostAndTag.Hostname, ShouldEqual, "alamotest2112-default")
		So(hostAndTag.Tag, ShouldEqual, "web.64cd4f4ff7-6bqb8")

		pod = core.Pod{}
		pod.SetName("alamotest2111--worker-64cd4f4ff7-6bqb8") // use a different pod name to tell if it fell back to deriving the hostname.
		pod.SetNamespace("default")
		pod.Annotations = make(map[string]string)
		pod.Annotations[drainAnnotationKey] = "syslog://localhost:129"
		pod.SetOwnerReferences([]meta.OwnerReference{meta.OwnerReference{Kind:"daemonset", Name:"alamotest2112"}})
		hostAndTag = GetHostNameAndTagFromPod(kube, &pod, true)
		So(hostAndTag.Hostname, ShouldEqual, "alamotest2112-default")
		So(hostAndTag.Tag, ShouldEqual, "worker.64cd4f4ff7-6bqb8")

		pod = core.Pod{}
		pod.SetName("alamotest2111--worker-64cd4f4ff7-6bqb8") // use a different pod name to tell if it fell back to deriving the hostname.
		pod.SetNamespace("default")
		pod.Annotations = make(map[string]string)
		pod.Annotations[drainAnnotationKey] = "syslog://localhost:129"
		pod.SetOwnerReferences([]meta.OwnerReference{meta.OwnerReference{Kind:"statefulset", Name:"alamotest2112"}})
		hostAndTag = GetHostNameAndTagFromPod(kube, &pod, true)
		So(hostAndTag.Hostname, ShouldEqual, "alamotest2112-default")
		So(hostAndTag.Tag, ShouldEqual, "worker.64cd4f4ff7-6bqb8")

		pod = core.Pod{}
		pod.SetName("alamotest2111--worker-64cd4f4ff7-6bqb8") // use a different pod name to tell if it fell back to deriving the hostname.
		pod.SetNamespace("default")
		pod.Annotations = make(map[string]string)
		pod.Annotations[drainAnnotationKey] = "syslog://localhost:129"
		pod.SetOwnerReferences([]meta.OwnerReference{meta.OwnerReference{Kind:"doesnotexist", Name:"alamotest2112"}})
		hostAndTag = GetHostNameAndTagFromPod(kube, &pod, true)
		So(hostAndTag.Hostname, ShouldEqual, "alamotest2111-default") // since kind does not exist it should back up to deriving the hostname.
		So(hostAndTag.Tag, ShouldEqual, "worker.64cd4f4ff7-6bqb8")

		pod = core.Pod{}
		pod.SetName("alamotest2110-64cd4f4ff7-6bqb8") // use a different pod name to tell if it fell back to deriving the hostname.
		pod.SetNamespace("default")
		pod.Annotations = make(map[string]string)
		pod.Annotations[drainAnnotationKey] = "syslog://localhost:129"
		pod.Annotations[hostnameAnnotationKey] = "foobar.com"
		pod.Annotations[tagAnnotationKey] = "alamotest2110"
		pod.SetOwnerReferences([]meta.OwnerReference{meta.OwnerReference{Kind:"deployment", Name:"alamotest2112"}})
		hostAndTag = GetHostNameAndTagFromPod(kube, &pod, true)
		So(hostAndTag.Hostname, ShouldEqual, "foobar.com")
		So(hostAndTag.Tag, ShouldEqual, "alamotest2110")

		pod = core.Pod{}
		pod.SetName("alamotest2110-64cd4f4ff7-6bqb8") // use a different pod name to tell if it fell back to deriving the hostname.
		pod.SetNamespace("default")
		pod.Annotations = make(map[string]string)
		pod.Annotations[drainAnnotationKey] = "syslog://localhost:129"
		pod.Annotations[hostnameAnnotationKey] = "foobar.com"
		pod.SetOwnerReferences([]meta.OwnerReference{meta.OwnerReference{Kind:"deployment", Name:"alamotest2112"}})
		hostAndTag = GetHostNameAndTagFromPod(kube, &pod, true)
		So(hostAndTag.Hostname, ShouldEqual, "foobar.com")
		So(hostAndTag.Tag, ShouldEqual, "web.64cd4f4ff7-6bqb8")
	})
	Convey("Test a new route being added and removed because of a new pod being created and then destroyed.", t, func() {
		pod := core.Pod{}
		pod.SetName("alamotest2112-64cd4f4ff7-6bqb8")
		pod.SetNamespace("default")
		pod.Annotations = make(map[string]string)
		pod.Annotations[drainAnnotationKey] = "syslog://localhost:123"
		pod.SetOwnerReferences([]meta.OwnerReference{meta.OwnerReference{Kind:"replicaset", Name:"alamotest2112-64cd4f4ff7"}})
		kube.Tracker().Add(pod.DeepCopyObject())
		select {
		case route := <-ds.AddRoute():
			So(route, ShouldNotBeNil)
			So(route.Endpoint, ShouldEqual, "syslog://localhost:123")
			So(route.Hostname, ShouldEqual, "alamotest2112.default")
		case <-time.NewTimer(time.Second * 5).C:
			log.Fatal("This should not have been called.")
		}
	})
	Convey("Test shutting down", t, func() {
		So(ds.Close(), ShouldBeNil)
	})
}