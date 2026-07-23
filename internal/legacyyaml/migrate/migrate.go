package migrate

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/grafana/oats/casefile"
	"github.com/grafana/oats/discovery"
	"github.com/grafana/oats/internal/legacyyaml"
	"github.com/grafana/oats/internal/legacyyaml/model"
	goyaml "go.yaml.in/yaml/v3"
)

// marshalYAML renders v as YAML with 2-space indentation, matching the repo's
// YAML convention and the shipped examples (goyaml.Marshal defaults to 4).
func marshalYAML(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := goyaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// preserveLineEndings converts generated YAML to CRLF when the source file
// uses CRLF. YAML encoders always emit LF, but an in-place migration should
// not create a whole-file diff solely because of line endings.
func preserveLineEndings(source, generated []byte) []byte {
	if bytes.Contains(source, []byte("\r\n")) {
		return bytes.ReplaceAll(generated, []byte("\n"), []byte("\r\n"))
	}
	return generated
}

// ConvertFile reads one legacy OATS yaml file and returns a best-effort
// current-format case yaml plus any warnings about dropped or lossy fields.
// The migrated case carries its own case-local fixture: block, so the output
// is self-contained.
func ConvertFile(path string) ([]byte, []string, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
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
	out, err := marshalYAML(c)
	if err != nil {
		return nil, warnings, err
	}
	return preserveLineEndings(source, out), warnings, nil
}

// TreeResult reports the outcome of a directory ("project") migration.
type TreeResult struct {
	// Written lists the absolute paths of the case files rewritten in place.
	Written []string
	// Config is the absolute path of the generated oats-config.yaml.
	Config string
	// Warnings aggregates every case's warnings, each prefixed with the
	// relative path of the case it came from.
	Warnings []string
}

// ConvertTree migrates every legacy OATS case found under dir in place and
// writes an oats-config.yaml listing them explicitly. Each legacy file is
// overwritten with its v3 equivalent (same path/filename); the generated
// config references each written file by its dir-relative path.
func ConvertTree(dir string) (*TreeResult, error) {
	cases, err := legacyyaml.ReadTestCases([]string{dir}, true)
	if err != nil {
		return nil, err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	result := &TreeResult{}
	seen := map[string]bool{}
	var relpaths []string
	for _, tc := range cases {
		// ReadTestCases expands matrix files into one entry per matrix row,
		// all sharing a Path. Convert each source file once; ConvertDefinition
		// handles the matrix internally.
		if seen[tc.Path] {
			continue
		}
		seen[tc.Path] = true

		source, err := os.ReadFile(tc.Path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", tc.Path, err)
		}
		c, warnings, err := ConvertDefinition(tc.Definition, deriveName(tc.Path))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", tc.Path, err)
		}
		out, err := marshalYAML(c)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", tc.Path, err)
		}
		out = preserveLineEndings(source, out)
		if err := os.WriteFile(tc.Path, out, 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", tc.Path, err)
		}
		result.Written = append(result.Written, tc.Path)

		rel, err := filepath.Rel(absDir, tc.Path)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		relpaths = append(relpaths, rel)
		for _, w := range warnings {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %s", rel, w))
		}
	}

	sort.Strings(result.Written)
	sort.Strings(relpaths)

	configPath := filepath.Join(absDir, "oats-config.yaml")
	if _, err := os.Stat(configPath); err == nil {
		result.Warnings = append(result.Warnings, "oats-config.yaml already existed and was overwritten as a migration artifact")
	}
	cfg := discovery.RootConfig{
		Meta:  discovery.Meta{Version: discovery.SupportedVersion},
		Cases: relpaths,
	}
	cfgOut, err := marshalYAML(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(configPath, cfgOut, 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", configPath, err)
	}
	result.Config = configPath
	return result, nil
}

// ConvertDefinition maps a legacy v1/v1.5-style OATS definition into the
// current case yaml shape. Unsupported fields are dropped with warnings. The
// legacy docker-compose/kubernetes fixture is carried over as a case-local
// fixture: block.
func ConvertDefinition(def model.TestCaseDefinition, name string) (*casefile.Case, []string, error) {
	var warnings []string
	c := &casefile.Case{
		Name:     name,
		Interval: def.Interval,
	}
	selectedMatrix := (*model.Matrix)(nil)

	if len(def.Matrix) > 0 {
		if len(def.Matrix) == 1 {
			selectedMatrix = &def.Matrix[0]
			c.Name = fmt.Sprintf("%s [%s]", name, selectedMatrix.Name)
			warnings = append(warnings, fmt.Sprintf("flattened single matrix entry %q into the migrated case", selectedMatrix.Name))
		} else {
			warnings = append(warnings, "matrix definitions are not migrated automatically when multiple entries exist; convert expanded matrix cases manually")
		}
	}
	if len(def.Include) > 0 {
		warnings = append(warnings, "include directives were resolved before migration; output is a flattened case")
	}
	if len(def.Expected.ComposeLogs) > 0 {
		warnings = append(warnings, "compose-logs assertions are not migrated")
	}

	// Guard against a docker-compose block with no files before building the
	// fixture, so the error message stays specific to its source.
	if selectedMatrix != nil && selectedMatrix.DockerCompose != nil && len(selectedMatrix.DockerCompose.Files) == 0 {
		return nil, warnings, fmt.Errorf("matrix docker-compose present but no files declared")
	}
	if def.DockerCompose != nil && len(def.DockerCompose.Files) == 0 &&
		(selectedMatrix == nil || selectedMatrix.DockerCompose == nil) {
		return nil, warnings, fmt.Errorf("docker-compose present but no files declared")
	}

	c.Fixture = fixtureFor(def, selectedMatrix)
	switch {
	case c.Fixture != nil:
		c.Seed.Type = "app"
		if c.Fixture.Compose != nil {
			warnings = append(warnings, "for parallel-safe app-seed runs, set fixture.compose.app_service + app_port (not derivable from the legacy file)")
		}
	default:
		warnings = append(warnings, "no docker-compose fixture found; defaulting seed.type to inline-otlp placeholder")
		c.Seed.Type = "inline-otlp"
		c.Seed.Traces = []casefile.SeedTrace{{Service: "migrated-service", Spans: []casefile.SeedSpan{{Name: "replace-me"}}}}
	}

	for _, in := range def.Input {
		c.Input = append(c.Input, casefile.Input{
			Scheme:  in.Scheme,
			Host:    in.Host,
			Method:  in.Method,
			Path:    in.Path,
			Headers: in.Headers,
			Body:    in.Body,
			Status:  in.Status,
		})
	}

	for _, tr := range def.Expected.Traces {
		if !keepForMatrix(tr.Signal.MatrixCondition, selectedMatrix) {
			continue
		}
		assertion, ws := convertSignal(tr.TraceQL, tr.Signal)
		warnings = append(warnings, ws...)
		c.Expected.Traces = append(c.Expected.Traces, casefile.TraceAssertion{TraceQL: tr.TraceQL, MatchSpans: assertion.Match, AssertionCommon: withoutMatch(assertion)})
	}
	for _, lg := range def.Expected.Logs {
		if !keepForMatrix(lg.Signal.MatrixCondition, selectedMatrix) {
			continue
		}
		assertion, ws := convertSignal(lg.LogQL, lg.Signal)
		warnings = append(warnings, ws...)
		c.Expected.Logs = append(c.Expected.Logs, casefile.LogAssertion{LogQL: lg.LogQL, AssertionCommon: assertion})
	}
	for _, m := range def.Expected.Metrics {
		if !keepForMatrix(m.MatrixCondition, selectedMatrix) {
			continue
		}
		c.Expected.Metrics = append(c.Expected.Metrics, casefile.MetricAssertion{PromQL: m.PromQL, Value: m.Value})
		if m.MatrixCondition != "" {
			warnings = append(warnings, fmt.Sprintf("metric %q matrix-condition dropped", m.PromQL))
		}
	}
	for _, p := range def.Expected.Profiles {
		if !keepForMatrix(p.MatrixCondition, selectedMatrix) {
			continue
		}
		var matches []casefile.MatchEntry
		if p.Flamebearers.NameEquals != "" {
			matches = append(matches, casefile.MatchEntry{Name: strPtr(p.Flamebearers.NameEquals)})
		}
		if p.Flamebearers.NameRegexp != "" {
			matches = append(matches, casefile.MatchEntry{MatchType: casefile.MatchTypeRegexp, Name: strPtr(p.Flamebearers.NameRegexp)})
		}
		c.Expected.Profiles = append(c.Expected.Profiles, casefile.ProfileAssertion{
			Query:           p.Query,
			AssertionCommon: casefile.AssertionCommon{Match: matches},
		})
		if p.MatrixCondition != "" {
			warnings = append(warnings, fmt.Sprintf("profile %q matrix-condition dropped", p.Query))
		}
	}
	for _, cc := range def.Expected.CustomChecks {
		if !keepForMatrix(cc.MatrixCondition, selectedMatrix) {
			continue
		}
		c.Expected.Custom = append(c.Expected.Custom, casefile.CustomCheck{Script: cc.Script})
		if cc.MatrixCondition != "" {
			warnings = append(warnings, "custom-check matrix-condition dropped after filtering selected matrix")
		}
	}

	if err := c.Validate(); err != nil {
		return nil, warnings, fmt.Errorf("migrated case failed validation: %w", err)
	}
	return c, warnings, nil
}

// fixtureFor builds the case-local fixture: block from a legacy definition. A
// single-matrix override (selectedMatrix) takes precedence over the config-level
// blocks. Returns nil when the legacy definition declares no fixture.
func fixtureFor(def model.TestCaseDefinition, selectedMatrix *model.Matrix) *casefile.FixtureConfig {
	dc := def.DockerCompose
	k8s := def.Kubernetes
	if selectedMatrix != nil {
		if selectedMatrix.DockerCompose != nil {
			dc = selectedMatrix.DockerCompose
		}
		if selectedMatrix.Kubernetes != nil {
			k8s = selectedMatrix.Kubernetes
		}
	}

	if dc != nil {
		// Legacy files carry no app_service/app_port; a warning covers it.
		compose := &casefile.ComposeFixture{Env: dc.Environment}
		if len(dc.Files) == 1 {
			compose.File = dc.Files[0]
		} else {
			compose.Files = dc.Files
		}
		return &casefile.FixtureConfig{Compose: compose}
	}
	if k8s != nil {
		return &casefile.FixtureConfig{K3D: &casefile.K3DFixture{
			K8sDir:           k8s.Dir,
			AppService:       k8s.AppService,
			AppDockerFile:    k8s.AppDockerFile,
			AppDockerContext: k8s.AppDockerContext,
			AppDockerTag:     k8s.AppDockerTag,
			AppPort:          k8s.AppDockerPort,
			ImportImages:     k8s.ImportImages,
		}}
	}
	return nil
}

func convertSignal(label string, s model.ExpectedSignal) (casefile.AssertionCommon, []string) {
	var warnings []string
	out := casefile.AssertionCommon{}
	if s.Count != nil {
		switch {
		case s.Count.Min == 0 && s.Count.Max == 0:
			out.Absent = true
		case s.Count.Max > 0 && s.Count.Min > 0 && s.Count.Max != s.Count.Min:
			out.Count = fmt.Sprintf(">= %d", s.Count.Min)
			warnings = append(warnings, fmt.Sprintf("%s count max=%d dropped; scalar count keeps only min bound", label, s.Count.Max))
		case s.Count.Max > 0 && s.Count.Min == s.Count.Max:
			out.Count = fmt.Sprintf("== %d", s.Count.Min)
		case s.Count.Min > 0:
			out.Count = fmt.Sprintf(">= %d", s.Count.Min)
		case s.Count.Max > 0:
			out.Count = fmt.Sprintf("<= %d", s.Count.Max)
		}
	}
	if s.NoExtraAttributes {
		warnings = append(warnings, fmt.Sprintf("%s no-extra-attributes is not supported in the current schema and was dropped", label))
	}
	if s.MatrixCondition != "" {
		warnings = append(warnings, fmt.Sprintf("%s matrix-condition dropped", label))
	}
	if s.NameEquals != "" || len(s.Attributes) > 0 {
		entry := casefile.MatchEntry{}
		if s.NameEquals != "" {
			entry.Name = strPtr(s.NameEquals)
		}
		for _, k := range sortedMapKeys(s.Attributes) {
			v := s.Attributes[k]
			entry.Attributes = append(entry.Attributes, casefile.AttributeMatcher{Key: k, Value: strPtr(v)})
		}
		out.Match = append(out.Match, entry)
	}
	if s.NameRegexp != "" || len(s.AttributeRegexp) > 0 {
		entry := casefile.MatchEntry{MatchType: casefile.MatchTypeRegexp}
		if s.NameRegexp != "" {
			entry.Name = strPtr(s.NameRegexp)
		}
		for _, k := range sortedMapKeys(s.AttributeRegexp) {
			v := s.AttributeRegexp[k]
			if v == ".*" {
				entry.Attributes = append(entry.Attributes, casefile.AttributeMatcher{Key: k})
			} else {
				entry.Attributes = append(entry.Attributes, casefile.AttributeMatcher{Key: k, Value: strPtr(v)})
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

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func withoutMatch(a casefile.AssertionCommon) casefile.AssertionCommon {
	a.Match = nil
	return a
}

func deriveName(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.ReplaceAll(base, "_", " ")
	base = strings.ReplaceAll(base, "-", " ")
	return strings.TrimSpace(base)
}

func keepForMatrix(matrixCondition string, selected *model.Matrix) bool {
	if matrixCondition == "" {
		return true
	}
	if selected == nil {
		return true
	}
	re, err := regexp.Compile(matrixCondition)
	if err != nil {
		return true
	}
	return re.MatchString(selected.Name)
}

func strPtr(s string) *string { return &s }
