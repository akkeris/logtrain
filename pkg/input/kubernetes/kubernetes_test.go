package kubernetes

import (
	"github.com/akkeris/logtrain/internal/storage"
	"github.com/papertrail/remote_syslog2/syslog"
	. "github.com/smartystreets/goconvey/convey"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"log"
	"os"
	"testing"
	"time"
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
	kube := fake.NewSimpleClientset()

	if err := os.RemoveAll("/tmp/kubernetes_test"); err != nil {
		log.Fatal(err)
	}
	if err := os.Mkdir("/tmp/kubernetes_test", 0755); err != nil {
		log.Fatal(err)
	}

	statefulset := apps.StatefulSet{}
	statefulset.SetName("alamotest2112")
	statefulset.SetNamespace("default")
	daemonset := apps.DaemonSet{}
	daemonset.SetName("alamotest2112")
	daemonset.SetNamespace("default")
	daemonset.Annotations = make(map[string]string)
	daemonset.Annotations[storage.AkkerisAppLabelKey] = "alamotest2112"
	daemonset.Annotations[storage.AkkerisDynoTypeLabelKey] = "worker"
	deployment := apps.Deployment{}
	deployment.SetName("alamotest2112")
	deployment.SetNamespace("default")
	replicaset := apps.ReplicaSet{}
	replicaset.SetName("alamotest2112-64cd4f4ff7")
	replicaset.SetNamespace("default")
	replicaset.SetOwnerReferences([]meta.OwnerReference{meta.OwnerReference{Kind: "deployment", Name: "alamotest2112"}})

	deploymentWithHostnameAndTag := apps.Deployment{}
	deploymentWithHostnameAndTag.SetName("alamotest2115")
	deploymentWithHostnameAndTag.SetNamespace("default")
	deploymentWithHostnameAndTag.Annotations = make(map[string]string)
	deploymentWithHostnameAndTag.Annotations[storage.HostnameAnnotationKey] = "foobar.com"
	deploymentWithHostnameAndTag.Annotations[storage.TagAnnotationKey] = "alamotest2110"

	deploymentWithHostname := apps.Deployment{}
	deploymentWithHostname.SetName("alamotest2116")
	deploymentWithHostname.SetNamespace("default")
	deploymentWithHostname.Annotations = make(map[string]string)
	deploymentWithHostname.Annotations[storage.HostnameAnnotationKey] = "foobar.com"

	if err := kube.Tracker().Add(replicaset.DeepCopyObject()); err != nil {
		log.Fatal(err.Error())
	}
	if err := kube.Tracker().Add(statefulset.DeepCopyObject()); err != nil {
		log.Fatal(err.Error())
	}
	if err := kube.Tracker().Add(daemonset.DeepCopyObject()); err != nil {
		log.Fatal(err.Error())
	}
	if err := kube.Tracker().Add(deployment.DeepCopyObject()); err != nil {
		log.Fatal(err.Error())
	}
	if err := kube.Tracker().Add(deploymentWithHostname.DeepCopyObject()); err != nil {
		log.Fatal(err.Error())
	}
	if err := kube.Tracker().Add(deploymentWithHostnameAndTag.DeepCopyObject()); err != nil {
		log.Fatal(err.Error())
	}

	pod := core.Pod{}
	pod.SetName("alamotest2112-64cd4f4ff7-6bqb8")
	pod.SetNamespace("default")
	pod.SetOwnerReferences([]meta.OwnerReference{meta.OwnerReference{Kind: "replicaset", Name: "alamotest2112-64cd4f4ff7"}})
	handler, err := Create("/tmp/kubernetes_test", fake.NewSimpleClientset(pod.DeepCopyObject(), replicaset.DeepCopyObject(), deployment.DeepCopyObject()))
	if err != nil {
		log.Fatal(err)
	}

	Convey("Ensure nothing blows up on the handler stubs", t, func() {
		So(handler.Dial(), ShouldBeNil)
		So(handler.Dial(), ShouldNotBeNil)
		So(handler.Pools(), ShouldEqual, true)
	})

	Convey("Test deriving hostname from pods", t, func() {
		hostAndTag := deriveHostnameFromPod("alamotest2112-64cd4f4ff7-6bqb8", "default", false)
		So(hostAndTag.Hostname, ShouldEqual, "alamotest2112.default")
		So(hostAndTag.Tag, ShouldEqual, "alamotest2112-64cd4f4ff7-6bqb8")
		hostAndTag = deriveHostnameFromPod("alamotest2112-64cd4f4ff7-6bqb8", "default", true)
		So(hostAndTag.Hostname, ShouldEqual, "alamotest2112-default")
		So(hostAndTag.Tag, ShouldEqual, "web.64cd4f4ff7-6bqb8")
		hostAndTag = deriveHostnameFromPod("alamotest2111--worker-64cd4f4ff7-6bqb8", "default", true)
		So(hostAndTag.Hostname, ShouldEqual, "alamotest2111-default")
		So(hostAndTag.Tag, ShouldEqual, "worker.64cd4f4ff7-6bqb8")
	})
	Convey("Test getting hostname from pods", t, func() {
		pod := core.Pod{}
		pod.SetName("alamotest2111-64cd4f4ff7-6bqb8") // use a different pod name to tell if it fell back to deriving the hostname.
		pod.SetNamespace("default")
		pod.Annotations = make(map[string]string)
		pod.Annotations[storage.DrainAnnotationKey] = "syslog://localhost:129"
		pod.SetOwnerReferences([]meta.OwnerReference{meta.OwnerReference{Kind: "replicaset", Name: "alamotest2112-64cd4f4ff7"}})
		hostAndTag := getHostnameAndTagFromPod(kube, &pod, false)
		So(hostAndTag.Hostname, ShouldEqual, "alamotest2112.default")
		So(hostAndTag.Tag, ShouldEqual, "alamotest2111-64cd4f4ff7-6bqb8")

		hostAndTag = getHostnameAndTagFromPod(kube, &pod, true)
		So(hostAndTag.Hostname, ShouldEqual, "alamotest2112-default")
		So(hostAndTag.Tag, ShouldEqual, "web.64cd4f4ff7-6bqb8")

		pod = core.Pod{}
		pod.SetName("alamotest2111--worker-64cd4f4ff7-6bqb8") // use a different pod name to tell if it fell back to deriving the hostname.
		pod.SetNamespace("default")
		pod.Annotations = make(map[string]string)
		pod.Annotations[storage.DrainAnnotationKey] = "syslog://localhost:129"
		pod.SetOwnerReferences([]meta.OwnerReference{meta.OwnerReference{Kind: "daemonset", Name: "alamotest2112"}})
		hostAndTag = getHostnameAndTagFromPod(kube, &pod, true)
		So(hostAndTag.Hostname, ShouldEqual, "alamotest2112-default")
		So(hostAndTag.Tag, ShouldEqual, "worker.64cd4f4ff7-6bqb8")

		pod = core.Pod{}
		pod.SetName("alamotest2111--worker-64cd4f4ff7-6bqb8") // use a different pod name to tell if it fell back to deriving the hostname.
		pod.SetNamespace("default")
		pod.Annotations = make(map[string]string)
		pod.Annotations[storage.DrainAnnotationKey] = "syslog://localhost:129"
		pod.SetOwnerReferences([]meta.OwnerReference{meta.OwnerReference{Kind: "statefulset", Name: "alamotest2112"}})
		hostAndTag = getHostnameAndTagFromPod(kube, &pod, true)
		So(hostAndTag.Hostname, ShouldEqual, "alamotest2112-default")
		So(hostAndTag.Tag, ShouldEqual, "worker.64cd4f4ff7-6bqb8")

		pod = core.Pod{}
		pod.SetName("alamotest2111--worker-64cd4f4ff7-6bqb8") // use a different pod name to tell if it fell back to deriving the hostname.
		pod.SetNamespace("default")
		pod.Annotations = make(map[string]string)
		pod.Annotations[storage.DrainAnnotationKey] = "syslog://localhost:129"
		pod.SetOwnerReferences([]meta.OwnerReference{meta.OwnerReference{Kind: "doesnotexist", Name: "alamotest2112"}})
		hostAndTag = getHostnameAndTagFromPod(kube, &pod, true)
		So(hostAndTag.Hostname, ShouldEqual, "alamotest2111-default") // since kind does not exist it should back up to deriving the hostname.
		So(hostAndTag.Tag, ShouldEqual, "worker.64cd4f4ff7-6bqb8")

		pod = core.Pod{}
		pod.SetName("alamotest2110-64cd4f4ff7-6bqb8") // use a different pod name to tell if it fell back to deriving the hostname.
		pod.SetNamespace("default")
		pod.Annotations = make(map[string]string)
		pod.SetOwnerReferences([]meta.OwnerReference{meta.OwnerReference{Kind: "deployment", Name: "alamotest2115"}})
		hostAndTag = getHostnameAndTagFromPod(kube, &pod, true)
		So(hostAndTag.Hostname, ShouldEqual, "foobar.com")
		So(hostAndTag.Tag, ShouldEqual, "alamotest2110")

		pod = core.Pod{}
		pod.SetName("alamotest2110-64cd4f4ff7-6bqb8") // use a different pod name to tell if it fell back to deriving the hostname.
		pod.SetNamespace("default")
		pod.Annotations = make(map[string]string)
		pod.SetOwnerReferences([]meta.OwnerReference{meta.OwnerReference{Kind: "deployment", Name: "alamotest2116"}})
		hostAndTag = getHostnameAndTagFromPod(kube, &pod, true)
		So(hostAndTag.Hostname, ShouldEqual, "foobar.com")
		So(hostAndTag.Tag, ShouldEqual, "web.64cd4f4ff7-6bqb8")
	})
	Convey("Ensure we can receive messages", t, func() {
		p := syslog.Packet{
			Severity: 0,
			Facility: 0,
			Message:  "line",
			Tag:      "alamotest2112-64cd4f4ff7-6bqb8",
			Hostname: "alamotest2112.default",
			Time:     time.Now(),
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
