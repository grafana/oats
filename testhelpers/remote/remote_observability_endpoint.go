package remote

import (
	"bufio"
	"context"
	"fmt"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type PortsConfig struct {
	TracesGRPCPort     int
	TracesHTTPPort     int
	TempoHTTPPort      int
	MimirHTTPPort      int
	PrometheusHTTPPort int
	LokiHttpPort       int
	PyroscopeHttpPort  int
}

type Endpoint struct {
	ports     PortsConfig
	start     func(context.Context) error
	stop      func(context.Context) error
	logReader func(func(io.ReadCloser, *sync.WaitGroup)) error
}

func NewEndpoint(ports PortsConfig, start func(context.Context) error, stop func(context.Context) error, logReader func(func(io.ReadCloser, *sync.WaitGroup)) error) *Endpoint {
	return &Endpoint{
		ports:     ports,
		start:     start,
		stop:      stop,
		logReader: logReader,
	}
}

func (e *Endpoint) TracerProvider(ctx context.Context, r *resource.Resource) (*trace.TracerProvider, error) {
	var exporter *otlptrace.Exporter
	var err error

	if e.ports.TracesGRPCPort != 0 {
		exporter, err = otlptracegrpc.New(ctx, otlptracegrpc.WithInsecure(), otlptracegrpc.WithEndpoint(fmt.Sprintf("localhost:%d", e.ports.TracesGRPCPort)))
		if err != nil {
			return nil, err
		}
	} else if e.ports.TracesHTTPPort != 0 {
		exporter, err = otlptracehttp.New(ctx, otlptracehttp.WithInsecure(), otlptracehttp.WithEndpoint(fmt.Sprintf("localhost:%d/v1/traces", e.ports.TracesHTTPPort)))
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

func (e *Endpoint) makeGetRequest(url string) ([]byte, error) {
	resp, getErr := http.Get(url)
	if getErr != nil {
		return nil, getErr
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("expected HTTP status 200, but got: %d", resp.StatusCode)
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return respBytes, nil
}

func (e *Endpoint) GetTraceByID(ctx context.Context, id string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return e.makeGetRequest(fmt.Sprintf("http://localhost:%d/api/traces/%s", e.ports.TempoHTTPPort, id))
}

func (e *Endpoint) SearchTempo(ctx context.Context, query string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return e.makeGetRequest(fmt.Sprintf("http://localhost:%d/api/search?q=%s", e.ports.TempoHTTPPort, url.QueryEscape(query)))
}

func (e *Endpoint) SearchTags(ctx context.Context, tags map[string]string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	var tb strings.Builder

	for tag, val := range tags {
		if tb.Len() != 0 {
			tb.WriteString("&")
		}
		s := tag + "=" + val
		tb.WriteString(url.QueryEscape(s))
	}

	return e.makeGetRequest(fmt.Sprintf("http://localhost:%d/api/search?tags=%s", e.ports.TempoHTTPPort, tb.String()))
}

func (e *Endpoint) RunPromQL(promQL string) ([]byte, error) {
	var u string
	if e.ports.MimirHTTPPort != 0 {
		u = fmt.Sprintf("http://localhost:%d/prometheus/api/v1/query?query=%s", e.ports.MimirHTTPPort, url.PathEscape(promQL))
	} else if e.ports.PrometheusHTTPPort != 0 {
		u = fmt.Sprintf("http://localhost:%d/api/v1/query?query=%s", e.ports.PrometheusHTTPPort, url.PathEscape(promQL))
	} else {
		return nil, fmt.Errorf("to run PromQL you must configure a MimirHTTPPort or a PrometheusHTTPPort")
	}

	resp, err := http.Get(u)
	if err != nil {
		return nil, fmt.Errorf("querying prometheus: %w", err)
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("can't read response body: %w", err)
	}

	return body, nil
}

func (e *Endpoint) SearchLoki(query string) ([]byte, error) {
	if e.ports.LokiHttpPort == 0 {
		return nil, fmt.Errorf("to search Loki you must configure a LokiHttpPort")
	}

	u := fmt.Sprintf("http://localhost:%d/loki/api/v1/query_range?since=5m&limit=1&query=%s", e.ports.LokiHttpPort, url.PathEscape(query))

	resp, err := http.Get(u)
	if err != nil {
		return nil, fmt.Errorf("querying loki: %w", err)
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("can't read response body: %w", err)
	}

	return body, nil
}

func (e *Endpoint) SearchPyroscope(query string) ([]byte, error) {
	if e.ports.PyroscopeHttpPort == 0 {
		return nil, fmt.Errorf("to search Pyroscope you must configure a PyroscopeHttpPort")
	}

	u := fmt.Sprintf("http://localhost:%d/pyroscope/render?from=from=now-1m&query=%s", e.ports.PyroscopeHttpPort, url.PathEscape(query))

	resp, err := http.Get(u)
	if err != nil {
		return nil, fmt.Errorf("querying pyroscope: %w", err)
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("can't read response body: %w", err)
	}

	return body, nil
}

func (e *Endpoint) Start(ctx context.Context) error {
	return e.start(ctx)
}

func (e *Endpoint) Stop(ctx context.Context) error {
	return e.stop(ctx)
}

func (e *Endpoint) SearchComposeLogs(message string) (bool, error) {
	found := false
	slog.Info("searching compose logs", "message", message)
	err := e.logReader(func(pipe io.ReadCloser, wg *sync.WaitGroup) {
		reader := bufio.NewReader(pipe)
		line, err := reader.ReadString('\n')
		for err == nil {
			if strings.Contains(line, message) {
				found = true
			}
			line, err = reader.ReadString('\n')
		}
		wg.Done()
	})
	if err != nil {
		return false, fmt.Errorf("error reading logs: %w", err)
	}
	return found, nil
}
