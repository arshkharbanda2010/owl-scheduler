package plugins

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func makeFilterNode(name string, cpuMilli, memBytes int64) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.NodeSpec{},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    *resource.NewMilliQuantity(cpuMilli, resource.DecimalSI),
				corev1.ResourceMemory: *resource.NewQuantity(memBytes, resource.DecimalSI),
			},
		},
	}
}

func makeFilterPod(name string, cpuMilli, memBytes int64) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
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
		},
	}
}

// --- NodeResourcesFit Tests ---

func TestNodeResourcesFit_Fits(t *testing.T) {
	f := &NodeResourcesFit{}
	node := makeFilterNode("n1", 2000, 4*1024*1024*1024) // 2 CPU, 4Gi
	pod := makeFilterPod("p1", 1000, 1*1024*1024*1024)    // 1 CPU, 1Gi

	ok, err := f.Filter(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected pod to fit")
	}
}

func TestNodeResourcesFit_CPUExceeds(t *testing.T) {
	f := &NodeResourcesFit{}
	node := makeFilterNode("n1", 1000, 4*1024*1024*1024) // 1 CPU, 4Gi
	pod := makeFilterPod("p1", 2000, 1*1024*1024*1024)    // 2 CPU, 1Gi

	ok, err := f.Filter(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected pod to not fit (CPU)")
	}
}

func TestNodeResourcesFit_MemoryExceeds(t *testing.T) {
	f := &NodeResourcesFit{}
	node := makeFilterNode("n1", 4000, 1*1024*1024*1024) // 4 CPU, 1Gi
	pod := makeFilterPod("p1", 1000, 2*1024*1024*1024)    // 1 CPU, 2Gi

	ok, err := f.Filter(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected pod to not fit (memory)")
	}
}

func TestNodeResourcesFit_NoRequests(t *testing.T) {
	f := &NodeResourcesFit{}
	node := makeFilterNode("n1", 1000, 1*1024*1024*1024)
	pod := makeFilterPod("p1", 0, 0) // no resource requests

	ok, err := f.Filter(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected pod with no requests to fit")
	}
}

func TestNodeResourcesFit_InitContainers(t *testing.T) {
	f := &NodeResourcesFit{}
	node := makeFilterNode("n1", 2000, 2*1024*1024*1024)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "main",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    *resource.NewMilliQuantity(500, resource.DecimalSI),
							corev1.ResourceMemory: *resource.NewQuantity(512*1024*1024, resource.DecimalSI),
						},
					},
				},
			},
			InitContainers: []corev1.Container{
				{
					Name: "init",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    *resource.NewMilliQuantity(1500, resource.DecimalSI),
							corev1.ResourceMemory: *resource.NewQuantity(1536*1024*1024, resource.DecimalSI),
						},
					},
				},
			},
		},
	}

	// Init container requests (1500m CPU) exceed regular (500m), so init dominates.
	// Node has 2000m CPU, init needs 1500m — fits.
	ok, err := f.Filter(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected pod to fit (init containers fit)")
	}
}

// --- NodeNameFilter Tests ---

func TestNodeNameFilter_NoNodeName(t *testing.T) {
	f := &NodeNameFilter{}
	pod := makeFilterPod("p1", 0, 0)
	pod.Spec.NodeName = ""
	node := makeFilterNode("any", 0, 0)

	ok, err := f.Filter(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected pass when no nodeName specified")
	}
}

func TestNodeNameFilter_Match(t *testing.T) {
	f := &NodeNameFilter{}
	pod := makeFilterPod("p1", 0, 0)
	pod.Spec.NodeName = "target-node"
	node := makeFilterNode("target-node", 0, 0)

	ok, err := f.Filter(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected pass for matching nodeName")
	}
}

func TestNodeNameFilter_NoMatch(t *testing.T) {
	f := &NodeNameFilter{}
	pod := makeFilterPod("p1", 0, 0)
	pod.Spec.NodeName = "target-node"
	node := makeFilterNode("other-node", 0, 0)

	ok, err := f.Filter(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected fail for non-matching nodeName")
	}
}

// --- NodeUnschedulableFilter Tests ---

func TestNodeUnschedulableFilter_Schedulable(t *testing.T) {
	f := &NodeUnschedulableFilter{}
	node := makeFilterNode("n1", 0, 0)
	node.Spec.Unschedulable = false
	pod := makeFilterPod("p1", 0, 0)

	ok, err := f.Filter(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected pass for schedulable node")
	}
}

func TestNodeUnschedulableFilter_UnschedulableNoToleration(t *testing.T) {
	f := &NodeUnschedulableFilter{}
	node := makeFilterNode("n1", 0, 0)
	node.Spec.Unschedulable = true
	pod := makeFilterPod("p1", 0, 0)

	ok, err := f.Filter(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected fail for unschedulable node without toleration")
	}
}

func TestNodeUnschedulableFilter_UnschedulableWithToleration(t *testing.T) {
	f := &NodeUnschedulableFilter{}
	node := makeFilterNode("n1", 0, 0)
	node.Spec.Unschedulable = true
	pod := makeFilterPod("p1", 0, 0)
	pod.Spec.Tolerations = []corev1.Toleration{
		{Key: "node.kubernetes.io/unschedulable", Effect: corev1.TaintEffectNoSchedule},
	}

	ok, err := f.Filter(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected pass for unschedulable node with toleration")
	}
}

// --- TaintTolerationFilter Tests ---

func TestTaintTolerationFilter_NoTaints(t *testing.T) {
	f := &TaintTolerationFilter{}
	node := makeFilterNode("n1", 0, 0)
	pod := makeFilterPod("p1", 0, 0)

	ok, err := f.Filter(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected pass for node with no taints")
	}
}

func TestTaintTolerationFilter_ToleratedTaint(t *testing.T) {
	f := &TaintTolerationFilter{}
	node := makeFilterNode("n1", 0, 0)
	node.Spec.Taints = []corev1.Taint{
		{Key: "dedicated", Value: "special", Effect: corev1.TaintEffectNoSchedule},
	}
	pod := makeFilterPod("p1", 0, 0)
	pod.Spec.Tolerations = []corev1.Toleration{
		{Key: "dedicated", Value: "special", Effect: corev1.TaintEffectNoSchedule},
	}

	ok, err := f.Filter(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected pass for tolerated taint")
	}
}

func TestTaintTolerationFilter_UntoleratedTaint(t *testing.T) {
	f := &TaintTolerationFilter{}
	node := makeFilterNode("n1", 0, 0)
	node.Spec.Taints = []corev1.Taint{
		{Key: "dedicated", Value: "special", Effect: corev1.TaintEffectNoSchedule},
	}
	pod := makeFilterPod("p1", 0, 0)

	ok, err := f.Filter(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected fail for untolerated taint")
	}
}

func TestTaintTolerationFilter_NoExecuteIgnored(t *testing.T) {
	f := &TaintTolerationFilter{}
	node := makeFilterNode("n1", 0, 0)
	node.Spec.Taints = []corev1.Taint{
		{Key: "node.kubernetes.io/unreachable", Effect: corev1.TaintEffectNoExecute},
	}
	pod := makeFilterPod("p1", 0, 0)

	ok, err := f.Filter(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected pass for NoExecute taint (handled at runtime)")
	}
}

func TestTaintTolerationFilter_WildcardToleration(t *testing.T) {
	f := &TaintTolerationFilter{}
	node := makeFilterNode("n1", 0, 0)
	node.Spec.Taints = []corev1.Taint{
		{Key: "any-key", Effect: corev1.TaintEffectNoSchedule},
	}
	pod := makeFilterPod("p1", 0, 0)
	pod.Spec.Tolerations = []corev1.Toleration{
		{Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
	}

	ok, err := f.Filter(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected pass for wildcard toleration")
	}
}
