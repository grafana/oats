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

var _ = Describe("test case", Ordered, Label("docker", "integration", "slow"), func() {
	for _, c := range yaml.ReadTestCases() {
		runTestCase(c)
	}
})

func runTestCase(c yaml.TestCase) {
	var otelComposeEndpoint *compose.ComposeEndpoint

	Describe(c.Name, func() {
		BeforeAll(func() {
			c.ValidateAndSetDashboard()
			var ctx = context.Background()
			var startErr error

			c.OutputDir = path.Join(".", "build", c.Name)
			err := os.MkdirAll(c.OutputDir, 0755)
			Expect(err).ToNot(HaveOccurred(), "expected no error creating output directory")
			err = exec.Command("cp", "-r", "configs", c.OutputDir).Run()
			Expect(err).ToNot(HaveOccurred(), "expected no error copying configs directory")

			otelComposeEndpoint = compose.NewEndpoint(
				c.GetDockerComposeFile(),
				path.Join(c.OutputDir, "output.log"),
				[]string{},
				compose.PortsConfig{PrometheusHTTPPort: 9090},
			)
			startErr = otelComposeEndpoint.Start(ctx)
			Expect(startErr).ToNot(HaveOccurred(), "expected no error starting a local observability endpoint")
		})

		AfterAll(func() {
			var ctx = context.Background()
			var stopErr error

			if otelComposeEndpoint != nil {
				stopErr = otelComposeEndpoint.Stop(ctx)
				Expect(stopErr).ToNot(HaveOccurred(), "expected no error stopping the local observability endpoint")
			}
		})

		It("should have all telemetry data", func() {
			waitForTelemetry(otelComposeEndpoint, c.Definition.Input, func(g Gomega, verbose bool) {
				yaml.AssertMetrics(g, c.Definition.Expected, otelComposeEndpoint, verbose, c) //better tree construction
			})
		})
	})
}

func waitForTelemetry(endpoint *compose.ComposeEndpoint, input []yaml.Input, asserter func(g Gomega, verbose bool)) {
	t := time.Now()
	ctx := context.Background()
	logger := endpoint.Logger()

	Eventually(ctx, func(g Gomega) {
		verbose := false
		if time.Since(t) > 10*time.Second {
			verbose = true
			t = time.Now()
		}

		if verbose {
			_, _ = fmt.Fprintf(logger, "waiting for telemetry data\n")
		}

		for _, i := range input {
			err := requests.DoHTTPGet(i.Url, 200)
			g.Expect(err).ToNot(HaveOccurred())
		}

		asserter(g, verbose)
	}).WithTimeout(30*time.Second).Should(Succeed(), "calling application for 30 seconds should cause telemetry to appear")

}
