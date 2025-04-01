package yaml

import (
	"context"
	"fmt"
	"github.com/grafana/oats/testhelpers/kubernetes"
	"github.com/grafana/oats/testhelpers/remote"
	"github.com/onsi/gomega/format"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/grafana/oats/testhelpers/compose"
	"github.com/grafana/oats/testhelpers/requests"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

type runner struct {
	testCase          *TestCase
	endpoint          *remote.Endpoint
	deadline          time.Time
	queryLogger       QueryLogger
	gomegaInst        gomega.Gomega
	additionalAsserts []func()
}

var VerboseLogging bool

func RunTestCase(c *TestCase) {
	format.MaxLength = 100000
	r := &runner{
		testCase: c,
	}

	ginkgo.BeforeAll(func() {
		c.OutputDir = prepareBuildDir(c.Name)
		c.validateAndSetVariables()
		logger, err := createLogger(c)
		gomega.Expect(err).ToNot(gomega.HaveOccurred(), "expected no error creating logger")
		r.queryLogger = NewQueryLogger(r.endpoint, logger)

		endpoint, err := startEndpoint(c, logger)
		gomega.Expect(err).ToNot(gomega.HaveOccurred(), "expected no error starting a observability endpoint")

		r.deadline = time.Now().Add(c.Timeout)
		r.endpoint = endpoint
		if os.Getenv("TESTCASE_MANUAL_DEBUG") == "true" {
			ginkgo.GinkgoWriter.Printf("stopping to let you manually debug on http://localhost:%d\n", r.testCase.PortConfig.GrafanaHTTPPort)

			for {
				r.eventually(func() {
					// do nothing - just feed input into the application
				})
				time.Sleep(1 * time.Second)
			}
		}

		ginkgo.GinkgoWriter.Printf("deadline = %v\n", r.deadline)
	})

	ginkgo.AfterAll(func() {
		var ctx = context.Background()
		var stopErr error

		if r.endpoint != nil {
			stopErr = r.endpoint.Stop(ctx)
			gomega.Expect(stopErr).ToNot(gomega.HaveOccurred(), "expected no error stopping the local observability endpoint")
		}
	})

	expected := c.Definition.Expected
	for _, composeLog := range expected.ComposeLogs {
		ginkgo.It(fmt.Sprintf("should have '%s' in internal compose logs", composeLog), func() {
			r.eventually(func() {
				found, err := r.endpoint.SearchComposeLogs(composeLog)
				r.gomegaInst.Expect(err).ToNot(gomega.HaveOccurred())
				r.gomegaInst.Expect(found).To(gomega.BeTrue())
			})
		})
	}

	// Assert logs traces first, because metrics and dashboards can take longer to appear
	// (depending on OTEL_METRIC_EXPORT_INTERVAL).
	for _, log := range expected.Logs {
		l := log
		ginkgo.It(fmt.Sprintf("should have '%s' in loki", l.LogQL), func() {
			r.eventually(func() {
				AssertLoki(r, l)
			})
		})
	}
	for _, trace := range expected.Traces {
		t := trace
		ginkgo.It(fmt.Sprintf("should have '%s' in tempo", t.TraceQL), func() {
			r.eventually(func() {
				AssertTempo(r, t)
			})
		})
	}
	for _, dashboard := range expected.Dashboards {
		dashboardAssert := NewDashboardAssert(dashboard)
		for i, panel := range dashboard.Panels {
			iCopy := i
			p := panel
			ginkgo.It(fmt.Sprintf("dashboard panel '%s'", p.Title), func() {
				r.eventually(func() {
					dashboardAssert.AssertDashboard(r, iCopy)
				})
			})
		}
	}
	for _, metric := range expected.Metrics {
		m := metric
		ginkgo.It(fmt.Sprintf("should have '%s' in prometheus", m.PromQL), func() {
			r.eventually(func() {
				AssertProm(r, m.PromQL, m.Value)
			})
		})
	}
	for _, customCheck := range expected.CustomChecks {
		c := customCheck
		ginkgo.It(fmt.Sprintf("custom check '%s'", c.Script), func() {
			r.eventually(func() {
				assertCustomCheck(r, c)
			})
		})
	}
}

func assertCustomCheck(r *runner, c CustomCheck) {
	r.queryLogger.LogQueryResult("running custom check %v\n", c.Script)
	cmd := exec.Command(c.Script)
	cmd.Dir = r.testCase.Dir
	cmd.Stdout = r.queryLogger.Logger
	cmd.Stderr = r.queryLogger.Logger

	err := cmd.Run()
	r.queryLogger.LogQueryResult("custom check %v response %v err=%v\n", c.Script, "", err)
	r.gomegaInst.Expect(err).ToNot(gomega.HaveOccurred())
}

func startEndpoint(c *TestCase, logger io.WriteCloser) (*remote.Endpoint, error) {
	ports := remote.PortsConfig{
		PrometheusHTTPPort: c.PortConfig.PrometheusHTTPPort,
		TempoHTTPPort:      c.PortConfig.TempoHTTPPort,
		LokiHttpPort:       c.PortConfig.LokiHTTPPort,
	}

	ginkgo.GinkgoWriter.Printf("Launching test for %s\n", c.Name)
	var endpoint *remote.Endpoint
	if c.Definition.Kubernetes != nil {
		endpoint = kubernetes.NewEndpoint(c.Definition.Kubernetes, ports, logger, c.Name, c.Dir)
	} else {
		endpoint = compose.NewEndpoint(c.CreateDockerComposeFile(), ports, logger)
	}

	var ctx = context.Background()
	startErr := endpoint.Start(ctx)
	return endpoint, startErr
}

func createLogger(c *TestCase) (io.WriteCloser, error) {
	logFile := filepath.Join(c.OutputDir, fmt.Sprintf("output-%s.log", c.Name))
	logs, err := os.OpenFile(logFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
	if err != nil {
		return nil, err
	}
	abs, _ := filepath.Abs(logFile)
	println("Logging to", abs)
	return logs, nil
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
	if r.deadline.Before(time.Now()) {
		ginkgo.Fail("deadline exceeded waiting for telemetry")
	}
	t := time.Now()
	ctx := context.Background()
	interval := r.testCase.Definition.Interval
	if interval == 0 {
		interval = DefaultTestCaseInterval
	}
	iterations := 0
	r.additionalAsserts = nil
	gomega.Eventually(ctx, func(g gomega.Gomega) {
		iterations++
		verbose := VerboseLogging
		if time.Since(t) > 10*time.Second {
			verbose = true
			t = time.Now()
		}
		r.queryLogger.Verbose = verbose
		r.queryLogger.LogQueryResult("waiting for telemetry data\n")

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
	ginkgo.GinkgoWriter.Println(iterations, "iterations to get telemetry data")
	for _, a := range r.additionalAsserts {
		a()
	}
}
