package discovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const validCaseYAML = `oats: 2
name: %s
seed:
  type: app
  compose: docker-compose.app.yml
expected:
  traces:
    - traceql: '{}'
      contains: ["x"]
`

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats.toml", `
[meta]
version = 2

[[suite]]
name = "lgtm"
cases = ["cases/*.yaml"]
fixture = "lgtm-shared"
tags = ["traces"]

[fixture.lgtm-shared]
type = "compose"
template = "lgtm"
`)
	writeFile(t, dir, "cases/a.yaml", strings.Replace(validCaseYAML, "%s", "case-a", 1))
	writeFile(t, dir, "cases/b.yaml", strings.Replace(validCaseYAML, "%s", "case-b", 1))

	cfg, err := Load(filepath.Join(dir, "oats.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	plans, err := cfg.PlanRun(Filter{})
	if err != nil {
		t.Fatalf("PlanRun: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans: got %d, want 1", len(plans))
	}
	if len(plans[0].Cases) != 2 {
		t.Errorf("cases: got %d, want 2", len(plans[0].Cases))
	}
	if plans[0].Cases[0].Name != "case-a" {
		t.Errorf("sort order: got %q first", plans[0].Cases[0].Name)
	}
	if plans[0].Fixture.Type != "compose" {
		t.Errorf("fixture resolved wrong: %+v", plans[0].Fixture)
	}
}

func TestLoad_RejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats.toml", `
[meta]
version = 2
typooo = "boom"
[[suite]]
name = "x"
cases = ["a.yaml"]
`)
	_, err := Load(filepath.Join(dir, "oats.toml"))
	if err == nil || !strings.Contains(err.Error(), "unknown keys") {
		t.Errorf("expected unknown-keys error, got %v", err)
	}
}

func TestPlanRun_FilterByTag(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats.toml", `
[meta]
version = 2

[[suite]]
name = "alpha"
cases = ["a.yaml"]
tags = ["traces"]

[[suite]]
name = "beta"
cases = ["b.yaml"]
tags = ["logs"]
`)
	writeFile(t, dir, "a.yaml", strings.Replace(validCaseYAML, "%s", "a", 1))
	writeFile(t, dir, "b.yaml", strings.Replace(validCaseYAML, "%s", "b", 1))

	cfg, err := Load(filepath.Join(dir, "oats.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	plans, err := cfg.PlanRun(Filter{Tags: []string{"traces"}})
	if err != nil {
		t.Fatalf("PlanRun: %v", err)
	}
	if len(plans) != 1 || plans[0].Suite.Name != "alpha" {
		t.Errorf("tag filter: %+v", planNames(plans))
	}
}

func TestPlanRun_FilterBySuiteName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats.toml", `
[meta]
version = 2

[[suite]]
name = "alpha"
cases = ["a.yaml"]

[[suite]]
name = "beta"
cases = ["b.yaml"]
`)
	writeFile(t, dir, "a.yaml", strings.Replace(validCaseYAML, "%s", "a", 1))
	writeFile(t, dir, "b.yaml", strings.Replace(validCaseYAML, "%s", "b", 1))

	cfg, err := Load(filepath.Join(dir, "oats.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	plans, err := cfg.PlanRun(Filter{Suites: []string{"beta"}})
	if err != nil {
		t.Fatalf("PlanRun: %v", err)
	}
	if len(plans) != 1 || plans[0].Suite.Name != "beta" {
		t.Errorf("suite filter: %+v", planNames(plans))
	}
}

func TestValidate_BadFixtureType(t *testing.T) {
	cfg := &RootConfig{
		Meta: Meta{Version: 2},
		Suites: []SuiteConfig{{
			Name: "s", Cases: []string{"a.yaml"}, Fixture: "x",
		}},
		Fixture: map[string]FixtureConfig{"x": {Type: "weird"}},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `unknown type "weird"`) {
		t.Errorf("expected unknown-type error, got %v", err)
	}
}

func TestValidate_FixtureRefNotDefined(t *testing.T) {
	cfg := &RootConfig{
		Meta: Meta{Version: 2},
		Suites: []SuiteConfig{{
			Name: "s", Cases: []string{"a.yaml"}, Fixture: "missing",
		}},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `fixture "missing" not defined`) {
		t.Errorf("expected fixture-missing error, got %v", err)
	}
}

func TestValidate_RemoteRequiresEndpoint(t *testing.T) {
	cfg := &RootConfig{
		Meta:    Meta{Version: 2},
		Suites:  []SuiteConfig{{Name: "s", Cases: []string{"a.yaml"}}},
		Fixture: map[string]FixtureConfig{"r": {Type: "remote"}},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("expected endpoint error, got %v", err)
	}
}

func TestPlanRun_EmptyGlobIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats.toml", `
[meta]
version = 2

[[suite]]
name = "s"
cases = ["nope/*.yaml"]
`)
	cfg, err := Load(filepath.Join(dir, "oats.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = cfg.PlanRun(Filter{})
	if err == nil || !strings.Contains(err.Error(), "matched zero files") {
		t.Errorf("expected zero-match error, got %v", err)
	}
}

func TestSummary(t *testing.T) {
	cfg := &RootConfig{
		Suites: []SuiteConfig{
			{Name: "alpha", Fixture: "x", Tags: []string{"traces"}, Cases: []string{"a.yaml"}},
		},
	}
	s := cfg.Summary()
	if !strings.Contains(s, "suite=alpha") {
		t.Errorf("Summary missing suite line: %q", s)
	}
}

func planNames(p []Plan) []string {
	out := make([]string, len(p))
	for i := range p {
		out[i] = p[i].Suite.Name
	}
	return out
}
