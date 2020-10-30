package storage

import (
	"os"
	"strings"
	"time"
	apps "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	api "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const AkkerisAppLabelKey = "akkeris.io/app-name"
const AkkerisDynoTypeLabelKey = "akkeris.io/dyno-type"
const DrainAnnotationKey = "logtrain.akkeris.io/drains"
const HostnameAnnotationKey = "logtrain.akkeris.io/hostname"
const TagAnnotationKey = "logtrain.akkeris.io/tag"

type KubernetesDataSource struct {
	useAkkerisHosts bool
	stop chan struct{}
	kube kubernetes.Interface
	add chan LogRoute
	remove chan LogRoute
	routes []LogRoute
}

func GetHostNameFromTLO(kube kubernetes.Interface, obj api.Object, useAkkerisHosts bool) string {
	if host, ok := obj.GetAnnotations()[HostnameAnnotationKey]; ok {
		return host
	}
	if useAkkerisHosts == true {
		return obj.GetName() + "-" + obj.GetNamespace()
	}
	return obj.GetName() + "." + obj.GetNamespace()
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

func (kds *KubernetesDataSource) addRouteFromObj(obj interface{}) {
	if kobj, ok := obj.(api.Object); ok {
		if annotation, ok := kobj.GetAnnotations()[DrainAnnotationKey]; ok {
			drains := strings.Split(annotation, ";")
			host := GetHostNameFromTLO(kds.kube, kobj, kds.useAkkerisHosts)
			for _, drain := range drains {
				kds.add <- LogRoute{
					Endpoint: strings.TrimSpace(drain),
					Hostname: host,
				}
			}
		}
	}
}

func (kds *KubernetesDataSource) removeRouteFromObj(obj interface{}) {
	if kobj, ok := obj.(api.Object); ok {
		if annotation, ok := kobj.GetAnnotations()[DrainAnnotationKey]; ok {
			drains := strings.Split(annotation, ";")
			host := GetHostNameFromTLO(kds.kube, kobj, kds.useAkkerisHosts)
			for _, drain := range drains {
				kds.remove <- LogRoute{
					Endpoint: strings.TrimSpace(drain),
					Hostname: host,
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
	kds := KubernetesDataSource{
		useAkkerisHosts: useAkkeris,
		stop: make(chan struct{}, 1),
		kube: kube,
		add: make(chan LogRoute, 1),
		remove: make(chan LogRoute, 1),
		routes: make([]LogRoute, 0),
	}

	// Watch replicasets
	listWatchReplicaSets := cache.NewListWatchFromClient(rest, "replicasets", "", fields.Everything())
	listWatchReplicaSets.ListFunc = func(options api.ListOptions) (runtime.Object, error) {
		return kube.AppsV1().ReplicaSets(api.NamespaceAll).List(options)
	}
	listWatchReplicaSets.WatchFunc = func(options api.ListOptions) (watch.Interface, error) {
		return kube.AppsV1().ReplicaSets(api.NamespaceAll).Watch(api.ListOptions{})
	}
	_, controllerReplicaSets := cache.NewInformer(
		listWatchReplicaSets, 
		&apps.ReplicaSet{},
		time.Second*0, 
		cache.ResourceEventHandlerFuncs{
			AddFunc: kds.addRouteFromObj,
			DeleteFunc: kds.removeRouteFromObj,
		},
	)
	go controllerReplicaSets.Run(kds.stop)


	// Watch deployments
	listWatchDeployments := cache.NewListWatchFromClient(rest, "deployments", "", fields.Everything())
	listWatchDeployments.ListFunc = func(options api.ListOptions) (runtime.Object, error) {
		return kube.AppsV1().Deployments(api.NamespaceAll).List(options)
	}
	listWatchDeployments.WatchFunc = func(options api.ListOptions) (watch.Interface, error) {
		return kube.AppsV1().Deployments(api.NamespaceAll).Watch(api.ListOptions{})
	}
	_, controllerDeployments := cache.NewInformer(
		listWatchDeployments, 
		&apps.Deployment{},
		time.Second*0, 
		cache.ResourceEventHandlerFuncs{
			AddFunc: kds.addRouteFromObj,
			DeleteFunc: kds.removeRouteFromObj,
		},
	)
	go controllerDeployments.Run(kds.stop)


	// Watch daemonsets
	listWatchDaemonSets := cache.NewListWatchFromClient(rest, "daemonsets", "", fields.Everything())
	listWatchDaemonSets.ListFunc = func(options api.ListOptions) (runtime.Object, error) {
		return kube.AppsV1().DaemonSets(api.NamespaceAll).List(options)
	}
	listWatchDaemonSets.WatchFunc = func(options api.ListOptions) (watch.Interface, error) {
		return kube.AppsV1().DaemonSets(api.NamespaceAll).Watch(api.ListOptions{})
	}
	_, controllerDaemonSets := cache.NewInformer(
		listWatchDaemonSets, 
		&apps.DaemonSet{},
		time.Second*0, 
		cache.ResourceEventHandlerFuncs{
			AddFunc: kds.addRouteFromObj,
			DeleteFunc: kds.removeRouteFromObj,
		},
	)
	go controllerDaemonSets.Run(kds.stop)


	// Watch statefulsets
	listWatchStatefulSets := cache.NewListWatchFromClient(rest, "statefulsets", "", fields.Everything())
	listWatchStatefulSets.ListFunc = func(options api.ListOptions) (runtime.Object, error) {
		return kube.AppsV1().StatefulSets(api.NamespaceAll).List(options)
	}
	listWatchStatefulSets.WatchFunc = func(options api.ListOptions) (watch.Interface, error) {
		return kube.AppsV1().StatefulSets(api.NamespaceAll).Watch(api.ListOptions{})
	}
	_, controllerStatefulSets := cache.NewInformer(
		listWatchStatefulSets, 
		&apps.StatefulSet{},
		time.Second*0, 
		cache.ResourceEventHandlerFuncs{
			AddFunc: kds.addRouteFromObj,
			DeleteFunc: kds.removeRouteFromObj,
		},
	)
	go controllerStatefulSets.Run(kds.stop)


	return &kds, nil
}
