package kubernetes

import (
    "encoding/json"
    "errors"
    //"log"
    "io"
    "os"
    "regexp"
    "strings"
    "sync"
    "time"
    "path/filepath"
    "github.com/fsnotify/fsnotify"
	"github.com/trevorlinton/go-tail/follower"
	"github.com/papertrail/remote_syslog2/syslog"
)

const kubeTime = "2006-01-02T15:04:05.000000000Z"

type kubeLine struct {
	Log string `json:"log"`
	Stream string `json:"stream"`
	Time string `json:"time"`
}

type kubeDetails struct {
	Container string
	DockerId string
	Namespace string
	Pod string
}

type fileWatcher struct {
	errors uint32
	follower *follower.Follower
	hostname string
	stop chan struct{}
	tag string
}

type Kubernetes struct {
	closing bool
	errors chan error
	followers map[string]fileWatcher
	followersMutex sync.Mutex
	packets chan syslog.Packet
	path string
	watcher *fsnotify.Watcher
}

func dir(root string) []string {
    var files []string
    err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
        if(info.IsDir() == false) {
            files = append(files, path)
        }
        return nil
    })
    if err != nil {
        panic(err)
    }
    return files
}

func (handler *Kubernetes) Close() error {
	handler.watcher.Close()
	for _, v := range handler.followers {
		v.follower.Close()
	}
	close(handler.packets)
	close(handler.errors)
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
		handler.add(file, io.SeekEnd)
	}
	watcher, err := handler.watcherEventLoop()
	if err != nil {
		return err
	}
	handler.watcher = watcher
	return nil
}

func (handler *Kubernetes) Errors() (chan error) {
	return handler.errors
}

func (handler *Kubernetes) Packets() (chan syslog.Packet) {
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
			DockerId: parts[0][7],
			Namespace: parts[0][5],
			Pod: parts[0][1],
		}, nil
	}
	return nil, errors.New("invalid filename, no match given")
}

func (handler *Kubernetes) add(file string, ioSeek int) {
	config := follower.Config{
		Offset:0,
		Whence:ioSeek,
		Reopen:true,
	}
	// The filename has additional details we need.
	details, err := kubeDetailsFromFileName(file)
	if err != nil {
		return
	}

	var hostname string = ""
	var tag string = ""
	if os.Getenv("AKKERIS") == "true" {
		// This gets akkeris specifc
		// TODO: find a more scalable way of dealing with this.
		dyno := strings.Split(details.Container, "--")
		app := dyno[0]
		dyno_type := "web"
		if len(dyno) > 1 {
			dyno_type = dyno[1]
		}
		process := strings.Replace(details.Pod, details.Container + "-", "", 1)
		hostname = app + "-" + details.Namespace
		tag = dyno_type + "." + process
	 	// end akkeris specific stuff.
	 } else {
	 	hostname = details.Pod + "." + details.Namespace
	 	tag = details.Container
	 }


	proc, err := follower.New(file, config)
	if err != nil {
		handler.Errors() <- err
		return
	}
	fw := fileWatcher{
		follower: proc,
		stop: make(chan struct{}, 1),
		hostname: hostname,
		tag: tag,
		errors: 0,
	}
	handler.followersMutex.Lock()
	handler.followers[file] = fw
	handler.followersMutex.Unlock()
	go func() {
		for {
			select {
			case line, ok := <-fw.follower.Lines():
				if ok == true {
					var data kubeLine
					if err := json.Unmarshal([]byte(line.String()), &data); err != nil {
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
				} else {
					if fw.follower.Err() != nil {
						// track errors with the lines, but don't report anything.
						// TODO: should we do more? we shouldnt report this on
						// the kubernetes error handler as one corrupted file could make
						// the entire input handler look broken. Brainstorm on this.
						fw.errors++
					}
				}
			case <-fw.stop:
				return
			}
		}
	}()
}

func (handler *Kubernetes) watcherEventLoop() (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := watcher.Add(handler.path); err != nil {
		return nil, err
	}
	go func() {
		for {
			select {
			case event := <-watcher.Events:
				if event.Op&fsnotify.Create == fsnotify.Create {
					handler.add(event.Name, io.SeekStart)
				} else if event.Op&fsnotify.Remove == fsnotify.Remove {
					select {
					case handler.followers[event.Name].stop <- struct{}{}:
					default:
					}
					handler.followersMutex.Lock()
					delete(handler.followers, event.Name)
					handler.followersMutex.Unlock()
					return
				}
			case err := <-watcher.Errors:
				select {
				case handler.Errors() <- err:
				default:
				}
			}
		}
	}()
	return watcher, nil
}

func Create(logpath string) (*Kubernetes, error) {
	if logpath == "" {
		logpath = "/var/log/containers"
	}
	return &Kubernetes{
		errors: make(chan error, 1),
		packets: make(chan syslog.Packet, 100),
		path: logpath,
		followers: make(map[string]fileWatcher),
		closing: false,
		followersMutex: sync.Mutex{},
	}, nil
}