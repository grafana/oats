package observability

import (
	"context"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
)

type Endpoint interface {
	TracerProvider(context.Context, *resource.Resource) (*trace.TracerProvider, error)
	GetTraceByID(context.Context, string) ([]byte, error)
}
