package yaml

import (
	"fmt"
	"github.com/grafana/oats/testhelpers/kubernetes"
	"log/slog"
	"path/filepath"
	"strconv"
	"time"

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
	ComposeLogs  []string          `yaml:"compose-logs"`
	Logs         []ExpectedLogs    `yaml:"logs"`
	Traces       []ExpectedTraces  `yaml:"traces"`
	Metrics      []ExpectedMetrics `yaml:"metrics"`
	CustomChecks []CustomCheck     `yaml:"custom-checks"`
}

type DockerCompose struct {
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
	d.Expected.CustomChecks = append(d.Expected.CustomChecks, other.Expected.CustomChecks...)
	if d.DockerCompose == nil {
		d.DockerCompose = other.DockerCompose
	}
	d.Input = append(d.Input, other.Input...)
}

type PortConfig struct {
	ApplicationPort    int
	GrafanaHTTPPort    int
	PrometheusHTTPPort int
	LokiHTTPPort       int
	TempoHTTPPort      int
}

type TestCase struct {
	Name        string
	Dir         string
	OutputDir   string
	Definition  TestCaseDefinition
	PortConfig  *PortConfig
	Timeout     time.Duration
	LgtmVersion string
	ManualDebug bool
}

func (r *runner) LogQueryResult(format string, a ...any) {
	if r.Verbose {
		result := fmt.Sprintf(format, a...)
		if len(result) > 1000 {
			result = result[:1000] + ".."
		}
		slog.Info(result)
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
	gomega.Expect(len(expected.Metrics) == 0 && len(expected.Traces) == 0 && len(expected.Logs) == 0).To(gomega.BeFalse())

	for _, c := range expected.CustomChecks {
		gomega.Expect(c.Script).ToNot(gomega.BeEmpty(), "script is empty in "+string(c.Script))
	}
	for _, l := range expected.Logs {
		out, _ := yaml.Marshal(l)
		gomega.Expect(l.LogQL).ToNot(gomega.BeEmpty(), "logQL is empty in "+string(out))
		gomega.Expect(l.Equals == "" && l.Contains == nil && l.Regexp == "").To(gomega.BeFalse())
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

	slog.Info("ports",
		"grafana", c.PortConfig.GrafanaHTTPPort,
		"prometheus", c.PortConfig.PrometheusHTTPPort,
		"loki", c.PortConfig.LokiHTTPPort,
		"tempo", c.PortConfig.TempoHTTPPort,
		"application", c.PortConfig.ApplicationPort)
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
