package apis

import (
	"context"

	corev1 "k8s.io/api/core/v1"
)

// SchedulerName is the name of the custom scheduler.
const SchedulerName = "owl-scheduler"

// PodInfo wraps a pod with metadata useful for scheduling.
type PodInfo struct {
	Pod       *corev1.Pod
	Namespace string
	Name      string
}

// NodeScore pairs a node name with its score from the scoring phase.
type NodeScore struct {
	Name  string
	Score int64
}

// FilterPlugin is the interface for filter plugins.
// A filter plugin determines whether a pod can be scheduled on a given node.
type FilterPlugin interface {
	// Name returns the name of the filter plugin.
	Name() string
	// Filter returns true if the pod can be scheduled on the node.
	Filter(ctx context.Context, pod *corev1.Pod, node *corev1.Node) (bool, error)
}

// ScorePlugin is the interface for scoring plugins.
// A score plugin ranks nodes that passed the filter phase.
type ScorePlugin interface {
	// Name returns the name of the score plugin.
	Name() string
	// Score returns a score for the given pod on the given node.
	Score(ctx context.Context, pod *corev1.Pod, node *corev1.Node) (int64, error)
}

// Binder is the interface for binding a pod to a node.
type Binder interface {
	// Bind binds the pod to the specified node.
	Bind(ctx context.Context, pod *corev1.Pod, node string) error
}

// SchedulerConfig holds configuration for the scheduler.
type SchedulerConfig struct {
	// SchedulerName is the name of the scheduler to match against spec.schedulerName.
	SchedulerName string `json:"schedulerName" yaml:"schedulerName"`
	// BindTimeout is the timeout for bind operations.
	BindTimeoutSeconds int `json:"bindTimeoutSeconds" yaml:"bindTimeoutSeconds"`
	// MaxRetryAttempts is the maximum number of retries for failed binds.
	MaxRetryAttempts int `json:"maxRetryAttempts" yaml:"maxRetryAttempts"`
	// WorkerCount is the number of concurrent scheduling workers.
	WorkerCount int `json:"workerCount" yaml:"workerCount"`
}

// DefaultConfig returns a SchedulerConfig with sensible defaults.
func DefaultConfig() *SchedulerConfig {
	return &SchedulerConfig{
		SchedulerName:      SchedulerName,
		BindTimeoutSeconds: 60,
		MaxRetryAttempts:   3,
		WorkerCount:        2,
	}
}
