package plugins

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

// ----- NodeResourcesFit -----

// NodeResourcesFit checks whether a node has sufficient resources to host a pod.
type NodeResourcesFit struct{}

func (f *NodeResourcesFit) Name() string { return "NodeResourcesFit" }

func (f *NodeResourcesFit) Filter(ctx context.Context, pod *corev1.Pod, node *corev1.Node) (bool, error) {
	// Calculate total requested resources for the pod.
	var podCPURequest, podMemoryRequest resource.Quantity
	for _, container := range pod.Spec.Containers {
		if req, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
			podCPURequest.Add(req)
		}
		if req, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
			podMemoryRequest.Add(req)
		}
	}

	// Account for init containers (use the max of init and regular).
	var initCPURequest, initMemoryRequest resource.Quantity
	for _, container := range pod.Spec.InitContainers {
		if req, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
			initCPURequest.Add(req)
		}
		if req, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
			initMemoryRequest.Add(req)
		}
	}
	if initCPURequest.Cmp(podCPURequest) > 0 {
		podCPURequest = initCPURequest
	}
	if initMemoryRequest.Cmp(podMemoryRequest) > 0 {
		podMemoryRequest = initMemoryRequest
	}

	// Get the node's allocatable resources.
	allocatable := node.Status.Allocatable
	allocatableCPU := allocatable.Cpu().MilliValue()
	allocatableMemory := allocatable.Memory().Value()

	// Check CPU.
	if !podCPURequest.IsZero() {
		if allocatableCPU < podCPURequest.MilliValue() {
			klog.V(3).InfoS("Node filtered: insufficient CPU",
				"plugin", f.Name(), "node", node.Name,
				"requested", podCPURequest.MilliValue(), "allocatable", allocatableCPU)
			return false, nil
		}
	}

	// Check Memory.
	if !podMemoryRequest.IsZero() {
		if allocatableMemory < podMemoryRequest.Value() {
			klog.V(3).InfoS("Node filtered: insufficient memory",
				"plugin", f.Name(), "node", node.Name,
				"requested", podMemoryRequest.Value(), "allocatable", allocatableMemory)
			return false, nil
		}
	}

	return true, nil
}

// ----- NodeName filter -----

// NodeNameFilter filters nodes based on the pod's spec.nodeName field.
type NodeNameFilter struct{}

func (f *NodeNameFilter) Name() string { return "NodeName" }

func (f *NodeNameFilter) Filter(ctx context.Context, pod *corev1.Pod, node *corev1.Node) (bool, error) {
	if pod.Spec.NodeName == "" {
		return true, nil
	}
	if pod.Spec.NodeName != node.Name {
		klog.V(4).InfoS("Node filtered by NodeName", "plugin", f.Name(), "node", node.Name, "expectedNode", pod.Spec.NodeName)
		return false, nil
	}
	return true, nil
}

// ----- NodeUnschedulable filter -----

// NodeUnschedulableFilter filters out nodes marked as unschedulable.
type NodeUnschedulableFilter struct{}

func (f *NodeUnschedulableFilter) Name() string { return "NodeUnschedulable" }

func (f *NodeUnschedulableFilter) Filter(ctx context.Context, pod *corev1.Pod, node *corev1.Node) (bool, error) {
	if !node.Spec.Unschedulable {
		return true, nil
	}
	// Check if the pod tolerates the unschedulable taint.
	for _, toleration := range pod.Spec.Tolerations {
		if toleration.Key == "node.kubernetes.io/unschedulable" &&
			toleration.Effect == corev1.TaintEffectNoSchedule {
			return true, nil
		}
	}
	klog.V(3).InfoS("Node filtered: unschedulable", "plugin", f.Name(), "node", node.Name)
	return false, nil
}

// ----- TaintToleration filter -----

// TaintTolerationFilter checks whether a pod tolerates the taints on a node.
type TaintTolerationFilter struct{}

func (f *TaintTolerationFilter) Name() string { return "TaintToleration" }

func (f *TaintTolerationFilter) Filter(ctx context.Context, pod *corev1.Pod, node *corev1.Node) (bool, error) {
	for _, taint := range node.Spec.Taints {
		// NoExecute taints are handled at runtime by TaintManager, not at scheduling time.
		if taint.Effect == corev1.TaintEffectNoExecute {
			continue
		}
		if !toleratesTaint(pod, &taint) {
			klog.V(3).InfoS("Node filtered: untolerated taint",
				"plugin", f.Name(), "node", node.Name, "taint", fmt.Sprintf("%s=%s:%s", taint.Key, taint.Value, taint.Effect))
			return false, nil
		}
	}
	return true, nil
}

// toleratesTaint returns true if the pod tolerates the given taint.
func toleratesTaint(pod *corev1.Pod, taint *corev1.Taint) bool {
	for _, toleration := range pod.Spec.Tolerations {
		if toleration.ToleratesTaint(taint) {
			return true
		}
	}
	return false
}
