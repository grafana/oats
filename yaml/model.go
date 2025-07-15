package yaml

import (
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/oats/testhelpers/kubernetes"

	"github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

type ExpectedMetrics struct {
	PromQL          string `yaml:"promql"`
	Value           string `yaml:"value"`
	MatrixCondition string `yaml:"matrix-condition"`
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
	MatrixCondition   string            `yaml:"matrix-condition"`
}

type Flamebearers struct {
	Contains string `yaml:"contains"`
}

type ExpectedProfiles struct {
	Query           string       `yaml:"query"`
	Flamebearers    Flamebearers `yaml:"flamebearers"`
	MatrixCondition string       `yaml:"matrix-condition"`
}

type ExpectedTraces struct {
	TraceQL         string         `yaml:"traceql"`
	Spans           []ExpectedSpan `yaml:"spans"`
	MatrixCondition string         `yaml:"matrix-condition"`
}

type CustomCheck struct {
	Script          string `yaml:"script"`
	MatrixCondition string `yaml:"matrix-condition"`
}

type Expected struct {
	ComposeLogs  []string           `yaml:"compose-logs"`
	Logs         []ExpectedLogs     `yaml:"logs"`
	Traces       []ExpectedTraces   `yaml:"traces"`
	Metrics      []ExpectedMetrics  `yaml:"metrics"`
	Profiles     []ExpectedProfiles `yaml:"profiles"`
	CustomChecks []CustomCheck      `yaml:"custom-checks"`
}

type Matrix struct {
	Name          string                 `yaml:"name"`
	DockerCompose *DockerCompose         `yaml:"docker-compose"`
	Kubernetes    *kubernetes.Kubernetes `yaml:"kubernetes"`
}

type DockerCompose struct {
	Files       []string `yaml:"files"`
	Environment []string `yaml:"env"`
}

type Input struct {
	Scheme  string            `yaml:"scheme"`
	Host    string            `yaml:"host"`
	Method  string            `yaml:"method"`
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers"`
	Body    string            `yaml:"body"`
	Status  string            `yaml:"status"`
}

type TestCaseDefinition struct {
	Include       []string               `yaml:"include"`
	DockerCompose *DockerCompose         `yaml:"docker-compose"`
	Kubernetes    *kubernetes.Kubernetes `yaml:"kubernetes"`
	Matrix        []Matrix               `yaml:"matrix"`
	Input         []Input                `yaml:"input"`
	Interval      time.Duration          `yaml:"interval"`
	Expected      Expected               `yaml:"expected"`
}

const DefaultTestCaseInterval = 100 * time.Millisecond

func (d *TestCaseDefinition) Merge(other TestCaseDefinition) {
	d.Expected.Logs = append(d.Expected.Logs, other.Expected.Logs...)
	d.Expected.Traces = append(d.Expected.Traces, other.Expected.Traces...)
	d.Expected.Metrics = append(d.Expected.Metrics, other.Expected.Metrics...)
	d.Expected.Profiles = append(d.Expected.Profiles, other.Expected.Profiles...)
	d.Expected.CustomChecks = append(d.Expected.CustomChecks, other.Expected.CustomChecks...)
	d.Matrix = append(d.Matrix, other.Matrix...)
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
	PyroscopeHttpPort  int
}

type TestCase struct {
	Path               string
	Name               string
	MatrixTestCaseName string
	Dir                string
	Host               string
	OutputDir          string
	Definition         TestCaseDefinition
	PortConfig         *PortConfig
	Timeout            time.Duration
	LgtmVersion        string
	LgtmLogSettings    map[string]bool
	ManualDebug        bool
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
		gomega.Expect(c.Definition.DockerCompose).ToNot(gomega.BeNil(), "%s does not appear to be a valid OATS YAML file", c.Path)
		validateDockerCompose(c.Definition.DockerCompose, c.Dir)
	}
	validateInput(c.Definition.Input)
	expected := c.Definition.Expected
	gomega.Expect(len(expected.Metrics)+len(expected.Traces)+len(expected.Logs)+len(expected.Profiles)).NotTo(gomega.BeZero(), "%s does not contain any expected metrics, traces, logs or profiles", c.Path)

	for _, c := range expected.CustomChecks {
		gomega.Expect(c.Script).ToNot(gomega.BeEmpty(), "script is empty in %s", string(c.Script))
	}
	for _, l := range expected.Logs {
		out, _ := yaml.Marshal(l)
		gomega.Expect(l.LogQL).ToNot(gomega.BeEmpty(), "logQL is empty in %s", string(out))
		gomega.Expect(l.Equals == "" && l.Contains == nil && l.Regexp == "").To(gomega.BeFalse())
		for _, s := range l.Contains {
			gomega.Expect(s).ToNot(gomega.BeEmpty(), "contains string is empty in %s", string(out))
		}
	}
	for _, d := range expected.Metrics {
		out, _ := yaml.Marshal(d)
		gomega.Expect(d.PromQL).ToNot(gomega.BeEmpty(), "promQL is empty in %s", string(out))
		gomega.Expect(d.Value).ToNot(gomega.BeEmpty(), "value is empty in %s", string(out))
	}
	for _, d := range expected.Traces {
		out, _ := yaml.Marshal(d)
		gomega.Expect(d.TraceQL).ToNot(gomega.BeEmpty(), "traceQL is empty in %s", string(out))
		gomega.Expect(d.Spans).ToNot(gomega.BeEmpty(), "spans are empty in %s", string(out))
		for _, span := range d.Spans {
			gomega.Expect(span.Name).ToNot(gomega.BeEmpty(), "span name is empty in %s", string(out))
			for k, v := range span.Attributes {
				gomega.Expect(k).ToNot(gomega.BeEmpty(), "attribute key is empty in %s", string(out))
				gomega.Expect(v).ToNot(gomega.BeEmpty(), "attribute value is empty in %s", string(out))
			}
		}
	}
	for _, p := range expected.Profiles {
		out, _ := yaml.Marshal(p)
		gomega.Expect(p.Query).ToNot(gomega.BeEmpty(), "query is empty in %s", string(out))
		gomega.Expect(p.Flamebearers.Contains).ToNot(gomega.BeEmpty(), "Flamebearers.contains is empty in %s", string(out))
	}

	if c.PortConfig == nil {
		// We're in non-parallel mode, so we can static ports here.
		c.PortConfig = &PortConfig{
			ApplicationPort:    8080,
			GrafanaHTTPPort:    3000,
			PrometheusHTTPPort: 9090,
			LokiHTTPPort:       3100,
			TempoHTTPPort:      3200,
			PyroscopeHttpPort:  4040,
		}
	}

	slog.Info("ports",
		"grafana", c.PortConfig.GrafanaHTTPPort,
		"prometheus", c.PortConfig.PrometheusHTTPPort,
		"loki", c.PortConfig.LokiHTTPPort,
		"tempo", c.PortConfig.TempoHTTPPort,
		"pyroscope", c.PortConfig.PyroscopeHttpPort,
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
		if i.Method != "" {
			gomega.Expect(strings.ToUpper(i.Method)).To(gomega.Or(
				gomega.Equal(http.MethodConnect),
				gomega.Equal(http.MethodDelete),
				gomega.Equal(http.MethodGet),
				gomega.Equal(http.MethodHead),
				gomega.Equal(http.MethodOptions),
				gomega.Equal(http.MethodPatch),
				gomega.Equal(http.MethodPost),
				gomega.Equal(http.MethodPut),
				gomega.Equal(http.MethodTrace),
			), "method must be a supported HTTP method or be empty")
		}
		if (i.Method == "" || i.Method == http.MethodGet) && i.Body != "" {
			gomega.Expect(i.Body).To(gomega.BeEmpty(), "body must be empty for GET requests")
		}
		if i.Scheme != "" {
			gomega.Expect(strings.ToLower(i.Scheme)).To(gomega.Or(
				gomega.Equal("http"),
				gomega.Equal("https"),
			), "scheme must be http, https or be empty")
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
