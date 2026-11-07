package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// RestartLister is the one thing the exporter needs from Kubernetes.
// Using an interface lets the tests swap in a fake.
type RestartLister interface {
	ListContainerRestarts(ctx context.Context, namespace string) ([]ContainerRestart, error)
}

// Exporter serves restart counts in the Prometheus text exposition format.
// It asks the API fresh on every scrape, so there is no cache to go stale.
type Exporter struct {
	Lister    RestartLister
	Namespace string
	Timeout   time.Duration
}

func (e *Exporter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), e.Timeout)
	defer cancel()

	start := time.Now()
	rows, err := e.Lister.ListContainerRestarts(ctx, e.Namespace)
	duration := time.Since(start).Seconds()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	success := 1
	if err != nil {
		// Still answer 200 so Prometheus records scrape_success=0 instead of a gap.
		log.Printf("scrape failed: %v", err)
		success = 0
		rows = nil
	}
	writeMetrics(w, rows, success, duration)
}

func writeMetrics(w io.Writer, rows []ContainerRestart, success int, duration float64) {
	// Stable output order makes diffs and tests predictable.
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Pod != b.Pod {
			return a.Pod < b.Pod
		}
		return a.Container < b.Container
	})

	fmt.Fprintln(w, "# HELP pod_restart_exporter_container_restarts_total Number of times a container has restarted, as reported by the Kubernetes API.")
	fmt.Fprintln(w, "# TYPE pod_restart_exporter_container_restarts_total counter")
	for _, r := range rows {
		fmt.Fprintf(w, "pod_restart_exporter_container_restarts_total{namespace=\"%s\",pod=\"%s\",container=\"%s\"} %d\n",
			escapeLabel(r.Namespace), escapeLabel(r.Pod), escapeLabel(r.Container), r.Restarts)
	}

	fmt.Fprintln(w, "# HELP pod_restart_exporter_scrape_success 1 if the last Kubernetes API call succeeded, 0 otherwise.")
	fmt.Fprintln(w, "# TYPE pod_restart_exporter_scrape_success gauge")
	fmt.Fprintf(w, "pod_restart_exporter_scrape_success %d\n", success)

	fmt.Fprintln(w, "# HELP pod_restart_exporter_scrape_duration_seconds Time spent querying the Kubernetes API.")
	fmt.Fprintln(w, "# TYPE pod_restart_exporter_scrape_duration_seconds gauge")
	fmt.Fprintf(w, "pod_restart_exporter_scrape_duration_seconds %g\n", duration)
}

// escapeLabel applies the Prometheus rules for label values: \ " and newline.
var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func escapeLabel(s string) string { return labelEscaper.Replace(s) }
