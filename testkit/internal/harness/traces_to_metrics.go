package harness

import (
	"context"

	"github.com/grafana/oats/testkit/internal/util"
	"github.com/onsi/ginkgo/v2"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/connector"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap/zaptest"
)

// TracesToMetrics is a test harness for connectors that accept traces and emit metrics.
type TracesToMetrics struct {
	connector.Traces
	Metrics func() []pmetric.Metrics
}

// NewTracesToMetrics constructs a new TracesToMetrics test harness.
func NewTracesToMetrics(t ginkgo.FullGinkgoTInterface, f connector.Factory) (*TracesToMetrics, error) {
	l := zaptest.NewLogger(t)

	set := connector.CreateSettings{
		ID: component.NewIDWithName(component.DataTypeTraces, "traces_to_metrics_harness"),
		TelemetrySettings: component.TelemetrySettings{
			Logger:         l,
			TracerProvider: trace.NewNoopTracerProvider(),
			MeterProvider:  noop.NewMeterProvider(),
			MetricsLevel:   0,
			Resource:       pcommon.Resource{},
		},
		BuildInfo: component.BuildInfo{},
	}

	sink := &consumertest.MetricsSink{}
	c, err := f.CreateTracesToMetrics(context.TODO(), set, f.CreateDefaultConfig(), sink)
	if err != nil {
		return nil, err
	}

	// return the harness and start the connector
	return &TracesToMetrics{
		Traces:  c,
		Metrics: sink.AllMetrics,
	}, c.Start(context.TODO(), &util.NoopHost{})
}
