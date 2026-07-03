package migrate

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/grafana/oats/casefile"
	"github.com/grafana/oats/model"
	legacyyaml "github.com/grafana/oats/yaml"
	goyaml "go.yaml.in/yaml/v3"
)

// ConvertFile reads one legacy OATS yaml file and returns a best-effort
// current-format case yaml plus any warnings about dropped or lossy fields.
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
// current case yaml shape. Unsupported fields are dropped with warnings.
func ConvertDefinition(def model.TestCaseDefinition, name string) (*casefile.Case, []string, error) {
	var warnings []string
	c := &casefile.Case{
		OatsSchemaVersion: casefile.SchemaVersion,
		Name:              name,
		Interval:          def.Interval,
	}
	selectedMatrix := (*model.Matrix)(nil)

	if def.Kubernetes != nil {
		c.Seed.Type = "app"
		warnings = append(warnings, "legacy kubernetes fixture migrated as an app-backed case; paste the suggested [fixture] block below into oats.toml")
		warnings = append(warnings, kubernetesFixtureHint(name, def))
	}
	if len(def.Matrix) > 0 {
		if len(def.Matrix) == 1 {
			selectedMatrix = &def.Matrix[0]
			c.Name = fmt.Sprintf("%s [%s]", name, selectedMatrix.Name)
			warnings = append(warnings, fmt.Sprintf("flattened single matrix entry %q into the migrated case", selectedMatrix.Name))
			if selectedMatrix.DockerCompose != nil {
				if len(selectedMatrix.DockerCompose.Files) == 0 {
					return nil, warnings, fmt.Errorf("matrix docker-compose present but no files declared")
				}
				c.Seed.Type = "app"
				c.Seed.Compose = selectedMatrix.DockerCompose.Files[0]
				warnings = append(warnings, "single matrix docker-compose fixture selected; paste the suggested [fixture] block below into oats.toml")
				warnings = append(warnings, matrixFixtureHint(c.Name, *selectedMatrix))
			}
			if selectedMatrix.Kubernetes != nil {
				c.Seed.Type = "app"
				warnings = append(warnings, "single matrix kubernetes fixture selected; paste the suggested [fixture] block below into oats.toml")
				warnings = append(warnings, matrixFixtureHint(c.Name, *selectedMatrix))
			}
		} else {
			warnings = append(warnings, "matrix definitions are not migrated automatically when multiple entries exist; convert expanded matrix cases manually")
			warnings = append(warnings, matrixExpansionHint(name, def.Matrix))
		}
	}
	if len(def.Include) > 0 {
		warnings = append(warnings, "include directives were resolved before migration; output is a flattened case")
	}
	if len(def.Expected.ComposeLogs) > 0 {
		warnings = append(warnings, "compose-logs assertions are not migrated")
	}

	if def.DockerCompose != nil {
		c.Seed.Type = "app"
		if len(def.DockerCompose.Files) == 0 {
			return nil, warnings, fmt.Errorf("docker-compose present but no files declared")
		}
		c.Seed.Compose = def.DockerCompose.Files[0]
		if len(def.DockerCompose.Files) > 1 || len(def.DockerCompose.Environment) > 0 {
			warnings = append(warnings, "legacy docker-compose fixture uses suite-level config richer than case-level seed.compose; paste the suggested [fixture] block below into oats.toml")
			warnings = append(warnings, composeFixtureHint(name, def))
		}
	} else if def.Kubernetes == nil && selectedMatrix == nil {
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

func composeFixtureHint(name string, def model.TestCaseDefinition) string {
	fixtureName := slug(name)
	var b strings.Builder
	fmt.Fprintf(&b, "suggested oats.toml fixture snippet for %q:\n", name)
	fmt.Fprintf(&b, "[fixture.%s]\n", fixtureName)
	fmt.Fprintf(&b, "type = \"compose\"\n")
	if len(def.DockerCompose.Files) == 1 {
		fmt.Fprintf(&b, "compose_file = %q\n", def.DockerCompose.Files[0])
	} else if len(def.DockerCompose.Files) > 1 {
		fmt.Fprintf(&b, "compose_files = [%s]\n", quotedList(def.DockerCompose.Files))
	}
	if len(def.DockerCompose.Environment) > 0 {
		fmt.Fprintf(&b, "env = [%s]\n", quotedList(def.DockerCompose.Environment))
	}
	return strings.TrimRight(b.String(), "\n")
}

func kubernetesFixtureHint(name string, def model.TestCaseDefinition) string {
	fixtureName := slug(name)
	k := def.Kubernetes
	var b strings.Builder
	fmt.Fprintf(&b, "suggested oats.toml fixture snippet for %q:\n", name)
	fmt.Fprintf(&b, "[fixture.%s]\n", fixtureName)
	fmt.Fprintf(&b, "type = \"k3d\"\n")
	fmt.Fprintf(&b, "k8s_dir = %q\n", k.Dir)
	fmt.Fprintf(&b, "app_service = %q\n", k.AppService)
	fmt.Fprintf(&b, "app_docker_file = %q\n", k.AppDockerFile)
	if k.AppDockerContext != "" {
		fmt.Fprintf(&b, "app_docker_context = %q\n", k.AppDockerContext)
	}
	fmt.Fprintf(&b, "app_docker_tag = %q\n", k.AppDockerTag)
	fmt.Fprintf(&b, "app_port = %d\n", k.AppDockerPort)
	if len(k.ImportImages) > 0 {
		fmt.Fprintf(&b, "import_images = [%s]\n", quotedList(k.ImportImages))
	}
	return strings.TrimRight(b.String(), "\n")
}

func matrixFixtureHint(name string, m model.Matrix) string {
	def := model.TestCaseDefinition{
		DockerCompose: m.DockerCompose,
		Kubernetes:    m.Kubernetes,
	}
	if m.DockerCompose != nil {
		return composeFixtureHint(name, def)
	}
	if m.Kubernetes != nil {
		return kubernetesFixtureHint(name, def)
	}
	return fmt.Sprintf("matrix %q has no fixture override", m.Name)
}

func matrixExpansionHint(name string, matrix []model.Matrix) string {
	var b strings.Builder
	fmt.Fprintf(&b, "suggested matrix expansion for %q:\n", name)
	for _, m := range matrix {
		fmt.Fprintf(&b, "- %s\n", m.Name)
		if m.DockerCompose != nil || m.Kubernetes != nil {
			fixture := indentLines(matrixFixtureHint(fmt.Sprintf("%s [%s]", name, m.Name), m), "  ")
			fmt.Fprintf(&b, "%s\n", fixture)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func indentLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func quotedList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, fmt.Sprintf("%q", item))
	}
	return strings.Join(quoted, ", ")
}

func slug(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		keep := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if keep {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "migrated"
	}
	return out
}

func strPtr(s string) *string { return &s }
