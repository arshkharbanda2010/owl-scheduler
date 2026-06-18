package plugins

import (
	"context"
	"math"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
)

const (
	MaxNodeScore int64 = 100
	MinNodeScore int64 = 0
)

// ----- LeastRequestedPriority -----

// LeastRequestedPriority scores nodes by spare capacity. More spare = higher score.
type LeastRequestedPriority struct{}

func (l *LeastRequestedPriority) Name() string { return "LeastRequestedPriority" }

func (l *LeastRequestedPriority) Score(ctx context.Context, pod *corev1.Pod, node *corev1.Node) (int64, error) {
	allocatableCPU := node.Status.Allocatable.Cpu().MilliValue()
	allocatableMemory := node.Status.Allocatable.Memory().Value()

	// Calculate used resources from the node's pod list (approximation via status).
	// For a more accurate view, the scheduler would track this via the informer.
	var usedCPU, usedMemory int64
	for _, podPtr := range node.Status.Pods {
		_ = podPtr // We don't have direct access to pod resources from node status.
	}

	// Use allocatable as a proxy; in production the scheduler tracks allocations.
	var cpuFraction, memFraction float64
	if allocatableCPU > 0 {
		cpuFraction = 1.0 - float64(usedCPU)/float64(allocatableCPU)
	} else {
		cpuFraction = 1.0
	}
	if allocatableMemory > 0 {
		memFraction = 1.0 - float64(usedMemory)/float64(allocatableMemory)
	} else {
		memFraction = 1.0
	}

	score := int64(((cpuFraction + memFraction) / 2.0) * float64(MaxNodeScore))
	return clamp(score), nil
}

// ----- BalancedResourceAllocation -----

// BalancedResourceAllocation scores nodes by how balanced CPU and memory usage is.
type BalancedResourceAllocation struct{}

func (b *BalancedResourceAllocation) Name() string { return "BalancedResourceAllocation" }

func (b *BalancedResourceAllocation) Score(ctx context.Context, pod *corev1.Pod, node *corev1.Node) (int64, error) {
	allocatableCPU := node.Status.Allocatable.Cpu().MilliValue()
	allocatableMemory := node.Status.Allocatable.Memory().Value()

	// Calculate the pod's resource requests.
	var podCPURequest, podMemoryRequest int64
	for _, c := range pod.Spec.Containers {
		if req, ok := c.Resources.Requests[corev1.ResourceCPU]; ok {
			podCPURequest += req.MilliValue()
		}
		if req, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
			podMemoryRequest += req.Value()
		}
	}

	// Estimate current usage from node status capacity vs allocatable.
	// In production, the scheduler would track actual pod allocations.
	var cpuUtil, memUtil float64
	if allocatableCPU > 0 {
		cpuUtil = float64(podCPURequest) / float64(allocatableCPU)
	}
	if allocatableMemory > 0 {
		memUtil = float64(podMemoryRequest) / float64(allocatableMemory)
	}

	// Score is higher when CPU and memory utilization are closer to each other.
	balance := 1.0 - math.Abs(cpuUtil-memUtil)
	if balance < 0 {
		balance = 0
	}
	score := int64(balance * float64(MaxNodeScore))

	klog.V(4).InfoS("Node scored",
		"plugin", b.Name(), "node", node.Name,
		"score", score, "cpuUtil", cpuUtil, "memUtil", memUtil)

	return clamp(score), nil
}

// ----- NodeAffinityScorer -----

// NodeAffinityScorer scores nodes based on the pod's node affinity preferences.
type NodeAffinityScorer struct{}

func (n *NodeAffinityScorer) Name() string { return "NodeAffinityPriority" }

func (n *NodeAffinityScorer) Score(ctx context.Context, pod *corev1.Pod, node *corev1.Node) (int64, error) {
	affinity := pod.Spec.Affinity
	if affinity == nil || affinity.NodeAffinity == nil ||
		len(affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution) == 0 {
		return MaxNodeScore / 2, nil // neutral score
	}

	var totalScore int64
	nodeLabels := node.Labels

	for _, term := range affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
		selector, err := nodeSelectorRequirementsAsSelector(term.Preference.MatchExpressions)
		if err != nil {
			continue
		}
		if selector.Matches(labels.Set(nodeLabels)) {
			totalScore += term.Weight
		}
	}

	return clamp(totalScore), nil
}

// nodeSelectorRequirementsAsSelector converts NodeSelectorRequirements to a labels.Selector.
func nodeSelectorRequirementsAsSelector(reqs []corev1.NodeSelectorRequirement) (labels.Selector, error) {
	if len(reqs) == 0 {
		return labels.Nothing(), nil
	}
	selector := labels.NewSelector()
	for _, expr := range reqs {
		req, err := labels.NewRequirement(expr.Key, string(expr.Operator), expr.Values)
		if err != nil {
			return nil, err
		}
		selector = selector.Add(*req)
	}
	return selector, nil
}

func clamp(score int64) int64 {
	if score < MinNodeScore {
		return MinNodeScore
	}
	if score > MaxNodeScore {
		return MaxNodeScore
	}
	return score
}
