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
		for _, c := range yaml.ReadTestCases() {
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
		c.ValidateAndSetDashboard()
		var ctx = context.Background()
		var startErr error

		c.OutputDir = path.Join(".", "build", c.Name)
		err := os.MkdirAll(c.OutputDir, 0755)
		Expect(err).ToNot(HaveOccurred(), "expected no error creating output directory")
		err = exec.Command("cp", "-r", "configs", c.OutputDir).Run()
		Expect(err).ToNot(HaveOccurred(), "expected no error copying configs directory")

		r.endpoint = compose.NewEndpoint(
			c.GetDockerComposeFile(),
			path.Join(c.OutputDir, "output.log"),
			[]string{},
			compose.PortsConfig{PrometheusHTTPPort: 9090},
		)
		startErr = r.endpoint.Start(ctx)
		Expect(startErr).ToNot(HaveOccurred(), "expected no error starting a local observability endpoint")

		r.deadline = time.Now().Add(c.Timeout)
		_, _ = fmt.Fprintf(r.endpoint.Logger(), "deadline = %v\n", r.deadline)
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
				r.eventually(func(g Gomega, verbose bool) {
					dashboardAssert.AssertDashboard(g, r.endpoint, verbose, i, &c.Dashboard.Content)
				})
			})
		}
	}
	for _, metric := range expected.Metrics {
		It(fmt.Sprintf("should have '%s' in prometheus", metric.PromQL), func() {
			r.eventually(func(g Gomega, verbose bool) {
				yaml.AssertProm(g, r.endpoint, verbose, metric.PromQL, metric.Value)
			})
		})
	}
}

func (r *runner) eventually(asserter func(g Gomega, verbose bool)) {
	if r.deadline.Before(time.Now()) {
		Fail("deadline exceeded waiting for telemetry")
	}
	t := time.Now()
	ctx := context.Background()
	logger := r.endpoint.Logger()

	Eventually(ctx, func(g Gomega) {
		verbose := false
		if time.Since(t) > 10*time.Second {
			verbose = true
			t = time.Now()
		}

		if verbose {
			_, _ = fmt.Fprintf(logger, "waiting for telemetry data\n")
		}

		for _, i := range r.testCase.Definition.Input {
			err := requests.DoHTTPGet(i.Url, 200)
			g.Expect(err).ToNot(HaveOccurred())
		}

		asserter(g, verbose)
	}).WithTimeout(r.deadline.Sub(time.Now())).Should(Succeed(), "calling application for %v should cause telemetry to appear", r.testCase.Timeout)
}
