package discovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/oats/casefile"
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

const validCaseYAML = `name: %s
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
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
suites:
  - name: lgtm
    cases: ["cases/*.yaml"]
    fixture: lgtm-shared
    tags: ["traces"]
fixture:
  lgtm-shared:
    compose:
      template: lgtm
`)
	writeFile(t, dir, "cases/a.yaml", strings.Replace(validCaseYAML, "%s", "case-a", 1))
	writeFile(t, dir, "cases/b.yaml", strings.Replace(validCaseYAML, "%s", "case-b", 1))

	cfg, err := Load(filepath.Join(dir, "oats-config.yaml"))
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
	if plans[0].Fixture.Kind() != "compose" {
		t.Errorf("fixture resolved wrong: %+v", plans[0].Fixture)
	}
}

func TestLoad_RejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
typooo: boom
suites:
  - name: x
    cases: ["a.yaml"]
`)
	_, err := Load(filepath.Join(dir, "oats-config.yaml"))
	if err == nil || !strings.Contains(err.Error(), "typooo") {
		t.Errorf("expected unknown-key error, got %v", err)
	}
}

func TestPlanRun_FilterByTag(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
suites:
  - name: alpha
    cases: ["a.yaml"]
    tags: ["traces"]
  - name: beta
    cases: ["b.yaml"]
    tags: ["logs"]
`)
	writeFile(t, dir, "a.yaml", strings.Replace(validCaseYAML, "%s", "a", 1))
	writeFile(t, dir, "b.yaml", strings.Replace(validCaseYAML, "%s", "b", 1))

	cfg, err := Load(filepath.Join(dir, "oats-config.yaml"))
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

func TestSummary_TopLevelCases(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
cases: ["cases/*.yaml"]
meta:
  version: 3
`)
	writeFile(t, dir, "cases/a.yaml", strings.Replace(validCaseYAML, "%s", "case-a", 1))

	cfg, err := Load(filepath.Join(dir, "oats-config.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := cfg.Summary()
	if !strings.Contains(got, `suite=cases`) {
		t.Fatalf("Summary missing synthesized suite label: %q", got)
	}
	if !strings.Contains(got, `cases=[cases/*.yaml]`) {
		t.Fatalf("Summary missing cases glob: %q", got)
	}
}

func TestPlanRun_FilterByPath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
suites:
  - name: alpha
    cases: ["a/*.yaml"]
  - name: beta
    cases: ["b/*.yaml"]
`)
	writeFile(t, dir, "a/x.yaml", strings.Replace(validCaseYAML, "%s", "ax", 1))
	writeFile(t, dir, "b/y.yaml", strings.Replace(validCaseYAML, "%s", "by", 1))

	cfg, err := Load(filepath.Join(dir, "oats-config.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Directory scope: only the suite whose cases live under a/ survives.
	plans, err := cfg.PlanRun(Filter{Paths: []string{filepath.Join(dir, "a")}})
	if err != nil {
		t.Fatalf("PlanRun dir scope: %v", err)
	}
	if len(plans) != 1 || plans[0].Suite.Name != "alpha" || len(plans[0].Cases) != 1 {
		t.Fatalf("dir path filter: %+v", planNames(plans))
	}

	// Single-file scope: the exact case file.
	plans, err = cfg.PlanRun(Filter{Paths: []string{filepath.Join(dir, "b", "y.yaml")}})
	if err != nil {
		t.Fatalf("PlanRun file scope: %v", err)
	}
	if len(plans) != 1 || plans[0].Suite.Name != "beta" || len(plans[0].Cases) != 1 {
		t.Fatalf("file path filter: %+v", planNames(plans))
	}
}

func TestPlanRun_FilterBySuiteName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
suites:
  - name: alpha
    cases: ["a.yaml"]
  - name: beta
    cases: ["b.yaml"]
`)
	writeFile(t, dir, "a.yaml", strings.Replace(validCaseYAML, "%s", "a", 1))
	writeFile(t, dir, "b.yaml", strings.Replace(validCaseYAML, "%s", "b", 1))

	cfg, err := Load(filepath.Join(dir, "oats-config.yaml"))
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

func TestValidate_EmptyFixtureRejected(t *testing.T) {
	cfg := &RootConfig{
		Meta: Meta{Version: 3},
		Suites: []SuiteConfig{{
			Name: "s", Cases: []string{"a.yaml"}, Fixture: "x",
		}},
		Fixture: map[string]casefile.FixtureConfig{"x": {}},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "set exactly one of compose/k3d/remote") {
		t.Errorf("expected empty-fixture error, got %v", err)
	}
}

func TestValidate_MultipleFixtureBlocksRejected(t *testing.T) {
	cfg := &RootConfig{
		Meta: Meta{Version: 3},
		Suites: []SuiteConfig{{
			Name: "s", Cases: []string{"a.yaml"}, Fixture: "x",
		}},
		Fixture: map[string]casefile.FixtureConfig{"x": {
			Compose: &casefile.ComposeFixture{Template: "lgtm"},
			Remote:  &casefile.RemoteFixture{Endpoint: "http://localhost:4318"},
		}},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "set exactly one of compose/k3d/remote") {
		t.Errorf("expected multiple-block error, got %v", err)
	}
}

func TestValidate_FixtureRefNotDefined(t *testing.T) {
	cfg := &RootConfig{
		Meta: Meta{Version: 3},
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
		Meta:    Meta{Version: 3},
		Suites:  []SuiteConfig{{Name: "s", Cases: []string{"a.yaml"}}},
		Fixture: map[string]casefile.FixtureConfig{"r": {Remote: &casefile.RemoteFixture{}}},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("expected endpoint error, got %v", err)
	}
}

func TestValidate_ComposeFilesConflict(t *testing.T) {
	cfg := &RootConfig{
		Meta: Meta{Version: 3},
		Suites: []SuiteConfig{{
			Name: "s", Cases: []string{"a.yaml"}, Fixture: "c",
		}},
		Fixture: map[string]casefile.FixtureConfig{"c": {
			Compose: &casefile.ComposeFixture{
				File: "one.yml",
				Files: []string{
					"two.yml",
				},
			},
		}},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "compose sets file or files, not both") {
		t.Errorf("expected compose conflict error, got %v", err)
	}
}

func TestValidate_K3DRequiresFields(t *testing.T) {
	cfg := &RootConfig{
		Meta: Meta{Version: 3},
		Suites: []SuiteConfig{{
			Name: "s", Cases: []string{"a.yaml"}, Fixture: "k",
		}},
		Fixture: map[string]casefile.FixtureConfig{"k": {
			K3D: &casefile.K3DFixture{},
		}},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "k3d requires") {
		t.Errorf("expected k3d field error, got %v", err)
	}
}

func TestPlanRun_EmptyGlobIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
suites:
  - name: s
    cases: ["nope/*.yaml"]
`)
	cfg, err := Load(filepath.Join(dir, "oats-config.yaml"))
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

func TestExampleV2SmokeConfigLoads(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "examples", "smoke", "oats-config.yaml"))
	if err != nil {
		t.Fatalf("Load example config: %v", err)
	}
	plans, err := cfg.PlanRun(Filter{})
	if err != nil {
		t.Fatalf("PlanRun example config: %v", err)
	}
	if len(plans) != 1 || len(plans[0].Cases) != 4 {
		t.Fatalf("expected one suite/four cases, got %+v", plans)
	}
	got := []string{plans[0].Cases[0].Name, plans[0].Cases[1].Name, plans[0].Cases[2].Name, plans[0].Cases[3].Name}
	want := []string{"custom check smoke", "inline seed smoke", "profile smoke", "rolldice smoke"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected case order: got %v want %v", got, want)
		}
	}
}

func TestExampleV2FixturesConfigLoads(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "examples", "fixtures", "oats-config.yaml"))
	if err != nil {
		t.Fatalf("Load fixture example config: %v", err)
	}
	plans, err := cfg.PlanRun(Filter{})
	if err != nil {
		t.Fatalf("PlanRun fixture example config: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("expected two suites, got %+v", plans)
	}
	if plans[0].Suite.Name != "compose-app" || plans[0].Fixture.Kind() != "compose" || len(plans[0].Cases) != 1 {
		t.Fatalf("unexpected first plan: %+v", plans[0])
	}
	if plans[1].Suite.Name != "k3d-app" || plans[1].Fixture.Kind() != "k3d" || len(plans[1].Cases) != 1 {
		t.Fatalf("unexpected second plan: %+v", plans[1])
	}
	if plans[1].Cases[0].Name != "k3d fixture app smoke" {
		t.Fatalf("unexpected k3d case name: %q", plans[1].Cases[0].Name)
	}
}

func planNames(p []Plan) []string {
	out := make([]string, len(p))
	for i := range p {
		out[i] = p[i].Suite.Name
	}
	return out
}

func TestLoadTopLevelCases(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
cases: ["cases/a.yaml", "cases/b.yaml"]
meta:
  version: 3
`)
	writeFile(t, dir, "cases/a.yaml", `
name: a
seed: { type: app }
expected:
  traces:
    - traceql: "{}"
      absent: true
`)
	writeFile(t, dir, "cases/b.yaml", `
name: b
seed: { type: app }
expected:
  traces:
    - traceql: "{}"
      absent: true
`)
	cfg, err := Load(filepath.Join(dir, "oats-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	plans, err := cfg.PlanRun(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(plans))
	}
	if plans[0].Suite.Name != "a" || plans[1].Suite.Name != "b" {
		t.Fatalf("unexpected suite names: %q, %q", plans[0].Suite.Name, plans[1].Suite.Name)
	}
}
