package storage

import (
	apps "k8s.io/api/apps/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"os"
	"strings"
	"time"
)

const AkkerisAppLabelKey = "akkeris.io/app-name"
const AkkerisDynoTypeLabelKey = "akkeris.io/dyno-type"
const DrainAnnotationKey = "logtrain.akkeris.io/drains"
const HostnameAnnotationKey = "logtrain.akkeris.io/hostname"
const TagAnnotationKey = "logtrain.akkeris.io/tag"

type KubernetesDataSource struct {
	useAkkerisHosts bool
	stop            chan struct{}
	kube            kubernetes.Interface
	add             chan LogRoute
	remove          chan LogRoute
	routes          []LogRoute
}

func GetHostNameFromTLO(kube kubernetes.Interface, obj meta.Object, useAkkerisHosts bool) string {
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
	if kobj, ok := obj.(meta.Object); ok {
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
	if kobj, ok := obj.(meta.Object); ok {
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

func (kds *KubernetesDataSource) reviewUpdateFromObj(oldObj interface{}, newObj interface{}) {
	if kOldObj, ok := oldObj.(meta.Object); ok {
		if kNewObj, ok := newObj.(meta.Object); ok {
			if annotationNew, ok := kNewObj.GetAnnotations()[DrainAnnotationKey]; ok && annotationNew != "" {
				if annotationOld, ok := kOldObj.GetAnnotations()[DrainAnnotationKey]; ok && annotationOld != "" {
					if annotationNew != annotationOld {
						// Drains updated
						oldDrains := strings.Split(annotationOld, ";")
						newDrains := strings.Split(annotationNew, ";")
						for _, dOld := range oldDrains {
							dOld = strings.TrimSpace(dOld)
							var found = false
							for _, dNew := range newDrains {
								dNew = strings.TrimSpace(dNew)
								if strings.ToLower(dOld) == strings.ToLower(dNew) {
									found = true
								}
							}
							if !found {
								// remove route as it was not found in the new set of routes
								kds.remove <- LogRoute{
									Endpoint: strings.TrimSpace(dOld),
									Hostname: GetHostNameFromTLO(kds.kube, kNewObj, kds.useAkkerisHosts),
								}
							}
						}
						for _, dNew := range newDrains {
							dNew = strings.TrimSpace(dNew)
							var found = false
							for _, dOld := range oldDrains {
								dOld = strings.TrimSpace(dOld)
								if strings.ToLower(dOld) == strings.ToLower(dNew) {
									found = true
								}
							}
							if !found {
								// add a new route as it wasn't found in the old set of routes
								kds.add <- LogRoute{
									Endpoint: dNew,
									Hostname: GetHostNameFromTLO(kds.kube, kNewObj, kds.useAkkerisHosts),
								}
							}
						}
					}
				} else {
					// New drain added
					kds.addRouteFromObj(newObj)
				}
			} else {
				if annon, ok := kOldObj.GetAnnotations()[DrainAnnotationKey]; ok && annon != "" {
					// Removed a drain
					kds.removeRouteFromObj(oldObj)
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
		stop:            make(chan struct{}, 1),
		kube:            kube,
		add:             make(chan LogRoute, 10),
		remove:          make(chan LogRoute, 10),
		routes:          make([]LogRoute, 0),
	}

	// TODO: Check permissions of service account before we run...

	// Do not watch replicasets. They're rarely directly used and are an order of magnitude
	// more resources to keep track of than deployments+statefulsets+daemonsets. Profiles caused
	// this to go from 100k

	// Watch deployments
	listWatchDeployments := cache.NewListWatchFromClient(rest, "deployments", "", fields.Everything())
	listWatchDeployments.ListFunc = func(options meta.ListOptions) (runtime.Object, error) {
		return kube.AppsV1().Deployments(meta.NamespaceAll).List(options)
	}
	listWatchDeployments.WatchFunc = func(options meta.ListOptions) (watch.Interface, error) {
		return kube.AppsV1().Deployments(meta.NamespaceAll).Watch(meta.ListOptions{})
	}
	_, controllerDeployments := cache.NewInformer(
		listWatchDeployments,
		&apps.Deployment{},
		time.Second*0,
		cache.ResourceEventHandlerFuncs{
			AddFunc:    kds.addRouteFromObj,
			DeleteFunc: kds.removeRouteFromObj,
			UpdateFunc: kds.reviewUpdateFromObj,
		},
	)
	go controllerDeployments.Run(kds.stop)

	// Watch daemonsets
	listWatchDaemonSets := cache.NewListWatchFromClient(rest, "daemonsets", "", fields.Everything())
	listWatchDaemonSets.ListFunc = func(options meta.ListOptions) (runtime.Object, error) {
		return kube.AppsV1().DaemonSets(meta.NamespaceAll).List(options)
	}
	listWatchDaemonSets.WatchFunc = func(options meta.ListOptions) (watch.Interface, error) {
		return kube.AppsV1().DaemonSets(meta.NamespaceAll).Watch(meta.ListOptions{})
	}
	_, controllerDaemonSets := cache.NewInformer(
		listWatchDaemonSets,
		&apps.DaemonSet{},
		time.Second*0,
		cache.ResourceEventHandlerFuncs{
			AddFunc:    kds.addRouteFromObj,
			DeleteFunc: kds.removeRouteFromObj,
			UpdateFunc: kds.reviewUpdateFromObj,
		},
	)
	go controllerDaemonSets.Run(kds.stop)

	// Watch statefulsets
	listWatchStatefulSets := cache.NewListWatchFromClient(rest, "statefulsets", "", fields.Everything())
	listWatchStatefulSets.ListFunc = func(options meta.ListOptions) (runtime.Object, error) {
		return kube.AppsV1().StatefulSets(meta.NamespaceAll).List(options)
	}
	listWatchStatefulSets.WatchFunc = func(options meta.ListOptions) (watch.Interface, error) {
		return kube.AppsV1().StatefulSets(meta.NamespaceAll).Watch(meta.ListOptions{})
	}
	_, controllerStatefulSets := cache.NewInformer(
		listWatchStatefulSets,
		&apps.StatefulSet{},
		time.Second*0,
		cache.ResourceEventHandlerFuncs{
			AddFunc:    kds.addRouteFromObj,
			DeleteFunc: kds.removeRouteFromObj,
			UpdateFunc: kds.reviewUpdateFromObj,
		},
	)
	go controllerStatefulSets.Run(kds.stop)

	return &kds, nil
}
