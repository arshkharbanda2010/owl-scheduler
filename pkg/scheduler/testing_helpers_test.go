package scheduler

import (
	"context"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// --- Fake Binder for Testing ---

type fakeBinder struct {
	mu       sync.Mutex
	binds    []bindCall
	bindErr  error
}

type bindCall struct {
	pod  *corev1.Pod
	node string
}

func (fb *fakeBinder) Bind(ctx context.Context, pod *corev1.Pod, node string) error {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	fb.binds = append(fb.binds, bindCall{pod: pod, node: node})
	if fb.bindErr != nil {
		return fb.bindErr
	}
	return nil
}

// --- Test Helpers ---

func defaultTestConfig() *SchedulerConfig {
	return &SchedulerConfig{
		SchedulerName:      "owl-scheduler",
		BindTimeoutSeconds: 10,
		MaxRetryAttempts:   1,
		WorkerCount:        1,
	}
}

func makeTestNode(name string, cpuMilli, memBytes int64) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.NewMilliQuantity(cpuMilli, resource.DecimalSI),
				corev1.ResourceMemory: resource.NewQuantity(memBytes, resource.DecimalSI),
			},
		},
	}
}

func makeTestPod(name, namespace string, cpuMilli, memBytes int64, priority int32) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "main",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    *resource.NewMilliQuantity(cpuMilli, resource.DecimalSI),
							corev1.ResourceMemory: *resource.NewQuantity(memBytes, resource.DecimalSI),
						},
					},
				},
			},
			Priority: &priority,
		},
	}
	return p
}

func int32PtrTest(i int32) *int32 {
	return &i
}
