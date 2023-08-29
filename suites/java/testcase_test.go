package java_test

import (
	"context"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/grafana/oats/internal/testhelpers/compose"
	"github.com/grafana/oats/internal/testhelpers/prometheus/responses"
	"github.com/grafana/oats/internal/testhelpers/requests"
)

type ExpectedMetrics struct {
	PromQL string `yaml:"promql"`
	Value  string `yaml:"value"`
}

type TestCase struct {
	name       string
	exampleDir string
	projectDir string
	Expected   struct {
		Metrics []ExpectedMetrics `yaml:"metrics"`
	} `yaml:"expected"`
}

type TemplateVars struct {
	Image          string
	JavaAgent      string
	ApplicationJar string
	Dashboard      string
}

var _ = Describe("testcases", Ordered, Label("docker", "integration", "slow"), func() {
	for _, c := range readTestCases() {
		runTestCase(c)
	}
})

func runTestCase(c TestCase) {
	var otelComposeEndpoint *compose.ComposeEndpoint

	Describe(c.name, func() {
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

		It("should have all data in prometheus", func() {
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

				for _, metric := range c.Expected.Metrics {
					if metric.PromQL != "" {
						assertPromQL(g, otelComposeEndpoint, verbose, metric)
					}
				}

			}).WithTimeout(30*time.Second).Should(Succeed(), "calling application for 30 seconds should cause metrics in Prometheus")
		})
	})
}

func assertPromQL(g Gomega, endpoint *compose.ComposeEndpoint, verbose bool, want ExpectedMetrics) {
	ctx := context.Background()
	logger := endpoint.Logger()
	b, err := endpoint.RunPromQL(ctx, want.PromQL)
	if verbose {
		_, _ = fmt.Fprintf(logger, "prom response %v err=%v\n", string(b), err)
	}
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(b)).Should(BeNumerically(">", 0))

	pr, err := responses.ParseQueryOutput(b)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(pr)).Should(BeNumerically(">", 0))
	_, _ = fmt.Fprintf(logger, "prom response %v err=%v\n", string(b), err)

	s := strings.Split(want.Value, " ")
	comp := s[0]
	val, err := strconv.ParseFloat(s[1], 64)
	if err != nil {
		g.Expect(err).ToNot(HaveOccurred())
	}
	got, err := strconv.ParseFloat(pr[0].Value[1].(string), 64)
	if err != nil {
		g.Expect(err).ToNot(HaveOccurred())
	}

	g.Expect(got).Should(BeNumerically(comp, val))
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
