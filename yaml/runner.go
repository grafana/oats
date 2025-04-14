package yaml

import (
	"context"
	"fmt"
	"github.com/grafana/oats/testhelpers/kubernetes"
	"github.com/grafana/oats/testhelpers/remote"
	"github.com/onsi/gomega/format"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/grafana/oats/testhelpers/compose"
	"github.com/grafana/oats/testhelpers/requests"
	"github.com/onsi/gomega"
)

type runner struct {
	testCase          *TestCase
	endpoint          *remote.Endpoint
	deadline          time.Time
	Verbose           bool
	gomegaInst        gomega.Gomega
	additionalAsserts []func()
}

var VerboseLogging bool

func RunTestCase(c *TestCase) {
	format.MaxLength = 100000
	r := &runner{
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
		var ctx = context.Background()
		var stopErr error

		if r.endpoint != nil {
			stopErr = r.endpoint.Stop(ctx)
			gomega.Expect(stopErr).ToNot(gomega.HaveOccurred(), "expected no error stopping the local observability endpoint")
		}
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

	// Assert logs traces first, because metrics and dashboards can take longer to appear
	// (depending on OTEL_METRIC_EXPORT_INTERVAL).
	for _, log := range expected.Logs {
		slog.Info("searching loki", "logql", log.LogQL)
		r.eventually(func() {
			AssertLoki(r, log)
		})
	}
	for _, trace := range expected.Traces {
		slog.Info("searching tempo", "traceql", trace.TraceQL)
		r.eventually(func() {
			AssertTempo(r, trace)
		})
	}
	for _, metric := range expected.Metrics {
		slog.Info("searching prometheus", "promql", metric.PromQL)
		r.eventually(func() {
			AssertProm(r, metric.PromQL, metric.Value)
		})
	}
	for _, profile := range expected.Profiles {
		slog.Info("searching pyroscope", "query", profile.Query)
		r.eventually(func() {
			AssertPyroscope(r, profile)
		})
	}
	for _, customCheck := range expected.CustomChecks {
		slog.Info("executing custom check", "check", customCheck.Script)
		r.eventually(func() {
			assertCustomCheck(r, customCheck)
		})
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

	slog.Info("Launching test", "name", c.Name)
	var endpoint *remote.Endpoint
	if c.Definition.Kubernetes != nil {
		endpoint = kubernetes.NewEndpoint(c.Definition.Kubernetes, ports, c.Name, c.Dir)
	} else {
		endpoint = compose.NewEndpoint(c.CreateDockerComposeFile(), ports)
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
	t := time.Now()
	ctx := context.Background()
	interval := r.testCase.Definition.Interval
	if interval == 0 {
		interval = DefaultTestCaseInterval
	}
	iterations := 0
	r.additionalAsserts = nil
	gomega.Eventually(ctx, func(g gomega.Gomega) {
		verbose := VerboseLogging
		if iterations == 0 || time.Since(t) > 10*time.Second {
			verbose = true
			t = time.Now()
		}
		iterations++
		r.Verbose = verbose
		r.LogQueryResult("waiting for telemetry data\n")

		for _, i := range r.testCase.Definition.Input {
			url := fmt.Sprintf("http://localhost:%d%s", r.testCase.PortConfig.ApplicationPort, i.Path)
			status := 200
			if i.Status != "" {
				parsedStatus, err := strconv.ParseInt(i.Status, 10, 64)
				if err == nil {
					status = int(parsedStatus)
				}
			}
			err := requests.DoHTTPGet(url, status)
			g.Expect(err).ToNot(gomega.HaveOccurred(), "expected no error calling application endpoint %s", url)
		}

		r.gomegaInst = g
		asserter()
	}).WithTimeout(time.Until(r.deadline)).WithPolling(interval).Should(gomega.Succeed(), "calling application for %v should cause telemetry to appear", r.testCase.Timeout)
	slog.Info(fmt.Sprintf("%d iterations to get telemetry data", iterations))
	for _, a := range r.additionalAsserts {
		a()
	}
}
