package yaml

import (
	"fmt"
	"github.com/grafana/oats/observability"
	"github.com/grafana/oats/testhelpers/kubernetes"
	"io"
	"path/filepath"
	"strconv"
	"time"

	"github.com/grafana/dashboard-linter/lint"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
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
	AllowDups  bool              `yaml:"allow-duplicates"`
}

type ExpectedLogs struct {
	LogQL             string            `yaml:"logql"`
	Equals            string            `yaml:"equals"`
	Contains          []string          `yaml:"contains"`
	Regexp            string            `yaml:"regexp"`
	Attributes        map[string]string `yaml:"attributes"`
	AttributeRegexp   map[string]string `yaml:"attribute-regexp"`
	NoExtraAttributes bool              `yaml:"no-extra-attributes"`
}

type ExpectedTraces struct {
	TraceQL string         `yaml:"traceql"`
	Spans   []ExpectedSpan `yaml:"spans"`
}

type CustomCheck struct {
	Script string `yaml:"script"`
}

type Expected struct {
	Logs         []ExpectedLogs      `yaml:"logs"`
	Traces       []ExpectedTraces    `yaml:"traces"`
	Metrics      []ExpectedMetrics   `yaml:"metrics"`
	Dashboards   []ExpectedDashboard `yaml:"dashboards"`
	CustomChecks []CustomCheck       `yaml:"custom-checks"`
}

type DockerCompose struct {
	Generator   string   `yaml:"generator"` // deprecated: only used by beyla
	Files       []string `yaml:"files"`
	Environment []string `yaml:"env"`
}

type Input struct {
	Path   string `yaml:"path"`
	Status string `yaml:"status"`
}

type TestCaseDefinition struct {
	Include       []string               `yaml:"include"`
	DockerCompose *DockerCompose         `yaml:"docker-compose"`
	Kubernetes    *kubernetes.Kubernetes `yaml:"kubernetes"`
	Input         []Input                `yaml:"input"`
	Interval      time.Duration          `yaml:"interval"`
	Expected      Expected               `yaml:"expected"`
}

const DefaultTestCaseInterval = 100 * time.Millisecond

func (d *TestCaseDefinition) Merge(other TestCaseDefinition) {
	d.Expected.Logs = append(d.Expected.Logs, other.Expected.Logs...)
	d.Expected.Traces = append(d.Expected.Traces, other.Expected.Traces...)
	d.Expected.Metrics = append(d.Expected.Metrics, other.Expected.Metrics...)
	d.Expected.Dashboards = append(d.Expected.Dashboards, other.Expected.Dashboards...)
	d.Expected.CustomChecks = append(d.Expected.CustomChecks, other.Expected.CustomChecks...)
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
	Verbose  bool
	endpoint observability.Endpoint
	Logger   io.WriteCloser
}

func NewQueryLogger(endpoint observability.Endpoint, logger io.WriteCloser) QueryLogger {
	return QueryLogger{
		endpoint: endpoint,
		Logger:   logger,
	}
}

func (q *QueryLogger) LogQueryResult(format string, a ...any) {
	result := fmt.Sprintf(format, a...)
	if q.Verbose {
		_, _ = q.Logger.Write([]byte(result))
		if len(result) > 1000 {
			result = result[:1000] + ".."
		}
		ginkgo.GinkgoWriter.Println(result)
	}
}

func (c *TestCase) validateAndSetVariables() {
	if c.Definition.Kubernetes != nil {
		validateK8s(c.Definition.Kubernetes)
		gomega.Expect(c.Definition.DockerCompose).To(gomega.BeNil(), "kubernetes and docker-compose are mutually exclusive")
	} else {
		validateDockerCompose(c.Definition.DockerCompose, c.Dir)
	}
	validateInput(c.Definition.Input)
	expected := c.Definition.Expected
	if len(expected.Metrics) == 0 && len(expected.Dashboards) == 0 && len(expected.Traces) == 0 && len(expected.Logs) == 0 {
		ginkgo.Fail("expected metrics or dashboards or traces or logs")
	}
	for _, c := range expected.CustomChecks {
		gomega.Expect(c.Script).ToNot(gomega.BeEmpty(), "script is empty in "+string(c.Script))
	}
	for _, l := range expected.Logs {
		out, _ := yaml.Marshal(l)
		gomega.Expect(l.LogQL).ToNot(gomega.BeEmpty(), "logQL is empty in "+string(out))
		if l.Equals == "" && l.Contains == nil && l.Regexp == "" {
			ginkgo.Fail("expected equals or contains or regexp in logs")
		}
		for _, s := range l.Contains {
			gomega.Expect(s).ToNot(gomega.BeEmpty(), "contains string is empty in "+string(out))
		}
	}
	for _, d := range expected.Metrics {
		out, _ := yaml.Marshal(d)
		gomega.Expect(d.PromQL).ToNot(gomega.BeEmpty(), "promQL is empty in "+string(out))
		gomega.Expect(d.Value).ToNot(gomega.BeEmpty(), "value is empty in "+string(out))
	}
	for _, d := range expected.Traces {
		out, _ := yaml.Marshal(d)
		gomega.Expect(d.TraceQL).ToNot(gomega.BeEmpty(), "traceQL is empty in "+string(out))
		gomega.Expect(d.Spans).ToNot(gomega.BeEmpty(), "spans are empty in "+string(out))
		for _, span := range d.Spans {
			gomega.Expect(span.Name).ToNot(gomega.BeEmpty(), "span name is empty in "+string(out))
			for k, v := range span.Attributes {
				gomega.Expect(k).ToNot(gomega.BeEmpty(), "attribute key is empty in "+string(out))
				gomega.Expect(v).ToNot(gomega.BeEmpty(), "attribute value is empty in "+string(out))
			}
		}
	}
	for _, d := range expected.Dashboards {
		out, _ := yaml.Marshal(d)
		gomega.Expect(d.Path).ToNot(gomega.BeEmpty(), "path is emtpy in "+string(out))
		gomega.Expect(d.Panels).ToNot(gomega.BeEmpty(), "panels are empty in "+string(out))
		for _, panel := range d.Panels {
			gomega.Expect(panel.Title).ToNot(gomega.BeEmpty(), "panel title is empty in "+string(out))
			gomega.Expect(panel.Value).ToNot(gomega.BeEmpty(), "value is empty in "+string(out))
		}

		gomega.Expect(c.Dashboard).To(gomega.BeNil(), "only one dashboard is supported")
		dashboardPath := filepath.Join(c.Dir, d.Path)
		c.Dashboard = &TestDashboard{
			Path: dashboardPath,
		}
	}

	if c.PortConfig == nil {
		// We're in non-parallel mode, so we can static ports here.
		c.PortConfig = &PortConfig{
			ApplicationPort:    8080,
			GrafanaHTTPPort:    3000,
			PrometheusHTTPPort: 9090,
			LokiHTTPPort:       3100,
			TempoHTTPPort:      3200,
		}
	}

	ginkgo.GinkgoWriter.Printf("grafana port: %d\n", c.PortConfig.GrafanaHTTPPort)
	ginkgo.GinkgoWriter.Printf("prometheus port: %d\n", c.PortConfig.PrometheusHTTPPort)
	ginkgo.GinkgoWriter.Printf("loki port: %d\n", c.PortConfig.LokiHTTPPort)
	ginkgo.GinkgoWriter.Printf("tempo port: %d\n", c.PortConfig.TempoHTTPPort)
	ginkgo.GinkgoWriter.Printf("application port: %d\n", c.PortConfig.ApplicationPort)
}

func validateK8s(kubernetes *kubernetes.Kubernetes) {
	gomega.Expect(kubernetes.Dir).ToNot(gomega.BeEmpty(), "k8s-dir is empty")
	gomega.Expect(kubernetes.AppService).ToNot(gomega.BeEmpty(), "k8s-app-service is empty")
	gomega.Expect(kubernetes.AppDockerFile).ToNot(gomega.BeEmpty(), "app-docker-file is empty")
	gomega.Expect(kubernetes.AppDockerTag).ToNot(gomega.BeEmpty(), "app-docker-tag is empty")
	gomega.Expect(kubernetes.AppDockerPort).ToNot(gomega.BeZero(), "app-docker-port is zero")
}

func validateInput(input []Input) {
	for _, i := range input {
		gomega.Expect(i.Path).ToNot(gomega.BeEmpty(), "input path is empty")
		if i.Status != "" {
			_, err := strconv.ParseInt(i.Status, 10, 32)
			gomega.Expect(err).To(gomega.BeNil(), "status must parse as integer or be empty")
		}
	}
}

func validateDockerCompose(d *DockerCompose, dir string) {
	if len(d.Files) > 0 {
		for i, filename := range d.Files {
			d.Files[i] = filepath.Join(dir, filename)
			gomega.Expect(d.Files[i]).To(gomega.BeARegularFile())
		}
	}
}
