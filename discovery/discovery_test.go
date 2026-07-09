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
fixture:
  compose:
    template: lgtm
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
    tags: ["traces"]
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

func TestPlanRun_NoFixtureDefaultsToComposeLGTM(t *testing.T) {
	// A suite with no fixture (and cases that declare none) now defaults to a
	// compose fixture booting the builtin lgtm stack, not an external setup.
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
suites:
  - name: inline
    cases: ["cases/*.yaml"]
`)
	writeFile(t, dir, "cases/a.yaml", `name: inline smoke
seed:
  type: inline-otlp
  logs:
    - service: a
      body: line
expected:
  logs:
    - logql: '{service_name="a"}'
      contains: line
`)

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
	if plans[0].Fixture.Kind() != "compose" {
		t.Fatalf("expected no-fixture suite to default to compose, got %q (%+v)", plans[0].Fixture.Kind(), plans[0].Fixture)
	}
	if got := plans[0].Fixture.Compose.EffectiveTemplate(); got != "lgtm" {
		t.Fatalf("expected default compose template lgtm, got %q", got)
	}
	if plans[0].FixtureSourceDir != dir {
		t.Fatalf("expected fixture source dir %q, got %q", dir, plans[0].FixtureSourceDir)
	}
}

// TestResolveSuiteFixture_SingleInferred verifies a suite infers its one shared
// fixture from cases that all declare the same fixture, with the source dir
// taken from the declaring cases.
func TestResolveSuiteFixture_SingleInferred(t *testing.T) {
	cfg := &RootConfig{SourceDir: "/cfg"}
	cases := []*casefile.Case{
		{Name: "a", SourcePath: "/suite/a.yaml", Fixture: &casefile.FixtureConfig{Compose: &casefile.ComposeFixture{Template: "lgtm"}}},
		{Name: "b", SourcePath: "/suite/b.yaml", Fixture: &casefile.FixtureConfig{Compose: &casefile.ComposeFixture{Template: "lgtm"}}},
	}
	fixture, sourceDir, err := cfg.resolveSuiteFixture(cases)
	if err != nil {
		t.Fatalf("resolveSuiteFixture: %v", err)
	}
	if fixture.Kind() != "compose" {
		t.Errorf("expected compose fixture, got %q (%+v)", fixture.Kind(), fixture)
	}
	if sourceDir != "/suite" {
		t.Errorf("expected source dir /suite, got %q", sourceDir)
	}
}

// TestResolveSuiteFixture_DefaultsToComposeLGTM verifies that when no case
// declares a fixture, the suite defaults to a compose fixture booting the
// builtin lgtm template, rooted at the config's source dir.
func TestResolveSuiteFixture_DefaultsToComposeLGTM(t *testing.T) {
	cfg := &RootConfig{SourceDir: "/cfg"}
	cases := []*casefile.Case{
		{Name: "a", SourcePath: "/suite/a.yaml"},
		{Name: "b", SourcePath: "/suite/b.yaml"},
	}
	fixture, sourceDir, err := cfg.resolveSuiteFixture(cases)
	if err != nil {
		t.Fatalf("resolveSuiteFixture: %v", err)
	}
	if fixture.Kind() != "compose" || fixture.Compose.EffectiveTemplate() != "lgtm" {
		t.Errorf("expected default compose/lgtm fixture, got %q (%+v)", fixture.Kind(), fixture)
	}
	if sourceDir != "/cfg" {
		t.Errorf("expected source dir /cfg, got %q", sourceDir)
	}
}

// TestResolveSuiteFixture_Disagreement verifies that a suite whose cases declare
// conflicting fixtures is rejected: a suite boots exactly one shared fixture.
func TestResolveSuiteFixture_Disagreement(t *testing.T) {
	cfg := &RootConfig{SourceDir: "/cfg"}
	cases := []*casefile.Case{
		{Name: "a", SourcePath: "/suite/a.yaml", Fixture: &casefile.FixtureConfig{Compose: &casefile.ComposeFixture{Template: "lgtm"}}},
		{Name: "b", SourcePath: "/suite/b.yaml", Fixture: &casefile.FixtureConfig{Remote: &casefile.RemoteFixture{Endpoint: "http://localhost:4318"}}},
	}
	if _, _, err := cfg.resolveSuiteFixture(cases); err == nil || !strings.Contains(err.Error(), "do not agree on one shared fixture") {
		t.Errorf("expected disagreement error, got %v", err)
	}
}

// A path-less fixture (remote) is the same fixture regardless of directory, so
// identical copies in different dirs must agree.
func TestResolveSuiteFixture_RemoteAcrossDirsAgrees(t *testing.T) {
	cfg := &RootConfig{SourceDir: "/cfg"}
	remote := func() *casefile.FixtureConfig {
		return &casefile.FixtureConfig{Remote: &casefile.RemoteFixture{Endpoint: "http://localhost:4318"}}
	}
	cases := []*casefile.Case{
		{Name: "a", SourcePath: "/suite/a/oats-case.yaml", Fixture: remote()},
		{Name: "b", SourcePath: "/suite/b/oats-case.yaml", Fixture: remote()},
	}
	got, _, err := cfg.resolveSuiteFixture(cases)
	if err != nil {
		t.Fatalf("expected remote fixtures in different dirs to agree, got %v", err)
	}
	if got.Kind() != "remote" {
		t.Errorf("resolved fixture kind: got %q want remote", got.Kind())
	}
}

// A dir-relative fixture (compose file/files) means different files in different
// dirs, so identical copies across dirs must NOT agree.
func TestResolveSuiteFixture_ComposeFilesAcrossDirsConflict(t *testing.T) {
	cfg := &RootConfig{SourceDir: "/cfg"}
	compose := func() *casefile.FixtureConfig {
		return &casefile.FixtureConfig{Compose: &casefile.ComposeFixture{File: "docker-compose.oats.yml"}}
	}
	cases := []*casefile.Case{
		{Name: "a", SourcePath: "/suite/a/oats-case.yaml", Fixture: compose()},
		{Name: "b", SourcePath: "/suite/b/oats-case.yaml", Fixture: compose()},
	}
	if _, _, err := cfg.resolveSuiteFixture(cases); err == nil || !strings.Contains(err.Error(), "different directories") {
		t.Errorf("expected different-directories error, got %v", err)
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
			{Name: "alpha", Tags: []string{"traces"}, Cases: []string{"a.yaml"}},
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
