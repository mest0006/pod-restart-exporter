package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A fake Kubernetes API that returns a fixed pod list.
const fakePods = `{"items":[
 {"metadata":{"name":"web-1","namespace":"prod"},
  "status":{"containerStatuses":[{"name":"app","restartCount":3},{"name":"sidecar","restartCount":0}]}},
 {"metadata":{"name":"db-0","namespace":"data"},
  "status":{"containerStatuses":[{"name":"postgres","restartCount":12}]}}
]}`

func TestListContainerRestarts(t *testing.T) {
	var gotPath string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodGet {
			t.Errorf("exporter must only GET, got %s", r.Method)
		}
		io.WriteString(w, fakePods)
	}))
	defer api.Close()

	rows, err := NewProxyClient(api.URL).ListContainerRestarts(context.Background(), "prod")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/namespaces/prod/pods" {
		t.Errorf("wrong API path %q", gotPath)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 containers, got %d", len(rows))
	}
}

func TestAPIErrorStatus(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer api.Close()

	if _, err := NewProxyClient(api.URL).ListContainerRestarts(context.Background(), ""); err == nil {
		t.Fatal("expected an error for HTTP 403")
	}
}

func TestMetricsOutput(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, fakePods)
	}))
	defer api.Close()

	exp := &Exporter{Lister: NewProxyClient(api.URL), Timeout: time.Second}
	rec := httptest.NewRecorder()
	exp.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()

	for _, want := range []string{
		`pod_restart_exporter_container_restarts_total{namespace="data",pod="db-0",container="postgres"} 12`,
		`pod_restart_exporter_container_restarts_total{namespace="prod",pod="web-1",container="app"} 3`,
		`pod_restart_exporter_scrape_success 1`,
		`# TYPE pod_restart_exporter_container_restarts_total counter`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing line %q in:\n%s", want, body)
		}
	}
	// Sorted output: namespace "data" comes before "prod".
	if strings.Index(body, `namespace="data"`) > strings.Index(body, `namespace="prod"`) {
		t.Error("output is not sorted by namespace")
	}
}

type failingLister struct{}

func (failingLister) ListContainerRestarts(context.Context, string) ([]ContainerRestart, error) {
	return nil, errors.New("api down")
}

func TestScrapeFailureReportsZero(t *testing.T) {
	exp := &Exporter{Lister: failingLister{}, Timeout: time.Second}
	rec := httptest.NewRecorder()
	exp.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("want 200 so Prometheus still records the failure, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "pod_restart_exporter_scrape_success 0") {
		t.Errorf("want scrape_success 0, got:\n%s", rec.Body.String())
	}
}

func TestEscapeLabel(t *testing.T) {
	got := escapeLabel("a\"b\\c\nd")
	want := `a\"b\\c\nd`
	if got != want {
		t.Errorf("escapeLabel = %q, want %q", got, want)
	}
}
