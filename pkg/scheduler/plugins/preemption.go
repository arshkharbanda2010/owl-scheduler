package plugins

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

// Preemption handles evicting lower-priority pods so higher-priority pods can schedule.
type Preemption struct {
	client kubernetes.Interface
}

// NewPreemption creates a new Preemption plugin.
func NewPreemption(client kubernetes.Interface) *Preemption {
	return &Preemption{client: client}
}

func (p *Preemption) Name() string { return "Preemption" }

// NodeResources represents the CPU and memory usage on a node.
type NodeResources struct {
	AllocatableCPU    int64
	AllocatableMemory int64
	UsedCPU           int64
	UsedMemory        int64
}

// CalculateNodeResources computes resource usage for a node from its pod list.
func CalculateNodeResources(node *corev1.Node, pods []*corev1.Pod) NodeResources {
	var usedCPU, usedMemory int64
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil {
			continue
		}
		for _, c := range pod.Spec.Containers {
			if req, ok := c.Resources.Requests[corev1.ResourceCPU]; ok {
				usedCPU += req.MilliValue()
			}
			if req, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
				usedMemory += req.Value()
			}
		}
	}
	return NodeResources{
		AllocatableCPU:    node.Status.Allocatable.Cpu().MilliValue(),
		AllocatableMemory: node.Status.Allocatable.Memory().Value(),
		UsedCPU:           usedCPU,
		UsedMemory:        usedMemory,
	}
}

// GetCandidates returns pods on the node that could be evicted (lower priority than preemptor).
func (p *Preemption) GetCandidates(ctx context.Context, preemptor *corev1.Pod, node *corev1.Node, nodePods []*corev1.Pod) []*corev1.Pod {
	preemptorPriority := GetPodPriority(preemptor)
	var candidates []*corev1.Pod

	for _, pod := range nodePods {
		if pod.DeletionTimestamp != nil {
			continue
		}
		if GetPodPriority(pod) >= preemptorPriority {
			continue
		}
		candidates = append(candidates, pod)
	}

	// Sort: lowest priority first, then oldest first.
	sort.SliceStable(candidates, func(i, j int) bool {
		pi := GetPodPriority(candidates[i])
		pj := GetPodPriority(candidates[j])
		if pi != pj {
			return pi < pj
		}
		return candidates[i].CreationTimestamp.Time.Before(candidates[j].CreationTimestamp.Time)
	})

	return candidates
}

// CheckPDB checks whether evicting the given pods would violate any PodDisruptionBudget.
func (p *Preemption) CheckPDB(ctx context.Context, pods []*v1.Pod, namespace string) (bool, error) {
	pdbs, err := p.client.PolicyV1().PodDisruptionBudgets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, err
	}

	for _, pod := range pods {
		for i := range pdbs.Items {
			pdb := &pdbs.Items[i]
			if pdb.Spec.Selector == nil {
				continue
			}
			selector, err := metav1.LabelSelectorAsSelector(pdb.Spec.Selector)
			if err != nil {
				continue
			}
			if !selector.Matches(labels.Set(pod.Labels)) {
				continue
			}
			if pdb.Status.DisruptionsAllowed <= 0 {
				klog.V(3).InfoS("Eviction blocked by PDB", "pdb", pdb.Name, "pod", pod.Name)
				return false, nil
			}
		}
	}

	return true, nil
}

// SelectVictims selects which pods to evict to make room for the preemptor.
func (p *Preemption) SelectVictims(ctx context.Context, preemptor *corev1.Pod, node *corev1.Node, nodePods []*corev1.Pod) ([]*corev1.Pod, error) {
	candidates := p.GetCandidates(ctx, preemptor, node, nodePods)
	if len(candidates) == 0 {
		return nil, nil
	}

	// Calculate needed resources.
	var neededCPU, neededMemory int64
	for _, c := range preemptor.Spec.Containers {
		if req, ok := c.Resources.Requests[corev1.ResourceCPU]; ok {
			neededCPU += req.MilliValue()
		}
		if req, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
			neededMemory += req.Value()
		}
	}

	resources := CalculateNodeResources(node, nodePods)
	availableCPU := resources.AllocatableCPU - resources.UsedCPU
	availableMemory := resources.AllocatableMemory - resources.UsedMemory

	if availableCPU >= neededCPU && availableMemory >= neededMemory {
		return nil, nil // no preemption needed
	}

	// Greedily select victims.
	var victims []*corev1.Pod
	var freedCPU, freedMemory int64

	for _, candidate := range candidates {
		allowed, err := p.CheckPDB(ctx, []*corev1.Pod{candidate}, candidate.Namespace)
		if err != nil || !allowed {
			continue
		}

		victims = append(victims, candidate)
		for _, c := range candidate.Spec.Containers {
			if req, ok := c.Resources.Requests[corev1.ResourceCPU]; ok {
				freedCPU += req.MilliValue()
			}
			if req, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
				freedMemory += req.Value()
			}
		}

		if availableCPU+freedCPU >= neededCPU && availableMemory+freedMemory >= neededMemory {
			break
		}
	}

	if len(victims) == 0 {
		return nil, nil
	}

	// Final PDB check on the complete set.
	allowed, err := p.CheckPDB(ctx, victims, preemptor.Namespace)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, nil
	}

	return victims, nil
}

// EvictPod sends an eviction request for a pod using the Eviction API.
func (p *Preemption) EvictPod(ctx context.Context, pod *corev1.Pod) error {
	eviction := &policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		DeleteOptions: &metav1.DeleteOptions{},
	}

	klog.V(2).InfoS("Evicting pod", "pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name))

	err := p.client.PolicyV1().Evictions(pod.Namespace).Evict(ctx, eviction)
	if err != nil {
		if errors.IsTooManyRequests(err) {
			return fmt.Errorf("eviction blocked by PDB: %w", err)
		}
		if errors.IsNotFound(err) {
			return nil // already gone
		}
		return fmt.Errorf("failed to evict pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}

	return nil
}

// Preempt performs the full preemption flow: find victims, check PDBs, evict.
func (p *Preemption) Preempt(ctx context.Context, preemptor *corev1.Pod, node *corev1.Node, nodePods []*corev1.Pod) ([]*corev1.Pod, error) {
	victims, err := p.SelectVictims(ctx, preemptor, node, nodePods)
	if err != nil {
		return nil, err
	}
	if len(victims) == 0 {
		return nil, nil
	}

	var evicted []*corev1.Pod
	for _, victim := range victims {
		if err := p.EvictPod(ctx, victim); err != nil {
			klog.ErrorS(err, "Failed to evict victim", "victim", fmt.Sprintf("%s/%s", victim.Namespace, victim.Name))
			continue
		}
		evicted = append(evicted, victim)
	}

	if len(evicted) == 0 {
		return nil, fmt.Errorf("failed to evict any preemption victims")
	}

	klog.InfoS("Preemption completed",
		"preemptor", fmt.Sprintf("%s/%s", preemptor.Namespace, preemptor.Name),
		"node", node.Name, "evicted", len(evicted))

	return evicted, nil
}
