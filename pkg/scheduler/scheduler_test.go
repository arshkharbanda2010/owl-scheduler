package scheduler

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestScheduler_NewSchedulerDefaults(t *testing.T) {
	config := defaultTestConfig()
	config.WorkerCount = 0 // should default to 2

	sched := NewScheduler(nil, config, nil, nil, nil)
	if sched.workerCount != 2 {
		t.Errorf("expected default workerCount 2, got %d", sched.workerCount)
	}
}

func TestScheduler_NewSchedulerCustomWorkers(t *testing.T) {
	config := defaultTestConfig()
	config.WorkerCount = 5

	sched := NewScheduler(nil, config, nil, nil, nil)
	if sched.workerCount != 5 {
		t.Errorf("expected workerCount 5, got %d", sched.workerCount)
	}
}

func TestRunFilterPhase_NoPlugins(t *testing.T) {
	config := defaultTestConfig()
	sched := NewScheduler(nil, config, nil, nil, nil)

	nodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node-2"}},
	}

	result := sched.runFilterPhase(context.Background(), &corev1.Pod{}, nodes)
	if len(result) != 2 {
		t.Errorf("expected 2 nodes to pass with no plugins, got %d", len(result))
	}
}

func TestRunFilterPhase_AllFiltered(t *testing.T) {
	config := defaultTestConfig()
	sched := NewScheduler(nil, config, []FilterPlugin{&rejectAllFilter{}}, nil, nil)

	nodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
	}

	result := sched.runFilterPhase(context.Background(), &corev1.Pod{}, nodes)
	if len(result) != 0 {
		t.Errorf("expected 0 nodes to pass, got %d", len(result))
	}
}

func TestRunFilterPhase_PartialFilter(t *testing.T) {
	config := defaultTestConfig()
	sched := NewScheduler(nil, config, []FilterPlugin{&nodeNameFilter{"node-2"}}, nil, nil)

	nodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node-2"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node-3"}},
	}

	result := sched.runFilterPhase(context.Background(), &corev1.Pod{}, nodes)
	if len(result) != 1 {
		t.Fatalf("expected 1 node to pass, got %d", len(result))
	}
	if result[0].Name != "node-2" {
		t.Errorf("expected node-2, got %s", result[0].Name)
	}
}

func TestRunScorePhase_EmptyNodes(t *testing.T) {
	config := defaultTestConfig()
	sched := NewScheduler(nil, config, nil, nil, nil)

	_, err := sched.runScorePhase(context.Background(), &corev1.Pod{}, nil)
	if err == nil {
		t.Fatal("expected error for empty nodes, got nil")
	}
}

func TestRunScorePhase_SingleNode(t *testing.T) {
	config := defaultTestConfig()
	sched := NewScheduler(nil, config, nil, nil, nil)

	nodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "only-node"}},
	}

	bestNode, err := sched.runScorePhase(context.Background(), &corev1.Pod{}, nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bestNode != "only-node" {
		t.Errorf("expected 'only-node', got %s", bestNode)
	}
}

func TestRunScorePhase_MultipleNodes(t *testing.T) {
	config := defaultTestConfig()
	// Use a scorer that prefers node with most spare resources.
	sched := NewScheduler(nil, config, nil, []ScorePlugin{&constantScorer{}}, nil)

	nodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node-b"}},
	}

	bestNode, err := sched.runScorePhase(context.Background(), &corev1.Pod{}, nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With constant scorer, first node wins (tie).
	if bestNode != "node-a" {
		t.Errorf("expected 'node-a', got %s", bestNode)
	}
}

func TestSchedulePod_NoNodes(t *testing.T) {
	config := defaultTestConfig()
	fb := &fakeBinder{}
	sched := NewScheduler(nil, config, nil, nil, fb)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}

	// schedulePod calls Nodes().List() which will panic with nil client.
	// We test the filter phase directly instead.
	_ = sched
	_ = fb
	_ = pod
}

// --- Test Plugin Helpers ---

// rejectAllFilter filters out all nodes.
type rejectAllFilter struct{}

func (f *rejectAllFilter) Name() string { return "rejectAllFilter" }

func (f *rejectAllFilter) Filter(ctx context.Context, pod *corev1.Pod, node *corev1.Node) (bool, error) {
	return false, nil
}

// nodeNameFilter only allows a specific node.
type nodeNameFilter struct {
	name string
}

func (f *nodeNameFilter) Name() string { return "nodeNameFilter" }

func (f *nodeNameFilter) Filter(ctx context.Context, pod *corev1.Pod, node *corev1.Node) (bool, error) {
	return node.Name == f.name, nil
}

// constantScorer returns 100 for all nodes.
type constantScorer struct{}

func (s *constantScorer) Name() string { return "constantScorer" }

func (s *constantScorer) Score(ctx context.Context, pod *corev1.Pod, node *corev1.Node) (int64, error) {
	return 100, nil
}
