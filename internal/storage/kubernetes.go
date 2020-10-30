package storage

import (
	"errors"
	"log"
	"os"
	"strings"
	"time"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	api "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Annotations on deployment, statefulset, or daemonset
const akkerisAppAnnotationKey = "akkeris.io/app-name"
const akkerisDynoTypeAnnotationKey = "akkeris.io/dyno-type"

// TODO: Having this on the pod will cause a scale down (say from 3 pods to 2 pods) to
// accidently remove the route.  Find a solution, including moving these to top-level
// objects?
const drainAnnotationKey = "logtrain.akkeris.io/drains"
const hostnameAnnotationKey = "logtrain.akkeris.io/hostname"
const tagAnnotationKey = "logtrain.akkeris.io/tag"

type KubernetesDataSource struct {
	useAkkerisHosts bool
	stop chan struct{}
	kube kubernetes.Interface
	add chan LogRoute
	remove chan LogRoute
	routes []LogRoute
}

type HostnameAndTag struct {
	Hostname string
	Tag string
}

func getTopLevelObject(kube kubernetes.Interface, obj api.Object) (api.Object, error) {
	refs := obj.GetOwnerReferences()
	for _, ref := range refs {
		if ref.Controller == nil || *ref.Controller == false {
			if strings.ToLower(ref.Kind) == "replicaset" || strings.ToLower(ref.Kind) == "replicasets" {
				nObj, err := kube.AppsV1().ReplicaSets(obj.GetNamespace()).Get(ref.Name, api.GetOptions{})
				if err != nil {
					return nil, err
				}
				return getTopLevelObject(kube, nObj)
			} else if strings.ToLower(ref.Kind) == "deployment" || strings.ToLower(ref.Kind) == "deployments" {
				nObj, err := kube.AppsV1().Deployments(obj.GetNamespace()).Get(ref.Name, api.GetOptions{})
				if err != nil {
					return nil, err
				}
				return getTopLevelObject(kube, nObj)
			} else if strings.ToLower(ref.Kind) == "daemonset" || strings.ToLower(ref.Kind) == "daemonsets" {
				nObj, err := kube.AppsV1().DaemonSets(obj.GetNamespace()).Get(ref.Name, api.GetOptions{})
				if err != nil {
					return nil, err
				}
				return getTopLevelObject(kube, nObj)
			} else if strings.ToLower(ref.Kind) == "statefulset" || strings.ToLower(ref.Kind) == "statefulsets" {
				nObj, err := kube.AppsV1().StatefulSets(obj.GetNamespace()).Get(ref.Name, api.GetOptions{})
				if err != nil {
					return nil, err
				}
				return getTopLevelObject(kube, nObj)
			} else {
				return nil, errors.New("unrecognized object type " + ref.Kind)
			}
		}
	}
	return obj, nil
}


func DeriveHostnameFromPod(podName string, podNamespace string, useAkkerisHosts bool) *HostnameAndTag {
	parts := strings.Split(podName, "-")
	if useAkkerisHosts {
		podId := strings.Join(parts[len(parts)-2:], "-")
		appAndDyno := strings.SplitN(strings.Join(parts[:len(parts)-2], "-"), "--", 2)
		if len(appAndDyno) < 2 {
			appAndDyno = append(appAndDyno, "web")
		}
		return &HostnameAndTag{
			Hostname: appAndDyno[0] + "-" + podNamespace,
			Tag: appAndDyno[1] + "." + podId,
		}
	}
	return &HostnameAndTag{
		Hostname: strings.Join(parts[:len(parts)-2], "-") + "." + podNamespace,
		Tag: podName,
	}
}

func akkerisGetTag(parts []string) string {
	podId := strings.Join(parts[len(parts)-2:], "-")
	appAndDyno := strings.SplitN(strings.Join(parts[:len(parts)-2], "-"), "--", 2)
	if len(appAndDyno) < 2 {
		appAndDyno = append(appAndDyno, "web")
	}
	return appAndDyno[1] + "." + podId
}

func GetHostNameAndTagFromPod(kube kubernetes.Interface, pod *v1.Pod, useAkkerisHosts bool) *HostnameAndTag {
	parts := strings.Split(pod.GetName(), "-")
	if host, ok := pod.GetAnnotations()[hostnameAnnotationKey]; ok {
		if tag, ok := pod.GetAnnotations()[tagAnnotationKey]; ok {
			return &HostnameAndTag{
				Hostname: host,
				Tag: tag,
			}
		} else {
			if useAkkerisHosts == true {
				return &HostnameAndTag{
					Hostname: host,
					Tag: akkerisGetTag(parts),
				}
			} else {
				return &HostnameAndTag{
					Hostname: host,
					Tag: pod.GetName(),
				}
			}
		}
	}
	top, err := getTopLevelObject(kube, pod)
	if err != nil {
		log.Printf("Unable to get top level object for pod %s/%s due to %s", pod.GetNamespace(), pod.GetName(), err.Error())
		return DeriveHostnameFromPod(pod.GetName(), pod.GetNamespace(), useAkkerisHosts)
	}
	if useAkkerisHosts == true {
		appName, ok1 := top.GetAnnotations()[akkerisAppAnnotationKey]
		dynoType, ok2 := top.GetAnnotations()[akkerisDynoTypeAnnotationKey]
		if ok1 && ok2 {
			podId := strings.Join(parts[len(parts)-2:], "-")
			return &HostnameAndTag{
				Hostname: appName + "-" + pod.GetNamespace(),
				Tag: dynoType + "." + podId,
			}
		}
		return &HostnameAndTag{
			Hostname: top.GetName() + "-" + pod.GetNamespace(),
			Tag: akkerisGetTag(parts),
		}
	}
	return &HostnameAndTag{
		Hostname: top.GetName() + "." + pod.GetNamespace(),
		Tag: pod.GetName(),
	}
}

/*
 * TODO: This method is a way of deriving hostnames for pods by looking at the services
 * pointing at said pod, there may be a future use case for wanting to report the service
 * hostname in logs, but for now we'll use the current logic of finding the top-level objects name.

import (
	"k8s.io/apimachinery/pkg/labels"
)

func GetHostNameFromServicesGoingToAPod(kube kubernetes.Interface, pod *v1.Pod, useAkkerisHosts bool) []string {
	if annotation, ok := pod.GetAnnotations()[hostnameAnnotationKey]; ok {
		return []string{annotation}
	} else {
		serviceList, err := kube.CoreV1().Services(pod.GetNamespace()).List(api.ListOptions{})
		if err != nil {
			return []string{DeriveHostnameFromPod(pod.GetName(), pod.GetNamespace(), useAkkerisHosts).Hostname}
		}
		hosts := make([]string, 0)
		for _, service := range serviceList.Items {
			if labels.SelectorFromSet(labels.Set(service.Spec.Selector)).Matches(labels.Set(pod.GetLabels())) {
				if useAkkerisHosts {
					hosts = append(hosts, service.GetName() + "-" + service.GetNamespace())
				} else {
					hosts = append(hosts, service.GetName() + "." + service.GetNamespace())
				}
			}
		}
		if len(hosts) == 0 {
			return []string{DeriveHostnameFromPod(pod.GetName(), pod.GetNamespace(), useAkkerisHosts).Hostname}
		}
		return hosts
	}
}
*/

func (kds *KubernetesDataSource) AddRoute() chan LogRoute {
	return kds.add
}

func (kds *KubernetesDataSource) RemoveRoute() chan LogRoute {
	return kds.remove
}

func (kds *KubernetesDataSource) GetAllRoutes() ([]LogRoute, error) {
	return kds.routes, nil
}

func (kds *KubernetesDataSource) Close() error {
	kds.stop <- struct{}{}
	return nil
}

func (kds *KubernetesDataSource) addRouteFromPod(obj interface{}) {
	if pod, ok := obj.(*v1.Pod); ok {
		if annotation, ok := pod.GetAnnotations()[drainAnnotationKey]; ok {
			drains := strings.Split(annotation, ";")
			hostAndTag := GetHostNameAndTagFromPod(kds.kube, pod, kds.useAkkerisHosts)
			for _, drain := range drains {
				kds.add <- LogRoute{
					Endpoint: strings.TrimSpace(drain),
					Hostname: hostAndTag.Hostname,
				}
			}
		}
	}
}

func (kds *KubernetesDataSource) removeRouteFromPod(obj interface{}) {
	if pod, ok := obj.(*v1.Pod); ok {
		if annotation, ok := pod.GetAnnotations()[drainAnnotationKey]; ok {
			drains := strings.Split(annotation, ";")
			hostAndTag := GetHostNameAndTagFromPod(kds.kube, pod, kds.useAkkerisHosts)
			for _, drain := range drains {
				kds.remove <- LogRoute{
					Endpoint: strings.TrimSpace(drain),
					Hostname: hostAndTag.Hostname,
				}
			}
		}
	}
}

func CreateKubernetesDataSource(kube kubernetes.Interface) (*KubernetesDataSource, error) {
	useAkkeris := false
	if os.Getenv("AKKERIS") == "true" {
		useAkkeris = true
	}
	rest := kube.CoreV1().RESTClient()
	listWatch := cache.NewListWatchFromClient(rest, "pods", "", fields.Everything())
	listWatch.ListFunc = func(options api.ListOptions) (runtime.Object, error) {
		return kube.CoreV1().Pods(api.NamespaceAll).List(options)
	}
	listWatch.WatchFunc = func(options api.ListOptions) (watch.Interface, error) {
		return kube.CoreV1().Pods(api.NamespaceAll).Watch(api.ListOptions{})
	}
	stop := make(chan struct{}, 1)
	kds := KubernetesDataSource{
		useAkkerisHosts: useAkkeris,
		stop: stop,
		kube: kube,
		add: make(chan LogRoute, 1),
		remove: make(chan LogRoute, 1),
		routes: make([]LogRoute, 0),
	}
	_, controller := cache.NewInformer(
		listWatch, 
		&v1.Pod{},
		time.Second*0, 
		cache.ResourceEventHandlerFuncs{
			AddFunc: kds.addRouteFromPod,
			DeleteFunc: kds.removeRouteFromPod,
		},
	)
	go controller.Run(stop)
	return &kds, nil
}
