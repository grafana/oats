package model

import (
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/oats/testhelpers/kubernetes"
	"github.com/onsi/gomega"
	"go.yaml.in/yaml/v3"
)

type ExpectedSignal struct {
	Contains          []string          `yaml:"contains"` // deprecated, use regexp instead
	NameEquals        string            `yaml:"equals"`
	NameRegexp        string            `yaml:"regexp"`
	Attributes        map[string]string `yaml:"attributes"`
	AttributeRegexp   map[string]string `yaml:"attribute-regexp"`
	NoExtraAttributes bool              `yaml:"no-extra-attributes"`
	MatrixCondition   string            `yaml:"matrix-condition"`
	Count             *ExpectedRange    `yaml:"count"`
}

func (s ExpectedSignal) ExpectAbsent() bool {
	return s.Count != nil && s.Count.Min == 0 && s.Count.Max == 0
}

type ExpectedMetrics struct {
	PromQL          string `yaml:"promql"`
	Value           string `yaml:"value"`
	MatrixCondition string `yaml:"matrix-condition"`
}

// Deprecated: use ExpectedSignal instead
type ExpectedSpan struct {
	Name string `yaml:"name"`
}

type ExpectedLogs struct {
	LogQL  string         `yaml:"logql"`
	Signal ExpectedSignal `yaml:",inline"`
}

type Flamebearers struct {
	Contains   string `yaml:"contains"` // deprecated, use regexp instead
	NameEquals string `yaml:"equals"`
	NameRegexp string `yaml:"regexp"`
}

type ExpectedProfiles struct {
	Query           string       `yaml:"query"`
	Flamebearers    Flamebearers `yaml:"flamebearers"`
	MatrixCondition string       `yaml:"matrix-condition"`
}

type ExpectedRange struct {
	Min int `yaml:"min"`
	Max int `yaml:"max"`
}

type ExpectedTraces struct {
	TraceQL string         `yaml:"traceql"`
	Spans   []ExpectedSpan `yaml:"spans"` // deprecated, use fields below instead
	Signal  ExpectedSignal `yaml:",inline"`
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

func (c *TestCase) ValidateAndSetVariables(g gomega.Gomega) {
	if c.Definition.Kubernetes != nil {
		validateK8s(c.Definition.Kubernetes, g)
		g.Expect(c.Definition.DockerCompose).To(gomega.BeNil(), "kubernetes and docker-compose are mutually exclusive")
	} else {
		g.Expect(c.Definition.DockerCompose).ToNot(gomega.BeNil(), "%s does not appear to be a valid OATS YAML file", c.Path)
		if c.Definition.DockerCompose != nil {
			validateDockerCompose(c.Definition.DockerCompose, c.Dir, g)
		}
	}
	ValidateInput(g, c.Definition.Input)
	expected := c.Definition.Expected
	g.Expect(len(expected.Metrics)+len(expected.Traces)+len(expected.Logs)+len(expected.Profiles)).NotTo(gomega.BeZero(), "%s does not contain any expected metrics, traces, logs or profiles", c.Path)

	for _, c := range expected.CustomChecks {
		g.Expect(c.Script).ToNot(gomega.BeEmpty(), "script is empty in %s", string(c.Script))
	}
	for _, l := range expected.Logs {
		out, _ := yaml.Marshal(l)
		g.Expect(l.LogQL).ToNot(gomega.BeEmpty(), "logQL is empty in %s", string(out))
		validateSignal(g, l.Signal, out)
	}
	for _, d := range expected.Metrics {
		out, _ := yaml.Marshal(d)
		g.Expect(d.PromQL).ToNot(gomega.BeEmpty(), "promQL is empty in %s", string(out))
		g.Expect(d.Value).ToNot(gomega.BeEmpty(), "value is empty in %s", string(out))
	}
	for _, d := range expected.Traces {
		out, _ := yaml.Marshal(d)
		g.Expect(d.TraceQL).ToNot(gomega.BeEmpty(), "traceQL is empty in %s", string(out))
		g.Expect(d.Spans).To(gomega.BeEmpty(), "spans are deprecated, add to 'traces' directly: %s", string(out))
		validateSignal(g, d.Signal, out)

		for _, span := range d.Spans {
			g.Expect(span.Name).ToNot(gomega.BeEmpty(), "span name is empty in %s", string(out))
		}
	}
	for _, p := range expected.Profiles {
		out, _ := yaml.Marshal(p)
		g.Expect(p.Query).ToNot(gomega.BeEmpty(), "query is empty in %s", string(out))
		f := p.Flamebearers
		g.Expect(f.Contains).To(gomega.BeNil(), "'contains' is deprecated, use 'regexp' instead in %s", string(out))
		g.Expect(f.NameEquals == "" && f.NameRegexp == "").To(gomega.BeFalse(),
			"either 'equals' or 'regexp' must be set in %s", string(out))
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

func validateSignal(g gomega.Gomega, signal ExpectedSignal, out []byte) {
	g.Expect(signal.Contains).To(gomega.BeNil(), "'contains' is deprecated, use 'regexp' instead in %s", string(out))
	if signal.ExpectAbsent() {
		// expect all fields to be empty
		g.Expect(signal.NameEquals).To(gomega.BeNil(), "expected 'equals' to be nil when count min=0 and max=0 in %s", string(out))
		g.Expect(signal.NameRegexp).To(gomega.BeNil(), "expected 'regexp' to be nil when count min=0 and max=0 in %s", string(out))
		g.Expect(len(signal.Attributes)).To(gomega.BeZero(), "expected 'attributes' to be empty when count min=0 and max=0 in %s", string(out))
		g.Expect(len(signal.AttributeRegexp)).To(gomega.BeZero(), "expected 'attribute-regexp' to be empty when count min=0 and max=0 in %s", string(out))
	} else {
		g.Expect(signal.NameEquals == "" && signal.NameRegexp == "").To(gomega.BeFalse(),
			"either 'equals' or 'regexp' must be set in %s", string(out))
		for k, v := range signal.Attributes {
			g.Expect(k).ToNot(gomega.BeEmpty(), "attribute key is empty in %s", string(out))
			g.Expect(v).ToNot(gomega.BeEmpty(), "attribute value is empty in %s", string(out))
		}
		for k, v := range signal.AttributeRegexp {
			g.Expect(k).ToNot(gomega.BeEmpty(), "attribute key is empty in %s", string(out))
			g.Expect(v).ToNot(gomega.BeEmpty(), "attribute value is empty in %s", string(out))
		}
	}

	// cases
	// count if nil -> 1 or more
	// min=0, max>0 -> not supported
	// min=0, max=0 -> exactly 0
	// min>0, max=0 -> min or more
	// min>0, max>=min -> between min and max
	c := signal.Count
	if c != nil {
		// 0..1+ is not supported
		g.Expect(c.Min == 0 && c.Max > 0).To(gomega.BeFalse(), "count min=0 and max>0 is not supported in %s", string(out))

		g.Expect(c.Min).To(gomega.BeNumerically(">=", 0), "count.min is negative in %s", string(out))
		g.Expect(c.Max).To(gomega.Or(gomega.Equal(0), gomega.BeNumerically(">=", c.Min)), "count.max is less than count.min in %s", string(out))
	}
}

func validateK8s(kubernetes *kubernetes.Kubernetes, g gomega.Gomega) {
	g.Expect(kubernetes.Dir).ToNot(gomega.BeEmpty(), "k8s-dir is empty")
	g.Expect(kubernetes.AppService).ToNot(gomega.BeEmpty(), "k8s-app-service is empty")
	g.Expect(kubernetes.AppDockerFile).ToNot(gomega.BeEmpty(), "app-docker-file is empty")
	g.Expect(kubernetes.AppDockerTag).ToNot(gomega.BeEmpty(), "app-docker-tag is empty")
	g.Expect(kubernetes.AppDockerPort).ToNot(gomega.BeZero(), "app-docker-port is zero")
}

func ValidateInput(g gomega.Gomega, input []Input) {
	for _, i := range input {
		g.Expect(i.Path).ToNot(gomega.BeEmpty(), "input path is empty")
		if i.Status != "" {
			_, err := strconv.ParseInt(i.Status, 10, 32)
			g.Expect(err).To(gomega.BeNil(), "status must parse as integer or be empty")
		}
		if i.Method != "" {
			g.Expect(strings.ToUpper(i.Method)).To(gomega.Or(
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
			g.Expect(i.Body).To(gomega.BeEmpty(), "body must be empty for GET requests")
		}
		if i.Scheme != "" {
			g.Expect(strings.ToLower(i.Scheme)).To(gomega.Or(
				gomega.Equal("http"),
				gomega.Equal("https"),
			), "scheme must be http, https or be empty")
		}
	}
}

func validateDockerCompose(d *DockerCompose, dir string, g gomega.Gomega) {
	if len(d.Files) > 0 {
		for i, filename := range d.Files {
			d.Files[i] = filepath.Join(dir, filename)
			g.Expect(d.Files[i]).To(gomega.BeARegularFile())
		}
	}
}
