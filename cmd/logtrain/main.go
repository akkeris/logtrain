package main

import (
	"context"
	"errors"
	"flag"
	"github.com/akkeris/logtrain/internal/debug"
	"github.com/akkeris/logtrain/internal/storage"
	envoy "github.com/akkeris/logtrain/pkg/input/envoy"
	http_events "github.com/akkeris/logtrain/pkg/input/http"
	kube "github.com/akkeris/logtrain/pkg/input/kubernetes"
	"github.com/akkeris/logtrain/pkg/input/sysloghttp"
	"github.com/akkeris/logtrain/pkg/input/syslogtcp"
	"github.com/akkeris/logtrain/pkg/input/syslogtls"
	"github.com/akkeris/logtrain/pkg/input/syslogudp"
	persistent "github.com/akkeris/logtrain/pkg/output/persistent"
	"github.com/akkeris/logtrain/pkg/router"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	rpprof "runtime/pprof"
	"syscall"
	"strings"
	"time"
)

var options struct {
	CpuProfile string
	MemProfile string
	KubeConfig string
}

var (
	syslogConnections = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:       "logtrain_connections",
		Help:       "Amount of outbound syslog connections.",
		Buckets: 	prometheus.LinearBuckets(0, 5, 10),
	}, []string{"syslog"})
	syslogPressure = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:       "logtrain_pressure",
		Help:       "The percentage of buffers that are full waiting to be sent.",
		Buckets: 	prometheus.LinearBuckets(0, 0.1, 10),
	}, []string{"syslog"})
	syslogSent = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:       "logtrain_packets_sent",
		Help:       "The amount of packets sent via a syslog (successful or not).",
		Buckets: 	prometheus.LinearBuckets(0, 100, 100),
	}, []string{"syslog"})
	syslogErrors = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:       "logtrain_errors",
		Help:       "The amount of packets that could not be sent.",
		Buckets: 	prometheus.LinearBuckets(0, 100, 100),
	}, []string{"syslog"})
	syslogDeadPackets = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:       "logtrain_deadpackets",
		Help:       "The amount of packets received with no route.",
		Buckets: 	prometheus.LinearBuckets(0, 100, 100),
	})
)

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
	prometheus.MustRegister(syslogErrors)
	prometheus.MustRegister(syslogSent)
	prometheus.MustRegister(syslogPressure)
	prometheus.MustRegister(syslogConnections)
	prometheus.MustRegister(syslogDeadPackets)
	prometheus.MustRegister(prometheus.NewBuildInfoCollector())
}

func createRouter(ds []storage.DataSource) (*router.Router, error) {
	r, err := router.NewRouter(ds, true, 40 /* max connections */)
	if err != nil {
		return nil, err
	}
	if err := r.Dial(); err != nil {
		return nil, err
	}
	for _, d := range ds {
		if err := d.Dial(); err != nil {
			return nil, err
		}
	}
	return r, nil
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
	// Check to see if we should add istio/envoy inputs
	if os.Getenv("ENVOY") == "true" {
		address := ":"
		if os.Getenv("ENVOY_PORT") != "" {
			address = address + os.Getenv("ENVOY_PORT")
		} else {
			address = address + "9001"
		}
		in, err := envoy.Create(address)
		if err != nil {
			return err
		}
		if err := in.Dial(); err != nil {
			return err
		}
		if err := router.AddInput(in, "istio+envoy"); err != nil {
			return err
		}
		addedInput = true
		log.Printf("[main] Added envoy on port %s\n", getOsOrDefault("ENVOY_PORT", "9001"))
	}

	// Check to see if http events will be used as an input
	if os.Getenv("HTTP_EVENTS") == "true" {
		handle, err := http_events.Create()
		if err != nil {
			return err
		}
		if err := handle.Dial(); err != nil {
			return err
		}
		server.mux.HandleFunc(getOsOrDefault("HTTP_EVENTS_PATH", "/events"), handle.HandlerFunc)
		if err := router.AddInput(handle, "http"); err != nil {
			return err
		}
		addedInput = true
		log.Printf("[main] Added http endpoint %s for JSON syslog payloads\n", getOsOrDefault("HTTP_EVENTS_PATH", "/events"))
	}

	// Check to see if syslog over http will be used as an input
	if os.Getenv("HTTP_SYSLOG") == "true" {
		handle, err := sysloghttp.Create()
		if err != nil {
			return err
		}
		if err := handle.Dial(); err != nil {
			return err
		}
		server.mux.HandleFunc(getOsOrDefault("HTTP_SYSLOG_PATH", "/syslog"), handle.HandlerFunc)
		if err := router.AddInput(handle, "sysloghttp"); err != nil {
			return err
		}
		addedInput = true
		log.Printf("[main] Added syslog over http on path %s \n", getOsOrDefault("HTTP_SYSLOG_PATH", "/syslog"))
	}

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

	// Check to see if we should add kubernetes as an input
	if os.Getenv("KUBERNETES") == "true" {
		k8sClient, err := storage.GetKubernetesClient(options.KubeConfig)
		if err != nil {
			return err
		}
		in, err := kube.Create(os.Getenv("KUBERNETES_LOG_PATH"), k8sClient)
		if err != nil {
			return err
		}
		if err := in.Dial(); err != nil {
			return err
		}
		if err := router.AddInput(in, "kubernetes"); err != nil {
			return err
		}
		addedInput = true
		log.Printf("[main] Added kubernetes file watcher\n")
	}

	if os.Getenv("PERSISTENT") == "true" {
		server.mux.HandleFunc(getOsOrDefault("PERSISTENT_PATH", "/logs/"), func (response http.ResponseWriter, req *http.Request) {
			segments := strings.Split(req.URL.Path, "/")
			if segments[2] == "" {
				http.NotFound(response, req)
				return
			}
			defer req.Body.Close()
			data, err := persistent.Get(segments[2])
			if err != nil {
				http.NotFound(response, req)
				debug.Errorf("[main] Error: Unable to get persistent logs [%s]: %s\n", segments[2], err.Error())
				return
			}
			response.WriteHeader(http.StatusOK)
			response.Write([]byte(*data))
		})
	}

	if !addedInput {
		return errors.New("No data inputs were found.")
	}
	return nil
}

func prometheusMetricsLoop(router *router.Router) {
	ticker := time.NewTicker(time.Minute * 5)
	for {
		select {
		case <-ticker.C:
			metrics := router.Metrics()
			for endpoint, metric := range metrics {
				syslogConnections.WithLabelValues(endpoint).Observe(float64(metric.Connections))
				syslogPressure.WithLabelValues(endpoint).Observe(metric.Pressure)
				syslogErrors.WithLabelValues(endpoint).Observe(float64(metric.Errors))
				syslogSent.WithLabelValues(endpoint).Observe(float64(metric.Sent))

				// TODO: sanitize endpoint.
			}
			syslogDeadPackets.Observe(float64(router.DeadPackets()))
			router.ResetMetrics()
		}
	}
}

func createHttpServer(port string) *httpServer {
	mux := http.NewServeMux()
	return &httpServer{
		mux: mux,
		server: &http.Server{
			Addr:           ":" + port,
			Handler:        mux,
			ReadTimeout:    60 * time.Second,
			WriteTimeout:   120 * time.Second, // Must be above 30 seconds for pprof.
			MaxHeaderBytes: 1 << 20,
		},
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

	ds, err := storage.FindDataSources(os.Getenv("KUBERNETES_DATASOURCE") == "true", options.KubeConfig, os.Getenv("POSTGRES") == "true", os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	if len(ds) == 0 {
		return errors.New("No data sources were defined, either kubernetes or postgresql are required.")
	}
	router, err := createRouter(ds)
	if err != nil {
		return err
	}
	if err := addInputsToRouter(router, httpServer); err != nil {
		return err
	}
	prometheusMetricsLoop(router) // This never returns
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
