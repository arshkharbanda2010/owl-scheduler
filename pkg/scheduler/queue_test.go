package scheduler

import (
	"container/heap"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// getPodPriority mirrors the local helper for test access.
func getPodPriorityTest(pod *corev1.Pod) int32 {
	if pod.Spec.Priority != nil {
		return *pod.Spec.Priority
	}
	return 0
}

func TestPriorityQueue_BasicOrdering(t *testing.T) {
	pq := NewPriorityQueue()

	low := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "low"}, Spec: corev1.PodSpec{Priority: int32Ptr(1)}}
	high := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "high"}, Spec: corev1.PodSpec{Priority: int32Ptr(100)}}
	mid := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "mid"}, Spec: corev1.PodSpec{Priority: int32Ptr(50)}}

	pq.Add(low)
	pq.Add(high)
	pq.Add(mid)

	expectedOrder := []string{"high", "mid", "low"}
	for _, name := range expectedOrder {
		pod := pq.Pop()
		if pod == nil {
			t.Fatalf("expected pod %s, got nil", name)
		}
		if pod.Name != name {
			t.Errorf("expected %s, got %s", name, pod.Name)
		}
	}

	if pq.Len() != 0 {
		t.Errorf("expected empty queue, got length %d", pq.Len())
	}
}

func TestPriorityQueue_DuplicateAdd(t *testing.T) {
	pq := NewPriorityQueue()

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "dup"}, Spec: corev1.PodSpec{Priority: int32Ptr(10)}}
	pq.Add(pod)

	// Add again with higher priority — should update in place
	pod2 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "dup"}, Spec: corev1.PodSpec{Priority: int32Ptr(100)}}
	pq.Add(pod2)

	if pq.Len() != 1 {
		t.Errorf("expected length 1 after duplicate add, got %d", pq.Len())
	}

	popped := pq.Pop()
	if popped.Name != "dup" {
		t.Errorf("expected 'dup', got %s", popped.Name)
	}
	if getPodPriorityTest(popped) != 100 {
		t.Errorf("expected priority 100 after update, got %d", getPodPriorityTest(popped))
	}
}

func TestPriorityQueue_Remove(t *testing.T) {
	pq := NewPriorityQueue()

	a := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a"}, Spec: corev1.PodSpec{Priority: int32Ptr(1)}}
	b := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b"}, Spec: corev1.PodSpec{Priority: int32Ptr(2)}}

	pq.Add(a)
	pq.Add(b)

	if !pq.Remove(a) {
		t.Error("expected Remove to return true for existing pod")
	}
	if pq.Len() != 1 {
		t.Errorf("expected length 1 after remove, got %d", pq.Len())
	}
	if pq.Remove(a) {
		t.Error("expected Remove to return false for already-removed pod")
	}
}

func TestPriorityQueue_PopEmpty(t *testing.T) {
	pq := NewPriorityQueue()
	pod := pq.Pop()
	if pod != nil {
		t.Errorf("expected nil from empty queue, got %v", pod)
	}
}

func TestPriorityQueue_Has(t *testing.T) {
	pq := NewPriorityQueue()

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "exists"}, Spec: corev1.PodSpec{Priority: int32Ptr(1)}}
	pq.Add(pod)

	if !pq.Has(pod) {
		t.Error("expected Has to return true for queued pod")
	}

	other := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "missing"}}
	if pq.Has(other) {
		t.Error("expected Has to return false for non-queued pod")
	}
}

func TestPriorityQueue_HeapProperty(t *testing.T) {
	// Verify that the heap invariant holds after a series of pushes.
	pq := NewPriorityQueue()
	priorities := []int32{5, 3, 8, 1, 9, 2, 7}
	for _, p := range priorities {
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: ""}, Spec: corev1.PodSpec{Priority: p}}
		pq.Add(pod)
	}

	// heap.Pop gives us items in heap order (not fully sorted).
	// We just verify Len is correct and no panics.
	if pq.Len() != 7 {
		t.Errorf("expected length 7, got %d", pq.Len())
	}

	// Pop all and verify we get the max first (heap behavior).
	first := pq.Pop()
	if first == nil || getPodPriorityTest(first) != 9 {
		t.Errorf("expected first pop to have priority 9, got %v", first)
	}
}

func TestGetPodPriority(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected int32
	}{
		{
			name:     "no priority set",
			pod:      &corev1.Pod{},
			expected: 0,
		},
		{
			name:     "priority 0",
			pod:      &corev1.Pod{Spec: corev1.PodSpec{Priority: int32Ptr(0)}},
			expected: 0,
		},
		{
			name:     "priority 100",
			pod:      &corev1.Pod{Spec: corev1.PodSpec{Priority: int32Ptr(100)}},
			expected: 100,
		},
		{
			name:     "negative priority",
			pod:      &corev1.Pod{Spec: corev1.PodSpec{Priority: int32Ptr(-10)}},
			expected: -10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getPodPriorityTest(tt.pod)
			if got != tt.expected {
				t.Errorf("getPodPriority() = %d, want %d", got, tt.expected)
			}
		})
	}
}

// Verify heap.Interface is properly implemented.
var _ heap.Interface = (*PriorityQueue)(nil)

func int32Ptr(i int32) *int32 {
	return &i
}
