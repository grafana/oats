package yaml_test

import (
	"context"
	"fmt"
	"github.com/grafana/oats/yaml"
	"os"
	"os/exec"
	"path"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/grafana/oats/internal/testhelpers/compose"
	"github.com/grafana/oats/internal/testhelpers/requests"
)

var _ = Describe("test case", Ordered, ContinueOnFailure, Label("docker", "integration", "slow"), func() {
	Describe("yaml test case", func() {
		cases, base := yaml.ReadTestCases()
		if base != "" {
			It("should have at least one test case", func() {
				Expect(cases).ToNot(BeEmpty(), "expected at least one test case in %s", base)
			})
		}

		for _, c := range cases {
			Describe(c.Name, func() {
				runTestCase(c)
			})
		}
	})
})

type runner struct {
	testCase *yaml.TestCase
	endpoint *compose.ComposeEndpoint
	deadline time.Time
}

func runTestCase(c yaml.TestCase) {
	r := &runner{
		testCase: &c,
	}

	BeforeAll(func() {
		c.OutputDir = prepareBuildDir(c.Name)
		c.ValidateAndSetDashboard()
		endpoint := startEndpoint(c)

		r.deadline = time.Now().Add(c.Timeout)
		r.endpoint = endpoint
		_, _ = fmt.Printf("deadline = %v\n", r.deadline)
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
	for _, dashboard := range expected.Dashboards {
		dashboardAssert := yaml.NewDashboardAssert(dashboard)
		for i, panel := range dashboard.Panels {
			It(fmt.Sprintf("dashboard panel '%s'", panel.Title), func() {
				r.eventually(func(g Gomega, queryLogger yaml.QueryLogger) {
					dashboardAssert.AssertDashboard(g, r.endpoint, queryLogger, i, &c.Dashboard.Content)
				})
			})
		}
	}
	for _, metric := range expected.Metrics {
		It(fmt.Sprintf("should have '%s' in prometheus", metric.PromQL), func() {
			r.eventually(func(g Gomega, queryLogger yaml.QueryLogger) {
				yaml.AssertProm(g, r.endpoint, queryLogger, metric.PromQL, metric.Value)
			})
		})
	}
	for _, trace := range expected.Traces {
		It(fmt.Sprintf("should have '%s' in tempo", trace.TraceQL), func() {
			r.eventually(func(g Gomega, queryLogger yaml.QueryLogger) {
				yaml.AssertTempo(g, r.endpoint, queryLogger, trace.TraceQL, trace.Spans)
			})
		})
	}
}

func startEndpoint(c yaml.TestCase) *compose.ComposeEndpoint {
	var ctx = context.Background()
	var startErr error

	endpoint := compose.NewEndpoint(
		c.CreateDockerComposeFile(),
		path.Join(c.OutputDir, "output.log"),
		[]string{},
		compose.PortsConfig{
			PrometheusHTTPPort: 9090,
			TempoHTTPPort:      3200,
		},
	)
	startErr = endpoint.Start(ctx)
	Expect(startErr).ToNot(HaveOccurred(), "expected no error starting a local observability endpoint")
	return endpoint
}

func prepareBuildDir(name string) string {
	dir := path.Join(".", "build", name)

	fileinfo, err := os.Stat(dir)
	if err == nil {
		if fileinfo.IsDir() {
			err := os.RemoveAll(dir)
			Expect(err).ToNot(HaveOccurred(), "expected no error removing output directory")
		}
	}
	err = os.MkdirAll(dir, 0755)
	Expect(err).ToNot(HaveOccurred(), "expected no error creating output directory")
	err = exec.Command("cp", "-r", "configs", dir).Run()
	Expect(err).ToNot(HaveOccurred(), "expected no error copying configs directory")
	return dir
}

func (r *runner) eventually(asserter func(g Gomega, queryLogger yaml.QueryLogger)) {
	if r.deadline.Before(time.Now()) {
		Fail("deadline exceeded waiting for telemetry")
	}
	t := time.Now()
	ctx := context.Background()

	Eventually(ctx, func(g Gomega) {
		verbose := false
		if time.Since(t) > 10*time.Second {
			verbose = true
			t = time.Now()
		}
		queryLogger := yaml.NewQueryLogger(r.endpoint, verbose)

		queryLogger.LogQueryResult("waiting for telemetry data\n")

		for _, i := range r.testCase.Definition.Input {
			err := requests.DoHTTPGet(i.Url, 200)
			g.Expect(err).ToNot(HaveOccurred(), "expected no error calling application endpoint %s", i.Url)
		}

		asserter(g, queryLogger)
	}).WithTimeout(r.deadline.Sub(time.Now())).Should(Succeed(), "calling application for %v should cause telemetry to appear", r.testCase.Timeout)
}
