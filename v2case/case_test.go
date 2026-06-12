package v2case

import (
	"strings"
	"testing"
)

func TestParse_AppSeed(t *testing.T) {
	src := []byte(`
oats: 2
name: rolldice traces have route attribute
seed:
  type: app
  compose: docker-compose.app.yml
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
