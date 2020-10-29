package storage

import (
	"log"
	"errors"
	"os"
	"strings"
	"time"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	api "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const akkerisAppAnnotationKey = "akkeris.io/app-name"
const akkerisDynoTypeAnnotationKey = "akkeris.io/dyno-type"
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
			rObj, err := kube.CoreV1().RESTClient().Get().Resource(ref.Kind + "/" + obj.GetNamespace() + "/" + ref.Name).Do().Get()
			if err != nil {
				return nil, err
			}
			// React to the first owner we find, unsure if its possible to have more than one
			// for a pod path.
			if newObj, ok := rObj.(api.Object); ok {
				return newObj, nil
			} else {
				return nil, errors.New("Cannot cast object returned as owner to objectmeta")
			}
		}
	}
	return obj, nil
}


func DeriveHostnameFromPod(podName string, podNamespace string, useAkkerisHosts bool) *HostnameAndTag {
	parts := strings.Split(podName, "-")
	if useAkkerisHosts {
		podId := strings.Join(parts[len(parts)-2:], "-")
		appAndDyno := strings.SplitN(strings.Join(parts[:len(parts)-2], "-"), "--", 1)
		if appAndDyno[1] == "" {
			appAndDyno[1] = "web"
		}
		return &HostnameAndTag{
			Hostname: appAndDyno[0] + "-" + podNamespace,
			Tag: appAndDyno[1] + "." + podId,
		}
	}
	return &HostnameAndTag{
		Hostname: strings.Join(parts[:len(parts)-2], "-") + "." + podNamespace,
		Tag: strings.Join(parts[len(parts)-2:], "-"),
	}
}

func GetHostNameAndTagFromPod(kube kubernetes.Interface, pod *v1.Pod, useAkkerisHosts bool) *HostnameAndTag {
	if host, ok := pod.GetAnnotations()[hostnameAnnotationKey]; ok {
		if tag, ok := pod.GetAnnotations()[tagAnnotationKey]; ok {
			return &HostnameAndTag{
				Hostname: host,
				Tag: tag,
			}
		} else {
			return &HostnameAndTag{
				Hostname: host,
				Tag: "",
			}
		}
	}
	top, err := getTopLevelObject(kube, pod)
	if err != nil {
		log.Printf("Unable to get top level object for pod %#+v due to %s", pod, err.Error())
		return DeriveHostnameFromPod(pod.GetName(), pod.GetNamespace(), useAkkerisHosts)
	}
	if useAkkerisHosts == true {
		if appName, ok := top.GetAnnotations()[akkerisAppAnnotationKey]; ok {
			if dynoType, ok := top.GetAnnotations()[akkerisDynoTypeAnnotationKey]; ok {
				tag := ""
				if dynoType == "web" {
					tag = strings.ReplaceAll(pod.GetName(), appName + "-", "")
				} else {
					tag = strings.ReplaceAll(pod.GetName(), appName + "--" + dynoType + "-", "")
				}
				return &HostnameAndTag{
					Hostname: appName + "-" + pod.GetNamespace(),
					Tag: dynoType + "." + tag,
				}
			}
		}
		return DeriveHostnameFromPod(pod.GetName(), pod.GetNamespace(), useAkkerisHosts)
	}
	return &HostnameAndTag{
		Hostname: top.GetName() + "." + pod.GetNamespace(),
		Tag: pod.GetName(),
	}
}

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
		return rest.Get().Resource("pods").Do().Get()
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
