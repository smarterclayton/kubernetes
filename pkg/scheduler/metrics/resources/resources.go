/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package resources provides a metrics collector that reports the
// resource consumption (requests and limits) of the pods in the cluster
// as the scheduler and kubelet would interpret it.
package resources

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/component-base/metrics"
)

const (
	// UnitByte is the unit of measure in bytes.
	unitByte = "byte"
	// UnitInteger is the unit of measure in integers.
	unitInteger = "integer"
)

type resourceLifecycleDescriptors struct {
	total   *metrics.Desc
	running *metrics.Desc
}

func (d resourceLifecycleDescriptors) Describe(ch chan<- *metrics.Desc) {
	ch <- d.total
	ch <- d.running
}

type resourceTypeMetricsDescriptors struct {
	extension        resourceLifecycleDescriptors
	cpu              resourceLifecycleDescriptors
	memory           resourceLifecycleDescriptors
	storage          resourceLifecycleDescriptors
	ephemeralStorage resourceLifecycleDescriptors
}

func (d resourceTypeMetricsDescriptors) Describe(ch chan<- *metrics.Desc) {
	d.extension.Describe(ch)
	d.cpu.Describe(ch)
	d.memory.Describe(ch)
	d.storage.Describe(ch)
	d.ephemeralStorage.Describe(ch)
}

type resourceMetricsDescriptors struct {
	requests resourceTypeMetricsDescriptors
	limits   resourceTypeMetricsDescriptors
}

func (d resourceMetricsDescriptors) Describe(ch chan<- *metrics.Desc) {
	d.requests.Describe(ch)
	d.limits.Describe(ch)
}

var podResourceDesc = resourceMetricsDescriptors{
	requests: resourceTypeMetricsDescriptors{
		extension: resourceLifecycleDescriptors{
			total: metrics.NewDesc("kube_pod_resource_request",
				"Resources requested by workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod for resources along with the unit for the resource if any.",
				[]string{"namespace", "pod", "node", "resource", "unit"},
				nil,
				metrics.ALPHA,
				""),
			running: metrics.NewDesc("kube_running_pod_resource_request",
				"Resources requested by running workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod for resources along with the unit for the resource if any.",
				[]string{"namespace", "pod", "node", "resource", "unit"},
				nil,
				metrics.ALPHA,
				""),
		},
		cpu: resourceLifecycleDescriptors{
			total: metrics.NewDesc("kube_pod_resource_request_cpu_cores",
				"CPU resources requested by workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod.",
				[]string{"namespace", "pod", "node"},
				nil,
				metrics.ALPHA,
				""),
			running: metrics.NewDesc("kube_running_pod_resource_request_cpu_cores",
				"CPU resources requested by running workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod.",
				[]string{"namespace", "pod", "node"},
				nil,
				metrics.ALPHA,
				""),
		},
		memory: resourceLifecycleDescriptors{
			total: metrics.NewDesc("kube_pod_resource_request_memory_bytes",
				"Memory resources requested by workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod.",
				[]string{"namespace", "pod", "node"},
				nil,
				metrics.ALPHA,
				""),
			running: metrics.NewDesc("kube_running_pod_resource_request_memory_bytes",
				"Memory resources requested by running workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod.",
				[]string{"namespace", "pod", "node"},
				nil,
				metrics.ALPHA,
				""),
		},
		storage: resourceLifecycleDescriptors{
			total: metrics.NewDesc("kube_pod_resource_request_storage_bytes",
				"Storage requested by workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod.",
				[]string{"namespace", "pod", "node"},
				nil,
				metrics.ALPHA,
				""),
			running: metrics.NewDesc("kube_running_pod_resource_request_storage_bytes",
				"Storage requested by running workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod.",
				[]string{"namespace", "pod", "node"},
				nil,
				metrics.ALPHA,
				""),
		},
		ephemeralStorage: resourceLifecycleDescriptors{
			total: metrics.NewDesc("kube_pod_resource_request_ephemeral_storage_bytes",
				"Ephemeral storage requested by workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod.",
				[]string{"namespace", "pod", "node"},
				nil,
				metrics.ALPHA,
				""),
			running: metrics.NewDesc("kube_running_pod_resource_request_ephemeral_storage_bytes",
				"Ephemeral storage requested by running workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod.",
				[]string{"namespace", "pod", "node"},
				nil,
				metrics.ALPHA,
				""),
		},
	},
	limits: resourceTypeMetricsDescriptors{
		extension: resourceLifecycleDescriptors{
			total: metrics.NewDesc("kube_pod_resource_limit",
				"Resources limit for workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod for resources along with the unit for the resource if any.",
				[]string{"namespace", "pod", "node", "resource", "unit"},
				nil,
				metrics.ALPHA,
				""),
			running: metrics.NewDesc("kube_running_pod_resource_limit",
				"Resource limit for running workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod for resources along with the unit for the resource if any.",
				[]string{"namespace", "pod", "node", "resource", "unit"},
				nil,
				metrics.ALPHA,
				""),
		},
		cpu: resourceLifecycleDescriptors{
			total: metrics.NewDesc("kube_pod_resource_limit_cpu_cores",
				"CPU limit for workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod.",
				[]string{"namespace", "pod", "node"},
				nil,
				metrics.ALPHA,
				""),
			running: metrics.NewDesc("kube_running_pod_resource_limit_cpu_cores",
				"CPU limit for running workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod.",
				[]string{"namespace", "pod", "node"},
				nil,
				metrics.ALPHA,
				""),
		},
		memory: resourceLifecycleDescriptors{
			total: metrics.NewDesc("kube_pod_resource_limit_memory_bytes",
				"Memory resources limit for workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod.",
				[]string{"namespace", "pod", "node"},
				nil,
				metrics.ALPHA,
				""),
			running: metrics.NewDesc("kube_running_pod_resource_limit_memory_bytes",
				"Memory resources limit for running workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod.",
				[]string{"namespace", "pod", "node"},
				nil,
				metrics.ALPHA,
				""),
		},
		storage: resourceLifecycleDescriptors{
			total: metrics.NewDesc("kube_pod_resource_limit_storage_bytes",
				"Storage limit for workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod.",
				[]string{"namespace", "pod", "node"},
				nil,
				metrics.ALPHA,
				""),
			running: metrics.NewDesc("kube_running_pod_resource_limit_storage_bytes",
				"Storage limit for running workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod.",
				[]string{"namespace", "pod", "node"},
				nil,
				metrics.ALPHA,
				""),
		},
		ephemeralStorage: resourceLifecycleDescriptors{
			total: metrics.NewDesc("kube_pod_resource_limit_ephemeral_storage_bytes",
				"Ephemeral storage limit for workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod.",
				[]string{"namespace", "pod", "node"},
				nil,
				metrics.ALPHA,
				""),
			running: metrics.NewDesc("kube_running_pod_resource_limit_ephemeral_storage_bytes",
				"Ephemeral storage limit for running workloads on the cluster, broken down by pod. This shows the resource usage the scheduler and kubelet expect per pod.",
				[]string{"namespace", "pod", "node"},
				nil,
				metrics.ALPHA,
				""),
		},
	},
}

// Handler creates a collector from the provided podLister and returns an http.Handler that
// will report the requested metrics in the prometheus format. It does not include any other
// metrics.
func Handler(podLister corelisters.PodLister) http.HandlerFunc {
	collector := NewPodResourcesMetricsCollector(podLister)
	registry := metrics.NewKubeRegistry()
	registry.CustomMustRegister(collector)
	return metrics.HandlerWithReset(registry, metrics.HandlerOpts{}).ServeHTTP
}

// Check if resourceMetricsCollector implements necessary interface
var _ metrics.StableCollector = &podResourceCollector{}

// NewPodResourcesMetricsCollector registers a O(pods) cardinality metric that
// reports the current resources requested by all pods on the cluster within
// the Kubernetes resource model. Metrics are broken down by pod, node, resource,
// and phase of lifecycle. Each pod returns two series per resource - one for
// their aggregate usage (required to schedule) and one for their phase specific
// usage. This allows admins to assess the cost per resource at different phases
// of startup and compare to actual resource usage.
func NewPodResourcesMetricsCollector(podLister corelisters.PodLister) metrics.StableCollector {
	return &podResourceCollector{
		lister: podLister,
	}
}

type podResourceCollector struct {
	metrics.BaseStableCollector
	lister corelisters.PodLister
}

func (c *podResourceCollector) DescribeWithStability(ch chan<- *metrics.Desc) {
	podResourceDesc.Describe(ch)
}

func (c *podResourceCollector) CollectWithStability(ch chan<- metrics.Metric) {
	pods, err := c.lister.List(labels.Everything())
	if err != nil {
		return
	}
	for _, p := range pods {
		_, reqs, currentReqs, limits, currentLimits, terminal := podRequestsAndLimitsByLifecycle(p)
		if terminal {
			// terminal pods are excluded from resource usage calculations
			continue
		}
		for _, t := range []struct {
			desc    resourceTypeMetricsDescriptors
			total   v1.ResourceList
			running v1.ResourceList
		}{
			{
				desc:    podResourceDesc.requests,
				total:   reqs,
				running: currentReqs,
			},
			{
				desc:    podResourceDesc.limits,
				total:   limits,
				running: currentLimits,
			},
		} {
			for resourceName, val := range t.total {
				currentVal := t.running[resourceName]
				switch resourceName {
				case v1.ResourceCPU:
					recordMetric(ch, t.desc.cpu.total, val, p.Namespace, p.Name, p.Spec.NodeName)
					recordMetric(ch, t.desc.cpu.running, currentVal, p.Namespace, p.Name, p.Spec.NodeName)
				case v1.ResourceMemory:
					recordMetric(ch, t.desc.memory.total, val, p.Namespace, p.Name, p.Spec.NodeName)
					recordMetric(ch, t.desc.memory.running, currentVal, p.Namespace, p.Name, p.Spec.NodeName)
				case v1.ResourceStorage:
					recordMetric(ch, t.desc.storage.total, val, p.Namespace, p.Name, p.Spec.NodeName)
					recordMetric(ch, t.desc.storage.running, currentVal, p.Namespace, p.Name, p.Spec.NodeName)
				case v1.ResourceEphemeralStorage:
					recordMetric(ch, t.desc.ephemeralStorage.total, val, p.Namespace, p.Name, p.Spec.NodeName)
					recordMetric(ch, t.desc.ephemeralStorage.running, currentVal, p.Namespace, p.Name, p.Spec.NodeName)
				default:
					if isHugePageResourceName(resourceName) {
						recordMetricWithUnit(ch, t.desc.extension.total, resourceName, unitByte, val, p.Namespace, p.Name, p.Spec.NodeName)
						recordMetricWithUnit(ch, t.desc.extension.running, resourceName, unitByte, currentVal, p.Namespace, p.Name, p.Spec.NodeName)
					}
					if isAttachableVolumeResourceName(resourceName) || isExtendedResourceName(resourceName) {
						recordMetricWithUnit(ch, t.desc.extension.total, resourceName, unitInteger, val, p.Namespace, p.Name, p.Spec.NodeName)
						recordMetricWithUnit(ch, t.desc.extension.running, resourceName, unitInteger, currentVal, p.Namespace, p.Name, p.Spec.NodeName)
					}
				}
			}
		}
	}
}

func recordMetric(ch chan<- metrics.Metric, desc *metrics.Desc, val resource.Quantity, namespace, name, nodeName string) {
	if !val.IsZero() {
		ch <- metrics.NewLazyConstMetric(desc, metrics.GaugeValue,
			val.AsApproximateFloat64(),
			namespace, name, nodeName,
		)
	}
}

func recordMetricWithUnit(ch chan<- metrics.Metric, desc *metrics.Desc, resourceName v1.ResourceName, unit string, val resource.Quantity, namespace, name, nodeName string) {
	if !val.IsZero() {
		ch <- metrics.NewLazyConstMetric(desc, metrics.GaugeValue,
			val.AsApproximateFloat64(),
			namespace, name, nodeName, sanitizeLabelName(string(resourceName)), unit,
		)
	}
}

// addResourceList adds the resources in newList to list
func addResourceList(list, newList v1.ResourceList) {
	for name, quantity := range newList {
		if value, ok := list[name]; !ok {
			list[name] = quantity.DeepCopy()
		} else {
			value.Add(quantity)
			list[name] = value
		}
	}
}

// maxResourceList sets list to the greater of list/newList for every resource
// either list
func maxResourceList(list, new v1.ResourceList) {
	for name, quantity := range new {
		if value, ok := list[name]; !ok {
			list[name] = quantity.DeepCopy()
			continue
		} else {
			if quantity.Cmp(value) > 0 {
				list[name] = quantity.DeepCopy()
			}
		}
	}
}

// podRequestsAndLimitsByLifecycle returns a dictionary of all defined resources summed up for all
// containers of the pod. If PodOverhead feature is enabled, pod overhead is added to the
// total container resource requests and to the total container limits which have a
// non-zero quantity. If the pod is assigned to a node and non-terminal, the current requests and
// limits are returned for the pod.
func podRequestsAndLimitsByLifecycle(pod *v1.Pod) (lifecycle string, reqs, currentReqs, limits, currentLimits v1.ResourceList, terminal bool) {
	var initializing, running bool
	switch {
	case len(pod.Spec.NodeName) == 0:
		lifecycle = "Pending"
	case pod.Status.Phase == v1.PodSucceeded, pod.Status.Phase == v1.PodFailed:
		lifecycle = "Completed"
		terminal = true
	default:
		if len(pod.Spec.InitContainers) > 0 && !hasConditionStatus(pod.Status.Conditions, v1.PodInitialized, v1.ConditionTrue) {
			lifecycle = "Initializing"
			initializing = true
		} else {
			lifecycle = "Running"
			running = true
		}
	}
	if terminal {
		return
	}

	reqs, limits, currentReqs, currentLimits = make(v1.ResourceList, 4), make(v1.ResourceList, 4), make(v1.ResourceList, 4), make(v1.ResourceList, 4)
	for _, container := range pod.Spec.Containers {
		addResourceList(reqs, container.Resources.Requests)
		addResourceList(limits, container.Resources.Limits)
	}
	// init containers define the minimum of any resource
	for _, container := range pod.Spec.InitContainers {
		maxResourceList(reqs, container.Resources.Requests)
		maxResourceList(limits, container.Resources.Limits)
	}

	// if PodOverhead feature is supported, add overhead for running a pod
	// to the sum of reqeuests and to non-zero limits:
	if pod.Spec.Overhead != nil {
		addResourceList(reqs, pod.Spec.Overhead)
		for name, quantity := range pod.Spec.Overhead {
			if value, ok := limits[name]; ok && !value.IsZero() {
				value.Add(quantity)
				limits[name] = value
			}
		}
		if initializing || running {
			addResourceList(reqs, pod.Spec.Overhead)
			for name, quantity := range pod.Spec.Overhead {
				if value, ok := limits[name]; ok && !value.IsZero() {
					value.Add(quantity)
					limits[name] = value
				}
			}
		}
	}

	if initializing || running {
		currentReqs = reqs
		currentLimits = limits
	}

	return
}

func hasConditionStatus(conditions []v1.PodCondition, name v1.PodConditionType, status v1.ConditionStatus) bool {
	for _, condition := range conditions {
		if condition.Type != name {
			continue
		}
		return condition.Status == status
	}
	return false
}

var invalidLabelCharRE = regexp.MustCompile(`[^a-zA-Z0-9_]`)

func sanitizeLabelName(s string) string {
	return invalidLabelCharRE.ReplaceAllString(s, "_")
}

func isHugePageResourceName(name v1.ResourceName) bool {
	return strings.HasPrefix(string(name), v1.ResourceHugePagesPrefix)
}

func isAttachableVolumeResourceName(name v1.ResourceName) bool {
	return strings.HasPrefix(string(name), v1.ResourceAttachableVolumesPrefix)
}

func isExtendedResourceName(name v1.ResourceName) bool {
	if isNativeResource(name) || strings.HasPrefix(string(name), v1.DefaultResourceRequestsPrefix) {
		return false
	}
	// Ensure it satisfies the rules in IsQualifiedName() after converted into quota resource name
	nameForQuota := fmt.Sprintf("%s%s", v1.DefaultResourceRequestsPrefix, string(name))
	if errs := validation.IsQualifiedName(nameForQuota); len(errs) != 0 {
		return false
	}
	return true
}

func isNativeResource(name v1.ResourceName) bool {
	return !strings.Contains(string(name), "/") ||
		isPrefixedNativeResource(name)
}

func isPrefixedNativeResource(name v1.ResourceName) bool {
	return strings.Contains(string(name), v1.ResourceDefaultNamespacePrefix)
}
