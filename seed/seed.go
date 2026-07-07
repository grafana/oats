// Package seed pushes known OTLP payloads into a running observability stack.
//
// The "inline-otlp" seed mode lets a case yaml carry its own data instead of
// booting an instrumented example app. This decouples gcx-contract tests from
// SDK behaviour: a case that wants to assert "gcx returns the trace I just
// pushed" can declare the trace inline, eliminating an entire layer of
// causation.
//
// The wire format is OTLP/HTTP JSON, which every supported backend accepts
// without an SDK on the test runner's side. We hand-write the JSON rather
// than pull in the OTel Go SDK because (a) the payloads are small and
// (b) we want the test author to see exactly what was sent when an assertion
// fails — no exporter middleware in between.
package seed

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var defaultHTTPClient = &http.Client{Timeout: 10 * time.Second}

// Payload describes one inline-otlp seed declaration as it arrives from a
// case yaml. All three signal slices are optional; emitting only the ones
// the case cares about keeps inline payloads short.
type Payload struct {
	Traces  []Trace
	Logs    []Log
	Metrics []Metric
}

type Trace struct {
	Service string
	Span    SpanFields
}

type SpanFields struct {
	Name string
	// Kind defaults to 1 (SPAN_KIND_INTERNAL) when zero.
	Kind int
	// Duration defaults to 200ms when zero.
	Duration time.Duration
}

type Log struct {
	Service        string
	Body           string
	SeverityNumber int    // defaults to 9 (INFO) when zero
	SeverityText   string // defaults to "INFO" when empty
}

type Metric struct {
	Service string
	Name    string
	Value   int64
}

// Sender pushes a Payload at an OTLP/HTTP endpoint (e.g. http://localhost:4318).
// The endpoint is the base URL — the canonical /v1/{traces,logs,metrics} paths
// are appended internally.
type Sender struct {
	OTLPEndpoint string
	Client       *http.Client
}

func (s *Sender) httpClient() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return defaultHTTPClient
}

// Send pushes all signals declared in p. Returns the first error encountered,
// but processes signals in a fixed order (traces, logs, metrics) so that a
// partial send leaves the backend in a predictable state for assertions.
// Cancelling ctx aborts in-flight and remaining requests so seeding does not
// outlive a cancelled run.
func (s *Sender) Send(ctx context.Context, p Payload) error {
	if s.OTLPEndpoint == "" {
		return fmt.Errorf("seed: OTLPEndpoint is empty")
	}
	now := time.Now()
	for _, t := range p.Traces {
		if err := s.sendTrace(ctx, t, now); err != nil {
			return fmt.Errorf("seed traces: %w", err)
		}
	}
	for _, l := range p.Logs {
		if err := s.sendLog(ctx, l, now); err != nil {
			return fmt.Errorf("seed logs: %w", err)
		}
	}
	for _, m := range p.Metrics {
		if err := s.sendMetric(ctx, m, now); err != nil {
			return fmt.Errorf("seed metrics: %w", err)
		}
	}
	return nil
}

func (s *Sender) sendTrace(ctx context.Context, t Trace, now time.Time) error {
	dur := t.Span.Duration
	if dur == 0 {
		dur = 200 * time.Millisecond
	}
	kind := t.Span.Kind
	if kind == 0 {
		kind = 1
	}
	end := now.UnixNano()
	start := now.Add(-dur).UnixNano()

	body := fmt.Sprintf(`{
  "resourceSpans": [{
    "resource": {"attributes": [
      {"key":"service.name","value":{"stringValue":%q}}
    ]},
    "scopeSpans": [{
      "scope": {"name":"oats-inline-seed"},
      "spans": [{
        "traceId": %q,
        "spanId": %q,
        "name": %q,
        "kind": %d,
        "startTimeUnixNano": "%d",
        "endTimeUnixNano": "%d"
      }]
    }]
  }]
}`, t.Service, mustRandHex(16), mustRandHex(8), t.Span.Name, kind, start, end)
	return s.post(ctx, "/v1/traces", body)
}

func (s *Sender) sendLog(ctx context.Context, l Log, now time.Time) error {
	sev := l.SeverityNumber
	if sev == 0 {
		sev = 9
	}
	sevText := l.SeverityText
	if sevText == "" {
		sevText = "INFO"
	}
	body := fmt.Sprintf(`{
  "resourceLogs": [{
    "resource": {"attributes": [
      {"key":"service.name","value":{"stringValue":%q}}
    ]},
    "scopeLogs": [{
      "scope": {"name":"oats-inline-seed"},
      "logRecords": [{
        "timeUnixNano": "%d",
        "severityNumber": %d,
        "severityText": %q,
        "body": {"stringValue": %q}
      }]
    }]
  }]
}`, l.Service, now.UnixNano(), sev, sevText, l.Body)
	return s.post(ctx, "/v1/logs", body)
}

func (s *Sender) sendMetric(ctx context.Context, m Metric, now time.Time) error {
	end := now.UnixNano()
	start := now.Add(-time.Second).UnixNano()
	body := fmt.Sprintf(`{
  "resourceMetrics": [{
    "resource": {"attributes": [
      {"key":"service.name","value":{"stringValue":%q}}
    ]},
    "scopeMetrics": [{
      "scope": {"name":"oats-inline-seed"},
      "metrics": [{
        "name": %q,
        "sum": {
          "isMonotonic": true,
          "aggregationTemporality": 2,
          "dataPoints": [{
            "startTimeUnixNano": "%d",
            "timeUnixNano": "%d",
            "asInt": "%d"
          }]
        }
      }]
    }]
  }]
}`, m.Service, m.Name, start, end, m.Value)
	return s.post(ctx, "/v1/metrics", body)
}

func (s *Sender) post(ctx context.Context, path, body string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.OTLPEndpoint+path, bytes.NewBufferString(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s\n%s", path, resp.Status, data)
	}
	return nil
}

func randHex(nBytes int) (string, error) {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func mustRandHex(nBytes int) string {
	s, err := randHex(nBytes)
	if err != nil {
		// Extremely rare; fall back to a stable all-zero ID rather than panic so
		// the run fails, if at all, through normal backend/query behavior.
		return strings.Repeat("0", nBytes*2)
	}
	return s
}
