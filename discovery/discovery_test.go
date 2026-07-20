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

// remoteCaseYAML builds a minimal case that points at a remote fixture. Optional
// tags let the tag-filter test exercise case-level tag matching.
func remoteCaseYAML(name, endpoint string, tags ...string) string {
	body := "name: " + name + "\n"
	if len(tags) > 0 {
		body += "tags: [" + strings.Join(tags, ", ") + "]\n"
	}
	body += "fixture:\n  remote:\n    endpoint: " + endpoint + "\n"
	body += `seed:
  type: app
expected:
  traces:
    - traceql: '{}'
      absent: true
`
	return body
}

// composeFileCaseYAML builds a case with a dir-relative compose file fixture, so
// its group key includes the case's directory.
func composeFileCaseYAML(name string) string {
	return "name: " + name + `
fixture:
  compose:
    file: docker-compose.oats.yml
seed:
  type: app
expected:
  traces:
    - traceql: '{}'
      absent: true
`
}

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
cases: ["cases/*.yaml"]
`)
	writeFile(t, dir, "cases/a.yaml", strings.Replace(validCaseYAML, "%s", "case-a", 1))
	writeFile(t, dir, "cases/b.yaml", strings.Replace(validCaseYAML, "%s", "case-b", 1))

	cfg, err := Load(filepath.Join(dir, "oats-config.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Both cases declare the same path-less compose fixture, so they group into
	// one plan.
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
cases: ["cases/*.yaml"]
`)
	writeFile(t, dir, "cases/a.yaml", remoteCaseYAML("a", "http://localhost:4318", "traces"))
	writeFile(t, dir, "cases/b.yaml", remoteCaseYAML("b", "http://localhost:4318", "logs"))

	cfg, err := Load(filepath.Join(dir, "oats-config.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Tag filtering is case-level (any-match) and runs before grouping, so only
	// the traces-tagged case survives.
	plans, err := cfg.PlanRun(Filter{Tags: []string{"traces"}})
	if err != nil {
		t.Fatalf("PlanRun: %v", err)
	}
	if len(plans) != 1 || len(plans[0].Cases) != 1 || plans[0].Cases[0].Name != "a" {
		t.Errorf("tag filter: %+v", planNames(plans))
	}
}

func TestSummary(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
cases: ["cases/*.yaml"]
`)
	writeFile(t, dir, "cases/a.yaml", remoteCaseYAML("case-a", "http://localhost:4318", "traces"))

	cfg, err := Load(filepath.Join(dir, "oats-config.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	plans, err := cfg.PlanRun(Filter{})
	if err != nil {
		t.Fatalf("PlanRun: %v", err)
	}

	got := Summary(plans)
	if !strings.Contains(got, "plan=case-a") {
		t.Fatalf("Summary missing plan name: %q", got)
	}
	if !strings.Contains(got, "fixture=remote") {
		t.Fatalf("Summary missing fixture kind: %q", got)
	}
	if !strings.Contains(got, "case-a") {
		t.Fatalf("Summary missing case name: %q", got)
	}
}

func TestPlanRun_FilterByPath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
cases: ["a/*.yaml", "b/*.yaml"]
`)
	writeFile(t, dir, "a/x.yaml", strings.Replace(validCaseYAML, "%s", "ax", 1))
	writeFile(t, dir, "b/y.yaml", strings.Replace(validCaseYAML, "%s", "by", 1))

	cfg, err := Load(filepath.Join(dir, "oats-config.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Directory scope: only the case under a/ survives.
	plans, err := cfg.PlanRun(Filter{Paths: []string{filepath.Join(dir, "a")}})
	if err != nil {
		t.Fatalf("PlanRun dir scope: %v", err)
	}
	if len(plans) != 1 || len(plans[0].Cases) != 1 || plans[0].Cases[0].Name != "ax" {
		t.Fatalf("dir path filter: %+v", planNames(plans))
	}

	// Single-file scope: the exact case file.
	plans, err = cfg.PlanRun(Filter{Paths: []string{filepath.Join(dir, "b", "y.yaml")}})
	if err != nil {
		t.Fatalf("PlanRun file scope: %v", err)
	}
	if len(plans) != 1 || len(plans[0].Cases) != 1 || plans[0].Cases[0].Name != "by" {
		t.Fatalf("file path filter: %+v", planNames(plans))
	}
}

func TestPlanRun_NoFixtureDefaultsToComposeLGTM(t *testing.T) {
	// A case that declares no fixture defaults to a compose fixture booting the
	// builtin lgtm stack, rooted at the config's source dir.
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
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
		t.Fatalf("expected no-fixture case to default to compose, got %q (%+v)", plans[0].Fixture.Kind(), plans[0].Fixture)
	}
	if got := plans[0].Fixture.Compose.EffectiveTemplate(); got != "lgtm" {
		t.Fatalf("expected default compose template lgtm, got %q", got)
	}
	if plans[0].FixtureSourceDir != dir {
		t.Fatalf("expected fixture source dir %q, got %q", dir, plans[0].FixtureSourceDir)
	}
}

// TestPlanRun_GroupsPathlessFixtureAcrossDirs verifies that cases with identical
// path-less fixtures (here remote) in different directories land in one plan:
// grouping keys on the fixture alone, since a remote fixture references no
// directory-relative paths.
func TestPlanRun_GroupsPathlessFixtureAcrossDirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
cases: ["*/oats-case.yaml"]
`)
	writeFile(t, dir, "a/oats-case.yaml", remoteCaseYAML("a", "http://localhost:4318"))
	writeFile(t, dir, "b/oats-case.yaml", remoteCaseYAML("b", "http://localhost:4318"))

	cfg, err := Load(filepath.Join(dir, "oats-config.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	plans, err := cfg.PlanRun(Filter{})
	if err != nil {
		t.Fatalf("PlanRun: %v", err)
	}
	if len(plans) != 1 || len(plans[0].Cases) != 2 {
		t.Fatalf("expected one plan with two cases, got %+v", planNames(plans))
	}
	if plans[0].Fixture.Kind() != "remote" {
		t.Fatalf("expected remote fixture, got %q", plans[0].Fixture.Kind())
	}
}

// TestPlanRun_SeparatesDirRelativeFixtureAcrossDirs verifies that cases with the
// same dir-relative fixture (compose file) in different directories land in
// separate plans: the relative file means different files per directory, so the
// group key includes the directory.
func TestPlanRun_SeparatesDirRelativeFixtureAcrossDirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
cases: ["*/oats-case.yaml"]
`)
	writeFile(t, dir, "a/oats-case.yaml", composeFileCaseYAML("a"))
	writeFile(t, dir, "b/oats-case.yaml", composeFileCaseYAML("b"))

	cfg, err := Load(filepath.Join(dir, "oats-config.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	plans, err := cfg.PlanRun(Filter{})
	if err != nil {
		t.Fatalf("PlanRun: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("expected two plans (one per dir), got %+v", planNames(plans))
	}
	for _, p := range plans {
		if len(p.Cases) != 1 || p.Fixture.Kind() != "compose" {
			t.Fatalf("unexpected plan: %+v", p)
		}
	}
}

func TestPlanRun_EmptyGlobIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
meta:
  version: 3
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

// skipIfAbsent skips a test that validates a shipped example config when
// examples/ is not present in the tree. In the split PR stack examples/ lands
// in a later PR; once merged (and on main) these tests run for real.
func skipIfAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not present in this build (ships with examples/)", path)
	}
}

func TestExampleV2SmokeConfigLoads(t *testing.T) {
	path := filepath.Join("..", "examples", "smoke", "oats-config.yaml")
	skipIfAbsent(t, path)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load example config: %v", err)
	}
	plans, err := cfg.PlanRun(Filter{})
	if err != nil {
		t.Fatalf("PlanRun example config: %v", err)
	}
	// All four smoke cases share one remote fixture, so they form a single plan.
	if len(plans) != 1 || len(plans[0].Cases) != 4 {
		t.Fatalf("expected one plan/four cases, got %+v", plans)
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
	path := filepath.Join("..", "examples", "fixtures", "oats-config.yaml")
	skipIfAbsent(t, path)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load fixture example config: %v", err)
	}
	plans, err := cfg.PlanRun(Filter{})
	if err != nil {
		t.Fatalf("PlanRun fixture example config: %v", err)
	}
	// The compose case and k3d case declare different fixtures, so they form two
	// independent plans, ordered by first fixture appearance (compose case sorts
	// before the k3d case by source path).
	if len(plans) != 2 {
		t.Fatalf("expected two plans, got %+v", plans)
	}
	if plans[0].Fixture.Kind() != "compose" || len(plans[0].Cases) != 1 {
		t.Fatalf("unexpected first plan: %+v", plans[0])
	}
	if plans[1].Fixture.Kind() != "k3d" || len(plans[1].Cases) != 1 {
		t.Fatalf("unexpected second plan: %+v", plans[1])
	}
	if plans[1].Cases[0].Name != "k3d fixture app smoke" {
		t.Fatalf("unexpected k3d case name: %q", plans[1].Cases[0].Name)
	}
}

func planNames(p []Plan) []string {
	out := make([]string, len(p))
	for i := range p {
		out[i] = p[i].Name
	}
	return out
}

func TestLoadTopLevelCases(t *testing.T) {
	// Two cases with different fixtures (distinct remote endpoints) become two
	// separate plans.
	dir := t.TempDir()
	writeFile(t, dir, "oats-config.yaml", `
cases: ["cases/a.yaml", "cases/b.yaml"]
meta:
  version: 3
`)
	writeFile(t, dir, "cases/a.yaml", remoteCaseYAML("a", "http://localhost:4318"))
	writeFile(t, dir, "cases/b.yaml", remoteCaseYAML("b", "http://localhost:4319"))
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
	if plans[0].Name != "a" || plans[1].Name != "b" {
		t.Fatalf("unexpected plan names: %q, %q", plans[0].Name, plans[1].Name)
	}
}
