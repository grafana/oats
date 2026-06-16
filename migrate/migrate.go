package migrate

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/grafana/oats/model"
	"github.com/grafana/oats/v2case"
	legacyyaml "github.com/grafana/oats/yaml"
	goyaml "go.yaml.in/yaml/v3"
)

// ConvertFile reads one legacy OATS yaml file and returns a best-effort v2
// case yaml plus any warnings about dropped or lossy fields.
func ConvertFile(path string) ([]byte, []string, error) {
	def, err := legacyyaml.LoadTestCaseDefinition(path)
	if err != nil {
		return nil, nil, err
	}
	if def == nil {
		return nil, nil, fmt.Errorf("%s is not a legacy OATS test case definition", path)
	}
	c, warnings, err := ConvertDefinition(*def, deriveName(path))
	if err != nil {
		return nil, warnings, err
	}
	out, err := goyaml.Marshal(c)
	if err != nil {
		return nil, warnings, err
	}
	return out, warnings, nil
}

// ConvertDefinition maps a legacy v1/v1.5-style OATS definition into the
// current v2 case shape. Unsupported fields are dropped with warnings.
func ConvertDefinition(def model.TestCaseDefinition, name string) (*v2case.Case, []string, error) {
	var warnings []string
	c := &v2case.Case{
		OatsVersion: v2case.SchemaVersion,
		Name:        name,
	}

	if def.Kubernetes != nil {
		return nil, warnings, fmt.Errorf("kubernetes fixtures are not yet supported by v2 migration")
	}
	if len(def.Matrix) > 0 {
		warnings = append(warnings, "matrix definitions are not migrated; convert expanded matrix cases manually")
	}
	if len(def.Include) > 0 {
		warnings = append(warnings, "include directives were resolved before migration; output is a flattened case")
	}
	if len(def.Input) > 0 {
		warnings = append(warnings, "input requests are not represented in v2 yet; dropped from migrated output")
	}
	if def.Interval != 0 {
		warnings = append(warnings, "interval is not represented in v2 yet; dropped from migrated output")
	}
	if len(def.Expected.ComposeLogs) > 0 {
		warnings = append(warnings, "compose-logs assertions are not migrated")
	}
	if len(def.Expected.CustomChecks) > 0 {
		warnings = append(warnings, "custom-checks are not migrated")
	}

	if def.DockerCompose != nil {
		c.Seed.Type = "app"
		if len(def.DockerCompose.Files) == 0 {
			return nil, warnings, fmt.Errorf("docker-compose present but no files declared")
		}
		c.Seed.Compose = def.DockerCompose.Files[0]
		if len(def.DockerCompose.Files) > 1 {
			warnings = append(warnings, fmt.Sprintf("multiple docker-compose files collapsed to first entry %q", def.DockerCompose.Files[0]))
		}
		if len(def.DockerCompose.Environment) > 0 {
			warnings = append(warnings, "docker-compose env overrides are not represented in v2 yet")
		}
	} else {
		warnings = append(warnings, "no docker-compose fixture found; defaulting seed.type to inline-otlp placeholder")
		c.Seed.Type = "inline-otlp"
		c.Seed.Traces = []v2case.SeedTrace{{Service: "migrated-service", Spans: []v2case.SeedSpan{{Name: "replace-me"}}}}
	}

	for _, tr := range def.Expected.Traces {
		assertion, ws := convertSignal(tr.TraceQL, tr.Signal)
		warnings = append(warnings, ws...)
		c.Expected.Traces = append(c.Expected.Traces, v2case.TraceAssertion{TraceQL: tr.TraceQL, AssertionCommon: assertion})
	}
	for _, lg := range def.Expected.Logs {
		assertion, ws := convertSignal(lg.LogQL, lg.Signal)
		warnings = append(warnings, ws...)
		c.Expected.Logs = append(c.Expected.Logs, v2case.LogAssertion{LogQL: lg.LogQL, AssertionCommon: assertion})
	}
	for _, m := range def.Expected.Metrics {
		c.Expected.Metrics = append(c.Expected.Metrics, v2case.MetricAssertion{PromQL: m.PromQL, Value: m.Value})
		if m.MatrixCondition != "" {
			warnings = append(warnings, fmt.Sprintf("metric %q matrix-condition dropped", m.PromQL))
		}
	}
	for _, p := range def.Expected.Profiles {
		var matches []v2case.MatchEntry
		if p.Flamebearers.NameEquals != "" {
			matches = append(matches, v2case.MatchEntry{Name: strPtr(p.Flamebearers.NameEquals)})
		}
		if p.Flamebearers.NameRegexp != "" {
			matches = append(matches, v2case.MatchEntry{MatchType: v2case.MatchTypeRegexp, Name: strPtr(p.Flamebearers.NameRegexp)})
		}
		c.Expected.Profiles = append(c.Expected.Profiles, v2case.ProfileAssertion{
			Query:           p.Query,
			AssertionCommon: v2case.AssertionCommon{Match: matches},
		})
		if p.MatrixCondition != "" {
			warnings = append(warnings, fmt.Sprintf("profile %q matrix-condition dropped", p.Query))
		}
	}

	if err := c.Validate(); err != nil {
		return nil, warnings, fmt.Errorf("migrated v2 case failed validation: %w", err)
	}
	return c, warnings, nil
}

func convertSignal(label string, s model.ExpectedSignal) (v2case.AssertionCommon, []string) {
	var warnings []string
	out := v2case.AssertionCommon{}
	if s.Count != nil {
		switch {
		case s.Count.Min == 0 && s.Count.Max == 0:
			out.Absent = true
		case s.Count.Max > 0 && s.Count.Min > 0 && s.Count.Max != s.Count.Min:
			out.Count = fmt.Sprintf(">= %d", s.Count.Min)
			warnings = append(warnings, fmt.Sprintf("%s count max=%d dropped; v2 scalar count keeps only min bound", label, s.Count.Max))
		case s.Count.Max > 0 && s.Count.Min == s.Count.Max:
			out.Count = fmt.Sprintf("== %d", s.Count.Min)
		case s.Count.Min > 0:
			out.Count = fmt.Sprintf(">= %d", s.Count.Min)
		case s.Count.Max > 0:
			out.Count = fmt.Sprintf("<= %d", s.Count.Max)
		}
	}
	if s.NoExtraAttributes {
		warnings = append(warnings, fmt.Sprintf("%s no-extra-attributes is not supported in v2 and was dropped", label))
	}
	if s.MatrixCondition != "" {
		warnings = append(warnings, fmt.Sprintf("%s matrix-condition dropped", label))
	}
	if s.NameEquals != "" || len(s.Attributes) > 0 {
		entry := v2case.MatchEntry{Attributes: map[string]v2case.AttributeExpectation{}}
		if s.NameEquals != "" {
			entry.Name = strPtr(s.NameEquals)
		}
		for k, v := range s.Attributes {
			entry.Attributes[k] = v2case.AttributeExpectation{Value: strPtr(v)}
		}
		out.Match = append(out.Match, entry)
	}
	if s.NameRegexp != "" || len(s.AttributeRegexp) > 0 {
		entry := v2case.MatchEntry{MatchType: v2case.MatchTypeRegexp, Attributes: map[string]v2case.AttributeExpectation{}}
		if s.NameRegexp != "" {
			entry.Name = strPtr(s.NameRegexp)
		}
		for k, v := range s.AttributeRegexp {
			if v == ".*" {
				present := true
				entry.Attributes[k] = v2case.AttributeExpectation{Present: &present}
			} else {
				entry.Attributes[k] = v2case.AttributeExpectation{Value: strPtr(v)}
			}
		}
		if entry.Name != nil || len(entry.Attributes) > 0 {
			out.Match = append(out.Match, entry)
		}
	}
	if len(out.Match) == 0 && !out.Absent && out.Count == "" {
		warnings = append(warnings, fmt.Sprintf("%s has no structural checks after migration", label))
	}
	return out, warnings
}

func deriveName(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.ReplaceAll(base, "_", " ")
	base = strings.ReplaceAll(base, "-", " ")
	return strings.TrimSpace(base)
}

func strPtr(s string) *string { return &s }
