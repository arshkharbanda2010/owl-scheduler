package plugins

import (
	"context"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// ----- PrioritySort -----

// PrioritySort orders pods by their PriorityClass value (higher first).
type PrioritySort struct{}

func (p *PrioritySort) Name() string { return "PrioritySort" }

// GetPodPriority extracts the priority from a pod's spec. Falls back to 0.
func GetPodPriority(pod *corev1.Pod) int32 {
	if pod.Spec.Priority != nil {
		return *pod.Spec.Priority
	}
	return 0
}

// SortPods sorts pods by priority (highest first), then FIFO by creation timestamp.
func SortPods(pods []*corev1.Pod) []*corev1.Pod {
	sorted := make([]*corev1.Pod, len(pods))
	copy(sorted, pods)

	sort.SliceStable(sorted, func(i, j int) bool {
		pi := GetPodPriority(sorted[i])
		pj := GetPodPriority(sorted[j])
		if pi != pj {
			return pi > pj
		}
		tsI := sorted[i].CreationTimestamp.Time
		tsJ := sorted[j].CreationTimestamp.Time
		if !tsI.Equal(tsJ) {
			return tsI.Before(tsJ)
		}
		return sorted[i].Name < sorted[j].Name
	})

	return sorted
}

// GetPodAge returns the age of a pod.
func GetPodAge(pod *corev1.Pod) time.Duration {
	return time.Since(pod.CreationTimestamp.Time)
}

// FormatPriority returns a human-readable priority string.
func FormatPriority(pod *corev1.Pod) string {
	priority := GetPodPriority(pod)
	if priority == 0 {
		return "default (0)"
	}
	return fmt.Sprintf("%d", priority)
}
