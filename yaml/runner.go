package yaml

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/oats/model"
	"github.com/grafana/oats/testhelpers/compose"
	"github.com/grafana/oats/testhelpers/kubernetes"
	"github.com/grafana/oats/testhelpers/remote"
	"github.com/grafana/oats/testhelpers/requests"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

type Runner struct {
	testCase   *model.TestCase
	endpoint   *remote.Endpoint
	deadline   time.Time
	host       string
	Verbose    bool
	gomegaInst gomega.Gomega
	Settings   model.Settings
}

var VerboseLogging bool

func RunTestCase(c *model.TestCase, s model.Settings) {
	slog.Info("start test", "name", c.Name)

	format.MaxLength = 100000
	r := NewRunner(c, s)

	c.OutputDir = PrepareBuildDir(c.Name)
	c.ValidateAndSetVariables(gomega.Default)
	end, err := r.StartEndpoint()
	gomega.Expect(err).ToNot(gomega.HaveOccurred(), "expected no error starting an observability endpoint")
	defer end()

	if s.ManualDebug {
		slog.Info(fmt.Sprintf("stopping to let you manually debug on http://%s:%d\n", r.host, r.testCase.PortConfig.GrafanaHTTPPort))

		for {
			r.eventually(func() {
				// do nothing - just feed input into the application
			})
			time.Sleep(1 * time.Second)
		}
	}

	slog.Info("deadline", "time", r.deadline)

	r.ExecuteChecks()
}

// NewRunner creates a partially initialized Runner.
func NewRunner(c *model.TestCase, s model.Settings) *Runner {
	r := &Runner{
		host:     s.Host,
		testCase: c,
		Settings: s,
	}
	return r
}

func (r *Runner) ExecuteChecks() {
	c := r.testCase
	r.deadline = time.Now().Add(r.Settings.Timeout)
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
		r.assertSignal(log.Signal, log.LogQL,
			func() {
				slog.Info("searching loki", "logql", log.LogQL)
			},
			func() {
				AssertLoki(r, log)
			})
	}
	for _, trace := range expected.Traces {
		r.assertSignal(trace.Signal, trace.TraceQL,
			func() {
				slog.Info("searching tempo", "traceql", trace.TraceQL)
			},
			func() {
				AssertTempo(r, trace)
			})
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
				r.assertCustomCheck(customCheck)
			})
		}
	}
}

func (r *Runner) assertCustomCheck(c model.CustomCheck) {
	r.LogQueryResult("running custom check %v\n", c.Script)
	cmd := exec.Command(c.Script)
	cmd.Dir = r.testCase.Dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	r.LogQueryResult("custom check %v response %v err=%v\n", c.Script, "", err)
	r.gomegaInst.Expect(err).ToNot(gomega.HaveOccurred())
}

func (r *Runner) StartEndpoint() (end func(), err error) {
	c := r.testCase
	host := r.host
	ports := remote.PortsConfig{
		PrometheusHTTPPort: c.PortConfig.PrometheusHTTPPort,
		TempoHTTPPort:      c.PortConfig.TempoHTTPPort,
		LokiHttpPort:       c.PortConfig.LokiHTTPPort,
		PyroscopeHttpPort:  c.PortConfig.PyroscopeHttpPort,
	}

	if c.Definition.Kubernetes != nil {
		r.endpoint = kubernetes.NewEndpoint(host, c.Definition.Kubernetes, ports, c.Name, c.Dir)
	} else {
		r.endpoint = compose.NewEndpoint(host, CreateDockerComposeFile(r), ports)
	}

	var ctx = context.Background()
	startErr := r.endpoint.Start(ctx)
	if startErr != nil {
		return nil, fmt.Errorf("error starting local observability endpoint: %w", startErr)
	}

	end = func() {
		slog.Info("stopping observability endpoint")

		var ctx = context.Background()

		stopErr := r.endpoint.Stop(ctx)
		gomega.Expect(stopErr).ToNot(gomega.HaveOccurred(), "expected no error stopping the local observability endpoint")
		slog.Info("stopped observability endpoint")
	}
	return end, nil
}

func PrepareBuildDir(name string) string {
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

func (r *Runner) assertSignal(signal model.ExpectedSignal, query string, startLog func(), asserter func()) {
	if r.MatchesMatrixCondition(signal.MatrixCondition, query) {
		startLog()
		if signal.ExpectAbsent() {
			r.consistently(asserter)
		} else {
			r.eventually(asserter)
		}
	}
}

func (r *Runner) eventually(asserter func()) {
	r.assertDeadline()
	timeout := time.Until(r.deadline)
	if r.Settings.PresentTimeout > 0 {
		timeout = r.Settings.PresentTimeout
	}
	r.eventuallyWithTimeout(asserter, timeout)
}

func (r *Runner) assertDeadline() bool {
	return gomega.Expect(time.Now()).Should(gomega.BeTemporally("<", r.deadline))
}

func (r *Runner) eventuallyWithTimeout(asserter func(), timeout time.Duration) {
	caller := newAssertCaller(r, timeout)
	gomega.Eventually(context.Background(), func(g gomega.Gomega) {
		r.callAsserter(g, caller, asserter)
	}).WithTimeout(timeout).WithPolling(caller.interval).Should(
		gomega.Succeed(),
		"calling application for %s should cause telemetry to appear within %v",
		r.testCase.Name,
		timeout,
	)
	slog.Info(fmt.Sprintf("time to get telemetry data: %v", time.Since(caller.start)))
}

func (r *Runner) consistently(asserter func()) {
	r.assertDeadline()
	timeout := r.Settings.AbsentTimeout
	caller := newAssertCaller(r, timeout)
	gomega.Consistently(context.Background(), func(g gomega.Gomega) {
		r.callAsserter(g, caller, asserter)
		// For absence checks, the asserter function should succeed (return no error) when the signal is absent.
		// We use Should(gomega.Succeed()) to assert that the asserter consistently validates the absence.
	}).WithTimeout(timeout).WithPolling(caller.interval).Should(gomega.Succeed(), "assertion should succeed for %v", timeout)
}

type asserterCaller struct {
	iterations int
	timeout    time.Duration
	start      time.Time
	printTime  time.Time
	interval   time.Duration
}

func newAssertCaller(r *Runner, timeout time.Duration) *asserterCaller {
	interval := r.testCase.Definition.Interval
	if interval == 0 {
		interval = model.DefaultTestCaseInterval
	}

	now := time.Now()
	return &asserterCaller{
		iterations: 0,
		timeout:    timeout,
		start:      now,
		printTime:  now,
		interval:   interval,
	}
}

func (r *Runner) callAsserter(g gomega.Gomega, caller *asserterCaller, asserter func()) {
	verbose := VerboseLogging
	if caller.iterations == 0 || time.Since(caller.printTime) > 10*time.Second {
		verbose = true
		caller.printTime = time.Now()
	}
	caller.iterations++
	r.Verbose = verbose
	r.LogQueryResult("waiting for telemetry data (timeout %v)\n", caller.timeout)

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
		r.LogQueryResult("Making HTTP request: %s %s\n", method, url)
		err := requests.DoHTTPRequest(url, method, headers, body, status)
		g.Expect(err).ToNot(gomega.HaveOccurred(), "expected no error calling application endpoint %s", url)
	}

	r.gomegaInst = g
	asserter()
}

func (r *Runner) MatchesMatrixCondition(matrixCondition string, subject string) bool {
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

func (r *Runner) LogQueryResult(format string, a ...any) {
	if r.Verbose {
		limit := r.Settings.LogLimit
		result := fmt.Sprintf(format, a...)
		if len(result) > limit {
			result = result[:limit] + ".."
		}
		slog.Info(result)
	}
}
