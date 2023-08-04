package util

import "go.opentelemetry.io/collector/pdata/pmetric"

// HasMetric returns true if a metric with matching name is present.
func HasMetric(metrics pmetric.Metrics, name string) bool {
	m := metrics.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics()
	found := false
	for i := 0; i < m.Len() && !found; i += 1 {
		found = m.At(i).Name() == name
	}
	return found
}
