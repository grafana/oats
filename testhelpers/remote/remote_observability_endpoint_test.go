package remote

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/sdk/resource"
)

func TestEndpointSearchQueryEscaping(t *testing.T) {
	const query = `{service_name="foo+bar"} |= "a&b"`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("query"); got != query {
			t.Errorf("query = %q, want %q", got, query)
		}
		if r.URL.Path == "/pyroscope/render" {
			if got := r.URL.Query().Get("from"); got != "now-1m" {
				t.Errorf("from = %q, want %q", got, "now-1m")
			}
		}
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	endpoint := &Endpoint{
		host: parsed.Hostname(),
		ports: PortsConfig{
			LokiHTTPPort:      port,
			PyroscopeHTTPPort: port,
		},
	}

	if _, err := endpoint.SearchLoki(query); err != nil {
		t.Fatalf("SearchLoki: %v", err)
	}
	if _, err := endpoint.SearchPyroscope(query); err != nil {
		t.Fatalf("SearchPyroscope: %v", err)
	}
}

func TestEndpointSearchQueryHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprint(w, "backend unavailable")
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	endpoint := &Endpoint{
		host: parsed.Hostname(),
		ports: PortsConfig{
			LokiHTTPPort:      port,
			PyroscopeHTTPPort: port,
		},
	}

	for name, search := range map[string]func(string) ([]byte, error){
		"Loki":      endpoint.SearchLoki,
		"Pyroscope": endpoint.SearchPyroscope,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := search("query")
			if err == nil {
				t.Fatal("expected HTTP status error")
			}
			if !strings.Contains(err.Error(), "503 Service Unavailable") {
				t.Fatalf("error = %q, want status", err)
			}
			if !strings.Contains(err.Error(), "backend unavailable") {
				t.Fatalf("error = %q, want response body", err)
			}
		})
	}
}

func TestEndpointHTTPMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/traces/trace-id":
			_, _ = fmt.Fprint(w, "trace")
		case "/api/search":
			if r.URL.Query().Get("q") == "" && r.URL.Query().Get("tags") == "" {
				t.Errorf("search request missing q or tags: %s", r.URL.RawQuery)
			}
			_, _ = fmt.Fprint(w, "search")
		case "/api/v1/query", "/prometheus/api/v1/query":
			if r.URL.Query().Get("query") == "" {
				t.Errorf("PromQL request missing query: %s", r.URL.RawQuery)
			}
			_, _ = fmt.Fprint(w, "metrics")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	endpoint := &Endpoint{
		host: parsed.Hostname(),
		ports: PortsConfig{
			TempoHTTPPort:      port,
			MimirHTTPPort:      port,
			PrometheusHTTPPort: port,
		},
	}

	for name, call := range map[string]func() ([]byte, error){
		"trace by id": func() ([]byte, error) {
			return endpoint.GetTraceByID(context.Background(), "trace-id")
		},
		"tempo search": func() ([]byte, error) {
			return endpoint.SearchTempo(context.Background(), "service.name=api")
		},
		"tempo tags": func() ([]byte, error) {
			return endpoint.SearchTags(context.Background(), map[string]string{"service.name": "api"})
		},
		"mimir query": func() ([]byte, error) {
			return endpoint.RunPromQL("up{job=\"oats\"}")
		},
	} {
		t.Run(name, func(t *testing.T) {
			body, err := call()
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			if len(body) == 0 {
				t.Fatal("request returned an empty body")
			}
		})
	}

	endpoint.ports.MimirHTTPPort = 0
	if body, err := endpoint.RunPromQL("up"); err != nil || string(body) != "metrics" {
		t.Fatalf("Prometheus query = %q, %v", body, err)
	}

	missingPorts := &Endpoint{}
	if _, err := missingPorts.RunPromQL("up"); err == nil || !strings.Contains(err.Error(), "MimirHTTPPort") {
		t.Fatalf("RunPromQL without a port error = %v", err)
	}
	if _, err := missingPorts.SearchLoki("{job=\"oats\"}"); err == nil || !strings.Contains(err.Error(), "LokiHTTPPort") {
		t.Fatalf("SearchLoki without a port error = %v", err)
	}
	if _, err := missingPorts.SearchPyroscope("process_cpu"); err == nil || !strings.Contains(err.Error(), "PyroscopeHTTPPort") {
		t.Fatalf("SearchPyroscope without a port error = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := endpoint.GetTraceByID(canceled, "trace-id"); err == nil {
		t.Fatal("GetTraceByID with canceled context succeeded")
	}
	if _, err := endpoint.SearchTempo(canceled, "query"); err == nil {
		t.Fatal("SearchTempo with canceled context succeeded")
	}
	if _, err := endpoint.SearchTags(canceled, nil); err == nil {
		t.Fatal("SearchTags with canceled context succeeded")
	}
}

func TestEndpointLifecycleAndComposeLogs(t *testing.T) {
	var started, stopped bool
	endpoint := NewEndpoint("unused", PortsConfig{}, func(context.Context) error {
		started = true
		return nil
	}, func(context.Context) error {
		stopped = true
		return nil
	}, func(consumer func(io.ReadCloser, *sync.WaitGroup)) error {
		var wg sync.WaitGroup
		wg.Add(1)
		consumer(io.NopCloser(strings.NewReader("first line\nneedle here\n")), &wg)
		wg.Wait()
		return nil
	})

	if err := endpoint.Start(context.Background()); err != nil || !started {
		t.Fatalf("Start = %v, started=%v", err, started)
	}
	found, err := endpoint.SearchComposeLogs("needle")
	if err != nil || !found {
		t.Fatalf("SearchComposeLogs = %v, found=%v", err, found)
	}
	if err := endpoint.Stop(context.Background()); err != nil || !stopped {
		t.Fatalf("Stop = %v, stopped=%v", err, stopped)
	}

	errorEndpoint := NewEndpoint("unused", PortsConfig{}, nil, nil, func(func(io.ReadCloser, *sync.WaitGroup)) error {
		return fmt.Errorf("log reader failed")
	})
	if found, err := errorEndpoint.SearchComposeLogs("needle"); found || err == nil || !strings.Contains(err.Error(), "log reader failed") {
		t.Fatalf("SearchComposeLogs error = found:%v err:%v", found, err)
	}
}

func TestMakeGetRequestRejectsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = fmt.Fprint(w, `{"error":"tempo unavailable"}`)
	}))
	defer server.Close()

	if _, err := (&Endpoint{}).makeGetRequest(server.URL); err == nil ||
		!strings.Contains(err.Error(), "502 Bad Gateway") ||
		!strings.Contains(err.Error(), "tempo unavailable") {
		t.Fatalf("makeGetRequest error = %v", err)
	}
}

func TestTracerProviderRequiresConfiguredPort(t *testing.T) {
	endpoint := &Endpoint{}
	if _, err := endpoint.TracerProvider(context.Background(), resource.Empty()); err == nil || !strings.Contains(err.Error(), "unknown exporter format") {
		t.Fatalf("TracerProvider without port error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := endpoint.TracerProvider(canceled, resource.Empty()); err == nil {
		t.Fatal("TracerProvider with canceled context succeeded")
	}
}
