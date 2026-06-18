package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/owl-scheduler/k8s-custom-scheduler/pkg/apis"
)

// Scheduler is the core scheduler that watches for unscheduled pods
// and runs them through the filter → score → bind pipeline.
type Scheduler struct {
	config *apis.SchedulerConfig

	// Clients
	client kubernetes.Interface

	// Scheduling queue
	queue *PriorityQueue

	// Pipeline phases
	filterPlugins []apis.FilterPlugin
	scorePlugins  []apis.ScorePlugin
	binder        apis.Binder

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Worker management
	workerCount int
}

// NewScheduler creates a new Scheduler instance.
func NewScheduler(
	client kubernetes.Interface,
	config *apis.SchedulerConfig,
	filterPlugins []apis.FilterPlugin,
	scorePlugins []apis.ScorePlugin,
	binder apis.Binder,
) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())

	workerCount := config.WorkerCount
	if workerCount <= 0 {
		workerCount = 2
	}

	return &Scheduler{
		config:        config,
		client:        client,
		queue:         NewPriorityQueue(),
		filterPlugins: filterPlugins,
		scorePlugins:  scorePlugins,
		binder:        binder,
		ctx:           ctx,
		cancel:        cancel,
		workerCount:   workerCount,
	}
}

// Run starts the scheduler. It blocks until the context is cancelled.
func (s *Scheduler) Run() error {
	klog.InfoS("Starting scheduler", "name", s.config.SchedulerName, "workers", s.workerCount)

	// Start the pod watcher
	s.startPodWatcher()

	// Start scheduling workers
	for i := 0; i < s.workerCount; i++ {
		s.wg.Add(1)
		go s.worker(i)
	}

	klog.InfoS("Scheduler started successfully", "name", s.config.SchedulerName)

	// Wait for shutdown
	<-s.ctx.Done()
	klog.InfoS("Scheduler shutting down", "name", s.config.SchedulerName)

	// Wait for all workers to finish
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		klog.InfoS("All workers stopped gracefully", "name", s.config.SchedulerName)
	case <-time.After(30 * time.Second):
		klog.ErrorS(nil, "Timed out waiting for workers to stop", "name", s.config.SchedulerName)
	}

	return nil
}

// Stop gracefully shuts down the scheduler.
func (s *Scheduler) Stop() {
	klog.InfoS("Stopping scheduler", "name", s.config.SchedulerName)
	s.cancel()
}

// startPodWatcher starts an informer that watches for unscheduled pods
// targeting this scheduler.
func (s *Scheduler) startPodWatcher() {
	// Field selector: only pods with spec.schedulerName == our scheduler name
	// and that are unscheduled (spec.nodeName == "")
	fieldSelector := fields.AndSelectors(
		fields.OneTermEqualSelector("spec.schedulerName", s.config.SchedulerName),
		fields.OneTermEqualSelector("spec.nodeName", ""),
	)

	// Use a shared informer factory with a field selector
	factory := informers.NewSharedInformerFactoryWithOptions(
		s.client,
		0, // resync period 0 — we don't need periodic resync
	)

	podInformer := factory.Core().V1().Pods().Informer()

	_, err := podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				klog.ErrorS(nil, "Expected Pod object in AddFunc", "type", fmt.Sprintf("%T", obj))
				return
			}
			s.handlePodAdd(pod)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			pod, ok := newObj.(*corev1.Pod)
			if !ok {
				klog.ErrorS(nil, "Expected Pod object in UpdateFunc", "type", fmt.Sprintf("%T", newObj))
				return
			}
			s.handlePodUpdate(oldObj.(*corev1.Pod), pod)
		},
		DeleteFunc: func(obj interface{}) {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				// Handle DeletedFinalStateUnknown
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					return
				}
				pod, ok = tombstone.Obj.(*corev1.Pod)
				if !ok {
					return
				}
			}
			s.handlePodDelete(pod)
		},
	})
	if err != nil {
		klog.ErrorS(err, "Failed to add event handler to pod informer")
		return
	}

	// Start the informer
	factory.Start(s.ctx.Done())

	// Wait for cache sync
	klog.V(3).InfoS("Waiting for pod informer cache sync")
	if !cache.WaitForCacheSync(s.ctx.Done(), podInformer.HasSynced) {
		klog.ErrorS(nil, "Failed to sync pod informer cache")
		return
	}
	klog.V(3).InfoS("Pod informer cache synced")
}

// handlePodAdd handles new pods added to the informer.
func (s *Scheduler) handlePodAdd(pod *corev1.Pod) {
	// Only queue pods that are in Pending phase and have no node assigned
	if pod.Spec.NodeName != "" {
		klog.V(4).InfoS("Skipping pod %s/%s: already assigned to node %s",
			pod.Namespace, pod.Name, pod.Spec.NodeName)
		return
	}
	if pod.DeletionTimestamp != nil {
		klog.V(4).InfoS("Skipping pod %s/%s: being deleted", pod.Namespace, pod.Name)
		return
	}

	klog.V(3).InfoS("Pod %s/%s added to scheduling queue", pod.Namespace, pod.Name)
	s.queue.Add(pod)
}

// handlePodUpdate handles pod updates.
func (s *Scheduler) handlePodUpdate(oldPod, newPod *corev1.Pod) {
	// If the pod got scheduled (nodeName changed from empty to something), remove from queue
	if oldPod.Spec.NodeName == "" && newPod.Spec.NodeName != "" {
		klog.V(3).InfoS("Pod %s/%s was scheduled to node %s, removing from queue",
			newPod.Namespace, newPod.Name, newPod.Spec.NodeName)
		s.queue.Remove(newPod)
		return
	}

	// If the pod is still unscheduled and pending, ensure it's in the queue
	if newPod.Spec.NodeName == "" && newPod.DeletionTimestamp == nil {
		s.queue.Add(newPod)
	}
}

// handlePodDelete handles pod deletions.
func (s *Scheduler) handlePodDelete(pod *corev1.Pod) {
	klog.V(3).InfoS("Pod %s/%s deleted, removing from queue", pod.Namespace, pod.Name)
	s.queue.Remove(pod)
}

// worker is a scheduling worker that continuously pops pods from the queue
// and schedules them.
func (s *Scheduler) worker(id int) {
	defer s.wg.Done()
	klog.V(3).InfoS("Scheduling worker %d started", id)

	wait.Until(func() {
		pod := s.queue.Pop()
		if pod == nil {
			return
		}

		klog.V(3).InfoS("Worker %d: processing pod %s/%s", id, pod.Namespace, pod.Name)

		if err := s.schedulePod(s.ctx, pod); err != nil {
			klog.ErrorS(err, "Failed to schedule pod",
				"worker", id,
				"pod", pod.Namespace+"/"+pod.Name)
			// Re-queue the pod for retry
			s.queue.Add(pod)
		}
	}, 100*time.Millisecond, s.ctx.Done())

	klog.V(3).InfoS("Scheduling worker %d stopped", id)
}

// schedulePod runs the full filter → score → bind pipeline for a single pod.
func (s *Scheduler) schedulePod(ctx context.Context, pod *corev1.Pod) error {
	klog.V(3).InfoS("Scheduling pod %s/%s", pod.Namespace, pod.Name)

	// Get the list of all nodes
	nodeList, err := s.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	if len(nodeList.Items) == 0 {
		return fmt.Errorf("no nodes available for scheduling")
	}

	// Phase 1: Filter
	klog.V(4).InfoS("Running filter phase for pod %s/%s", pod.Namespace, pod.Name)
	filteredNodes := s.runFilterPhase(ctx, pod, nodeList.Items)
	if len(filteredNodes) == 0 {
		return fmt.Errorf("no nodes passed filter phase for pod %s/%s", pod.Namespace, pod.Name)
	}
	klog.V(3).InfoS("Filter phase passed: %d/%d nodes eligible for pod %s/%s",
		len(filteredNodes), len(nodeList.Items), pod.Namespace, pod.Name)

	// Phase 2: Score
	klog.V(4).InfoS("Running score phase for pod %s/%s", pod.Namespace, pod.Name)
	bestNode, err := s.runScorePhase(ctx, pod, filteredNodes)
	if err != nil {
		return fmt.Errorf("scoring phase failed for pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	klog.V(3).InfoS("Score phase complete: best node for pod %s/%s is %s",
		pod.Namespace, pod.Name, bestNode)

	// Phase 3: Bind
	klog.V(4).InfoS("Running bind phase for pod %s/%s to node %s", pod.Namespace, pod.Name, bestNode)
	if err := s.binder.Bind(ctx, pod, bestNode); err != nil {
		return fmt.Errorf("bind phase failed for pod %s/%s to node %s: %w",
			pod.Namespace, pod.Name, bestNode, err)
	}

	klog.InfoS("Successfully scheduled pod",
		"pod", pod.Namespace+"/"+pod.Name,
		"node", bestNode)

	return nil
}

// runFilterPhase runs all filter plugins against the given nodes.
// Returns the list of nodes that passed all filters.
func (s *Scheduler) runFilterPhase(ctx context.Context, pod *corev1.Pod, nodes []corev1.Node) []corev1.Node {
	var filtered []corev1.Node

	for i := range nodes {
		node := &nodes[i]
		passed := true

		for _, plugin := range s.filterPlugins {
			result, err := plugin.Filter(ctx, pod, node)
			if err != nil {
				klog.ErrorS(err, "Filter plugin error",
					"plugin", plugin.Name(),
					"pod", pod.Namespace+"/"+pod.Name,
					"node", node.Name)
				passed = false
				break
			}
			if !result {
				klog.V(4).InfoS("Node %s filtered out by plugin %s for pod %s/%s",
					node.Name, plugin.Name(), pod.Namespace, pod.Name)
				passed = false
				break
			}
		}

		if passed {
			filtered = append(filtered, *node)
		}
	}

	return filtered
}

// runScorePhase runs all score plugins and returns the name of the best-scoring node.
// If there is a tie, the first node with the highest score wins.
func (s *Scheduler) runScorePhase(ctx context.Context, pod *corev1.Pod, nodes []corev1.Node) (string, error) {
	if len(nodes) == 0 {
		return "", fmt.Errorf("no nodes to score")
	}

	type nodeScore struct {
		name  string
		score int64
	}

	scores := make([]nodeScore, 0, len(nodes))

	for i := range nodes {
		node := &nodes[i]
		var totalScore int64

		for _, plugin := range s.scorePlugins {
			score, err := plugin.Score(ctx, pod, node)
			if err != nil {
				klog.ErrorS(err, "Score plugin error",
					"plugin", plugin.Name(),
					"pod", pod.Namespace+"/"+pod.Name,
					"node", node.Name)
				return "", fmt.Errorf("score plugin %s failed for node %s: %w", plugin.Name(), node.Name, err)
			}
			totalScore += score
		}

		scores = append(scores, nodeScore{name: node.Name, score: totalScore})
		klog.V(4).InfoS("Node %s scored %d for pod %s/%s", node.Name, totalScore, pod.Namespace, pod.Name)
	}

	// Find the node with the highest score
	best := scores[0]
	for _, ns := range scores[1:] {
		if ns.score > best.score {
			best = ns
		}
	}

	return best.name, nil
}
