package observability

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/grafana/oats/internal/testhelpers/common"
	"github.com/grafana/oats/internal/testhelpers/otelcollector"
	"github.com/grafana/oats/internal/testhelpers/prometheus"
	"github.com/grafana/oats/internal/testhelpers/tempo"
	"github.com/grafana/oats/observability"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
)

func NewLocalEndpoint() *LocalEndpoint {
	endpoint := &LocalEndpoint{
		mutex: &sync.Mutex{},
	}

	return endpoint
}

var _ observability.Endpoint = &LocalEndpoint{}

type LocalEndpoint struct {
	mutex   *sync.Mutex
	stopped bool

	networkName           string
	tempoEndpoint         *tempo.LocalEndpoint
	prometheusEndpoint    *prometheus.LocalEndpoint
	otelCollectorEndpoint *otelcollector.LocalEndpoint
}

func (e *LocalEndpoint) PrometheusClient(ctx context.Context) (v1.API, error) {
	if e.prometheusEndpoint == nil {
		return nil, fmt.Errorf("no prometheus endpoint")
	}
	return e.prometheusEndpoint.PromClient(ctx)
}

func (e *LocalEndpoint) Start(ctx context.Context) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if e.networkName == "" {
		sharedNetworkUUID, err := uuid.NewRandom()
		if err != nil {
			return err
		}

		sharedNetworkName := fmt.Sprintf("ginkgoTest%s", sharedNetworkUUID.String())

		_, err = common.ContainerNetwork(sharedNetworkName)
		if err != nil {
			return fmt.Errorf("getting shared container network: %s", err)
		}

		e.networkName = sharedNetworkName
	}

	promEndpoint, err := prometheus.NewLocalEndpoint(ctx, e.networkName)
	if err != nil {
		return fmt.Errorf("creating local Prometheus endpoint: %s", err)
	}

	promEndpointAddress, err := promEndpoint.Start(ctx)
	if err != nil {
		return fmt.Errorf("starting local Prometheus endpoint: %s", err)
	}

	tempoEndpoint, err := tempo.NewLocalEndpoint(ctx, e.networkName, promEndpointAddress)
	if err != nil {
		return fmt.Errorf("creating local Tempo endpoint: %s", err)
	}

	_, err = tempoEndpoint.Start(ctx)
	if err != nil {
		_ = promEndpoint.Stop(ctx)

		return fmt.Errorf("starting local Tempo endpoint: %s", err)
	}

	otlpTraceEndpoint, err := tempoEndpoint.OTLPTraceEndpoint(ctx)
	if err != nil {
		_ = promEndpoint.Stop(ctx)
		_ = tempoEndpoint.Stop(ctx)

		return fmt.Errorf("getting OTLP trace endpoint from local Tempo endpoint: %s", err)
	}

	collectorEndpoint, err := otelcollector.NewLocalEndpoint(ctx, e.networkName, otlpTraceEndpoint, promEndpointAddress)
	if err != nil {
		_ = promEndpoint.Stop(ctx)
		_ = tempoEndpoint.Stop(ctx)

		return fmt.Errorf("creating a local OpenTelemetry Collector: %s", err)
	}

	_, err = collectorEndpoint.Start(ctx)
	if err != nil {
		_ = promEndpoint.Stop(ctx)
		_ = tempoEndpoint.Stop(ctx)

		return fmt.Errorf("starting local OpenTelemetry Collector: %s", err)
	}

	e.tempoEndpoint = tempoEndpoint
	e.prometheusEndpoint = promEndpoint
	e.otelCollectorEndpoint = collectorEndpoint

	return nil
}

func (e *LocalEndpoint) Stop(ctx context.Context) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if e.stopped {
		return nil
	}

	var stopErr error

	if e.otelCollectorEndpoint != nil {
		stopErr = e.otelCollectorEndpoint.Stop(ctx)
		if stopErr != nil {
			return stopErr
		}

		e.otelCollectorEndpoint = nil
	}

	if e.prometheusEndpoint != nil {
		stopErr = e.prometheusEndpoint.Stop(ctx)
		if stopErr != nil {
			return stopErr
		}

		e.prometheusEndpoint = nil
	}

	if e.tempoEndpoint != nil {
		stopErr = e.tempoEndpoint.Stop(ctx)
		if stopErr != nil {
			return stopErr
		}

		e.tempoEndpoint = nil
	}

	e.stopped = true

	return nil
}

func (e *LocalEndpoint) TracerProvider(ctx context.Context, r *resource.Resource) (*trace.TracerProvider, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if e.stopped {
		return nil, fmt.Errorf("cannot return OpenTelemetry TraceProvider from stopped local observability endpoint")
	}

	if e.otelCollectorEndpoint == nil {
		return nil, fmt.Errorf("cannot return OpenTelemetry TraceProvider from nil OpenTelemetry Collector endpoint")
	}

	return e.otelCollectorEndpoint.TracerProvider(ctx, r)
}

func (e *LocalEndpoint) GetTraceByID(ctx context.Context, traceID string) ([]byte, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if e.tempoEndpoint == nil {
		return nil, fmt.Errorf("cannot search for traces from nil Tempo endpoint")
	}

	return e.tempoEndpoint.GetTraceByID(ctx, traceID)
}

func (e *LocalEndpoint) SearchTags(ctx context.Context, tags map[string]string) ([]byte, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if e.tempoEndpoint == nil {
		return nil, fmt.Errorf("cannot search for tags from nil Tempo endpoint")
	}

	return e.tempoEndpoint.SearchTags(ctx, tags)
}

func (e *LocalEndpoint) OTLPEndpointAddress(ctx context.Context) (*common.LocalEndpointAddress, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if e.otelCollectorEndpoint == nil {
		return nil, fmt.Errorf("cannot return OpenTelemetry Endpoint from nil OpenTelemetry Collector endpoint")
	}

	return e.otelCollectorEndpoint.OTLPEndpoint(ctx)
}

func (e *LocalEndpoint) ContainerNetwork(ctx context.Context) (string, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	if e.networkName == "" {
		return "", fmt.Errorf("empty container network name")
	}

	// Create the network if it does not exist
	_, err := common.ContainerNetwork(e.networkName)
	if err != nil {
		return "", fmt.Errorf("getting, or creating container network: %s", err)
	}

	return e.networkName, nil
}

func (e *LocalEndpoint) RunPromQL(ctx context.Context, query string) ([]byte, error) {
	return nil, nil
}
