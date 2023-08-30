package yaml

import (
	"github.com/grafana/dashboard-linter/lint"
	"github.com/onsi/ginkgo/v2"
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

type DockerCompose struct {
	Generator string   `yaml:"generator"`
	File      string   `yaml:"file"`
	Resources []string `yaml:"resources"`
}

type TestCaseDefinition struct {
	DockerCompose DockerCompose `yaml:"docker-compose"`
	Expected      Expected      `yaml:"expected"`
}

type TestDashboard struct {
	Path    string
	Content lint.Dashboard
}

type TestCase struct {
	Name       string
	Dir        string
	OutputDir  string
	Definition TestCaseDefinition
	Dashboard  *TestDashboard
}

func ReadTestCases() []TestCase {
	var cases []TestCase

	base := os.Getenv("TESTCASE_BASE_PATH")
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
			s := strings.Split(dir, "/")
			name := strings.ReplaceAll(strings.Join(s[len(s)-2:], "/"), "/", "-")
			cases = append(cases, TestCase{
				Name:       name,
				Dir:        dir,
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
	validateDockerCompose(&c.Definition.DockerCompose, c.Dir)
	expected := c.Definition.Expected
	if len(expected.Metrics) == 0 && len(expected.Dashboards) == 0 {
		ginkgo.Fail("expected metrics or dashboards")
	}
	for _, d := range expected.Metrics {
		Expect(d.PromQL).ToNot(BeEmpty())
		Expect(d.Value).ToNot(BeEmpty())
	}
	for _, d := range expected.Dashboards {
		out, _ := yaml.Marshal(d)
		Expect(d.Path).ToNot(BeEmpty())
		Expect(d.Panels).ToNot(BeEmpty())
		for _, panel := range d.Panels {
			Expect(panel.Title).ToNot(BeEmpty(), string(out))
			Expect(panel.Value).ToNot(BeEmpty(), string(out))
		}

		Expect(c.Dashboard).To(BeNil(), "only one dashboard is supported")
		dashboardPath := path.Join(c.Dir, d.Path)
		c.Dashboard = &TestDashboard{
			Path: dashboardPath,
		}
	}
}

func validateDockerCompose(d *DockerCompose, dir string) {
	if d.File != "" {
		d.File = path.Join(dir, d.File)
		Expect(d.File).To(BeARegularFile())
		for _, resource := range d.Resources {
			Expect(path.Join(path.Dir(d.File), resource)).To(BeAnExistingFile())
		}
	} else {
		Expect(d.Generator).ToNot(BeEmpty())
		Expect(d.Resources).To(BeEmpty())
	}
}
