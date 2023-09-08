package yaml

import (
	"bytes"
	"encoding/json"
	"github.com/grafana/dashboard-linter/lint"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// relative to docker-compose.yml
var generatedDashboard = filepath.FromSlash("./dashboard.json")

func (c *TestCase) CreateDockerComposeFile() string {
	p := filepath.Join(c.OutputDir, "docker-compose.yml")
	content := c.getContent(c.Definition.DockerCompose)
	err := os.WriteFile(p, content, 0644)
	Expect(err).ToNot(HaveOccurred())
	return p
}

func (c *TestCase) getContent(compose *DockerCompose) []byte {
	if compose.Generator != "" {
		return c.generateDockerComposeFile()
	} else {
		return readComposeFile(compose)
	}
}

func readComposeFile(compose *DockerCompose) []byte {
	b, err := os.ReadFile(compose.File)
	Expect(err).ToNot(HaveOccurred())
	return replaceRefs(compose, b)
}

func replaceRefs(compose *DockerCompose, bytes []byte) []byte {
	baseDir := filepath.Dir(compose.File)
	lines := strings.Split(string(bytes), "\n")
	for i, line := range lines {
		for _, resource := range compose.Resources {
			lines[i] = strings.ReplaceAll(line, "./"+resource, filepath.Join(baseDir, resource))
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

func (c *TestCase) generateDockerComposeFile() []byte {
	dashboard := ""
	if c.Dashboard != nil {
		dashboard = c.readDashboardFile()
	} else {
		configDir, err := filepath.Abs("configs")
		Expect(err).ToNot(HaveOccurred())
		dashboard = filepath.Join(configDir, "grafana-test-dashboard.json")
	}
	configDir, err := filepath.Abs("configs")
	Expect(err).ToNot(HaveOccurred())

	name, vars := c.getTemplateVars()
	vars["Dashboard"] = filepath.ToSlash(dashboard)
	vars["ConfigDir"] = filepath.ToSlash(configDir)
	vars["ApplicationPort"] = c.PortConfig.ApplicationPort
	vars["GrafanaHTTPPort"] = c.PortConfig.GrafanaHTTPPort
	vars["PrometheusHTTPPort"] = c.PortConfig.PrometheusHTTPPort
	vars["LokiHTTPPort"] = c.PortConfig.LokiHTTPPort
	vars["TempoHTTPPort"] = c.PortConfig.TempoHTTPPort

	t := template.Must(template.ParseFiles(name))

	buf := bytes.NewBufferString("")
	err = t.Execute(buf, vars)
	Expect(err).ToNot(HaveOccurred())
	compose := c.Definition.DockerCompose
	if compose.File != "" {
		files, err := joinComposeFiles(buf.Bytes(), readComposeFile(compose))
		Expect(err).ToNot(HaveOccurred())
		return files
	}
	return buf.Bytes()
}

func (c *TestCase) getTemplateVars() (string, map[string]any) {
	generator := c.Definition.DockerCompose.Generator
	switch generator {
	case "java":
		return c.javaTemplateVars()
	default:
		ginkgo.Fail("unknown generator " + generator)
		return "", nil
	}
}

func joinComposeFiles(base []byte, add []byte) ([]byte, error) {
	a := map[string]any{}
	b := map[string]any{}

	err := yaml.Unmarshal(base, &a)
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(add, &b)
	if err != nil {
		return nil, err
	}

	//not a generic solution, but works for our use case
	elems := b["services"].(map[string]any)
	for k, v := range a["services"].(map[string]any) {
		elems[k] = v
	}

	return yaml.Marshal(b)
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
	newFile := filepath.Join(c.OutputDir, generatedDashboard)
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		lines[i] = strings.ReplaceAll(line, "${DS_GRAFANACLOUD-GREGORZEITLINGER-PROM}", "prometheus")
	}
	err = os.WriteFile(newFile, []byte(strings.Join(lines, "\n")), 0644)
	Expect(err).ToNot(HaveOccurred())
	return generatedDashboard
}
