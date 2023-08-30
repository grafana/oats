package yaml

import (
	"bytes"
	"encoding/json"
	"github.com/grafana/dashboard-linter/lint"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
	"path"
	"strings"
	"text/template"
)

var skipComposeLines = []string{
	"services:",
	"version:",
}

func (c *TestCase) GetDockerComposeFile() string {
	p := path.Join(c.OutputDir, "docker-compose.yml")
	lines := c.getContent(c.Definition.DockerCompose)
	err := os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0644)
	Expect(err).ToNot(HaveOccurred())
	return p
}

func (c *TestCase) getContent(compose DockerCompose) []string {
	if compose.Generator != "" {
		return c.generateDockerComposeFile()
	} else {
		return readComposeFile(compose)
	}
}

func readComposeFile(compose DockerCompose) []string {
	b, err := os.ReadFile(compose.File)
	Expect(err).ToNot(HaveOccurred())
	return replaceRefs(compose, b)
}

func replaceRefs(compose DockerCompose, bytes []byte) []string {
	baseDir := path.Dir(compose.File)
	lines := strings.Split(string(bytes), "\n")
	for i, line := range lines {
		for _, resource := range compose.Resources {
			lines[i] = strings.ReplaceAll(line, "./"+resource, path.Join(baseDir, resource))
		}
	}
	return lines
}

func (c *TestCase) generateDockerComposeFile() []string {

	dashboard := "./configs/grafana-test-dashboard.json"
	if c.Dashboard != nil {
		dashboard = c.readDashboardFile()
	}
	name, vars := c.getTemplateVars(dashboard)
	t := template.Must(template.ParseFiles(name))

	buf := bytes.NewBufferString("")
	err := t.Execute(buf, vars)
	Expect(err).ToNot(HaveOccurred())
	lines := strings.Split(buf.String(), "\n")
	compose := c.Definition.DockerCompose
	if compose.File != "" {
		return joinComposeFiles(readComposeFile(compose), lines)
	}
	return lines
}

func (c *TestCase) getTemplateVars(dashboard string) (string, any) {
	generator := c.Definition.DockerCompose.Generator
	switch generator {
	case "java":
		return c.javaTemplateVars(dashboard)
	default:
		Fail("unknown generator " + generator)
		return "", nil
	}
}

func joinComposeFiles(base []string, add []string) []string {
	for _, l := range add {
		if !skipLine(l) {
			base = append(base, l)
		}
	}
	return base
}

func skipLine(line string) bool {
	for _, skip := range skipComposeLines {
		if strings.HasPrefix(line, skip) {
			return true
		}
	}
	return false
}

func (c *TestCase) readDashboardFile() string {
	content, err := os.ReadFile(c.Dashboard.Path)
	Expect(err).ToNot(HaveOccurred())

	c.Dashboard.Content = c.parseDashboard(content)
	return c.replaceDatasource(content, err)
}

func (c *TestCase) parseDashboard(content []byte) lint.Dashboard {
	d := lint.Dashboard{}
	err := json.Unmarshal(content, &d)
	Expect(err).ToNot(HaveOccurred())
	return d
}

func (c *TestCase) replaceDatasource(content []byte, err error) string {
	// we need the ./ in docker-compose.yml
	newFile := "./" + path.Join(c.OutputDir, "dashboard.json")
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		lines[i] = strings.ReplaceAll(line, "${DS_GRAFANACLOUD-GREGORZEITLINGER-PROM}", "prometheus")
	}
	err = os.WriteFile(newFile, []byte(strings.Join(lines, "\n")), 0644)
	Expect(err).ToNot(HaveOccurred())
	return newFile
}
