# Owl Scheduler

A custom Kubernetes scheduler built in Go with a filter-score-bind pipeline, priority queue, preemption, and full Helm/deployment manifests.

## Features

- **Filter Plugins**: NodeResourcesFit, NodeName, NodeUnschedulable, TaintToleration
- **Score Plugins**: LeastRequestedPriority, BalancedResourceAllocation, NodeAffinity
- **Priority Queue**: Heap-based, ordered by PriorityClass
- **Preemption**: PDB-aware victim selection with Eviction API
- **Binder**: Retry with exponential backoff
- **Deployment**: Multi-replica with leader election, security contexts, Helm chart

## Project Structure

```
owl-scheduler/
├── cmd/scheduler/main.go          # Entrypoint
├── pkg/
│   ├── apis/types.go              # Shared interfaces
│   ├── scheduler/
│   │   ├── scheduler.go           # Core scheduling loop
│   │   ├── binder.go              # Pod binding with retries
│   │   ├── queue.go               # Priority queue
│   │   └── plugins/
│   │       ├── filtering.go       # Node filters
│   │       ├── scoring.go         # Node scorers
│   │       ├── priority.go        # Pod priority sorting
│   │       └── preemption.go      # PDB-aware preemption
├── manifests/                     # Raw K8s YAML
├── helm/k8s-scheduler/            # Helm chart
├── Dockerfile                     # Multi-stage distroless build
└── scripts/                       # Build + test scripts
```

## Quick Start

```bash
# Build locally
go build -o bin/scheduler ./cmd/scheduler

# Build Docker image
docker build -t owl-scheduler:0.1.0 .

# Deploy to cluster
kubectl apply -f manifests/

# Or with Helm
helm install owl-scheduler ./helm/k8s-scheduler
```

## Usage

Pods opt in to this scheduler by setting:
```yaml
spec:
  schedulerName: owl-scheduler
```

## License

MIT
