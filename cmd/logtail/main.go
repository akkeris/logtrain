package main

import (
	"context"
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
	"github.com/trevorlinton/remote_syslog2/syslog"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	rpprof "runtime/pprof"
	"strings"
	"syscall"
	"time"
)

var options struct {
	CpuProfile string
	MemProfile string
	KubeConfig string
}

var maxLogSize = 99990

type httpServer struct {
	mux    *http.ServeMux
	server *http.Server
}

func cancelOnInterrupt(ctx context.Context, f context.CancelFunc) {
	term := make(chan os.Signal)
	signal.Notify(term, os.Interrupt, syscall.SIGTERM)
	for {
		select {
		case <-term:
			log.Printf("Exiting\n")
			if options.CpuProfile != "" {
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
	flag.StringVar(&options.CpuProfile, "cpuprofile", "", "write cpu profile to file")
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
			return errors.New("The syslog tls environment variable SYSLOG_TLS_CERT_PEM was not found.")
		}
		if os.Getenv("SYSLOG_TLS_KEY_PEM") == "" {
			return errors.New("The syslog tls environment variable SYSLOG_TLS_KEY_PEM was not found.")
		}
		if os.Getenv("SYSLOG_TLS_SERVER_NAME") == "" {
			return errors.New("The syslog tls environment variable SYSLOG_TLS_SERVER_NAME was not found.")
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
		return errors.New("No data inputs were found.")
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

func createHttpServer(port string) *httpServer {
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
	if os.Getenv("SYSLOG_TLS") == "true" {
		return "syslog+tls://" + os.Getenv("NAME") + "." + os.Getenv("NAMESPACE") + ":" + getOsOrDefault("SYSLOG_TCP_PORT", "9004")
	} else if os.Getenv("SYSLOG_TCP") == "true" {
		return "syslog+tcp://" + os.Getenv("NAME") + "." + os.Getenv("NAMESPACE") + ":" + getOsOrDefault("SYSLOG_TLS_PORT", "9002")
	} else {
		return "syslog+udp://" + os.Getenv("NAME") + "." + os.Getenv("NAMESPACE") + ":" + getOsOrDefault("SYSLOG_UDP_PORT", "9003")
	}
}

func runWithContext(ctx context.Context) error {
	if options.CpuProfile != "" {
		f, err := os.Create(options.CpuProfile)
		if err != nil {
			log.Fatal("Cannot create profile: " + err.Error())
		}
		rpprof.StartCPUProfile(f)
		defer rpprof.StopCPUProfile()
	}
	httpServer := createHttpServer(getOsOrDefault("HTTP_PORT", "9000"))
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

	go httpServer.server.ListenAndServe()

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
		return errors.New("No data sources were defined, either kubernetes or postgresql are required.")
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

	// respond to tail requests
	httpServer.mux.HandleFunc("/tails/", func(w http.ResponseWriter, req *http.Request) {
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
		host := pathSegments[2]

		uid := uuid.New()
		c := make(chan syslog.Packet, 1)
		memory.GlobalInternalOutputs[uid.String()] = c
		timer := time.NewTimer(time.Minute * 30)

		// place a route to send everything coming from our syslog listener to
		// go to our channel we made above
		ds.EmitNewRoute(storage.LogRoute{
			Hostname: host,
			Endpoint: "memory://localhost/" + uid.String(),
		})

		// tell all other logtrains to forward traffic to us.
		dssw[0].EmitNewRoute(storage.LogRoute{
			Hostname: host,
			Endpoint: getTailsEndpoint(),
		})

		// To keep alive hte response writer we cannot return, returning out of
		// this will cause the writer to be closed, this is executed as part of
		// an existing go-routine from the http server.
		for {
			select {
			case msg := <-c:
				if _, err := w.Write([]byte(msg.Generate(maxLogSize) + "\n")); err != nil {
					ds.EmitRemoveRoute(storage.LogRoute{
						Hostname: host,
						Endpoint: "internal://localhost/" + uid.String(),
					})
					dssw[0].EmitRemoveRoute(storage.LogRoute{
						Hostname: host,
						Endpoint: getTailsEndpoint(),
					})
					close(c)
					return
				}
			case <-timer.C:
				ds.EmitRemoveRoute(storage.LogRoute{
					Hostname: host,
					Endpoint: "internal://localhost/" + uid.String(),
				})
				dssw[0].EmitRemoveRoute(storage.LogRoute{
					Hostname: host,
					Endpoint: getTailsEndpoint(),
				})
				close(c)
				return
			}
		}
	})

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
	if err := run(); err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		debug.Fatalf("%s\n", err.Error())
	}
	log.Printf("[main] Exiting\n")
}
