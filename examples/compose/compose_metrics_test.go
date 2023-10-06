package compose_test

import (
	"context"
	"path"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/grafana/oats/testhelpers/compose"
	"github.com/grafana/oats/testhelpers/prometheus/responses"
	"github.com/grafana/oats/testhelpers/requests"
)

var _ = Describe("provisioning a local observability endpoint with Docker", Ordered, Label("docker", "integration", "slow"), func() {
	var endpoint *compose.ComposeEndpoint

	BeforeAll(func() {
		var ctx context.Context = context.Background()
		var startErr error

		endpoint = compose.NewEndpoint(
			path.Join(".", "docker-compose-traces.yml"),
			path.Join(".", "test-suite-metrics.log"),
			[]string{},
			compose.PortsConfig{MimirHTTPPort: 9009},
		)
		startErr = endpoint.Start(ctx)
		Expect(startErr).ToNot(HaveOccurred(), "expected no error starting a local observability endpoint")
	})

	AfterAll(func() {
		var ctx context.Context = context.Background()
		var stopErr error

		if endpoint != nil {
			stopErr = endpoint.Stop(ctx)
			Expect(stopErr).ToNot(HaveOccurred(), "expected no error stopping the local observability endpoint")
		}
	})

	// Traces generated by auto-instrumentation
	Describe("observability.LocalEndpoint", func() {
		It("can create metrics with auto-instrumentation", func() {
			ctx := context.Background()
			const apiCount = 3

			// Run repeated /smoke APIs to ensure data is flowing from the application to Tempo
			Eventually(ctx, func(g Gomega) {
				err := requests.DoHTTPGet("http://localhost:8080/smoke", 200)
				g.Expect(err).ToNot(HaveOccurred())

				b, err := endpoint.RunPromQL(ctx, `http_server_duration_count{http_route="/smoke"}`)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(len(b)).Should(BeNumerically(">", 0))

				pr, err := responses.ParseQueryOutput(b)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(len(pr)).Should(BeNumerically(">", 0))
			}).WithTimeout(30*time.Second).Should(Succeed(), "calling /smoke for 30 seconds should cause metrics in Mimir")

			for i := 0; i < apiCount; i++ {
				Expect(requests.DoHTTPGet("http://localhost:8080/greeting?delay=30ms&status=204", 204)).ShouldNot(HaveOccurred())
			}

			var pr []responses.Result

			Eventually(ctx, func(g Gomega) {
				b, err := endpoint.RunPromQL(ctx, `http_server_duration_count{`+
					`http_method="GET",`+
					`http_status_code="204",`+
					`job="integration-test/testserver",`+
					`http_route="/greeting"}`)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(len(b)).Should(BeNumerically(">", 0))

				pr, err = responses.ParseQueryOutput(b)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(len(pr)).Should(BeNumerically(">", 0))
			}).WithTimeout(30*time.Second).Should(Succeed(), "metrics should appear in Mimir with /greeting as http.target")

			Expect(responses.EnoughPromResults(pr)).ToNot(HaveOccurred())
			count, err := responses.TotalPromCount(pr)
			Expect(err).ToNot(HaveOccurred())
			Expect(count).Should(Equal(apiCount))
		})
	})
})
