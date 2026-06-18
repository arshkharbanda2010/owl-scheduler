package plugins

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func makeScoreNode(name string, cpuMilli, memBytes int64) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"node": name}},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    *resource.NewMilliQuantity(cpuMilli, resource.DecimalSI),
				corev1.ResourceMemory: *resource.NewQuantity(memBytes, resource.DecimalSI),
			},
		},
	}
}

func makeScorePod(name string, cpuMilli, memBytes int64) *corev1.Pod {
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

// --- LeastRequestedPriority Tests ---

func TestLeastRequestedPriority_SpareCapacity(t *testing.T) {
	s := &LeastRequestedPriority{}
	// Node with plenty of resources — should score high.
	node := makeScoreNode("n1", 10000, 10*1024*1024*1024)
	pod := makeScorePod("p1", 100, 100*1024*1024)

	score, err := s.Score(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score <= 0 {
		t.Errorf("expected positive score for node with spare capacity, got %d", score)
	}
	if score > MaxNodeScore {
		t.Errorf("score %d exceeds MaxNodeScore %d", score, MaxNodeScore)
	}
}

func TestLeastRequestedPriority_HighUtilization(t *testing.T) {
	s := &LeastRequestedPriority{}
	// Node with minimal resources — should score low.
	node := makeScoreNode("n1", 100, 100*1024*1024)
	pod := makeScorePod("p1", 10, 10*1024*1024)

	score, err := s.Score(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With tiny allocatable, score should be low (most is "used").
	if score < 0 || score > MaxNodeScore {
		t.Errorf("score %d out of range [0, %d]", score, MaxNodeScore)
	}
}

// --- BalancedResourceAllocation Tests ---

func TestBalancedResourceAllocation_Balanced(t *testing.T) {
	s := &BalancedResourceAllocation{}
	node := makeScoreNode("n1", 4000, 4000*1024*1024) // 4 CPU, 4Gi
	pod := makeScorePod("p1", 1000, 1000*1024*1024)    // 1 CPU, 1Gi (25% each)

	score, err := s.Score(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Perfectly balanced — should score 100.
	if score != MaxNodeScore {
		t.Errorf("expected max score for perfectly balanced node, got %d", score)
	}
}

func TestBalancedResourceAllocation_Imbalanced(t *testing.T) {
	s := &BalancedResourceAllocation{}
	node := makeScoreNode("n1", 4000, 4000*1024*1024)
	pod := makeScorePod("p1", 3000, 500*1024*1024) // 75% CPU, 12.5% memory

	score, err := s.Score(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Imbalanced — should score less than max.
	if score >= MaxNodeScore {
		t.Errorf("expected < max score for imbalanced node, got %d", score)
	}
	if score < 0 {
		t.Errorf("expected non-negative score, got %d", score)
	}
}

// --- NodeAffinityScorer Tests ---

func TestNodeAffinityScorer_NoAffinity(t *testing.T) {
	s := &NodeAffinityScorer{}
	node := makeScoreNode("n1", 0, 0)
	pod := makeScorePod("p1", 0, 0)

	score, err := s.Score(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score != MaxNodeScore/2 {
		t.Errorf("expected neutral score %d, got %d", MaxNodeScore/2, score)
	}
}

func TestNodeAffinityScorer_MatchingAffinity(t *testing.T) {
	s := &NodeAffinityScorer{}
	node := makeScoreNode("n1", 0, 0)
	node.Labels["zone"] = "us-east-1a"
	pod := makeScorePod("p1", 0, 0)
	pod.Spec.Affinity = &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
				{
					Weight: 50,
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{Key: "zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"us-east-1a"}},
						},
					},
				},
			},
		},
	}

	score, err := s.Score(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score != 50 {
		t.Errorf("expected score 50, got %d", score)
	}
}

func TestNodeAffinityScorer_NonMatchingAffinity(t *testing.T) {
	s := &NodeAffinityScorer{}
	node := makeScoreNode("n1", 0, 0)
	node.Labels["zone"] = "us-west-2"
	pod := makeScorePod("p1", 0, 0)
	pod.Spec.Affinity = &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
				{
					Weight: 100,
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{Key: "zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"eu-west-1"}},
						},
					},
				},
			},
		},
	}

	score, err := s.Score(context.Background(), pod, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score != 0 {
		t.Errorf("expected score 0 for non-matching affinity, got %d", score)
	}
}

// --- PrioritySort Tests ---

func TestPrioritySort_HigherFirst(t *testing.T) {
	high := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "high"}, Spec: corev1.PodSpec{Priority: int32Ptr(100)}}
	low := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "low"}, Spec: corev1.PodSpec{Priority: int32Ptr(1)}}

	sorted := SortPods([]*corev1.Pod{low, high})
	if sorted[0].Name != "high" {
		t.Errorf("expected 'high' first, got %s", sorted[0].Name)
	}
	if sorted[1].Name != "low" {
		t.Errorf("expected 'low' second, got %s", sorted[1].Name)
	}
}

func TestPrioritySort_FIFOEqualPriority(t *testing.T) {
	a := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a", CreationTimestamp: metav1.Unix(1000, 0)}, Spec: corev1.PodSpec{Priority: int32Ptr(10)}}
	b := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b", CreationTimestamp: metav1.Unix(500, 0)}, Spec: corev1.PodSpec{Priority: int32Ptr(10)}}

	sorted := SortPods([]*corev1.Pod{a, b})
	if sorted[0].Name != "b" {
		t.Errorf("expected 'b' first (older), got %s", sorted[0].Name)
	}
}

func TestPrioritySort_NoPriority(t *testing.T) {
	a := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a"}}
	b := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b"}}

	sorted := SortPods([]*corev1.Pod{a, b})
	// Both priority 0, sort by name for determinism.
	if sorted[0].Name != "a" {
		t.Errorf("expected 'a' first (alphabetical), got %s", sorted[0].Name)
	}
}

func TestGetPodPriority(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected int32
	}{
		{"nil priority", &corev1.Pod{}, 0},
		{"zero priority", &corev1.Pod{Spec: corev1.PodSpec{Priority: int32Ptr(0)}}, 0},
		{"positive", &corev1.Pod{Spec: corev1.PodSpec{Priority: int32Ptr(42)}}, 42},
		{"negative", &corev1.Pod{Spec: corev1.PodSpec{Priority: int32Ptr(-5)}}, -5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetPodPriority(tt.pod)
			if got != tt.expected {
				t.Errorf("GetPodPriority() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestFormatPriority(t *testing.T) {
	p := &corev1.Pod{Spec: corev1.PodSpec{Priority: int32Ptr(100)}}
	if got := FormatPriority(p); got != "100" {
		t.Errorf("FormatPriority() = %q, want %q", got, "100")
	}
	p2 := &corev1.Pod{}
	if got := FormatPriority(p2); got != "default (0)" {
		t.Errorf("FormatPriority() = %q, want %q", got, "default (0)")
	}
}

func int32Ptr(i int32) *int32 {
	return &i
}
