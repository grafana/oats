package compose_test

import (
	"context"
	"fmt"
	"path"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/grafana/oats/internal/testhelpers/compose"
	"github.com/grafana/oats/internal/testhelpers/prometheus/responses"
	"github.com/grafana/oats/internal/testhelpers/requests"
)

type TestCase struct {
	Start string `yaml:"start"`
	Calls []struct {
		URL string `yaml:"url"`
	} `yaml:"calls"`
	Expected struct {
		Metrics []struct {
			PromQL string `yaml:"promql"`
			Value  string `yaml:"value"`
		}
	} `yaml:"expected"`
}

var _ = Describe("testcases", Ordered, Label("docker", "integration", "slow"), func() {
	var otelComposeEndpoint *compose.ComposeEndpoint
	//var cmd *exec.Cmd

	// how a testcase would look like
	// 1. name = the full path where the oats.yaml file is located
	// 2. oats.yaml has the following structure:
	//	- start: run.sh --no-agent (to start the application - maybe also a docker-compose file)
	//  - calls:
	//    - url: /smoke
	//  - expected:
	//    - metrics:
	//	    - promql: http_server_duration_count{http_route="/smoke"}
	//        value: > 0
	//      - dashboard: jdbc-dashboard.json
	//        panels:
	//        - title: "HTTP Server Duration Count"
	//          value: > 0

	// How to use this test case?
	// In CI, do the following steps - probably once a day, because it's too slow for PRs:
	// 1. Check out the oats repo
	// 2. Install ginkgo
	// 3. `export TESTCASE_BASE_PATH=/path/to/oats.yaml` (or to a parent directory of oats.yaml)
	// 4. start ginkgo
	// 5. this test case scans the directory tree for oats.yaml files and runs them

	// details
	// Generate a whole docker-compose file with all the services
	// - application: ls $dir/build/libs | grep -v plain | grep jar
	// - take docker-compose file from the parent directory
	// - grafana: mount dashboard

	BeforeAll(func() {
		var ctx = context.Background()
		var startErr error

		otelComposeEndpoint = compose.NewEndpoint(
			//path.Join(".", "docker-compose-ingest-only.yml"),
			path.Join(".", "docker-compose-testcase.yml"),
			path.Join(".", "test-suite-testcases.log"),
			[]string{},
			//compose.PortsConfig{MimirHTTPPort: 9009},
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

	//AfterEach(func() {
	//	if cmd != nil {
	//		_ = cmd.Process.Kill()
	//	}
	//})

	// Traces generated by auto-instrumentation
	Describe("Execute testcases", func() {
		It("run testcase", func() {
			ctx := context.Background()
			//cmd := exec.Command("/home/gregor/source/otel-distro/examples/jdbc/spring-boot-non-reactive-2.7/run.sh", "--no-agent")
			//cmd.Dir = "/home/gregor/source/otel-distro/examples/jdbc/spring-boot-non-reactive-2.7"
			//cmd.Env = []string{"JAVA_HOME=/home/gregor/.asdf/installs/java/temurin-8.0.372+7/"}
			logger := otelComposeEndpoint.Logger()
			//cmd.Stdout = logger
			//cmd.Stderr = logger
			//err := cmd.Start()
			//Expect(err).ToNot(HaveOccurred())

			t := time.Now()

			Eventually(ctx, func(g Gomega) {
				verbose := false
				if time.Since(t) > 10*time.Second {
					verbose = true
					t = time.Now()
				}

				if verbose {
					_, _ = fmt.Fprintf(logger, "waiting for telemetry data\n")
				}

				err := requests.DoHTTPGet("http://localhost:8080/stock", 200)
				g.Expect(err).ToNot(HaveOccurred())

				b, err := otelComposeEndpoint.RunPromQL(ctx, `db_client_connections_max{pool_name="HikariPool-1"}`)
				if verbose {
					_, _ = fmt.Fprintf(logger, "prom response %v err=%v\n", string(b), err)
				}
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(len(b)).Should(BeNumerically(">", 0))

				pr, err := responses.ParseQueryOutput(b)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(len(pr)).Should(BeNumerically(">", 0))
			}).WithTimeout(300*time.Second).Should(Succeed(), "calling application for 30 seconds should cause metrics in Prometheus")
		})
	})
})
