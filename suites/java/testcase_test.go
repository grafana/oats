package java_test

import (
	"context"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/grafana/oats/internal/testhelpers/compose"
	"github.com/grafana/oats/internal/testhelpers/prometheus/responses"
	"github.com/grafana/oats/internal/testhelpers/requests"
)

type TestCase struct {
	name       string
	exampleDir string
	projectDir string
	Expected   struct {
		Metrics []struct {
			PromQL string `yaml:"promql"`
			Value  string `yaml:"value"`
		}
	} `yaml:"expected"`
}

type TemplateVars struct {
	Image          string
	JavaAgent      string
	ApplicationJar string
	Dashboard      string
}

var _ = Describe("testcases", Ordered, Label("docker", "integration", "slow"), func() {

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

	for _, c := range readTestCases() {
		runTestCase(c)
	}

})

func runTestCase(c TestCase) {
	var otelComposeEndpoint *compose.ComposeEndpoint

	BeforeAll(func() {
		var ctx = context.Background()
		var startErr error

		otelComposeEndpoint = compose.NewEndpoint(
			generateDockerComposeFile(c),
			path.Join(".", fmt.Sprintf("testcase-%s.log", c.name)),
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

	It(c.name, func() {
		ctx := context.Background()
		logger := otelComposeEndpoint.Logger()

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
}

func readTestCases() []TestCase {
	var cases []TestCase

	base := os.Getenv("JAVA_TESTCASE_BASE_PATH")
	if base != "" {
		err := filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.Name() != "oats.yaml" {
				return nil
			}
			testCase := TestCase{}
			content, err := os.ReadFile(p)
			err = yaml.Unmarshal(content, &testCase)
			if err != nil {
				return err
			}
			dir := path.Dir(p)
			testCase.name = strings.ReplaceAll(strings.SplitAfter(dir, "examples/")[1], "/", "-")
			testCase.exampleDir = dir
			testCase.projectDir = strings.Split(dir, "examples/")[0]

			cases = append(cases, testCase)
			return nil
		})
		if err != nil {
			panic(err)
		}
	}
	return cases
}

func generateDockerComposeFile(c TestCase) string {
	p := path.Join(".", fmt.Sprintf("docker-compose-generated-%s.yml", c.name))

	t := template.Must(template.ParseFiles("./docker-compose-template.yml"))
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	Expect(err).ToNot(HaveOccurred())
	defer f.Close()

	templateVars := TemplateVars{
		Image:          imageName(c.exampleDir),
		JavaAgent:      path.Join(c.projectDir, "agent/build/libs/grafana-opentelemetry-java.jar"),
		ApplicationJar: applicationJar(c),
		Dashboard:      "./configs/grafana-test-dashboard.json",
	}

	err = t.Execute(f, templateVars)
	Expect(err).ToNot(HaveOccurred())

	return p
}

func applicationJar(c TestCase) string {
	pattern := c.exampleDir + "/build/libs/*SNAPSHOT.jar"
	matches, err := filepath.Glob(pattern)
	if err != nil {
		panic(err)
	}
	if len(matches) != 1 {
		panic("expected exactly one match for " + pattern + " but got " + fmt.Sprintf("%v", matches))
	}

	return matches[0]
}

func imageName(dir string) string {
	content, err := os.ReadFile(path.Join(dir, ".tool-versions"))
	if err != nil {
		panic(err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "java ") {
			// find major version in java temurin-8.0.372+7 using regex
			major := regexp.MustCompile("java temurin-(\\d+).*").FindStringSubmatch(line)[1]
			return fmt.Sprintf("eclipse-temurin:%s-jre", major)
		}
	}
	panic("no java version found")
}
