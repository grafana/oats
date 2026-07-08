package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grafana/oats/casefile"
	"github.com/grafana/oats/discovery"
	"github.com/grafana/oats/model"
	"github.com/grafana/oats/testhelpers/kubernetes"
)

func TestConvertDefinition_MapsSignalsToMatchSchema(t *testing.T) {
	def := model.TestCaseDefinition{
		DockerCompose: &model.DockerCompose{Files: []string{"docker-compose.yml"}},
		Input:         []model.Input{{Path: "/stock"}},
		Interval:      500 * time.Millisecond,
		Expected: model.Expected{
			Traces: []model.ExpectedTraces{{
				TraceQL: `{ name = "GET /stock" }`,
				Signal: model.ExpectedSignal{
					NameEquals:      "GET /stock",
					Attributes:      map[string]string{"service.name": "shop", "db.system": "h2"},
					AttributeRegexp: map[string]string{"trace_id": ".*", "span.kind": "server"},
				},
			}},
			Logs: []model.ExpectedLogs{{
				LogQL: `{service_name="shop"}`,
				Signal: model.ExpectedSignal{
					NameRegexp: `error|warn`,
					Count:      &model.ExpectedRange{Min: 1, Max: 3},
				},
			}},
			Profiles: []model.ExpectedProfiles{{
				Query: `process_cpu:cpu:nanoseconds`,
				Flamebearers: model.Flamebearers{
					NameEquals: "main",
				},
			}},
			Metrics: []model.ExpectedMetrics{{
				PromQL: `up{job="shop"}`,
				Value:  `>= 1`,
			}},
		},
	}

	c, warnings, err := ConvertDefinition(def, "legacy case")
	if err != nil {
		fatalf(t, "ConvertDefinition: %v", err)
	}
	if c.Seed.Type != "app" {
		fatalf(t, "unexpected seed mapping: %+v", c.Seed)
	}
	if c.Fixture == nil || c.Fixture.Compose == nil || c.Fixture.Compose.File != "docker-compose.yml" {
		fatalf(t, "expected case-local compose fixture, got %+v", c.Fixture)
	}
	if c.Interval != 500*time.Millisecond {
		fatalf(t, "expected interval to carry over, got %v", c.Interval)
	}
	if len(c.Input) != 1 || c.Input[0].Path != "/stock" {
		fatalf(t, "expected input to carry over, got %+v", c.Input)
	}
	if len(c.Expected.Traces) != 1 || len(c.Expected.Traces[0].MatchSpans) != 2 {
		fatalf(t, "expected trace strict+regexp split, got %+v", c.Expected.Traces)
	}
	if got := c.Expected.Traces[0].MatchSpans[0].Attributes; len(got) != 2 || got[0].Key != "db.system" || got[1].Key != "service.name" {
		fatalf(t, "expected strict attributes sorted, got %+v", got)
	}
	if got := c.Expected.Traces[0].MatchSpans[1].Attributes[0]; got.Key != "span.kind" || got.Value == nil || *got.Value != "server" {
		fatalf(t, "expected regexp attributes sorted, got %+v", c.Expected.Traces[0].MatchSpans[1].Attributes)
	}
	if got := c.Expected.Traces[0].MatchSpans[1].Attributes[1]; got.Key != "trace_id" || got.Value != nil {
		fatalf(t, "expected trace_id .* to map to presence, got %+v", got)
	}
	if c.Expected.Logs[0].Count != ">= 1" {
		fatalf(t, "expected lossy count min mapping, got %q", c.Expected.Logs[0].Count)
	}
	if len(c.Expected.Profiles[0].Match) != 1 || c.Expected.Profiles[0].Match[0].Name == nil || *c.Expected.Profiles[0].Match[0].Name != "main" {
		fatalf(t, "unexpected profile match mapping: %+v", c.Expected.Profiles[0].Match)
	}
	if len(warnings) == 0 {
		fatalf(t, "expected warnings for lossy count")
	}
	joined := strings.Join(warnings, "\n")
	if strings.Contains(joined, "input requests are not represented") || !strings.Contains(joined, "count max=3 dropped") {
		fatalf(t, "expected warnings not found:\n%s", joined)
	}
}

func TestConvertDefinition_SingleMatrixComposeRequiresFile(t *testing.T) {
	def := model.TestCaseDefinition{
		Matrix: []model.Matrix{{
			Name:          "docker",
			DockerCompose: &model.DockerCompose{},
		}},
	}
	_, _, err := ConvertDefinition(def, "matrix no files")
	if err == nil || !strings.Contains(err.Error(), "matrix docker-compose present but no files declared") {
		fatalf(t, "expected helpful matrix file error, got %v", err)
	}
}

func TestConvertFile_RendersYAML(t *testing.T) {
	sample := filepath.Join("..", "yaml", "testdata", "valid-tests", "oats.yaml")
	out, warnings, err := ConvertFile(sample)
	if err != nil {
		fatalf(t, "ConvertFile: %v", err)
	}
	if len(warnings) == 0 {
		fatalf(t, "expected at least one warning for flattened include or fixture migration")
	}
	text := string(out)
	for _, want := range []string{"oats-schema-version: 3", "seed:", "input:", "path: /stock", "match_spans:", "match_type: regexp", "key: db.system", "value: h2", "promql: foo"} {
		if !strings.Contains(text, want) {
			fatalf(t, "expected migrated yaml to contain %q:\n%s", want, text)
		}
	}
}

func TestConvertFile_MatrixSampleIsParseable(t *testing.T) {
	sample := filepath.Join("..", "yaml", "testdata", "valid-tests", "matrix-test.oats.yaml")
	out, warnings, err := ConvertFile(sample)
	if err != nil {
		fatalf(t, "ConvertFile matrix sample: %v", err)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "matrix definitions are not migrated automatically when multiple entries exist") {
		fatalf(t, "expected multi-matrix warning:\n%s", joined)
	}
	c, err := casefile.Parse(out)
	if err != nil {
		fatalf(t, "migrated matrix yaml should parse as v2: %v\n%s", err, string(out))
	}
	if c.Seed.Type != "inline-otlp" {
		fatalf(t, "expected multi-matrix sample to fall back to inline-otlp placeholder, got %+v", c.Seed)
	}
	if len(c.Input) == 0 || len(c.Expected.Traces) == 0 || len(c.Expected.Metrics) == 0 {
		fatalf(t, "expected included template fields to survive migration, got %+v", c)
	}
}

func TestConvertFile_CustomChecksRoundTrip(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "custom.oats.yaml")
	if err := os.WriteFile(legacy, []byte(`
oats-schema-version: 2
docker-compose:
  files:
    - docker-compose.yml
expected:
  custom-checks:
    - script: ./verify.sh
    - script: |
        #!/bin/sh
        exit 0
`), 0o644); err != nil {
		fatalf(t, "WriteFile: %v", err)
	}

	out, warnings, err := ConvertFile(legacy)
	if err != nil {
		fatalf(t, "ConvertFile custom checks: %v", err)
	}
	// A compose fixture always warns that app_service/app_port must be set by
	// hand; that is the only expected warning here.
	if len(warnings) != 1 || !strings.Contains(warnings[0], "app_service + app_port") {
		fatalf(t, "expected only the app_service warning for straightforward custom checks, got %v", warnings)
	}
	c, err := casefile.Parse(out)
	if err != nil {
		fatalf(t, "migrated custom-check yaml should parse as v2: %v\n%s", err, string(out))
	}
	if got := len(c.Expected.Custom); got != 2 {
		fatalf(t, "expected 2 migrated custom checks, got %+v", c.Expected.Custom)
	}
	if c.Expected.Custom[0].Script != "./verify.sh" || !strings.Contains(c.Expected.Custom[1].Script, "exit 0") {
		fatalf(t, "unexpected custom check migration: %+v", c.Expected.Custom)
	}
}

func TestConvertDefinition_KubernetesProducesFixtureBlock(t *testing.T) {
	def := model.TestCaseDefinition{
		Kubernetes: &kubernetes.Kubernetes{
			Dir:              "k8s",
			AppService:       "dice",
			AppDockerFile:    "Dockerfile",
			AppDockerContext: "..",
			AppDockerTag:     "dice:test",
			AppDockerPort:    8080,
			ImportImages:     []string{"busybox:latest"},
		},
		Expected: model.Expected{
			Logs: []model.ExpectedLogs{{
				LogQL: `{service_name="dice"}`,
				Signal: model.ExpectedSignal{
					NameEquals: "hello",
				},
			}},
		},
	}

	c, _, err := ConvertDefinition(def, "k8s case")
	if err != nil {
		fatalf(t, "ConvertDefinition kubernetes: %v", err)
	}
	if c.Seed.Type != "app" {
		fatalf(t, "expected app seed for kubernetes migration, got %+v", c.Seed)
	}
	if c.Fixture == nil || c.Fixture.K3D == nil {
		fatalf(t, "expected case-local k3d fixture, got %+v", c.Fixture)
	}
	k := c.Fixture.K3D
	if k.K8sDir != "k8s" || k.AppService != "dice" || k.AppDockerFile != "Dockerfile" ||
		k.AppDockerContext != ".." || k.AppDockerTag != "dice:test" || k.AppPort != 8080 {
		fatalf(t, "unexpected k3d fixture: %+v", k)
	}
	if len(k.ImportImages) != 1 || k.ImportImages[0] != "busybox:latest" {
		fatalf(t, "unexpected import images: %+v", k.ImportImages)
	}
}

func TestConvertDefinition_FlattensSingleMatrixEntry(t *testing.T) {
	def := model.TestCaseDefinition{
		Matrix: []model.Matrix{{
			Name: "docker",
			DockerCompose: &model.DockerCompose{
				Files: []string{"docker-compose.yml"},
			},
		}},
		Expected: model.Expected{
			Logs: []model.ExpectedLogs{
				{
					LogQL: `{service_name="dice"}`,
					Signal: model.ExpectedSignal{
						NameEquals:      "base",
						MatrixCondition: "",
					},
				},
				{
					LogQL: `{service_name="dice"}`,
					Signal: model.ExpectedSignal{
						NameEquals:      "docker-only",
						MatrixCondition: "docker",
					},
				},
				{
					LogQL: `{service_name="dice"}`,
					Signal: model.ExpectedSignal{
						NameEquals:      "k8s-only",
						MatrixCondition: "k8s",
					},
				},
			},
		},
	}

	c, warnings, err := ConvertDefinition(def, "matrix case")
	if err != nil {
		fatalf(t, "ConvertDefinition single matrix: %v", err)
	}
	if c.Name != "matrix case [docker]" {
		fatalf(t, "unexpected case name: %q", c.Name)
	}
	if c.Seed.Type != "app" {
		fatalf(t, "expected app seed, got %+v", c.Seed)
	}
	if got := len(c.Expected.Logs); got != 2 {
		fatalf(t, "expected 2 kept log assertions, got %d (%+v)", got, c.Expected.Logs)
	}
	if c.Fixture == nil || c.Fixture.Compose == nil || c.Fixture.Compose.File != "docker-compose.yml" {
		fatalf(t, "expected single-matrix compose fixture, got %+v", c.Fixture)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, `flattened single matrix entry "docker"`) {
		fatalf(t, "expected flatten warning:\n%s", joined)
	}
}

func TestConvertDefinition_MultiMatrixWarnsAndFallsBack(t *testing.T) {
	def := model.TestCaseDefinition{
		Matrix: []model.Matrix{
			{
				Name: "docker",
				DockerCompose: &model.DockerCompose{
					Files: []string{"docker-compose.yml"},
				},
			},
			{
				Name: "k8s",
				Kubernetes: &kubernetes.Kubernetes{
					Dir:           "k8s",
					AppService:    "dice",
					AppDockerFile: "Dockerfile",
					AppDockerTag:  "dice:test",
					AppDockerPort: 8080,
				},
			},
		},
		Expected: model.Expected{
			Logs: []model.ExpectedLogs{{
				LogQL: `{service_name="dice"}`,
				Signal: model.ExpectedSignal{
					NameEquals:      "any",
					MatrixCondition: "docker|k8s",
				},
			}},
		},
	}

	c, warnings, err := ConvertDefinition(def, "matrix case")
	if err != nil {
		fatalf(t, "ConvertDefinition multi matrix: %v", err)
	}
	// Multiple matrix entries are not migrated automatically: no fixture is
	// selected and the case falls back to the inline-otlp placeholder.
	if c.Fixture != nil {
		fatalf(t, "expected no fixture for multi-matrix case, got %+v", c.Fixture)
	}
	if c.Seed.Type != "inline-otlp" {
		fatalf(t, "expected inline-otlp fallback, got %+v", c.Seed)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "matrix definitions are not migrated automatically when multiple entries exist") {
		fatalf(t, "expected multi-matrix warning:\n%s", joined)
	}
}

func TestConvertDefinition_MigratesCustomChecks(t *testing.T) {
	def := model.TestCaseDefinition{
		Expected: model.Expected{
			CustomChecks: []model.CustomCheck{
				{Script: "./verify.sh"},
				{Script: "./skip.sh", MatrixCondition: "docker"},
			},
		},
	}

	c, warnings, err := ConvertDefinition(def, "custom checks")
	if err != nil {
		fatalf(t, "ConvertDefinition custom checks: %v", err)
	}
	if got := len(c.Expected.Custom); got != 2 {
		fatalf(t, "expected 2 custom checks, got %+v", c.Expected.Custom)
	}
	if len(warnings) == 0 || !strings.Contains(strings.Join(warnings, "\n"), "defaulting seed.type to inline-otlp placeholder") {
		fatalf(t, "expected placeholder warning, got %v", warnings)
	}
}

func TestConvertTree_RewritesCasesAndWritesConfig(t *testing.T) {
	dir := t.TempDir()
	composeCase := filepath.Join(dir, "compose.oats.yaml")
	if err := os.WriteFile(composeCase, []byte(`
oats-schema-version: 2
docker-compose:
  files:
    - docker-compose.yml
expected:
  metrics:
    - promql: up
      value: ">= 1"
`), 0o644); err != nil {
		fatalf(t, "WriteFile compose: %v", err)
	}
	nested := filepath.Join(dir, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		fatalf(t, "MkdirAll: %v", err)
	}
	minimalCase := filepath.Join(nested, "minimal.oats.yaml")
	if err := os.WriteFile(minimalCase, []byte(`
oats-schema-version: 2
expected:
  custom-checks:
    - script: ./verify.sh
`), 0o644); err != nil {
		fatalf(t, "WriteFile minimal: %v", err)
	}

	res, err := ConvertTree(dir)
	if err != nil {
		fatalf(t, "ConvertTree: %v", err)
	}
	if len(res.Written) != 2 {
		fatalf(t, "expected 2 written cases, got %v", res.Written)
	}

	// Both legacy files must now be valid v3 cases parseable in place.
	compose, err := casefile.Load(composeCase)
	if err != nil {
		fatalf(t, "compose case should be v3 after migration: %v", err)
	}
	if compose.OatsSchemaVersion != casefile.SchemaVersion {
		fatalf(t, "expected schema version %d, got %d", casefile.SchemaVersion, compose.OatsSchemaVersion)
	}
	if compose.Fixture == nil || compose.Fixture.Compose == nil || compose.Fixture.Compose.File != "docker-compose.yml" {
		fatalf(t, "expected migrated compose fixture, got %+v", compose.Fixture)
	}
	if _, err := casefile.Load(minimalCase); err != nil {
		fatalf(t, "minimal case should be v3 after migration: %v", err)
	}

	// The generated config must list both cases as explicit relative paths.
	configPath := filepath.Join(dir, "oats-config.yaml")
	if res.Config != configPath {
		fatalf(t, "unexpected config path: %q", res.Config)
	}
	cfg, err := discovery.Load(configPath)
	if err != nil {
		fatalf(t, "discovery.Load should accept generated config: %v", err)
	}
	want := []string{"compose.oats.yaml", "nested/minimal.oats.yaml"}
	if len(cfg.Cases) != len(want) {
		fatalf(t, "expected cases %v, got %v", want, cfg.Cases)
	}
	for i, w := range want {
		if cfg.Cases[i] != w {
			fatalf(t, "expected sorted explicit case %q at %d, got %v", w, i, cfg.Cases)
		}
	}
}

func fatalf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Fatalf(format, args...)
}
