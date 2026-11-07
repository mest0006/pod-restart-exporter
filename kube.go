package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// Paths Kubernetes mounts into every pod that uses a service account.
const (
	saTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	saCAPath    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

// ContainerRestart is one row of output: how many times a container restarted.
type ContainerRestart struct {
	Namespace string
	Pod       string
	Container string
	Restarts  int
}

// podList holds only the fields we need from GET /api/v1/pods.
type podList struct {
	Items []struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Status struct {
			ContainerStatuses []struct {
				Name         string `json:"name"`
				RestartCount int    `json:"restartCount"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

// KubeClient talks to the Kubernetes API with read-only GET requests.
type KubeClient struct {
	baseURL   string
	tokenPath string // re-read on every request so rotated tokens keep working
	http      *http.Client
}

// NewInClusterClient uses the pod's service account token and CA certificate.
func NewInClusterClient() (*KubeClient, error) {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, fmt.Errorf("not running in a cluster: KUBERNETES_SERVICE_HOST/PORT are not set (use -api-url for local runs)")
	}
	caPEM, err := os.ReadFile(saCAPath)
	if err != nil {
		return nil, fmt.Errorf("read service account CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("no valid certificates in %s", saCAPath)
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	return &KubeClient{
		baseURL:   "https://" + net.JoinHostPort(host, port),
		tokenPath: saTokenPath,
		http:      &http.Client{Transport: transport, Timeout: 10 * time.Second},
	}, nil
}

// NewProxyClient talks to a plain-HTTP API endpoint such as `kubectl proxy`.
func NewProxyClient(apiURL string) *KubeClient {
	return &KubeClient{
		baseURL: strings.TrimRight(apiURL, "/"),
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// ListContainerRestarts returns restart counts for every container in the
// given namespace, or in all namespaces when namespace is empty.
func (c *KubeClient) ListContainerRestarts(ctx context.Context, namespace string) ([]ContainerRestart, error) {
	path := "/api/v1/pods"
	if namespace != "" {
		path = "/api/v1/namespaces/" + namespace + "/pods"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if c.tokenPath != "" {
		token, err := os.ReadFile(c.tokenPath)
		if err != nil {
			return nil, fmt.Errorf("read service account token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list pods: unexpected status %s", resp.Status)
	}

	var pods podList
	if err := json.NewDecoder(resp.Body).Decode(&pods); err != nil {
		return nil, fmt.Errorf("decode pod list: %w", err)
	}

	var out []ContainerRestart
	for _, p := range pods.Items {
		for _, cs := range p.Status.ContainerStatuses {
			out = append(out, ContainerRestart{
				Namespace: p.Metadata.Namespace,
				Pod:       p.Metadata.Name,
				Container: cs.Name,
				Restarts:  cs.RestartCount,
			})
		}
	}
	return out, nil
}
