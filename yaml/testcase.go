package yaml

import (
	"github.com/grafana/dashboard-linter/lint"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
	"os"
	"path"
	"path/filepath"
	"strings"
)

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

type Expected struct {
	Metrics    []ExpectedMetrics   `yaml:"metrics"`
	Dashboards []ExpectedDashboard `yaml:"dashboards"`
}

type TestCaseDefinition struct {
	Expected Expected `yaml:"expected"`
}

type TestDashboard struct {
	Path    string
	Content lint.Dashboard
}

type TestCase struct {
	Name       string
	ExampleDir string
	ProjectDir string
	Definition TestCaseDefinition
	Dashboard  *TestDashboard
}

func ReadTestCases() []TestCase {
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
			def := TestCaseDefinition{}
			content, err := os.ReadFile(p)
			err = yaml.Unmarshal(content, &def)
			if err != nil {
				return err
			}
			dir := path.Dir(p)
			name := strings.ReplaceAll(strings.SplitAfter(dir, "examples/")[1], "/", "-")
			projectDir := strings.Split(dir, "examples/")[0]
			cases = append(cases, TestCase{
				Name:       name,
				ExampleDir: dir,
				ProjectDir: projectDir,
				Definition: def,
			})
			return nil
		})
		if err != nil {
			panic(err)
		}
	}
	return cases
}

func (c *TestCase) ValidateAndSetDashboard() {
	expectedMetrics := c.Definition.Expected.Metrics
	Expect(expectedMetrics).ToNot(BeEmpty())
	for _, d := range c.Definition.Expected.Metrics {
		Expect(d.PromQL).ToNot(BeEmpty())
		Expect(d.Value).ToNot(BeEmpty())
	}
	for _, d := range c.Definition.Expected.Dashboards {
		out, _ := yaml.Marshal(d)
		Expect(d.Path).ToNot(BeEmpty())
		Expect(d.Panels).ToNot(BeEmpty())
		for _, panel := range d.Panels {
			Expect(panel.Title).ToNot(BeEmpty(), string(out))
			Expect(panel.Value).ToNot(BeEmpty(), string(out))
		}

		Expect(c.Dashboard).To(BeNil(), "only one dashboard is supported")
		dashboardPath := path.Join(c.ExampleDir, d.Path)
		c.Dashboard = &TestDashboard{
			Path: dashboardPath,
		}
	}
}
