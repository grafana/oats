package yaml

import (
	"encoding/json"
	"fmt"
	"github.com/grafana/dashboard-linter/lint"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

type TemplateVars struct {
	Image          string
	JavaAgent      string
	ApplicationJar string
	Dashboard      string
}

func (c *TestCase) GenerateDockerComposeFile() string {
	p := path.Join(".", fmt.Sprintf("docker-compose-generated-%s.yml", c.Name))

	t := template.Must(template.ParseFiles("./docker-compose-template.yml"))
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	Expect(err).ToNot(HaveOccurred())
	defer f.Close()

	dashboard := "./configs/grafana-test-dashboard.json"
	if c.Dashboard != nil {
		dashboard = c.readDashboardFile()
	}

	templateVars := TemplateVars{
		Image:          imageName(c.ExampleDir),
		JavaAgent:      path.Join(c.ProjectDir, "agent/build/libs/grafana-opentelemetry-java.jar"),
		ApplicationJar: c.applicationJar(),
		Dashboard:      dashboard,
	}

	err = t.Execute(f, templateVars)
	Expect(err).ToNot(HaveOccurred())

	return p
}

func (c *TestCase) readDashboardFile() string {
	content, err := os.ReadFile(c.Dashboard.Path)
	Expect(err).ToNot(HaveOccurred())

	c.Dashboard.Content = c.parseDashboard(content)
	return c.replaceDatasourceId(content, err)
}

func (c *TestCase) parseDashboard(content []byte) lint.Dashboard {
	d := lint.Dashboard{}
	err := json.Unmarshal(content, &d)
	Expect(err).ToNot(HaveOccurred())
	return d
}

func (c *TestCase) replaceDatasourceId(content []byte, err error) string {
	newFile := fmt.Sprintf("./generated-dashboard%s.json", c.Name)
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		lines[i] = strings.ReplaceAll(line, "${DS_GRAFANACLOUD-GREGORZEITLINGER-PROM}", "prometheus")
	}
	err = os.WriteFile(newFile, []byte(strings.Join(lines, "\n")), 0644)
	Expect(err).ToNot(HaveOccurred())
	return newFile
}

func (c *TestCase) applicationJar() string {
	pattern := c.ExampleDir + "/build/libs/*SNAPSHOT.jar"
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
