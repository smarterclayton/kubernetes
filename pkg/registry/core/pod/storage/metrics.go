package storage

import (
	"sync"

	"k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
)

const metricsSubsystem = "kube_apiserver"

var (
	// EvictionsTotal is the number of successful evictions performed.
	// This metric is incremented exactly one time per pod.
	evictionsTotal = metrics.NewGauge(
		&metrics.GaugeOpts{
			Subsystem:      metricsSubsystem,
			Name:           "evictions_total",
			Help:           "The number of successful evictions performed for each pod. This is incremented exactly once when a pod is marked deleted.",
			StabilityLevel: metrics.ALPHA,
		},
	)
)

var registerMetricsOnce sync.Once

// registerMetrics registers storage metrics.
func registerMetrics() {
	registerMetricsOnce.Do(func() {
		legacyregistry.MustRegister(evictionsTotal)
	})
}
