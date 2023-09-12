package yaml

import (
	"fmt"
	"github.com/grafana/dashboard-linter/lint"
	"github.com/grafana/oats/internal/testhelpers/compose"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
	"path/filepath"
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
	Path string `yaml:"path"`
}

type TestCaseDefinition struct {
	Include       []string       `yaml:"include"`
	DockerCompose *DockerCompose `yaml:"docker-compose"`
	Input         []Input        `yaml:"input"`
	Expected      Expected       `yaml:"expected"`
}

func (d *TestCaseDefinition) Merge(other TestCaseDefinition) {
	d.Expected.Traces = append(d.Expected.Traces, other.Expected.Traces...)
	d.Expected.Metrics = append(d.Expected.Metrics, other.Expected.Metrics...)
	d.Expected.Dashboards = append(d.Expected.Dashboards, other.Expected.Dashboards...)
	if d.DockerCompose == nil {
		d.DockerCompose = other.DockerCompose
	}
	d.Input = append(d.Input, other.Input...)
}

type TestDashboard struct {
	Path    string
	Content lint.Dashboard
}

type PortConfig struct {
	ApplicationPort    int
	GrafanaHTTPPort    int
	PrometheusHTTPPort int
	LokiHTTPPort       int
	TempoHTTPPort      int
}

type TestCase struct {
	Name       string
	Dir        string
	OutputDir  string
	Definition TestCaseDefinition
	PortConfig *PortConfig
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
		ginkgo.GinkgoWriter.Println(result)
	}
}

func (c *TestCase) validateAndSetVariables() {
	validateDockerCompose(c.Definition.DockerCompose, c.Dir)
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
		dashboardPath := filepath.Join(c.Dir, d.Path)
		c.Dashboard = &TestDashboard{
			Path: dashboardPath,
		}
	}

	if c.PortConfig == nil {
		// In parallel execution, we allocate the ports before we start executing in parallel
		// to avoid taking the same port.
		c.PortConfig = NewPortAllocator(1).AllocatePorts()
	}

	ginkgo.GinkgoWriter.Printf("grafana port: %d\n", c.PortConfig.GrafanaHTTPPort)
	ginkgo.GinkgoWriter.Printf("prometheus port: %d\n", c.PortConfig.PrometheusHTTPPort)
	ginkgo.GinkgoWriter.Printf("loki port: %d\n", c.PortConfig.LokiHTTPPort)
	ginkgo.GinkgoWriter.Printf("tempo port: %d\n", c.PortConfig.TempoHTTPPort)
	ginkgo.GinkgoWriter.Printf("application port: %d\n", c.PortConfig.ApplicationPort)
}

func validateInput(input []Input) {
	Expect(input).ToNot(BeEmpty(), "input is empty")
	for _, i := range input {
		Expect(i.Path).ToNot(BeEmpty(), "input path is empty")
	}
}

func validateDockerCompose(d *DockerCompose, dir string) {
	if d.File != "" {
		d.File = filepath.Join(dir, d.File)
		Expect(d.File).To(BeARegularFile())
		for _, resource := range d.Resources {
			Expect(filepath.Join(filepath.Dir(d.File), resource)).To(BeAnExistingFile())
		}
	} else {
		Expect(d.Generator).ToNot(BeEmpty(), "generator needed if no file is specified")
		Expect(d.Resources).To(BeEmpty(), "resources requires file")
	}
}
