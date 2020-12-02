package kubernetes

import (
	"encoding/json"
	"errors"
	"github.com/akkeris/logtrain/internal/debug"
	"github.com/akkeris/logtrain/internal/storage"
	"github.com/fsnotify/fsnotify"
	"github.com/json-iterator/go"
	"github.com/influxdata/tail"
	"github.com/trevorlinton/remote_syslog2/syslog"
	"github.com/valyala/fastjson"
	"io"
	api "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const kubeTime = "2006-01-02T15:04:05.000000000Z"

type kubeLine struct {
	Log    string `json:"log"`
	Stream string `json:"stream"`
	Time   string `json:"time"`
}

type kubeDetails struct {
	Container string
	DockerId  string
	Namespace string
	Pod       string
}

type fileWatcher struct {
	errors   uint32
	follower *tail.Tail
	hostname string
	stop     chan struct{}
	tag      string
}

type hostnameAndTag struct {
	Hostname string
	Tag      string
}

type Kubernetes struct {
	kube           kubernetes.Interface
	closing        bool
	errors         chan error
	followers      map[string]fileWatcher
	followersMutex sync.Mutex
	packets        chan syslog.Packet
	path           string
	watcher        *fsnotify.Watcher
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

func deriveHostnameFromPod(podName string, podNamespace string, useAkkerisHosts bool) *hostnameAndTag {
	parts := strings.Split(podName, "-")
	if useAkkerisHosts {
		podId := strings.Join(parts[len(parts)-2:], "-")
		appAndDyno := strings.SplitN(strings.Join(parts[:len(parts)-2], "-"), "--", 2)
		if len(appAndDyno) < 2 {
			appAndDyno = append(appAndDyno, "web")
		}
		return &hostnameAndTag{
			Hostname: appAndDyno[0] + "-" + podNamespace,
			Tag:      appAndDyno[1] + "." + podId,
		}
	}
	return &hostnameAndTag{
		Hostname: strings.Join(parts[:len(parts)-2], "-") + "." + podNamespace,
		Tag:      podName,
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

func getHostnameAndTagFromPod(kube kubernetes.Interface, obj api.Object, useAkkerisHosts bool) *hostnameAndTag {
	parts := strings.Split(obj.GetName(), "-")
	top, err := getTopLevelObject(kube, obj)
	if err != nil {
		debug.Errorf("[kubernetes/input]: Unable to get top level object for obj %s/%s/%s due to %s", obj.GetResourceVersion(), obj.GetNamespace(), obj.GetName(), err.Error())
		return deriveHostnameFromPod(obj.GetName(), obj.GetNamespace(), useAkkerisHosts)
	}
	if host, ok := top.GetAnnotations()[storage.HostnameAnnotationKey]; ok {
		if tag, ok := top.GetAnnotations()[storage.TagAnnotationKey]; ok {
			return &hostnameAndTag{
				Hostname: host,
				Tag:      tag,
			}
		} else {
			if useAkkerisHosts == true {
				return &hostnameAndTag{
					Hostname: host,
					Tag:      akkerisGetTag(parts),
				}
			} else {
				return &hostnameAndTag{
					Hostname: host,
					Tag:      top.GetName(),
				}
			}
		}
	}
	if useAkkerisHosts == true {
		appName, ok1 := top.GetLabels()[storage.AkkerisAppLabelKey]
		dynoType, ok2 := top.GetLabels()[storage.AkkerisDynoTypeLabelKey]
		if ok1 && ok2 {
			podId := strings.Join(parts[len(parts)-2:], "-")
			return &hostnameAndTag{
				Hostname: appName + "-" + obj.GetNamespace(),
				Tag:      dynoType + "." + podId,
			}
		}
		return &hostnameAndTag{
			Hostname: top.GetName() + "-" + obj.GetNamespace(),
			Tag:      akkerisGetTag(parts),
		}
	}
	return &hostnameAndTag{
		Hostname: top.GetName() + "." + obj.GetNamespace(),
		Tag:      obj.GetName(),
	}
}

func dir(root string) []string {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && info.IsDir() == false {
			files = append(files, path)
		} else if err == nil && info != nil && info.IsDir() == true {
			if path != root {
				return filepath.SkipDir
			}
		}
		if err != nil {
			debug.Errorf("[kubernetes/input]: Error while listing files: %s\n", err.Error())
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	return files
}

func (handler *Kubernetes) Close() error {
	debug.Infof("[kubernetes/input]: Close was called\n")
	handler.closing = true
	handler.watcher.Close()
	for _, v := range handler.followers {
		select {
		case v.stop <- struct{}{}:
		default:
		}
	}
	close(handler.packets)
	close(handler.errors)
	debug.Infof("[kubernetes/input]: Closed\n")
	return nil
}

func (handler *Kubernetes) Dial() error {
	if handler.watcher != nil {
		return errors.New("Dial may only be called once.")
	}
	for _, file := range dir(handler.path) {
		/* Seek the end of the file if we've just started,
		 * if say we're erroring and restarting frequently we
		 * do not want to start from the beginning of the log file
		 * and rebroadcast the entire log contents
		 */
		if err := handler.add(file, io.SeekEnd); err != nil {
			debug.Errorf("[kubernetes/input]: Error watching file: %s, due to: %s\n", file, err.Error())
		}
	}
	watcher, err := handler.watcherEventLoop()
	if err != nil {
		debug.Errorf("[kubernetes/input]: Error starting watcher event loop: %s\n", err.Error())
		return err
	}
	handler.watcher = watcher
	return nil
}

func (handler *Kubernetes) Errors() chan error {
	return handler.errors
}

func (handler *Kubernetes) Packets() chan syslog.Packet {
	return handler.packets
}

func (handler *Kubernetes) Pools() bool {
	return true
}

func kubeDetailsFromFileName(file string) (*kubeDetails, error) {
	re := regexp.MustCompile(`(?P<pod_name>([a-z0-9][-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*)_(?P<namespace>[^_]+)_(?P<container_name>.+)-(?P<docker_id>[a-z0-9]{64})\.log$`)
	file = filepath.Base(file)
	if re.MatchString(file) {
		parts := re.FindAllStringSubmatch(file, -1)
		if len(parts[0]) != 8 {
			return nil, errors.New("invalid filename, too many parts")
		}
		return &kubeDetails{
			Container: parts[0][6],
			DockerId:  parts[0][7],
			Namespace: parts[0][5],
			Pod:       parts[0][1],
		}, nil
	}
	return nil, errors.New("invalid filename, no match given")
}

func (handler *Kubernetes) parseWithStandardJson(file string, fw *fileWatcher, hostAndTag *hostnameAndTag) {
	debug.Infof("[kubernetes/input] Watching (standard parser): %s (%s/%s)\n", file, hostAndTag.Hostname, hostAndTag.Tag)
	for {
		// TODO: investigate if this is better served by using a range instead of select such as in:
		// https://github.com/trevorlinton/go-tail/blob/master/main.go#L53
		select {
		case line, ok := <-fw.follower.Lines:
			if ok && line.Err == nil {
				var data kubeLine
				if err := json.Unmarshal([]byte(line.Text), &data); err != nil {
					// track errors with the lines, but don't report anything.
					// TODO: should we do more? we shouldnt report this on
					// the kubernetes error handler as one corrupted file could make
					// the entire input handler look broken. Brainstorm on this.
					fw.errors++
				} else {
					t, err := time.Parse(kubeTime, data.Time)
					if err != nil {
						t = time.Now()
					}
					handler.Packets() <- syslog.Packet{
						Severity: 0,
						Facility: 0,
						Time:     t,
						Hostname: fw.hostname,
						Tag:      fw.tag,
						Message:  data.Log,
					}
				}
			} else if ok && line.Err != nil {
				debug.Errorf("[kubernetes/input]: Error following file %s: %s", file, line.Err.Error())
				// track errors with the lines, but don't report anything.
				// TODO: should we do more? we shouldnt report this on
				// the kubernetes error handler as one corrupted file could make
				// the entire input handler look broken. Brainstorm on this.
				fw.errors++
			} else if !ok {
				debug.Debugf("[kubernetes/input]: Watcher was closed on  %s (%s/%s)\n", file, hostAndTag.Hostname, hostAndTag.Tag)
				return
			}
		case <-fw.stop:
			fw.follower.Stop()
			fw.follower.Cleanup()
			debug.Infof("[kubernetes/input]: Received message to stop watcher for %s.", file)
			return
		}
	}
}

func (handler *Kubernetes) parseWithJsonIterator(file string, fw *fileWatcher, hostAndTag *hostnameAndTag) {
	debug.Infof("[kubernetes/input] Watching (json-iterator parser): %s (%s/%s)\n", file, hostAndTag.Hostname, hostAndTag.Tag)
	var json = jsoniter.ConfigCompatibleWithStandardLibrary
	for {
		// TODO: investigate if this is better served by using a range instead of select such as in:
		// https://github.com/trevorlinton/go-tail/blob/master/main.go#L53
		select {
		case line, ok := <-fw.follower.Lines:
			if ok && line.Err == nil {
				var data kubeLine
				if err := json.Unmarshal([]byte(line.Text), &data); err != nil {
					// track errors with the lines, but don't report anything.
					// TODO: should we do more? we shouldnt report this on
					// the kubernetes error handler as one corrupted file could make
					// the entire input handler look broken. Brainstorm on this.
					fw.errors++
				} else {
					t, err := time.Parse(kubeTime, data.Time)
					if err != nil {
						t = time.Now()
					}
					handler.Packets() <- syslog.Packet{
						Severity: 0,
						Facility: 0,
						Time:     t,
						Hostname: fw.hostname,
						Tag:      fw.tag,
						Message:  data.Log,
					}
				}
			} else if ok && line.Err != nil {
				debug.Errorf("[kubernetes/input]: Error following file %s: %s", file, line.Err.Error())
				// track errors with the lines, but don't report anything.
				// TODO: should we do more? we shouldnt report this on
				// the kubernetes error handler as one corrupted file could make
				// the entire input handler look broken. Brainstorm on this.
				fw.errors++
			} else if !ok {
				debug.Debugf("[kubernetes/input]: Watcher was closed on  %s (%s/%s)\n", file, hostAndTag.Hostname, hostAndTag.Tag)
				return
			}
		case <-fw.stop:
			fw.follower.Stop()
			fw.follower.Cleanup()
			debug.Infof("[kubernetes/input]: Received message to stop watcher for %s.", file)
			return
		}
	}
}

func (handler *Kubernetes) parseWithFastJson(file string, fw *fileWatcher, hostAndTag *hostnameAndTag) {
	debug.Infof("[kubernetes/input] Watching (fastjson parser): %s (%s/%s)\n", file, hostAndTag.Hostname, hostAndTag.Tag)
	var parser fastjson.Parser
	for {
		// TODO: investigate if this is better served by using a range instead of select such as in:
		// https://github.com/trevorlinton/go-tail/blob/master/main.go#L53
		select {
		case line, ok := <-fw.follower.Lines:
			if ok && line.Err == nil {
				v, err := parser.Parse(line.Text)
				if err != nil {
					// track errors with the lines, but don't report anything.
					// TODO: should we do more? we shouldnt report this on
					// the kubernetes error handler as one corrupted file could make
					// the entire input handler look broken. Brainstorm on this.
					fw.errors++
				} else {
					t, err := time.Parse(kubeTime, string(v.GetStringBytes("time")))
					if err != nil {
						t = time.Now()
					}
					handler.Packets() <- syslog.Packet{
						Severity: 0,
						Facility: 0,
						Time:     t,
						Hostname: fw.hostname,
						Tag:      fw.tag,
						Message:  string(v.GetStringBytes("log")),
					}
				}
			} else if ok && line.Err != nil {
				debug.Errorf("[kubernetes/input]: Error following file %s: %s", file, line.Err.Error())
				// track errors with the lines, but don't report anything.
				// TODO: should we do more? we shouldnt report this on
				// the kubernetes error handler as one corrupted file could make
				// the entire input handler look broken. Brainstorm on this.
				fw.errors++
			} else if !ok {
				debug.Debugf("[kubernetes/input]: Watcher was closed on  %s (%s/%s)\n", file, hostAndTag.Hostname, hostAndTag.Tag)
				return
			}
		case <-fw.stop:
			fw.follower.Stop()
			fw.follower.Cleanup()
			debug.Infof("[kubernetes/input]: Received message to stop watcher for %s.", file)
			return
		}
	}
}

func (handler *Kubernetes) add(file string, ioSeek int) error {
	// The filename has additional details we need.
	details, err := kubeDetailsFromFileName(file)
	if err != nil {
		return err
	}

	config := tail.Config{
		Follow: true,
		Location: &tail.SeekInfo{
			Offset: 0,
			Whence: ioSeek,
		},
		ReOpen: true,
		Logger: debug.LoggerDebug,
	}

	useAkkerisHosts := os.Getenv("AKKERIS") == "true"
	hostAndTag := deriveHostnameFromPod(details.Pod, details.Namespace, useAkkerisHosts)
	pod, err := handler.kube.CoreV1().Pods(details.Namespace).Get(details.Pod, api.GetOptions{})
	if err != nil {
		debug.Errorf("Unable to get pod details from kubernetes for pod %#+v due to %s\n", details, err.Error())
	} else {
		hostAndTag = getHostnameAndTagFromPod(handler.kube, pod, useAkkerisHosts)
	}
	proc, err := tail.TailFile(file, config)
	if err != nil {
		handler.Errors() <- err
		return err
	}
	fw := fileWatcher{
		follower: proc,
		stop:     make(chan struct{}, 1),
		hostname: hostAndTag.Hostname,
		tag:      hostAndTag.Tag,
		errors:   0,
	}
	handler.followersMutex.Lock()
	handler.followers[file] = fw
	handler.followersMutex.Unlock()
	if os.Getenv("JSON_PARSER") == "fast" {
		go handler.parseWithFastJson(file, &fw, hostAndTag)
	} else if os.Getenv("JSON_PARSER") == "iterator" {
		go handler.parseWithJsonIterator(file, &fw, hostAndTag)
	} else {
		go handler.parseWithStandardJson(file, &fw, hostAndTag)
	}
	return nil
}

func (handler *Kubernetes) watcherEventLoop() (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		debug.Errorf("[kubernetes/input] Cannot create file watcher for [%s]: %s\n", handler.path, err.Error())
		return nil, err
	}

	if err := watcher.Add(handler.path); err != nil {
		debug.Errorf("[kubernetes/input] Cannot add [%s] path to file watcher: %s\n", handler.path, err.Error())
		return nil, err
	}
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					debug.Errorf("[kubernetes/input] the watcher event channel was closed for %s\n", handler.path)
					panic("watcher event channel was closed")
				}
				if event.Op&fsnotify.Create == fsnotify.Create {
					debug.Debugf("[kubernetes/input] Watcher loop saw a new create event: %s\n", event.Name)
					go handler.add(event.Name, io.SeekStart)
				} else if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Chmod == fsnotify.Chmod || event.Op&fsnotify.Rename == fsnotify.Rename {
					if _, ok := handler.followers[event.Name]; !ok {
						debug.Debugf("[kubernetes/input] Watcher loop saw a write/chmod/rename event for a new file: %s\n", event.Name)
						go handler.add(event.Name, io.SeekStart)
					}
				} else if event.Op&fsnotify.Remove == fsnotify.Remove {
					if follower, ok := handler.followers[event.Name]; ok {
						debug.Debugf("[kubernetes/input] Watcher loop a remove event: %s\n", event.Name)
						go func() {
							select {
							case follower.stop <- struct{}{}:
							default:
							}
							handler.followersMutex.Lock()
							delete(handler.followers, event.Name)
							handler.followersMutex.Unlock()
							debug.Debugf("[kubernetes/input] Successfully processed remove event: %s\n", event.Name)
						}()
					} else {
						debug.Debugf("[kubernetes/input] Watcher loop could not find follower %s to remove!\n", event.Name)
					}
					return
				}
			case err, ok := <-watcher.Errors:
				if !ok || handler.closing {
					return
				}
				debug.Debugf("[kubernetes/input] Watcher loop encountered an error: %s\n", err.Error())
				select {
				case handler.Errors() <- err:
				default:
				}
			}
		}
	}()
	return watcher, nil
}

func Create(logpath string, kube kubernetes.Interface) (*Kubernetes, error) {
	if logpath == "" {
		logpath = "/var/log/containers"
	}

	// TODO: Check permissions of service account, and directory exists... before we run...

	return &Kubernetes{
		kube:           kube,
		errors:         make(chan error, 1),
		packets:        make(chan syslog.Packet, 100),
		path:           logpath,
		followers:      make(map[string]fileWatcher),
		closing:        false,
		followersMutex: sync.Mutex{},
	}, nil
}
