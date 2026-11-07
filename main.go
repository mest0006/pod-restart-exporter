// Command pod-restart-exporter exposes Kubernetes container restart counts
// as Prometheus metrics. It only reads from the API (list pods), never writes.
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
)

func main() {
	listen := flag.String("listen", ":9101", "address to serve /metrics on")
	apiURL := flag.String("api-url", "", "plain-HTTP API URL for local runs, e.g. http://127.0.0.1:8001 from `kubectl proxy`; empty means in-cluster")
	namespace := flag.String("namespace", "", "only watch this namespace (empty = all namespaces)")
	timeout := flag.Duration("timeout", 5*time.Second, "timeout for each Kubernetes API call")
	flag.Parse()

	var client *KubeClient
	if *apiURL != "" {
		client = NewProxyClient(*apiURL)
		log.Printf("using API at %s", *apiURL)
	} else {
		c, err := NewInClusterClient()
		if err != nil {
			log.Fatal(err)
		}
		client = c
		log.Print("using in-cluster service account")
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", &Exporter{Lister: client, Namespace: *namespace, Timeout: *timeout})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })

	srv := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	// Shut down cleanly when Kubernetes sends SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Printf("listening on %s", *listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
