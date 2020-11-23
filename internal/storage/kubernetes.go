package storage

import (
	"errors"
	"github.com/akkeris/logtrain/internal/debug"
	apps "k8s.io/api/apps/v1"
	authorization "k8s.io/api/authorization/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"strings"
	"time"
)

const AkkerisAppLabelKey = "akkeris.io/app-name"
const AkkerisDynoTypeLabelKey = "akkeris.io/dyno-type"
const DrainAnnotationKey = "logtrain.akkeris.io/drains"
const HostnameAnnotationKey = "logtrain.akkeris.io/hostname"
const TagAnnotationKey = "logtrain.akkeris.io/tag"

// KubernetesDataSource uses kubernetes as a datasource for routes by listening to annotations
type KubernetesDataSource struct {
	useAkkerisHosts bool
	stop            chan struct{}
	kube            kubernetes.Interface
	add             chan LogRoute
	remove          chan LogRoute
	routes          []LogRoute
	closed          bool
	writable        bool
}

// GetKubernetesClient returns a new kubernetes client by testing the in cluster config or checking the file path
func GetKubernetesClient(kubeConfigPath string) (kubernetes.Interface, error) {
	var clientConfig *rest.Config
	var err error
	if kubeConfigPath == "" {
		clientConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	} else {
		config, err := clientcmd.LoadFromFile(kubeConfigPath)
		if err != nil {
			return nil, err
		}

		clientConfig, err = clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{}).ClientConfig()
		if err != nil {
			return nil, err
		}
	}
	return kubernetes.NewForConfig(clientConfig)
}

func kubeObjectFromHost(hostName string, useAkkerisHosts bool) (string, string) {
	if useAkkerisHosts {
		parts := strings.Split(hostName, "-")
		return parts[0], strings.Join(parts[1:], "-")
	}
	parts := strings.Split(hostName, ".")
	return parts[0], parts[1]
}

// GetHostNameFromTLO derives a hostname from an object in kubernetes
func GetHostNameFromTLO(kube kubernetes.Interface, obj meta.Object, useAkkerisHosts bool) string {
	if host, ok := obj.GetAnnotations()[HostnameAnnotationKey]; ok {
		return host
	}
	if useAkkerisHosts == true {
		return obj.GetName() + "-" + obj.GetNamespace()
	}
	return obj.GetName() + "." + obj.GetNamespace()
}

// AddRoute returns a channel where new routes are published to
func (kds *KubernetesDataSource) AddRoute() chan LogRoute {
	return kds.add
}

// RemoveRoute returns a channel where route removals are published
func (kds *KubernetesDataSource) RemoveRoute() chan LogRoute {
	return kds.remove
}

// GetAllRoutes returns all routes the datasource is aware of
func (kds *KubernetesDataSource) GetAllRoutes() ([]LogRoute, error) {
	return kds.routes, nil
}

// EmitNewRoute always returns an error as this datasource is not currently writable.
func (kds *KubernetesDataSource) EmitNewRoute(route LogRoute) error {
	if kds.writable == false {
		return errors.New("cannot write to this datasource")
	}
	name, namespace := kubeObjectFromHost(route.Hostname, kds.useAkkerisHosts)

	deployment, err := kds.kube.AppsV1().Deployments(namespace).Get(name, meta.GetOptions{})
	if err == nil {
		annotations := deployment.GetAnnotations()
		if annotations[DrainAnnotationKey] == "" {
			annotations[DrainAnnotationKey] = route.Endpoint
		} else {
			annotations[DrainAnnotationKey] = annotations[DrainAnnotationKey] + ";" + route.Endpoint
		}
		deployment.SetAnnotations(annotations)
		if _, err = kds.kube.AppsV1().Deployments(namespace).Update(deployment); err != nil {
			return err
		}
		return nil
	}

	daemonset, err := kds.kube.AppsV1().DaemonSets(namespace).Get(name, meta.GetOptions{})
	if err == nil {
		annotations := daemonset.GetAnnotations()
		if annotations[DrainAnnotationKey] == "" {
			annotations[DrainAnnotationKey] = route.Endpoint
		} else {
			annotations[DrainAnnotationKey] = annotations[DrainAnnotationKey] + ";" + route.Endpoint
		}
		daemonset.SetAnnotations(annotations)
		if _, err = kds.kube.AppsV1().DaemonSets(namespace).Update(daemonset); err != nil {
			return err
		}
		return nil
	}

	statefulset, err := kds.kube.AppsV1().StatefulSets(namespace).Get(name, meta.GetOptions{})
	if err == nil {
		annotations := statefulset.GetAnnotations()
		if annotations[DrainAnnotationKey] == "" {
			annotations[DrainAnnotationKey] = route.Endpoint
		} else {
			annotations[DrainAnnotationKey] = annotations[DrainAnnotationKey] + ";" + route.Endpoint
		}
		statefulset.SetAnnotations(annotations)
		if _, err = kds.kube.AppsV1().StatefulSets(namespace).Update(statefulset); err != nil {
			return err
		}
		return nil
	}
	return err
}

// EmitRemoveRoute always returns an error as this datasource is not currently writable.
func (kds *KubernetesDataSource) EmitRemoveRoute(route LogRoute) error {
	if kds.writable == false {
		return errors.New("cannot write to this datasource")
	}
	name, namespace := kubeObjectFromHost(route.Hostname, kds.useAkkerisHosts)

	deployment, err := kds.kube.AppsV1().Deployments(namespace).Get(name, meta.GetOptions{})
	if err == nil {
		annotations := deployment.GetAnnotations()
		drains := strings.Split(annotations[DrainAnnotationKey], ";")
		newdrs := make([]string, 0)
		for _, drain := range drains {
			if strings.TrimSpace(strings.ToLower(route.Endpoint)) != strings.TrimSpace(strings.ToLower(drain)) {
				newdrs = newdrs
			}
		}
		annotations[DrainAnnotationKey] = strings.Join(newdrs, ";")
		deployment.SetAnnotations(annotations)
		if _, err = kds.kube.AppsV1().Deployments(namespace).Update(deployment); err != nil {
			return err
		}
		return nil
	}

	daemonset, err := kds.kube.AppsV1().DaemonSets(namespace).Get(name, meta.GetOptions{})
	if err == nil {
		annotations := daemonset.GetAnnotations()
		drains := strings.Split(annotations[DrainAnnotationKey], ";")
		newdrs := make([]string, 0)
		for _, drain := range drains {
			if strings.TrimSpace(strings.ToLower(route.Endpoint)) != strings.TrimSpace(strings.ToLower(drain)) {
				newdrs = newdrs
			}
		}
		annotations[DrainAnnotationKey] = strings.Join(newdrs, ";")
		daemonset.SetAnnotations(annotations)
		if _, err = kds.kube.AppsV1().DaemonSets(namespace).Update(daemonset); err != nil {
			return err
		}
		return nil
	}

	statefulset, err := kds.kube.AppsV1().StatefulSets(namespace).Get(name, meta.GetOptions{})
	if err == nil {
		annotations := statefulset.GetAnnotations()
		drains := strings.Split(annotations[DrainAnnotationKey], ";")
		newdrs := make([]string, 0)
		for _, drain := range drains {
			if strings.TrimSpace(strings.ToLower(route.Endpoint)) != strings.TrimSpace(strings.ToLower(drain)) {
				newdrs = newdrs
			}
		}
		annotations[DrainAnnotationKey] = strings.Join(newdrs, ";")
		statefulset.SetAnnotations(annotations)
		if _, err = kds.kube.AppsV1().StatefulSets(namespace).Update(statefulset); err != nil {
			return err
		}
		return nil
	}
	return err
}

// Writable returns false always as this datasource is not writable.
func (kds *KubernetesDataSource) Writable() bool {
	return kds.writable
}

// Close closes the data sources
func (kds *KubernetesDataSource) Close() error {
	if kds.closed {
		return errors.New("this datasource is already closed")
	}
	kds.closed = true
	kds.stop <- struct{}{}
	close(kds.add)
	close(kds.remove)
	return nil
}

func (kds *KubernetesDataSource) addRoute(host string, drain string) {
	debug.Debugf("[kubernetes] adding route %s->%s\n", host, drain)
	route := LogRoute{
		Endpoint: strings.TrimSpace(drain),
		Hostname: host,
	}
	kds.routes = append(kds.routes, route)
	kds.add <- route
}

func (kds *KubernetesDataSource) removeRoute(host string, drain string) {
	debug.Debugf("[kubernetes] removing route %s->%s\n", host, drain)
	route := LogRoute{
		Endpoint: strings.TrimSpace(drain),
		Hostname: host,
	}
	newRoutes := make([]LogRoute, 0)
	for _, r := range kds.routes {
		if route.Endpoint != r.Endpoint && route.Hostname != r.Hostname {
			newRoutes = append(newRoutes, r)
		}
	}
	kds.routes = newRoutes
	kds.remove <- route
}

func (kds *KubernetesDataSource) addRouteFromObj(obj interface{}) {
	if kobj, ok := obj.(meta.Object); ok {
		if annotation, ok := kobj.GetAnnotations()[DrainAnnotationKey]; ok {
			drains := strings.Split(annotation, ";")
			host := GetHostNameFromTLO(kds.kube, kobj, kds.useAkkerisHosts)
			for _, drain := range drains {
				kds.addRoute(host, drain)
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
				kds.removeRoute(host, drain)
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
								host := GetHostNameFromTLO(kds.kube, kNewObj, kds.useAkkerisHosts)
								drain := strings.TrimSpace(dOld)
								kds.removeRoute(host, drain)
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
								host := GetHostNameFromTLO(kds.kube, kNewObj, kds.useAkkerisHosts)
								kds.addRoute(host, dNew)
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

func hasAccessTo(kube kubernetes.Interface, verb, group, resource string) bool {
	policy := authorization.SelfSubjectAccessReview{
		Spec: authorization.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorization.ResourceAttributes{
				Namespace: "*",
				Verb:      verb,
				Group:     group,
				Version:   "*",
				Resource:  resource,
				Name:      "",
			},
		},
	}
	result, err := kube.AuthorizationV1().SelfSubjectAccessReviews().Create(&policy)
	if err != nil {
		debug.Errorf("[kubernetes/datasource]: unable to do a policy review of %s %s %s: %s", verb, group, resource, err.Error())
		return false
	}
	return result.Status.Allowed
}

// CreateKubernetesDataSource creates a new kubernetes data source from a kube client
func CreateKubernetesDataSource(kube kubernetes.Interface, checkPermissions bool) (*KubernetesDataSource, error) {
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
		closed:          false,
		writable:        false,
	}

	if checkPermissions && (
		!hasAccessTo(kube, "get", "", "deployments") ||
		!hasAccessTo(kube, "get", "", "statefulsets") ||
		!hasAccessTo(kube, "get", "", "daemonset")) {
		return nil, errors.New("kubernetes cannot be used as data source, no permissions to get depoyments, statefulsets, and deamonsets")
	}
	if checkPermissions && (
		!hasAccessTo(kube, "list", "", "deployments") ||
		!hasAccessTo(kube, "list", "", "statefulsets") ||
		!hasAccessTo(kube, "list", "", "daemonset")) {
		return nil, errors.New("kubernetes cannot be used as data source, no permissions to list depoyments, statefulsets, and deamonsets")
	}

	if hasAccessTo(kube, "update", "", "deployments") &&
		hasAccessTo(kube, "update", "", "statefulsets") &&
		hasAccessTo(kube, "update", "", "daemonset") {
		kds.writable = true
	}

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
	deployments, err := kube.AppsV1().Deployments(meta.NamespaceAll).List(meta.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, item := range deployments.Items {
		kds.addRouteFromObj(item)
	}

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
	daemonsets, err := kube.AppsV1().DaemonSets(meta.NamespaceAll).List(meta.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, item := range daemonsets.Items {
		kds.addRouteFromObj(item)
	}

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
	statefulsets, err := kube.AppsV1().StatefulSets(meta.NamespaceAll).List(meta.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, item := range statefulsets.Items {
		kds.addRouteFromObj(item)
	}

	go controllerDeployments.Run(kds.stop)
	go controllerDaemonSets.Run(kds.stop)
	go controllerStatefulSets.Run(kds.stop)

	return &kds, nil
}
