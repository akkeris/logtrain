package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"github.com/akkeris/logtrain/internal/debug"
	"github.com/akkeris/logtrain/internal/storage"
	"github.com/akkeris/logtrain/pkg/input/syslogtcp"
	"github.com/akkeris/logtrain/pkg/input/syslogtls"
	"github.com/akkeris/logtrain/pkg/input/syslogudp"
	"github.com/akkeris/logtrain/pkg/output/memory"
	"github.com/akkeris/logtrain/pkg/router"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	rpprof "runtime/pprof"
	"strings"
	"sync"
	"syscall"
	"time"
)

var options struct {
	CPUProfile string
	MemProfile string
	KubeConfig string
}

type httpServer struct {
	mux    *http.ServeMux
	server *http.Server
}

type tailsRequest struct {
	Hostname string `hostname`
}

type activeTail struct {
	created time.Time
	uid     string
	host    string
}

var maxLogSize = 99990
var activeTails map[string]activeTail
var activeTailsMutex *sync.Mutex

func cancelOnInterrupt(ctx context.Context, f context.CancelFunc) {
	term := make(chan os.Signal)
	signal.Notify(term, os.Interrupt, syscall.SIGTERM)
	for {
		select {
		case <-term:
			log.Printf("Exiting\n")
			if options.CPUProfile != "" {
				rpprof.StopCPUProfile()
			}

			if options.MemProfile != "" {
				f, err := os.Create(options.MemProfile)
				if err != nil {
					debug.Fatalf("Cannot create memory profile: %s\n", err.Error())
				}
				rpprof.WriteHeapProfile(f)
			}
			f()
			os.Exit(0)
		case <-ctx.Done():
			os.Exit(0)
		}
	}
}

func init() {
	flag.StringVar(&options.CPUProfile, "cpuprofile", "", "write cpu profile to file")
	flag.StringVar(&options.MemProfile, "memprofile", "", "write mem profile to file")
	flag.StringVar(&options.KubeConfig, "kube-config", "", "specify the kube config path to be used")
	flag.Parse()
	prometheus.MustRegister(prometheus.NewBuildInfoCollector())
}

func getOsOrDefault(key string, def string) string {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	return val
}

func addInputsToRouter(router *router.Router, server *httpServer) error {
	var addedInput = false

	// Check to see if syslog over tcp will be used.
	if os.Getenv("SYSLOG_TCP") == "true" {
		handle, err := syslogtcp.Create("0.0.0.0:" + getOsOrDefault("SYSLOG_TCP_PORT", "9002"))
		if err != nil {
			return err
		}
		if err := handle.Dial(); err != nil {
			return err
		}
		if err := router.AddInput(handle, "syslogtcp"); err != nil {
			return err
		}
		addedInput = true
		log.Printf("[main] Added syslog over tcp on port %s \n", getOsOrDefault("SYSLOG_TCP_PORT", "9002"))
	}

	// Check to see if syslog over udp will be used.
	if os.Getenv("SYSLOG_UDP") == "true" {
		handle, err := syslogudp.Create("0.0.0.0:" + getOsOrDefault("SYSLOG_UDP_PORT", "9003"))
		if err != nil {
			return err
		}
		if err := handle.Dial(); err != nil {
			return err
		}
		if err := router.AddInput(handle, "syslogudp"); err != nil {
			return err
		}
		addedInput = true
		log.Printf("[main] Added syslog over udp on port %s \n", getOsOrDefault("SYSLOG_UDP_PORT", "9003"))
	}

	// Check to see if syslog over tls will be used.
	if os.Getenv("SYSLOG_TLS") == "true" {
		if os.Getenv("SYSLOG_TLS_CERT_PEM") == "" {
			return errors.New("the syslog tls environment variable SYSLOG_TLS_CERT_PEM was not found")
		}
		if os.Getenv("SYSLOG_TLS_KEY_PEM") == "" {
			return errors.New("the syslog tls environment variable SYSLOG_TLS_KEY_PEM was not found")
		}
		if os.Getenv("SYSLOG_TLS_SERVER_NAME") == "" {
			return errors.New("the syslog tls environment variable SYSLOG_TLS_SERVER_NAME was not found")
		}
		handle, err := syslogtls.Create(os.Getenv("SYSLOG_TLS_SERVER_NAME"), os.Getenv("SYSLOG_TLS_KEY_PEM"), os.Getenv("SYSLOG_TLS_CERT_PEM"), os.Getenv("SYSLOG_TLS_CA_PEM"), "0.0.0.0:"+getOsOrDefault("SYSLOG_TLS_PORT", "9004"))
		if err != nil {
			return err
		}
		if err := handle.Dial(); err != nil {
			return err
		}
		if err := router.AddInput(handle, "syslogtls"); err != nil {
			return err
		}
		addedInput = true
		log.Printf("[main] Added syslog over tcp+tls on port %s \n", getOsOrDefault("SYSLOG_TLS_PORT", "9004"))
	}

	if !addedInput {
		return errors.New("no data inputs were found")
	}
	return nil
}

func createRouter(ds []storage.DataSource) (*router.Router, error) {
	r, err := router.NewRouter(ds, true, 40 /* max connections */)
	if err != nil {
		return nil, err
	}
	if err := r.Dial(); err != nil {
		return nil, err
	}
	return r, nil
}

func createHTTPServer(port string) *httpServer {
	mux := http.NewServeMux()
	service := &httpServer{
		mux: mux,
		server: &http.Server{
			Addr:           ":" + port,
			Handler:        mux,
			IdleTimeout:    600 * time.Second,
			ReadTimeout:    600 * time.Second,
			WriteTimeout:   600 * time.Second, // Must be above 30 seconds for pprof.
			MaxHeaderBytes: 1 << 20,
		},
	}
	service.server.SetKeepAlivesEnabled(true)
	return service
}

func getTailsEndpoint() string {
	if os.Getenv("NAME") == "" || os.Getenv("NAMESPACE") == "" {
		return ""
	}
	if os.Getenv("SYSLOG_TLS") == "true" {
		return "syslog+tls://" + os.Getenv("NAME") + "." + os.Getenv("NAMESPACE") + ":" + getOsOrDefault("SYSLOG_TCP_PORT", "9004")
	} else if os.Getenv("SYSLOG_TCP") == "true" {
		return "syslog+tcp://" + os.Getenv("NAME") + "." + os.Getenv("NAMESPACE") + ":" + getOsOrDefault("SYSLOG_TLS_PORT", "9002")
	} else {
		return "syslog+udp://" + os.Getenv("NAME") + "." + os.Getenv("NAMESPACE") + ":" + getOsOrDefault("SYSLOG_UDP_PORT", "9003")
	}
}

func runSweepTails() {
	activeTailsMutex = &sync.Mutex{}
	activeTailsMutex.Lock()
	activeTails = make(map[string]activeTail, 0)
	activeTailsMutex.Unlock()
	timer := time.NewTimer(time.Second * 60)

	for {
		select {
		case <-timer.C:
			activeTailsMutex.Lock()
			newActiveTails := make(map[string]activeTail, 0)
			for key, val := range activeTails {
				if val.created.Add(time.Minute * 5).Before(time.Now()) {
					newActiveTails[key] = val
				}
			}
			activeTails = newActiveTails
			activeTailsMutex.Unlock()
		}
	}
}

func runWithContext(ctx context.Context) error {
	go runSweepTails() // collect expired tails and remove them from the map.

	if options.CPUProfile != "" {
		f, err := os.Create(options.CPUProfile)
		if err != nil {
			log.Fatal("Cannot create profile: " + err.Error())
		}
		rpprof.StartCPUProfile(f)
		defer rpprof.StopCPUProfile()
	}
	httpServer := createHTTPServer(getOsOrDefault("HTTP_PORT", "9000"))
	if os.Getenv("PROFILE") == "true" {
		httpServer.mux.HandleFunc("/debug/pprof/", http.HandlerFunc(pprof.Index))
		httpServer.mux.HandleFunc("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline))
		httpServer.mux.HandleFunc("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
		httpServer.mux.HandleFunc("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))
		httpServer.mux.HandleFunc("/debug/pprof/trace", http.HandlerFunc(pprof.Trace))
		httpServer.mux.HandleFunc("/debug/pprof/heap", pprof.Handler("heap").ServeHTTP)
		httpServer.mux.HandleFunc("/debug/pprof/block", pprof.Handler("block").ServeHTTP)
		httpServer.mux.HandleFunc("/debug/pprof/goroutine", pprof.Handler("goroutine").ServeHTTP)
		httpServer.mux.HandleFunc("/debug/pprof/mutex", pprof.Handler("mutex").ServeHTTP)
		httpServer.mux.HandleFunc("/metrics", promhttp.Handler().ServeHTTP)
	} else if os.Getenv("METRICS") == "true" {
		httpServer.mux.HandleFunc("/metrics", promhttp.Handler().ServeHTTP)
	}

	dss, err := storage.FindDataSources(os.Getenv("KUBERNETES_DATASOURCE") == "true", options.KubeConfig, os.Getenv("POSTGRES") == "true", os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}

	// Find only the writable resources
	dssw := make([]storage.DataSource, 0)
	for _, d := range dss {
		if d.Writable() == true {
			dssw = append(dssw, d)
		}
	}
	if len(dssw) == 0 {
		return errors.New("no data sources were defined, either kubernetes or postgresql are required")
	}
	debug.Infof("[logtail/main]: Found %d writable datasources\n", len(dssw))
	// Clear any routes pointing to us as we've restarted we should not
	// yet have any drains explictly pointing to us.
	routes, err := dssw[0].GetAllRoutes()
	if err != nil {
		return err
	}
	debug.Infof("[logtail/main]: Found %d existing routes in first datasource.\n", len(routes))
	tailsEndpoint := getTailsEndpoint()
	for _, route := range routes {
		if route.Endpoint == tailsEndpoint {
			debug.Infof("[logtail/main]: Removing route %s->%s as its stale.\n", route.Hostname, route.Endpoint)
			if err := dssw[0].EmitRemoveRoute(route); err != nil {
				debug.Errorf("[logtail/main]: Error, failed to remove route %s->%s due to: %s\n", route.Hostname, route.Endpoint, err.Error())
			}
		}
	}

	// create a dummy data source
	ds := storage.CreateMemoryDataSource()

	// create a router
	router, err := createRouter([]storage.DataSource{ds})
	if err != nil {
		return err
	}

	// Add listeners to router
	if err := addInputsToRouter(router, httpServer); err != nil {
		return err
	}

	httpServer.mux.HandleFunc("/tails", func(w http.ResponseWriter, req *http.Request) {
		var tailsReq tailsRequest
		defer req.Body.Close()
		if req.Method != http.MethodPost {
			http.NotFound(w, req)
			return
		}
		b, err := ioutil.ReadAll(req.Body)
		if err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(b, &tailsReq); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		uid := uuid.New()
		activeTailsMutex.Lock()
		activeTails[uid.String()] = activeTail{
			created: time.Now(),
			uid:     uid.String(),
			host:    tailsReq.Hostname,
		}
		activeTailsMutex.Unlock()

		var url = "http://" + os.Getenv("NAME") + "." + os.Getenv("NAMESPACE") + "/tails/" + uid.String()
		if os.Getenv("LOGTAIL_HOST_SUFFIX") != "" {
			url = "https://" + os.Getenv("NAME") + os.Getenv("LOGTAIL_HOST_SUFFIX") + "/tails/" + uid.String()
		}

		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Location", url)
		w.Write([]byte("{\"url\":\"" + url + "\"}"))
	})

	// respond to tail requests
	httpServer.mux.HandleFunc("/tails/", func(w http.ResponseWriter, req *http.Request) {
		defer req.Body.Close()
		if req.Method != http.MethodPost {
			http.NotFound(w, req)
			return
		}
		pathSegments := strings.Split(req.URL.Path, "/")
		if len(pathSegments) < 3 {
			http.NotFound(w, req)
			return
		}
		if pathSegments[2] == "" {
			http.NotFound(w, req)
			return
		}
		uid := pathSegments[2]
		debug.Infof("[logtail/main]: looking for uuid %s\n", uid)
		activeTailsMutex.Lock()
		tail, ok := activeTails[uid]
		activeTailsMutex.Unlock()
		if !ok {
			debug.Infof("[logtail/main]: logtail for uuid %s was not found\n", uid)
			http.NotFound(w, req)
			return
		}

		if tail.created.Add(time.Minute * 5).Before(time.Now()) {
			activeTailsMutex.Lock()
			delete(activeTails, uid)
			activeTailsMutex.Unlock()
			http.NotFound(w, req)
			return
		}

		defer func() {
			activeTailsMutex.Lock()
			if _, ok := activeTails[uid]; ok {
				delete(activeTails, uid)
			}
			activeTailsMutex.Unlock()
		}()

		uid = tail.uid
		host := tail.host

		channel := memory.NewMemoryChannel(uid)
		timer := time.NewTimer(time.Minute * 30)

		internalEndpoint := "memory://localhost/" + uid
		debug.Infof("Opening stream reading %s (%s)\n", host, internalEndpoint)

		// place a route to send everything coming from our syslog listener to
		// go to our channel we made above
		ds.EmitNewRoute(storage.LogRoute{
			Hostname: host,
			Endpoint: internalEndpoint,
		})

		// tell all other logtrains to forward traffic to us.
		dssw[0].EmitNewRoute(storage.LogRoute{
			Hostname: host,
			Endpoint: tailsEndpoint,
		})

		w.Header().Set("Content-Type", "text/syslog+rfc5424; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusCreated)

		flusher, canFlush := w.(http.Flusher)

		for {
			select {
			case msg, ok := <-channel:
				if !ok {
					debug.Infof("Closing stream reading %s (%s) due to memory channel that was closed.\n", host, internalEndpoint)
					return
				}
				if _, err := w.Write([]byte(msg.Generate(maxLogSize) + "\n")); err != nil {
					debug.Infof("Closing stream reading %s (%s) due to write error: %s\n", host, internalEndpoint, err.Error())
					ds.EmitRemoveRoute(storage.LogRoute{
						Hostname: host,
						Endpoint: internalEndpoint,
					})
					dssw[0].EmitRemoveRoute(storage.LogRoute{
						Hostname: host,
						Endpoint: tailsEndpoint,
					})
					return
				}
				if canFlush {
					flusher.Flush()
				}

			case <-req.Context().Done():
				debug.Infof("Closing stream reading %s (%s) due to request closing\n", host, internalEndpoint)
				ds.EmitRemoveRoute(storage.LogRoute{
					Hostname: host,
					Endpoint: internalEndpoint,
				})
				dssw[0].EmitRemoveRoute(storage.LogRoute{
					Hostname: host,
					Endpoint: tailsEndpoint,
				})
				return

			// Maximum time has expired for the log stream to be open, close it after this.
			case <-timer.C:
				debug.Infof("Closing stream reading %s (%s) becuase timer expired.\n", host, internalEndpoint)
				ds.EmitRemoveRoute(storage.LogRoute{
					Hostname: host,
					Endpoint: internalEndpoint,
				})
				dssw[0].EmitRemoveRoute(storage.LogRoute{
					Hostname: host,
					Endpoint: tailsEndpoint,
				})
				return
			}
		}
	})

	debug.Infof("[logtail/main]: Ready and listening...\n")
	httpServer.server.ListenAndServe()
	return nil
}

func run() error {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	go cancelOnInterrupt(ctx, cancelFunc)
	return runWithContext(ctx)
}

func main() {
	log.Printf("[main] Starting\n")
	if getTailsEndpoint() == "" {
		log.Fatalf("[main] required value NAME and NAMESPACE was not set.")
	}
	if err := run(); err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		debug.Fatalf("%s\n", err.Error())
	}
	log.Printf("[main] Exiting\n")
}
