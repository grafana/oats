package seed

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type recordingHandler struct {
	mu       sync.Mutex
	requests map[string][]byte
}

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	h.mu.Lock()
	h.requests[r.URL.Path] = body
	h.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func newRecorder() (*httptest.Server, *recordingHandler) {
	h := &recordingHandler{requests: make(map[string][]byte)}
	return httptest.NewServer(h), h
}

func TestSender_SendTracesLogsMetrics(t *testing.T) {
	srv, h := newRecorder()
	defer srv.Close()

	s := &Sender{OTLPEndpoint: srv.URL}
	err := s.Send(context.Background(), Payload{
		Traces: []Trace{{
			Service: "svc-a",
			Span:    SpanFields{Name: "do-thing"},
		}},
		Logs: []Log{{
			Service: "svc-a",
			Body:    "hello",
		}},
		Metrics: []Metric{{
			Service: "svc-a",
			Name:    "things_total",
			Value:   42,
		}},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if _, ok := h.requests["/v1/traces"]; !ok {
		t.Error("traces endpoint not hit")
	}
	if _, ok := h.requests["/v1/logs"]; !ok {
		t.Error("logs endpoint not hit")
	}
	if _, ok := h.requests["/v1/metrics"]; !ok {
		t.Error("metrics endpoint not hit")
	}

	for _, want := range []struct {
		path, needle string
	}{
		{"/v1/traces", "do-thing"},
		{"/v1/traces", "svc-a"},
		{"/v1/logs", "hello"},
		{"/v1/metrics", "things_total"},
	} {
		if !strings.Contains(string(h.requests[want.path]), want.needle) {
			t.Errorf("payload at %s missing %q:\n%s", want.path, want.needle, h.requests[want.path])
		}
	}
}

func TestSender_EscapesSpecialCharacters(t *testing.T) {
	// Values a case author could plausibly write that Go's %q renders as
	// non-JSON (\v, a control char) or that need escaping (" \ newline tab).
	// Every captured payload must parse as JSON and round-trip the value.
	srv, h := newRecorder()
	defer srv.Close()

	tricky := "a\"b\\c\nd\te\vf\x01g"
	s := &Sender{OTLPEndpoint: srv.URL}
	err := s.Send(context.Background(), Payload{
		Traces:  []Trace{{Service: tricky, Span: SpanFields{Name: tricky}}},
		Logs:    []Log{{Service: tricky, Body: tricky, SeverityText: tricky}},
		Metrics: []Metric{{Service: tricky, Name: tricky, Value: 1}},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	for _, path := range []string{"/v1/traces", "/v1/logs", "/v1/metrics"} {
		if !json.Valid(h.requests[path]) {
			t.Errorf("payload at %s is not valid JSON:\n%s", path, h.requests[path])
		}
	}

	// Spot-check the value round-trips intact via the trace span name.
	var traces struct {
		ResourceSpans []struct {
			ScopeSpans []struct {
				Spans []struct {
					Name string `json:"name"`
				} `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	if err := json.Unmarshal(h.requests["/v1/traces"], &traces); err != nil {
		t.Fatalf("unmarshal traces: %v", err)
	}
	if got := traces.ResourceSpans[0].ScopeSpans[0].Spans[0].Name; got != tricky {
		t.Errorf("span name round-trip: got %q, want %q", got, tricky)
	}
}

func TestSender_EmptyEndpointFailsLoudly(t *testing.T) {
	s := &Sender{}
	if err := s.Send(context.Background(), Payload{}); err == nil {
		t.Fatal("expected an error for empty endpoint")
	}
}

func TestSender_BackendErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("rejected"))
	}))
	defer srv.Close()

	s := &Sender{OTLPEndpoint: srv.URL}
	err := s.Send(context.Background(), Payload{Traces: []Trace{{Service: "x", Span: SpanFields{Name: "y"}}}})
	if err == nil {
		t.Fatal("expected an error from 400 response")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("error should include backend body, got: %v", err)
	}
}

func TestSender_SpanDefaultsApply(t *testing.T) {
	srv, h := newRecorder()
	defer srv.Close()

	s := &Sender{OTLPEndpoint: srv.URL}
	err := s.Send(context.Background(), Payload{Traces: []Trace{{
		Service: "svc",
		Span:    SpanFields{Name: "n"},
	}}})
	if err != nil {
		t.Fatal(err)
	}

	// Decode just enough to verify defaults made it into the wire payload.
	var parsed struct {
		ResourceSpans []struct {
			ScopeSpans []struct {
				Spans []struct {
					Kind              int    `json:"kind"`
					StartTimeUnixNano string `json:"startTimeUnixNano"`
					EndTimeUnixNano   string `json:"endTimeUnixNano"`
				} `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	if err := json.Unmarshal(h.requests["/v1/traces"], &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	span := parsed.ResourceSpans[0].ScopeSpans[0].Spans[0]
	if span.Kind != 1 {
		t.Errorf("Kind default: got %d, want 1", span.Kind)
	}
	if span.StartTimeUnixNano == "" || span.EndTimeUnixNano == "" {
		t.Errorf("timestamps empty: %+v", span)
	}
}
