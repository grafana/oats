package migrate

import (
	"strings"
	"testing"
	"time"

	"github.com/grafana/oats/model"
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
	if len(c.Expected.Traces) != 1 || len(c.Expected.Traces[0].Match) != 2 {
		fatalf(t, "expected trace strict+regexp split, got %+v", c.Expected.Traces)
	}
	if got := c.Expected.Traces[0].Match[1].Attributes["trace_id"].Present; got == nil || !*got {
		fatalf(t, "expected trace_id .* to map to present:true")
	}
	if c.Expected.Logs[0].Count != ">= 1" {
		fatalf(t, "expected lossy count min mapping, got %q", c.Expected.Logs[0].Count)
	}
	if len(c.Expected.Profiles[0].Match) != 1 || c.Expected.Profiles[0].Match[0].Name == nil || *c.Expected.Profiles[0].Match[0].Name != "main" {
		fatalf(t, "unexpected profile match mapping: %+v", c.Expected.Profiles[0].Match)
	}
	if len(warnings) == 0 {
		fatalf(t, "expected warnings for dropped input/interval or lossy count")
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "input requests are not represented") || !strings.Contains(joined, "count max=3 dropped") {
		fatalf(t, "expected warnings not found:\n%s", joined)
	}
}

func TestConvertFile_RendersYAML(t *testing.T) {
	out, warnings, err := ConvertFile("/home/gregor/source/oats-v2/yaml/testdata/valid-tests/oats.yaml")
	if err != nil {
		fatalf(t, "ConvertFile: %v", err)
	}
	if len(warnings) == 0 {
		fatalf(t, "expected at least one warning for flattened include/input migration")
	}
	text := string(out)
	for _, want := range []string{"oats: 2", "seed:", "match:", "match_type: regexp", "db.system: h2", "promql: foo"} {
		if !strings.Contains(text, want) {
			fatalf(t, "expected migrated yaml to contain %q:\n%s", want, text)
		}
	}
}

func fatalf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Fatalf(format, args...)
}
