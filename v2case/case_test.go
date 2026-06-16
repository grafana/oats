package v2case

import (
	"strings"
	"testing"
	"time"
)

func TestParse_AppSeed(t *testing.T) {
	src := []byte(`
oats: 2
name: rolldice traces have route attribute
interval: 250ms
seed:
  type: app
  compose: docker-compose.app.yml
input:
  - path: /rolldice?rolls=5
expected:
  traces:
    - traceql: '{ span.http.route = "/rolldice" }'
      contains: ["GET /rolldice"]
  metrics:
    - promql: 'rolls_total'
      value: '>= 0'
`)
	c, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Name != "rolldice traces have route attribute" {
		t.Errorf("Name: got %q", c.Name)
	}
	if c.Seed.Type != "app" || c.Seed.Compose != "docker-compose.app.yml" {
		t.Errorf("Seed: %+v", c.Seed)
	}
	if c.Interval != 250*time.Millisecond {
		t.Errorf("Interval: got %v", c.Interval)
	}
	if len(c.Input) != 1 || c.Input[0].Path != "/rolldice?rolls=5" {
		t.Errorf("Input: %+v", c.Input)
	}
	if len(c.Expected.Traces) != 1 || c.Expected.Traces[0].TraceQL == "" {
		t.Errorf("Expected.Traces: %+v", c.Expected.Traces)
	}
	if got := c.Expected.Metrics[0].Value; got != ">= 0" {
		t.Errorf("Value: got %q", got)
	}
}

func TestParse_InlineOTLPSeed(t *testing.T) {
	src := []byte(`
oats: 2
name: gcx returns seeded trace
seed:
  type: inline-otlp
  traces:
    - service: gcx-e2e-seed
      spans:
        - name: seed-operation
expected:
  traces:
    - traceql: '{ resource.service.name = "gcx-e2e-seed" }'
      contains: ["gcx-e2e-seed", "seed-operation"]
`)
	c, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Seed.Type != "inline-otlp" {
		t.Errorf("Seed.Type: got %q", c.Seed.Type)
	}
	if len(c.Seed.Traces) != 1 || c.Seed.Traces[0].Service != "gcx-e2e-seed" {
		t.Errorf("Seed.Traces: %+v", c.Seed.Traces)
	}
}

func TestParse_MatchAssertions(t *testing.T) {
	src := []byte(`
oats: 2
name: structured match
seed:
  type: app
  compose: x.yml
expected:
  logs:
    - logql: '{service_name="svc"}'
      match:
        - name: "seed-log-line"
          attributes:
            service_name: svc
            trace_id:
              present: true
        - match_type: regexp
          attributes:
            level: "info|warn"
`)
	c, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := len(c.Expected.Logs[0].Match); got != 2 {
		t.Fatalf("match entries: got %d, want 2", got)
	}
	if c.Expected.Logs[0].Match[0].EffectiveMatchType() != MatchTypeStrict {
		t.Fatalf("default match_type should be strict")
	}
	if c.Expected.Logs[0].Match[0].Attributes["trace_id"].Present == nil {
		t.Fatalf("trace_id.present should be set")
	}
	if c.Expected.Logs[0].Match[1].EffectiveMatchType() != MatchTypeRegexp {
		t.Fatalf("second entry should be regexp")
	}
}

func TestParse_RejectsUnknownFields(t *testing.T) {
	src := []byte(`
oats: 2
name: typo
seed:
  type: app
  compose: x.yml
  composr: y.yml  # typo
expected:
  traces:
    - traceql: '{ ... }'
      contains: ["x"]
`)
	_, err := Parse(src)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestValidate_MissingOats(t *testing.T) {
	c := &Case{Name: "x", Seed: Seed{Type: "app", Compose: "y"}, Expected: Expected{Traces: []TraceAssertion{{TraceQL: "{}"}}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "oats:") {
		t.Errorf("expected oats version error, got %v", err)
	}
}

func TestValidate_MissingName(t *testing.T) {
	c := &Case{OatsVersion: 2, Seed: Seed{Type: "app", Compose: "y"}, Expected: Expected{Traces: []TraceAssertion{{TraceQL: "{}"}}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "name:") {
		t.Errorf("expected name error, got %v", err)
	}
}

func TestValidate_UnknownSeedType(t *testing.T) {
	c := &Case{OatsVersion: 2, Name: "x", Seed: Seed{Type: "weird"}, Expected: Expected{Traces: []TraceAssertion{{TraceQL: "{}"}}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "seed.type") {
		t.Errorf("expected seed type error, got %v", err)
	}
}

func TestValidate_AppSeedNeedsCompose(t *testing.T) {
	c := &Case{OatsVersion: 2, Name: "x", Seed: Seed{Type: "app"}, Expected: Expected{Traces: []TraceAssertion{{TraceQL: "{}"}}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "seed.compose") {
		t.Errorf("expected compose error, got %v", err)
	}
}

func TestValidate_InlineOTLPNeedsPayload(t *testing.T) {
	c := &Case{OatsVersion: 2, Name: "x", Seed: Seed{Type: "inline-otlp"}, Expected: Expected{Traces: []TraceAssertion{{TraceQL: "{}"}}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "at least one trace") {
		t.Errorf("expected payload error, got %v", err)
	}
}

func TestValidate_NoExpectations(t *testing.T) {
	c := &Case{OatsVersion: 2, Name: "x", Seed: Seed{Type: "app", Compose: "y"}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "expected:") {
		t.Errorf("expected 'no expectations' error, got %v", err)
	}
}

func TestValidate_RejectsInputWithoutPath(t *testing.T) {
	_, err := Parse([]byte(`
oats: 2
name: bad input
seed:
  type: app
  compose: x.yml
input:
  - method: POST
expected:
  metrics:
    - promql: up
      value: ">= 1"
`))
	if err == nil || !strings.Contains(err.Error(), "input[0].path") {
		t.Fatalf("expected input path error, got %v", err)
	}
}

func TestValidate_RejectsUnknownMatchType(t *testing.T) {
	_, err := Parse([]byte(`
oats: 2
name: bad match type
seed:
  type: app
  compose: x.yml
expected:
  traces:
    - traceql: '{}'
      match:
        - match_type: glob
          name: x
`))
	if err == nil || !strings.Contains(err.Error(), "match_type") {
		t.Fatalf("expected match_type error, got %v", err)
	}
}

func TestValidate_RejectsPresentFalse(t *testing.T) {
	_, err := Parse([]byte(`
oats: 2
name: bad present
seed:
  type: app
  compose: x.yml
expected:
  logs:
    - logql: '{job="x"}'
      match:
        - attributes:
            trace_id:
              present: false
`))
	if err == nil || !strings.Contains(err.Error(), "only true is allowed") {
		t.Fatalf("expected present error, got %v", err)
	}
}

func TestValidate_RejectsInvalidMatchRegexp(t *testing.T) {
	_, err := Parse([]byte(`
oats: 2
name: bad regexp
seed:
  type: app
  compose: x.yml
expected:
  traces:
    - traceql: '{}'
      match:
        - match_type: regexp
          name: '['
`))
	if err == nil || !strings.Contains(err.Error(), "invalid regexp") {
		t.Fatalf("expected regexp error, got %v", err)
	}
}

func TestIsHermetic(t *testing.T) {
	c := &Case{}
	if !c.IsHermetic() {
		t.Error("default should be hermetic")
	}
	f := false
	c.Hermetic = &f
	if c.IsHermetic() {
		t.Error("explicit false should override")
	}
}
