package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/grafana/oats/casefile"
)

func TestRunCase_TraceStructuredMatchPass(t *testing.T) {
	exec := &stubExec{stdout: `{"data":{"result":[{"spanName":"operation","attributes":{"service.name":"svc"}}]}}`}
	r, _ := newRunner(t, exec, Options{Timeout: 100 * time.Millisecond, Interval: 5 * time.Millisecond, SeedSettleDelay: 1})

	c := mustParse(t, `
name: structured trace
seed:
  type: app
expected:
  traces:
    - traceql: '{}'
      match_spans:
        - name: operation
          attributes:
            - key: service.name
              value: svc
`)
	if !r.RunCase(context.Background(), c) {
		t.Fatal("expected structured trace case to pass")
	}
}

func TestRunCase_ProfileStructuredMatchPass(t *testing.T) {
	exec := &stubExec{stdout: `{"flamebearer":{"names":["main","worker"]}}`}
	r, _ := newRunner(t, exec, Options{Timeout: 100 * time.Millisecond, Interval: 5 * time.Millisecond, SeedSettleDelay: 1})

	c := mustParse(t, `
name: structured profile
seed:
  type: app
expected:
  profiles:
    - query: process_cpu
      match:
        - name: worker
`)
	if !r.RunCase(context.Background(), c) {
		t.Fatal("expected structured profile case to pass")
	}
}

func TestFetchTraceRowsByID(t *testing.T) {
	exec := &stubExec{stdout: `{"data":{"result":[{"spanName":"operation"}]}}`}
	r, _ := newRunner(t, exec, Options{Timeout: 100 * time.Millisecond})
	c := mustParse(t, `
name: trace lookup
seed:
  type: app
expected:
  traces:
    - traceql: '{}'
`)

	rows, count, err := r.fetchTraceRows(context.Background(), c, `{"traces":[{"traceID":"abc"}]}`)
	if err != nil {
		t.Fatalf("fetchTraceRows: %v", err)
	}
	if count != 1 || len(rows) != 1 || rows[0].Name != "operation" {
		t.Fatalf("rows=%#v count=%d", rows, count)
	}
	if len(exec.captured) != 1 || exec.captured[0][0] != "traces" || exec.captured[0][1] != "get" || exec.captured[0][len(exec.captured[0])-1] != "abc" {
		t.Fatalf("unexpected TraceGet args: %#v", exec.captured)
	}
}

func TestDoInputValidation(t *testing.T) {
	r, _ := newRunner(t, &stubExec{}, Options{Timeout: time.Millisecond})
	if err := r.doInput(casefile.Input{Path: "/health"}); err == nil || !strings.Contains(err.Error(), "application endpoint") {
		t.Fatalf("missing endpoint error = %v", err)
	}
	if err := r.doInput(casefile.Input{}); err != nil {
		t.Fatalf("empty input should be ignored: %v", err)
	}

	invalidStatus, _ := newRunner(t, &stubExec{}, Options{Timeout: time.Millisecond})
	invalidStatus.endpoint.AppHost = "127.0.0.1"
	invalidStatus.endpoint.AppPort = 1
	if err := invalidStatus.doInput(casefile.Input{Path: "/health", Status: "created"}); err == nil || !strings.Contains(err.Error(), "not an integer") {
		t.Fatalf("invalid status error = %v", err)
	}
}

func TestSeedCaseRejectsUnknownType(t *testing.T) {
	r, _ := newRunner(t, &stubExec{}, Options{})
	c := mustParse(t, `
name: unknown seed
seed:
  type: app
expected:
  traces:
    - traceql: '{}'
`)
	c.Seed.Type = "unknown"
	if err := r.seedCase(context.Background(), c); err == nil || !strings.Contains(err.Error(), "unknown seed type") {
		t.Fatalf("seedCase error = %v", err)
	}
}
