package compose

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
)

type PortsConfig struct {
	TracesGRPCPort int
	TracesHTTPPort int
	TempoHTTPPort  int
}

type ComposeEndpoint struct {
	ComposeFilePath string
	OutputPath      string
	Env             []string
	Ports           PortsConfig
}

var compose *Compose

func NewEndpoint(composeFilePath, outputPath string, env []string, ports PortsConfig) *ComposeEndpoint {
	endpoint := &ComposeEndpoint{
		ComposeFilePath: composeFilePath,
		Env:             env,
		OutputPath:      outputPath,
		Ports:           ports,
	}

	return endpoint
}

func (e *ComposeEndpoint) Start(ctx context.Context) error {
	var err error
	compose, err = ComposeSuite("docker-compose-traces.yml", path.Join(e.OutputPath, "test-suite-traces.log"))
	if err != nil {
		return err
	}
	err = compose.Up()

	return err
}

func (e *ComposeEndpoint) Stop(ctx context.Context) error {
	return compose.Close()
}

func (e *ComposeEndpoint) TracerProvider(ctx context.Context, r *resource.Resource) (*trace.TracerProvider, error) {
	var exporter *otlptrace.Exporter
	var err error

	if e.Ports.TracesGRPCPort != 0 {
		exporter, err = otlptracegrpc.New(ctx, otlptracegrpc.WithInsecure(), otlptracegrpc.WithEndpoint(fmt.Sprintf("localhost:%d", e.Ports.TracesGRPCPort)))
		if err != nil {
			return nil, err
		}
	} else if e.Ports.TracesHTTPPort != 0 {
		exporter, err = otlptracehttp.New(ctx, otlptracehttp.WithInsecure(), otlptracehttp.WithEndpoint(fmt.Sprintf("localhost:%d/v1/traces", e.Ports.TracesHTTPPort)))
		if err != nil {
			return nil, err
		}
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if exporter == nil {
		return nil, fmt.Errorf("unknown exporter format, specify an OTel trace GRPC or HTTP port")
	}

	traceProvider := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(r),
	)

	return traceProvider, nil
}

func (e *ComposeEndpoint) GetTraceByID(ctx context.Context, id string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	url := fmt.Sprintf("http://localhost:%d/api/traces/%s", e.Ports.TempoHTTPPort, id)

	resp, getErr := http.Get(url)
	if getErr != nil {
		return nil, getErr
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("expected HTTP status 200, but got: %d", resp.StatusCode)
	}

	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return respBytes, nil
}
