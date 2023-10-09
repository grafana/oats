package yaml

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/grafana/oats/testhelpers/compose"
	"github.com/grafana/oats/testhelpers/requests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type runner struct {
	testCase *TestCase
	endpoint *compose.ComposeEndpoint
	deadline time.Time
}

func RunTestCase(c *TestCase) {
	r := &runner{
		testCase: c,
	}

	BeforeAll(func() {
		c.OutputDir = prepareBuildDir(c.Name)
		c.validateAndSetVariables()
		endpoint := c.startEndpoint()

		r.deadline = time.Now().Add(c.Timeout)
		r.endpoint = endpoint
		if os.Getenv("TESTCASE_MANUAL_DEBUG") == "true" {
			GinkgoWriter.Printf("stopping to let you manually debug on http://localhost:%d\n", r.testCase.PortConfig.GrafanaHTTPPort)

			for {
				r.eventually(func(g Gomega, queryLogger QueryLogger) {
					// do nothing - just feed input into the application
				})
				time.Sleep(1 * time.Second)
			}
		}

		GinkgoWriter.Printf("deadline = %v\n", r.deadline)
	})

	AfterAll(func() {
		var ctx = context.Background()
		var stopErr error

		if r.endpoint != nil {
			stopErr = r.endpoint.Stop(ctx)
			Expect(stopErr).ToNot(HaveOccurred(), "expected no error stopping the local observability endpoint")
		}
	})

	expected := c.Definition.Expected
	// Assert logs traces first, because metrics and dashboards can take longer to appear
	// (depending on OTEL_METRIC_EXPORT_INTERVAL).
	for _, log := range expected.Logs {
		It(fmt.Sprintf("should have '%s' in loki", log.LogQL), func() {
			r.eventually(func(g Gomega, queryLogger QueryLogger) {
				AssertLoki(g, r.endpoint, queryLogger, log.LogQL, log.Contains)
			})
		})
	}
	for _, trace := range expected.Traces {
		It(fmt.Sprintf("should have '%s' in tempo", trace.TraceQL), func() {
			r.eventually(func(g Gomega, queryLogger QueryLogger) {
				AssertTempo(g, r.endpoint, queryLogger, trace.TraceQL, trace.Spans)
			})
		})
	}
	for _, dashboard := range expected.Dashboards {
		dashboardAssert := NewDashboardAssert(dashboard)
		for i, panel := range dashboard.Panels {
			It(fmt.Sprintf("dashboard panel '%s'", panel.Title), func() {
				r.eventually(func(g Gomega, queryLogger QueryLogger) {
					dashboardAssert.AssertDashboard(g, r.endpoint, queryLogger, i, &c.Dashboard.Content)
				})
			})
		}
	}
	for _, metric := range expected.Metrics {
		It(fmt.Sprintf("should have '%s' in prometheus", metric.PromQL), func() {
			r.eventually(func(g Gomega, queryLogger QueryLogger) {
				AssertProm(g, r.endpoint, queryLogger, metric.PromQL, metric.Value)
			})
		})
	}
}

func (c *TestCase) startEndpoint() *compose.ComposeEndpoint {
	var ctx = context.Background()

	endpoint := compose.NewEndpoint(
		c.CreateDockerComposeFile(),
		filepath.Join(c.OutputDir, "output.log"),
		[]string{},
		compose.PortsConfig{
			PrometheusHTTPPort: c.PortConfig.PrometheusHTTPPort,
			TempoHTTPPort:      c.PortConfig.TempoHTTPPort,
			LokiHttpPort:       c.PortConfig.LokiHTTPPort,
		},
	)
	startErr := endpoint.Start(ctx)
	Expect(startErr).ToNot(HaveOccurred(), "expected no error starting a local observability endpoint")
	return endpoint
}

func prepareBuildDir(name string) string {
	dir := filepath.Join(".", "build", name)

	fileinfo, err := os.Stat(dir)
	if err == nil {
		if fileinfo.IsDir() {
			err := os.RemoveAll(dir)
			Expect(err).ToNot(HaveOccurred(), "expected no error removing output directory")
		}
	}
	err = os.MkdirAll(dir, 0755)
	Expect(err).ToNot(HaveOccurred(), "expected no error creating output directory")
	return dir
}

func (r *runner) eventually(asserter func(g Gomega, queryLogger QueryLogger)) {
	if r.deadline.Before(time.Now()) {
		Fail("deadline exceeded waiting for telemetry")
	}
	t := time.Now()
	ctx := context.Background()
	interval := r.testCase.Definition.Interval
	if interval == 0 {
		interval = DefaultTestCaseInterval
	}
	iterations := 0
	Eventually(ctx, func(g Gomega) {
		iterations++
		verbose := false
		if time.Since(t) > 10*time.Second {
			verbose = true
			t = time.Now()
		}
		queryLogger := NewQueryLogger(r.endpoint, verbose)
		queryLogger.LogQueryResult("waiting for telemetry data\n")

		for _, i := range r.testCase.Definition.Input {
			url := fmt.Sprintf("http://localhost:%d%s", r.testCase.PortConfig.ApplicationPort, i.Path)
			err := requests.DoHTTPGet(url, 200)
			g.Expect(err).ToNot(HaveOccurred(), "expected no error calling application endpoint %s", url)
		}

		asserter(g, queryLogger)
	}).WithTimeout(r.deadline.Sub(time.Now())).WithPolling(interval).Should(Succeed(), "calling application for %v should cause telemetry to appear", r.testCase.Timeout)
	GinkgoWriter.Println(iterations, "iterations to get telemetry data")
}
