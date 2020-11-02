
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/rest"
	"github.com/akkeris/logtrain/internal/storage"
	kube "github.com/akkeris/logtrain/pkg/input/kubernetes"
	envoy "github.com/akkeris/logtrain/pkg/input/envoy"
	http_events "github.com/akkeris/logtrain/pkg/input/http"
	"github.com/akkeris/logtrain/pkg/input/sysloghttp"
	"github.com/akkeris/logtrain/pkg/input/syslogtcp"
	"github.com/akkeris/logtrain/pkg/input/syslogudp"
	"github.com/akkeris/logtrain/pkg/input/syslogtls"
	"github.com/akkeris/logtrain/pkg/router"
)

var options struct {
	KubeConfig string
	Port int
}

type httpServer struct {
	mux *http.ServeMux
	server *http.Server
}

func getKubernetesClient(kubeConfigPath string) (kubernetes.Interface, error) {
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

func cancelOnInterrupt(ctx context.Context, f context.CancelFunc) {
	term := make(chan os.Signal)
	signal.Notify(term, os.Interrupt, syscall.SIGTERM)
	for {
		select {
		case <-term:
			f()
			os.Exit(0)
		case <-ctx.Done():
			os.Exit(0)
		}
	}
}

func init() {
	flag.IntVar(&options.Port, "port", 9000, "use '--port' option to specify the port for broker to listen on")
	//flag.IntVar(&options.Port, "port", 9000, "use '--port' option to specify the port for broker to listen on")
	flag.StringVar(&options.KubeConfig, "kube-config", "", "specify the kube config path to be used")
	flag.Parse()
}

func findDataSources() ([]storage.DataSource, error) {
	ds := make([]storage.DataSource, 0)

	if os.Getenv("KUBERNETES") == "true" {
		k8sClient, err := getKubernetesClient(options.KubeConfig)
		if err != nil {
			return nil, err
		}
		kds, err := storage.CreateKubernetesDataSource(k8sClient)
		if err != nil {
			return nil, err
		}
		ds = append(ds, kds)
	}

	return ds, nil
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
		address := "0.0.0.0:"
		if os.Getenv("ENVOY_PORT") != "" {
			address = address + os.Getenv("ENVOY_PORT")
		} else {
			address = address + ":9001"
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
		server.mux.Handle(getOsOrDefault("HTTP_EVENTS_PATH", "/events"), handle.HandlerFunc)
		if err := router.AddInput(handle, "http"); err != nil {
			return err
		}
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
		server.mux.Handle(getOsOrDefault("HTTP_SYSLOG_PATH", "/syslog"), handle.HandlerFunc)
		if err := router.AddInput(handle, "sysloghttp"); err != nil {
			return err
		}
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
	}

	// Check to see if syslog over udp will be used.
	if os.Getenv("SYSLOG_UDP") == "true" {
		handle, err := syslogudp.Create("0.0.0.0:" + getOsOrDefault("SYSLOG_UDP_PORT","9003"))
		if err != nil {
			return err
		}
		if err := handle.Dial(); err != nil {
			return err
		}
		if err := router.AddInput(handle, "syslogudp"); err != nil {
			return err
		}
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
		//Create(server_name string, key_pem string, cert_pem string, ca_pem string, address string)
		handle, err := syslogtls.Create(os.Getenv("SYSLOG_TLS_SERVER_NAME"), os.Getenv("SYSLOG_TLS_KEY_PEM"), os.Getenv("SYSLOG_TLS_CERT_PEM"), os.Getenv("SYSLOG_TLS_CA_PEM"), "0.0.0.0:" + getOsOrDefault("SYSLOG_TLS_PORT","9004"))
		if err != nil {
			return err
		}
		if err := handle.Dial(); err != nil {
			return err
		}
		if err := router.AddInput(handle, "syslogtls"); err != nil {
			return err
		}
	}

	// Check to see if we should add kubernetes as an input
	if os.Getenv("KUBERNETES") == "true" {
		k8sClient, err := getKubernetesClient(options.KubeConfig)
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
	}

	if !addedInput {
		return errors.New("No data inputs were found.")
	}
	return nil
}

func printMetricsLoop(router *router.Router) {
	ticker := time.NewTicker(time.Minute * 5)
	for {
		select {
		case <-ticker.C:
			metrics := router.Metrics()
			for endpoint, metric := range metrics {
				log.Printf("metrics endpoint=%s connections=%d maxconnections=%d pressure=%f sent=%d errors=%d", endpoint, metric.Connections, metric.MaxConnections, metric.Pressure, metric.Sent, metric.Errors)
			}
		}
	}
}

func createHttpServer(port string) *httpServer {
	mux := http.NewServeMux()
	server := httpServer {
		mux: mux,
		server: &http.Server{
			Addr:           ":" + port,
			Handler:        mux,
			ReadTimeout:    1 * time.Second,
			WriteTimeout:   1 * time.Second,
			MaxHeaderBytes: 1 << 20,
		},
	}
	go server.ListenAndServer()
	return server
}

func runWithContext(ctx context.Context) error {
	httpServer := createHttpServer(getOsOrDefault("HTTP_PORT","9000"))

	ds, err := findDataSources()
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
	if err := addInputsToRouter(router, server); err != nil {
		return err
	}
	printMetricsLoop(router) // This never returns
	return nil
}

func run() error {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	go cancelOnInterrupt(ctx, cancelFunc)
	return runWithContext(ctx)
}

func main() {
	log.Printf("Starting\n")
	if err := run(); err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		log.Fatalln(err)
	}
	log.Printf("Exiting\n")

}

