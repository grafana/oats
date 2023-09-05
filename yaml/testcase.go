package yaml

import (
	"fmt"
	"github.com/grafana/dashboard-linter/lint"
	"github.com/grafana/oats/internal/testhelpers/compose"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type ExpectedDashboardPanel struct {
	Title string `yaml:"title"`
	Value string `yaml:"value"`
}

type ExpectedDashboard struct {
	Path   string                   `yaml:"path"`
	Panels []ExpectedDashboardPanel `yaml:"panels"`
}

type ExpectedMetrics struct {
	PromQL string `yaml:"promql"`
	Value  string `yaml:"value"`
}

type ExpectedSpan struct {
	Name       string            `yaml:"name"`
	Attributes map[string]string `yaml:"attributes"`
}

type ExpectedTraces struct {
	TraceQL string         `yaml:"traceql"`
	Spans   []ExpectedSpan `yaml:"spans"`
}

type Expected struct {
	Traces     []ExpectedTraces    `yaml:"traces"`
	Metrics    []ExpectedMetrics   `yaml:"metrics"`
	Dashboards []ExpectedDashboard `yaml:"dashboards"`
}

type JavaGeneratorParams struct {
	OtelJmxConfig string `yaml:"otel-jmx-config"`
}

type DockerCompose struct {
	Generator           string              `yaml:"generator"`
	File                string              `yaml:"file"`
	Resources           []string            `yaml:"resources"`
	JavaGeneratorParams JavaGeneratorParams `yaml:"java-generator-params"`
}

type Input struct {
	Url string `yaml:"url"`
}

type TestCaseDefinition struct {
	DockerCompose DockerCompose `yaml:"docker-compose"`
	Input         []Input       `yaml:"input"`
	Expected      Expected      `yaml:"expected"`
}

type TestDashboard struct {
	Path    string
	Content lint.Dashboard
}

type TestCase struct {
	Name       string
	Dir        string
	OutputDir  string
	Definition TestCaseDefinition
	Dashboard  *TestDashboard
	Timeout    time.Duration
}

type QueryLogger struct {
	verbose  bool
	endpoint *compose.ComposeEndpoint
}

func NewQueryLogger(endpoint *compose.ComposeEndpoint, verbose bool) QueryLogger {
	return QueryLogger{
		endpoint: endpoint,
		verbose:  verbose,
	}
}

func (q *QueryLogger) LogQueryResult(format string, a ...any) {
	result := fmt.Sprintf(format, a...)
	if q.verbose {
		_, _ = fmt.Fprintf(q.endpoint.Logger(), result)
		if len(result) > 100 {
			result = result[:100] + ".."
		}
		fmt.Print(result)
	}
}

func ReadTestCases() ([]TestCase, string) {
	var cases []TestCase

	base := os.Getenv("TESTCASE_BASE_PATH")
	if base != "" {
		abs, err := filepath.Abs(base)
		if err != nil {
			panic(err)
		}
		base = abs
		timeout := os.Getenv("TESTCASE_TIMEOUT")
		if timeout == "" {
			timeout = "30s"
		}
		duration, err := time.ParseDuration(timeout)
		if err != nil {
			panic(err)
		}

		err = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.Name() != "oats.yaml" {
				return nil
			}
			def := TestCaseDefinition{}
			content, err := os.ReadFile(p)
			err = yaml.Unmarshal(content, &def)
			if err != nil {
				return err
			}
			dir := path.Dir(p)
			name := strings.TrimPrefix(dir, base)
			name = strings.TrimPrefix(name, "/")
			name = strings.ReplaceAll(name, "/", "-")
			cases = append(cases, TestCase{
				Name:       name,
				Dir:        dir,
				Definition: def,
				Timeout:    duration,
			})
			return nil
		})
		if err != nil {
			panic(err)
		}
	}
	return cases, base
}

func (c *TestCase) ValidateAndSetDashboard() {
	validateDockerCompose(&c.Definition.DockerCompose, c.Dir)
	validateInput(c.Definition.Input)
	expected := c.Definition.Expected
	if len(expected.Metrics) == 0 && len(expected.Dashboards) == 0 && len(expected.Traces) == 0 {
		ginkgo.Fail("expected metrics or dashboards or traces")
	}
	for _, d := range expected.Metrics {
		out, _ := yaml.Marshal(d)
		Expect(d.PromQL).ToNot(BeEmpty(), "promQL is empty in "+string(out))
		Expect(d.Value).ToNot(BeEmpty(), "value is empty in "+string(out))
	}
	for _, d := range expected.Traces {
		out, _ := yaml.Marshal(d)
		Expect(d.TraceQL).ToNot(BeEmpty(), "traceQL is empty in "+string(out))
		Expect(d.Spans).ToNot(BeEmpty(), "spans are empty in "+string(out))
		for _, span := range d.Spans {
			Expect(span.Name).ToNot(BeEmpty(), "span name is empty in "+string(out))
			for k, v := range span.Attributes {
				Expect(k).ToNot(BeEmpty(), "attribute key is empty in "+string(out))
				Expect(v).ToNot(BeEmpty(), "attribute value is empty in "+string(out))
			}
		}
	}
	for _, d := range expected.Dashboards {
		out, _ := yaml.Marshal(d)
		Expect(d.Path).ToNot(BeEmpty(), "path is emtpy in "+string(out))
		Expect(d.Panels).ToNot(BeEmpty(), "panels are empty in "+string(out))
		for _, panel := range d.Panels {
			Expect(panel.Title).ToNot(BeEmpty(), "panel title is empty in "+string(out))
			Expect(panel.Value).ToNot(BeEmpty(), "value is empty in "+string(out))
		}

		Expect(c.Dashboard).To(BeNil(), "only one dashboard is supported")
		dashboardPath := path.Join(c.Dir, d.Path)
		c.Dashboard = &TestDashboard{
			Path: dashboardPath,
		}
	}
}

func validateInput(input []Input) {
	Expect(input).ToNot(BeEmpty())
	for _, i := range input {
		Expect(i.Url).ToNot(BeEmpty())
	}
}

func validateDockerCompose(d *DockerCompose, dir string) {
	if d.File != "" {
		d.File = path.Join(dir, d.File)
		Expect(d.File).To(BeARegularFile())
		for _, resource := range d.Resources {
			Expect(path.Join(path.Dir(d.File), resource)).To(BeAnExistingFile())
		}
	} else {
		Expect(d.Generator).ToNot(BeEmpty())
		Expect(d.Resources).To(BeEmpty())
	}
}
