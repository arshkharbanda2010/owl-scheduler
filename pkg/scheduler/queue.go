package scheduler

import (
	"container/heap"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// podItem wraps a pod with metadata for the priority queue.
type podItem struct {
	pod      *corev1.Pod
	priority int32
	index    int // index in the heap, maintained by heap.Interface
}

// PriorityQueue implements heap.Interface and holds podItems.
// Pods with higher priority values are dequeued first.
type PriorityQueue struct {
	items []*podItem
	mu    sync.Mutex
	// lookup tracks pods currently in the queue by namespace/name to avoid duplicates.
	lookup map[string]struct{}
}

// NewPriorityQueue creates and initializes a new PriorityQueue.
func NewPriorityQueue() *PriorityQueue {
	pq := &PriorityQueue{
		items:  make([]*podItem, 0),
		lookup: make(map[string]struct{}),
	}
	heap.Init(pq)
	return pq
}

// Len returns the number of items in the queue.
func (pq *PriorityQueue) Len() int {
	return len(pq.items)
}

// Less defines the ordering: higher priority first (max-heap).
func (pq *PriorityQueue) Less(i, j int) bool {
	return pq.items[i].priority > pq.items[j].priority
}

// Swap swaps two items in the queue.
func (pq *PriorityQueue) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
	pq.items[i].index = i
	pq.items[j].index = j
}

// Push adds an item to the queue. Called by heap.Push.
func (pq *PriorityQueue) Push(x interface{}) {
	n := len(pq.items)
	item := x.(*podItem)
	item.index = n
	pq.items = append(pq.items, item)
}

// Pop removes and returns the highest-priority item. Called by heap.Pop.
func (pq *PriorityQueue) Pop() interface{} {
	old := pq.items
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	item.index = -1
	pq.items = old[:n-1]
	return item
}

// podKey returns a unique key for a pod.
func podKey(pod *corev1.Pod) string {
	return pod.Namespace + "/" + pod.Name
}

// Add inserts a pod into the priority queue. If the pod is already queued,
// it updates the priority and re-heapifies.
func (pq *PriorityQueue) Add(pod *corev1.Pod) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	key := podKey(pod)
	priority := getPodPriority(pod)

	if _, exists := pq.lookup[key]; exists {
		klog.V(4).InfoS("Pod %s already in scheduling queue, updating priority", key)
		// Find and update the existing item
		for _, item := range pq.items {
			if podKey(item.pod) == key {
				item.priority = priority
				heap.Fix(pq, item.index)
				return
			}
		}
	}

	item := &podItem{
		pod:      pod,
		priority: priority,
	}
	heap.Push(pq, item)
	pq.lookup[key] = struct{}{}
	klog.V(4).InfoS("Pod %s added to scheduling queue with priority %d", key, priority)
}

// Pop removes and returns the highest-priority pod from the queue.
// Returns nil if the queue is empty.
func (pq *PriorityQueue) Pop() *corev1.Pod {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if pq.Len() == 0 {
		return nil
	}

	item := heap.Pop(pq).(*podItem)
	delete(pq.lookup, podKey(item.pod))
	klog.V(4).InfoS("Pod %s popped from scheduling queue (priority %d)", podKey(item.pod), item.priority)
	return item.pod
}

// Remove removes a pod from the priority queue.
func (pq *PriorityQueue) Remove(pod *corev1.Pod) bool {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	key := podKey(pod)
	if _, exists := pq.lookup[key]; !exists {
		return false
	}

	for _, item := range pq.items {
		if podKey(item.pod) == key {
			heap.Remove(pq, item.index)
			delete(pq.lookup, key)
			klog.V(4).InfoS("Pod %s removed from scheduling queue", key)
			return true
		}
	}
	return false
}

// Has returns true if the pod is in the queue.
func (pq *PriorityQueue) Has(pod *corev1.Pod) bool {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	_, exists := pq.lookup[podKey(pod)]
	return exists
}

// getPodPriority extracts the priority from a pod's spec.
// Falls back to 0 if not set.
func getPodPriority(pod *corev1.Pod) int32 {
	if pod.Spec.Priority != nil {
		return *pod.Spec.Priority
	}
	return 0
}
