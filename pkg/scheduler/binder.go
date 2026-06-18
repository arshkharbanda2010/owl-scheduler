package scheduler

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/owl-scheduler/k8s-custom-scheduler/pkg/apis"
)

// defaultBinder implements the Binder interface using the Kubernetes API.
type defaultBinder struct {
	client        kubernetes.Interface
	maxRetries    int
	retryInterval time.Duration
}

// NewDefaultBinder creates a new defaultBinder with the given client and config.
func NewDefaultBinder(client kubernetes.Interface, config *apis.SchedulerConfig) *defaultBinder {
	maxRetries := config.MaxRetryAttempts
	if maxRetries <= 0 {
		maxRetries = 3
	}
	return &defaultBinder{
		client:        client,
		maxRetries:    maxRetries,
		retryInterval: 1 * time.Second,
	}
}

// Bind binds the specified pod to the given node by creating a Binding subresource.
// It retries on transient failures up to maxRetries times.
func (b *defaultBinder) Bind(ctx context.Context, pod *corev1.Pod, node string) error {
	binding := &corev1.Binding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		Target: corev1.ObjectReference{
			Kind:       "Node",
			APIVersion: "v1",
			Name:       node,
		},
	}

	var lastErr error
	for attempt := 1; attempt <= b.maxRetries; attempt++ {
		klog.V(3).InfoS("Attempting to bind pod %s/%s to node %s (attempt %d/%d)",
			pod.Namespace, pod.Name, node, attempt, b.maxRetries)

		err := b.client.CoreV1().Pods(pod.Namespace).Bind(ctx, binding, metav1.CreateOptions{})
		if err == nil {
			klog.InfoS("Successfully bound pod to node",
				"pod", pod.Namespace+"/"+pod.Name,
				"node", node,
				"attempt", attempt)
			return nil
		}

		lastErr = err

		// Do not retry on certain errors
		if errors.IsNotFound(err) {
			return fmt.Errorf("pod %s/%s not found, will not retry: %w", pod.Namespace, pod.Name, err)
		}
		if errors.IsConflict(err) {
			// Pod may have already been scheduled; check if it's bound
			klog.V(3).InfoS("Conflict binding pod %s/%s, pod may already be scheduled: %v",
				pod.Namespace, pod.Name, err)
			return fmt.Errorf("conflict binding pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
		if errors.IsInvalid(err) {
			return fmt.Errorf("invalid binding for pod %s/%s, will not retry: %w", pod.Namespace, pod.Name, err)
		}

		klog.ErrorS(err, "Failed to bind pod to node, will retry",
			"pod", pod.Namespace+"/"+pod.Name,
			"node", node,
			"attempt", attempt,
			"maxRetries", b.maxRetries)

		if attempt < b.maxRetries {
			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled while retrying bind for pod %s/%s: %w",
					pod.Namespace, pod.Name, ctx.Err())
			case <-time.After(b.retryInterval):
				// Exponential backoff
				b.retryInterval *= 2
			}
		}
	}

	return fmt.Errorf("failed to bind pod %s/%s to node %s after %d attempts: %w",
		pod.Namespace, pod.Name, node, b.maxRetries, lastErr)
}
