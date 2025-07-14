package yaml

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/oats/testhelpers/kubernetes"
	"github.com/grafana/oats/testhelpers/remote"
	"github.com/onsi/gomega/format"

	"maps"

	"github.com/grafana/oats/testhelpers/compose"
	"github.com/grafana/oats/testhelpers/requests"
	"github.com/onsi/gomega"
)

type runner struct {
	testCase          *TestCase
	endpoint          *remote.Endpoint
	deadline          time.Time
	host              string
	Verbose           bool
	gomegaInst        gomega.Gomega
	additionalAsserts []func()
}

var VerboseLogging bool

func RunTestCase(c *TestCase) {
	format.MaxLength = 100000
	r := &runner{
		host:     c.Host,
		testCase: c,
	}

	c.OutputDir = prepareBuildDir(c.Name)
	c.validateAndSetVariables()
	endpoint, err := startEndpoint(c)
	gomega.Expect(err).ToNot(gomega.HaveOccurred(), "expected no error starting an observability endpoint")

	r.deadline = time.Now().Add(c.Timeout)
	r.endpoint = endpoint
	if c.ManualDebug {
		slog.Info(fmt.Sprintf("topping to let you manually debug on http://localhost:%d\n", r.testCase.PortConfig.GrafanaHTTPPort))

		for {
			r.eventually(func() {
				// do nothing - just feed input into the application
			})
			time.Sleep(1 * time.Second)
		}
	}

	slog.Info("deadline", "time", r.deadline)

	defer func() {
		slog.Info("stopping observability endpoint")

		var ctx = context.Background()

		stopErr := r.endpoint.Stop(ctx)
		gomega.Expect(stopErr).ToNot(gomega.HaveOccurred(), "expected no error stopping the local observability endpoint")
		slog.Info("stopped observability endpoint")
	}()

	expected := c.Definition.Expected
	for _, composeLog := range expected.ComposeLogs {
		slog.Info("searching for compose log", "log", composeLog)
		r.eventually(func() {
			found, err := r.endpoint.SearchComposeLogs(composeLog)
			r.gomegaInst.Expect(err).ToNot(gomega.HaveOccurred())
			r.gomegaInst.Expect(found).To(gomega.BeTrue())
		})
	}

	// Assert logs traces first, because metrics can take longer to appear
	// (depending on OTEL_METRIC_EXPORT_INTERVAL).
	for _, log := range expected.Logs {
		if r.MatchesMatrixCondition(log.MatrixCondition, log.LogQL) {
			slog.Info("searching loki", "logql", log.LogQL)
			r.eventually(func() {
				AssertLoki(r, log)
			})
		}
	}
	for _, trace := range expected.Traces {
		if r.MatchesMatrixCondition(trace.MatrixCondition, trace.TraceQL) {
			slog.Info("searching tempo", "traceql", trace.TraceQL)
			r.eventually(func() {
				AssertTempo(r, trace)
			})
		}
	}
	for _, metric := range expected.Metrics {
		if r.MatchesMatrixCondition(metric.MatrixCondition, metric.PromQL) {
			slog.Info("searching prometheus", "promql", metric.PromQL)
			r.eventually(func() {
				AssertProm(r, metric.PromQL, metric.Value)
			})
		}
	}
	for _, profile := range expected.Profiles {
		if r.MatchesMatrixCondition(profile.MatrixCondition, profile.Query) {
			slog.Info("searching pyroscope", "query", profile.Query)
			r.eventually(func() {
				AssertPyroscope(r, profile)
			})
		}
	}
	for _, customCheck := range expected.CustomChecks {
		if r.MatchesMatrixCondition(customCheck.MatrixCondition, customCheck.Script) {
			slog.Info("executing custom check", "check", customCheck.Script)
			r.eventually(func() {
				assertCustomCheck(r, customCheck)
			})
		}
	}
}

func assertCustomCheck(r *runner, c CustomCheck) {
	r.LogQueryResult("running custom check %v\n", c.Script)
	cmd := exec.Command(c.Script)
	cmd.Dir = r.testCase.Dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	r.LogQueryResult("custom check %v response %v err=%v\n", c.Script, "", err)
	r.gomegaInst.Expect(err).ToNot(gomega.HaveOccurred())
}

func startEndpoint(c *TestCase) (*remote.Endpoint, error) {
	ports := remote.PortsConfig{
		PrometheusHTTPPort: c.PortConfig.PrometheusHTTPPort,
		TempoHTTPPort:      c.PortConfig.TempoHTTPPort,
		LokiHttpPort:       c.PortConfig.LokiHTTPPort,
		PyroscopeHttpPort:  c.PortConfig.PyroscopeHttpPort,
	}

	slog.Info("start test", "name", c.Name)
	var endpoint *remote.Endpoint
	if c.Definition.Kubernetes != nil {
		endpoint = kubernetes.NewEndpoint(c.Host, c.Definition.Kubernetes, ports, c.Name, c.Dir)
	} else {
		endpoint = compose.NewEndpoint(c.Host, c.CreateDockerComposeFile(), ports)
	}

	var ctx = context.Background()
	startErr := endpoint.Start(ctx)
	return endpoint, startErr
}

func prepareBuildDir(name string) string {
	dir := filepath.Join(".", "build", name)

	fileinfo, err := os.Stat(dir)
	if err == nil {
		if fileinfo.IsDir() {
			err := os.RemoveAll(dir)
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), "expected no error removing output directory")
		}
	}
	err = os.MkdirAll(dir, 0755)
	gomega.Expect(err).ToNot(gomega.HaveOccurred(), "expected no error creating output directory")
	return dir
}

func (r *runner) eventually(asserter func()) {
	gomega.Expect(time.Now()).Should(gomega.BeTemporally("<", r.deadline))
	start := time.Now()
	printTime := start
	ctx := context.Background()
	interval := r.testCase.Definition.Interval
	if interval == 0 {
		interval = DefaultTestCaseInterval
	}
	iterations := 0
	r.additionalAsserts = nil
	gomega.Eventually(ctx, func(g gomega.Gomega) {
		verbose := VerboseLogging
		if iterations == 0 || time.Since(printTime) > 10*time.Second {
			verbose = true
			printTime = time.Now()
		}
		iterations++
		r.Verbose = verbose
		r.LogQueryResult("waiting for telemetry data\n")

		for _, i := range r.testCase.Definition.Input {
			scheme := "http"
			if i.Scheme != "" {
				scheme = i.Scheme
			}
			host := r.host
			if i.Host != "" {
				host = i.Host
			}
			url := fmt.Sprintf("%s://%s:%d%s", scheme, host, r.testCase.PortConfig.ApplicationPort, i.Path)
			body := i.Body
			method := http.MethodGet
			if i.Method != "" {
				method = strings.ToUpper(i.Method)
			}
			status := 200
			if i.Status != "" {
				parsedStatus, err := strconv.ParseInt(i.Status, 10, 64)
				if err == nil {
					status = int(parsedStatus)
				}
			}
			headers := make(map[string]string)
			if i.Headers != nil {
				maps.Copy(headers, i.Headers)
			} else {
				headers["Accept"] = "application/json"
			}
			err := requests.DoHTTPRequest(url, method, headers, body, status)
			g.Expect(err).ToNot(gomega.HaveOccurred(), "expected no error calling application endpoint %s", url)
		}

		r.gomegaInst = g
		asserter()
	}).WithTimeout(time.Until(r.deadline)).WithPolling(interval).Should(gomega.Succeed(), "calling application for %v should cause telemetry to appear", r.testCase.Timeout)
	slog.Info(fmt.Sprintf("time to get telemetry data: %v", time.Since(start)))
	for _, a := range r.additionalAsserts {
		a()
	}
}

func (r *runner) MatchesMatrixCondition(matrixCondition string, subject string) bool {
	if matrixCondition == "" {
		return true
	}
	name := r.testCase.MatrixTestCaseName
	if name == "" {
		slog.Info("matrix condition ignored we're not in a matrix test", "condition", matrixCondition)
		return true
	}
	if regexp.MustCompile(matrixCondition).MatchString(name) {
		return true
	}
	slog.Info("matrix condition not matched - ignoring assertion",
		"test case", r.testCase.Name,
		"name", name,
		"subject", subject)
	return false
}
