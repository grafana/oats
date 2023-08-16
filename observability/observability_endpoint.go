package observability

import (
	"context"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
)

type Endpoint interface {
	TracerProvider(context.Context, *resource.Resource) (*trace.TracerProvider, error)
	GetTraceByID(context.Context, string) ([]byte, error)
	SearchTags(context.Context, map[string]string) ([]byte, error)

	// Metrics
	RunPromQL(context.Context, string) ([]byte, error)

	Start(context.Context) error
	Stop(context.Context) error

	PrometheusClient(context.Context) (v1.API, error)
}
