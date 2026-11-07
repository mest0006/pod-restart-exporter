# pod-restart-exporter

A small Prometheus exporter written in Go that reports how many times each Kubernetes container has restarted. It has read-only access: it can list pods and nothing else.

```
pod_restart_exporter_container_restarts_total{namespace="prod",pod="web-1",container="app"} 3
pod_restart_exporter_scrape_success 1
pod_restart_exporter_scrape_duration_seconds 0.0063
```

## Why

Crash-looping containers are one of the most common signs of a broken deployment. This exporter turns restart counts into a metric you can graph and alert on, for example:

```promql
increase(pod_restart_exporter_container_restarts_total[15m]) > 3
```

## Design

- **Go standard library only.** It has no external dependencies. It calls the Kubernetes REST API (`GET /api/v1/pods`) directly and writes the Prometheus text format itself.
- **Fresh data on every scrape.** Each `/metrics` request queries the API, with a timeout (`-timeout`, default 5s).
- **Failures are visible.** If the API call fails, the exporter still answers with `scrape_success 0`, so the failure shows up in Prometheus instead of as a silent gap.
- **Least privilege.** The Helm chart grants only `list` on `pods`. If `watchNamespace` is set, it uses a namespaced Role instead of a ClusterRole.
- **Hardened pod.** It runs as non-root on a distroless image with a read-only root filesystem, all capabilities dropped and the RuntimeDefault seccomp profile. That passes the Pod Security "restricted" profile.
- **Graceful shutdown** on SIGTERM.

| File | What it does |
|---|---|
| `main.go` | Flags, HTTP server, `/metrics` and `/healthz`, shutdown |
| `kube.go` | Kubernetes API client (in-cluster service account or `kubectl proxy`) |
| `metrics.go` | Prometheus text output, sorting, label escaping |
| `exporter_test.go` | Tests using a fake API server |

## Run locally

```bash
go test -v ./...                  # run the tests
kubectl proxy --port=8001 &       # opens an authenticated tunnel to your cluster
go run . -api-url http://127.0.0.1:8001
curl localhost:9101/metrics
```

## Deploy to a cluster

```bash
# Build and push the image (if you forked this, use your own GitHub username)
docker build -t ghcr.io/mest0006/pod-restart-exporter:0.1.0 .
docker push ghcr.io/mest0006/pod-restart-exporter:0.1.0
# For kind instead of pushing: kind load docker-image ghcr.io/mest0006/pod-restart-exporter:0.1.0

helm lint chart/pod-restart-exporter
helm install restarts chart/pod-restart-exporter -n monitoring --create-namespace

kubectl -n monitoring port-forward svc/restarts-pod-restart-exporter 9101 &
curl localhost:9101/metrics
```

Test that it works by making a pod crash on purpose:

```bash
kubectl run crashy --image=busybox --restart=Always -- sh -c "exit 1"
# wait a minute, then curl again: crashy's restart count goes up
kubectl delete pod crashy
```

Real output from a local [kind](https://kind.sigs.k8s.io/) cluster, about 100 seconds after starting `crashy`:

```console
$ curl -s localhost:9101/metrics | grep -E '^# (HELP|TYPE) pod_restart_exporter_container_restarts_total|crashy'
# HELP pod_restart_exporter_container_restarts_total Number of times a container has restarted, as reported by the Kubernetes API.
# TYPE pod_restart_exporter_container_restarts_total counter
pod_restart_exporter_container_restarts_total{namespace="default",pod="crashy",container="crashy"} 4

$ kubectl get pod crashy
NAME     READY   STATUS   RESTARTS      AGE
crashy   0/1     Error    4 (66s ago)   104s
```

The exporter's count matches the `RESTARTS` column from `kubectl`.

## Possible next steps

- Switch to `client-go` informers (a watch-based cache) so large clusters aren't listed on every scrape.
- Add a `reason` label from `lastState.terminated.reason` (for example OOMKilled or Error).
- Add a GitHub Actions workflow that runs tests, builds the image and runs `helm lint`.
