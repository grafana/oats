package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grafana/oats/model"
	"github.com/grafana/oats/testhelpers/kubernetes"
	"github.com/grafana/oats/v2case"
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
					Attributes:      map[string]string{"db.system": "h2"},
					AttributeRegexp: map[string]string{"trace_id": ".*"},
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
	if c.Seed.Type != "app" || c.Seed.Compose != "docker-compose.yml" {
		fatalf(t, "unexpected seed mapping: %+v", c.Seed)
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
	if got := c.Expected.Traces[0].MatchSpans[1].Attributes[0]; got.Key != "trace_id" || got.Value != nil {
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
	for _, want := range []string{"oats: 2", "seed:", "input:", "path: /stock", "match_spans:", "match_type: regexp", "key: db.system", "value: h2", "promql: foo"} {
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
	for _, want := range []string{
		`matrix definitions are not migrated automatically when multiple entries exist`,
		`suggested matrix expansion for "matrix test.oats"`,
		`[fixture.matrix-test-oats-docker]`,
		`[fixture.matrix-test-oats-k8s]`,
	} {
		if !strings.Contains(joined, want) {
			fatalf(t, "expected matrix warning to contain %q:\n%s", want, joined)
		}
	}
	c, err := v2case.Parse(out)
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
	if len(warnings) != 0 {
		fatalf(t, "expected no warnings for straightforward custom checks, got %v", warnings)
	}
	c, err := v2case.Parse(out)
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

func TestConvertDefinition_KubernetesProducesFixtureHint(t *testing.T) {
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

	c, warnings, err := ConvertDefinition(def, "k8s case")
	if err != nil {
		fatalf(t, "ConvertDefinition kubernetes: %v", err)
	}
	if c.Seed.Type != "app" {
		fatalf(t, "expected app seed for kubernetes migration, got %+v", c.Seed)
	}
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{
		`[fixture.k8s-case]`,
		`type = "k3d"`,
		`k8s_dir = "k8s"`,
		`app_service = "dice"`,
		`app_docker_file = "Dockerfile"`,
		`app_docker_context = ".."`,
		`app_docker_tag = "dice:test"`,
		`app_port = 8080`,
		`import_images = ["busybox:latest"]`,
	} {
		if !strings.Contains(joined, want) {
			fatalf(t, "expected kubernetes migration warning to contain %q:\n%s", want, joined)
		}
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
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{
		`flattened single matrix entry "docker"`,
		`[fixture.matrix-case-docker]`,
		`compose_file = "docker-compose.yml"`,
	} {
		if !strings.Contains(joined, want) {
			fatalf(t, "expected warning to contain %q:\n%s", want, joined)
		}
	}
}

func TestConvertDefinition_MultiMatrixEmitsExpansionHint(t *testing.T) {
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

	_, warnings, err := ConvertDefinition(def, "matrix case")
	if err != nil {
		fatalf(t, "ConvertDefinition multi matrix: %v", err)
	}
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{
		`suggested matrix expansion for "matrix case"`,
		`- docker`,
		`[fixture.matrix-case-docker]`,
		`- k8s`,
		`[fixture.matrix-case-k8s]`,
		`type = "k3d"`,
	} {
		if !strings.Contains(joined, want) {
			fatalf(t, "expected warning to contain %q:\n%s", want, joined)
		}
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

func fatalf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Fatalf(format, args...)
}
