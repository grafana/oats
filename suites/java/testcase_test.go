package java_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/grafana/dashboard-linter/lint"
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

var promQlVariables = []string{"$job", "$instance", "$pod", "$namespace", "$container"}

type ExpectedDashboardPanel struct {
	Title string `yaml:"title"`
	Value string `yaml:"value"`
}

type ExpectedDashboard struct {
	Path   string                   `yaml:"path"`
	Panels []ExpectedDashboardPanel `yaml:"panels"`
}

type ExpectedMetrics struct {
	PromQL string `yaml:"promql"`
	Value  string `yaml:"value"`
}

type TestCaseDefinition struct {
	Expected struct {
		Metrics    []ExpectedMetrics   `yaml:"metrics"`
		Dashboards []ExpectedDashboard `yaml:"dashboards"`
	} `yaml:"expected"`
}

type testDashboard struct {
	path    string
	content lint.Dashboard
}

type testCase struct {
	name       string
	exampleDir string
	projectDir string
	definition TestCaseDefinition
	dashboard  *testDashboard
}

func (c *testCase) validateAndSetDashboard() {
	expectedMetrics := c.definition.Expected.Metrics
	Expect(expectedMetrics).ToNot(BeEmpty())
	for _, d := range c.definition.Expected.Metrics {
		Expect(d.PromQL).ToNot(BeEmpty())
		Expect(d.Value).ToNot(BeEmpty())
	}
	for _, d := range c.definition.Expected.Dashboards {
		out, _ := yaml.Marshal(d)
		Expect(d.Path).ToNot(BeEmpty())
		Expect(d.Panels).ToNot(BeEmpty())
		for _, panel := range d.Panels {
			Expect(panel.Title).ToNot(BeEmpty(), string(out))
			Expect(panel.Value).ToNot(BeEmpty(), string(out))
		}

		Expect(c.dashboard).To(BeNil(), "only one dashboard is supported")
		dashboardPath := path.Join(c.exampleDir, d.Path)
		c.dashboard = &testDashboard{
			path: dashboardPath,
		}
	}
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

func runTestCase(c testCase) {
	var otelComposeEndpoint *compose.ComposeEndpoint

	Describe(c.name, func() {
		BeforeAll(func() {
			c.validateAndSetDashboard()
			var ctx = context.Background()
			var startErr error

			otelComposeEndpoint = compose.NewEndpoint(
				c.generateDockerComposeFile(),
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

				expected := c.definition.Expected
				for _, metric := range expected.Metrics {
					assertProm(g, otelComposeEndpoint, verbose, metric.PromQL, metric.Value)
				}
				for _, dashboard := range expected.Dashboards {
					assertDashboard(g, otelComposeEndpoint, verbose, dashboard, &c.dashboard.content)

				}

			}).WithTimeout(30*time.Second).Should(Succeed(), "calling application for 30 seconds should cause metrics in Prometheus")
		})
	})
}

func assertDashboard(g Gomega, endpoint *compose.ComposeEndpoint, verbose bool, want ExpectedDashboard, dashboard *lint.Dashboard) {
	wantPanelValues := map[string]string{}
	for _, panel := range want.Panels {
		wantPanelValues[panel.Title] = panel.Value
	}

	for _, panel := range dashboard.Panels {
		wantValue := wantPanelValues[panel.Title]
		if wantValue == "" {
			continue
		}
		wantPanelValues[panel.Title] = ""
		g.Expect(panel.Targets).To(HaveLen(1))

		assertProm(g, endpoint, verbose, replaceVariables(panel.Targets[0].Expr), wantValue)
	}

	for panel, expected := range wantPanelValues {
		g.Expect(expected).To(BeEmpty(), "panel '%s' not found", panel)
	}
}

func replaceVariables(promQL string) string {
	for _, variable := range promQlVariables {
		promQL = strings.ReplaceAll(promQL, variable, ".*")
	}
	return promQL
}

func assertProm(g Gomega, endpoint *compose.ComposeEndpoint, verbose bool, promQL string, value string) {
	ctx := context.Background()
	logger := endpoint.Logger()
	b, err := endpoint.RunPromQL(ctx, promQL)
	if verbose {
		_, _ = fmt.Fprintf(logger, "prom response %v err=%v\n", string(b), err)
	}
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(b)).Should(BeNumerically(">", 0))

	pr, err := responses.ParseQueryOutput(b)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(pr)).Should(BeNumerically(">", 0))
	_, _ = fmt.Fprintf(logger, "prom response %v err=%v\n", string(b), err)

	s := strings.Split(value, " ")
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

func readTestCases() []testCase {
	var cases []testCase

	base := os.Getenv("JAVA_TESTCASE_BASE_PATH")
	if base != "" {
		err := filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.Name() != "oats.yaml" {
				return nil
			}
			def := TestCaseDefinition{}
			content, err := os.ReadFile(p)
			err = yaml.Unmarshal(content, &def)
			if err != nil {
				return err
			}
			dir := path.Dir(p)
			name := strings.ReplaceAll(strings.SplitAfter(dir, "examples/")[1], "/", "-")
			projectDir := strings.Split(dir, "examples/")[0]
			cases = append(cases, testCase{
				name:       name,
				exampleDir: dir,
				projectDir: projectDir,
				definition: def,
			})
			return nil
		})
		if err != nil {
			panic(err)
		}
	}
	return cases
}

func (c *testCase) generateDockerComposeFile() string {
	p := path.Join(".", fmt.Sprintf("docker-compose-generated-%s.yml", c.name))

	t := template.Must(template.ParseFiles("./docker-compose-template.yml"))
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	Expect(err).ToNot(HaveOccurred())
	defer f.Close()

	dashboard := "./configs/grafana-test-dashboard.json"
	if c.dashboard != nil {
		dashboard = c.readDashboardFile()
	}

	templateVars := TemplateVars{
		Image:          imageName(c.exampleDir),
		JavaAgent:      path.Join(c.projectDir, "agent/build/libs/grafana-opentelemetry-java.jar"),
		ApplicationJar: c.applicationJar(),
		Dashboard:      dashboard,
	}

	err = t.Execute(f, templateVars)
	Expect(err).ToNot(HaveOccurred())

	return p
}

func (c *testCase) readDashboardFile() string {
	content, err := os.ReadFile(c.dashboard.path)
	Expect(err).ToNot(HaveOccurred())

	c.dashboard.content = c.parseDashboard(content)
	return c.replaceDatasourceId(content, err)
}

func (c *testCase) parseDashboard(content []byte) lint.Dashboard {
	d := lint.Dashboard{}
	err := json.Unmarshal(content, &d)
	Expect(err).ToNot(HaveOccurred())
	return d
}

func (c *testCase) replaceDatasourceId(content []byte, err error) string {
	newFile := fmt.Sprintf("./generated-dashboard%s.json", c.name)
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		lines[i] = strings.ReplaceAll(line, "${DS_GRAFANACLOUD-GREGORZEITLINGER-PROM}", "prometheus")
	}
	err = os.WriteFile(newFile, []byte(strings.Join(lines, "\n")), 0644)
	Expect(err).ToNot(HaveOccurred())
	return newFile
}

func (c *testCase) applicationJar() string {
	pattern := c.exampleDir + "/build/libs/*SNAPSHOT.jar"
	matches, err := filepath.Glob(pattern)
	Expect(err).ToNot(HaveOccurred())
	Expect(matches).To(HaveLen(1))

	return matches[0]
}

func imageName(dir string) string {
	content, err := os.ReadFile(path.Join(dir, ".tool-versions"))
	Expect(err).ToNot(HaveOccurred())
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "java ") {
			// find major version in java temurin-8.0.372+7 using regex
			major := regexp.MustCompile("java temurin-(\\d+).*").FindStringSubmatch(line)[1]
			return fmt.Sprintf("eclipse-temurin:%s-jre", major)
		}
	}
	Fail("no java version found")
	return ""
}
