package yaml

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/grafana/dashboard-linter/lint"
	"github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// relative to docker-compose.yml
var generatedDashboard = filepath.FromSlash("./dashboard.json")

func (c *TestCase) CreateDockerComposeFile() string {
	p := filepath.Join(c.OutputDir, "docker-compose.yml")
	content := c.getContent(c.Definition.DockerCompose)
	err := os.WriteFile(p, content, 0644)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	return p
}

func (c *TestCase) getContent(compose *DockerCompose) []byte {
	return c.generateDockerComposeFile()
}

func (c *TestCase) generateDockerComposeFile() []byte {
	dashboard := ""
	if c.Dashboard != nil {
		dashboard = c.readDashboardFile()
	} else {
		configDir, err := filepath.Abs("configs")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		dashboard = filepath.Join(configDir, "grafana-test-dashboard.json")
	}
	configDir, err := filepath.Abs("configs")
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	generator := c.Definition.DockerCompose.Generator
	if generator == "" {
		generator = "docker-lgtm"
	}
	name := filepath.FromSlash("./docker-compose-" + generator + "-template.yml")
	vars := map[string]any{}
	vars["Dashboard"] = filepath.ToSlash(dashboard)
	vars["ConfigDir"] = filepath.ToSlash(configDir)
	vars["ApplicationPort"] = c.PortConfig.ApplicationPort
	vars["GrafanaHTTPPort"] = c.PortConfig.GrafanaHTTPPort
	vars["PrometheusHTTPPort"] = c.PortConfig.PrometheusHTTPPort
	vars["LokiHTTPPort"] = c.PortConfig.LokiHTTPPort
	vars["TempoHTTPPort"] = c.PortConfig.TempoHTTPPort

	env := os.Environ()

	for k, v := range vars {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	env = append(env, c.Definition.DockerCompose.Environment...)

	t := template.Must(template.ParseFiles(name))

	buf := bytes.NewBufferString("")
	err = t.Execute(buf, vars)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	generated, err := filepath.Abs(strings.TrimSuffix(name, filepath.Ext(name)) + "-generated.yml")
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	_ = os.WriteFile(generated, buf.Bytes(), 0644)
	defer func(name string) {
		_ = os.Remove(name)
	}(generated)
	compose := c.Definition.DockerCompose
	files := []string{generated}
	for _, filename := range compose.Files {
		t = template.Must(template.ParseFiles(filename))
		addbuf := bytes.NewBufferString("")
		err = t.Execute(addbuf, vars)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		name := strings.TrimSuffix(filename, filepath.Ext(filename)) + "-generated.yml"
		err = os.WriteFile(name, addbuf.Bytes(), 0644)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer func(name string) {
			_ = os.Remove(name)
		}(name)
		files = append(files, name)
	}

	base := filepath.FromSlash("./docker-compose-include-base.yml")
	t = template.Must(template.ParseFiles(base))
	buf = bytes.NewBufferString("")
	vars = map[string]any{}
	vars["files"] = files
	err = t.Execute(buf, vars)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	f, err := os.CreateTemp("", "docker-compose-base.yml")
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	_, err = f.Write(buf.Bytes())
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	defer os.Remove(f.Name())

	// uses docker compose to merge templates
	args := []string{"compose", "-f", f.Name(), "config"}
	cmd := exec.Command("docker", args...)
	cmd.Env = env
	content, err := cmd.Output()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	return content
}

func joinComposeFiles(template []byte, addition []byte) ([]byte, error) {
	base := map[string]any{}
	add := map[string]any{}

	err := yaml.Unmarshal(template, &base)
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(addition, &add)
	if err != nil {
		return nil, err
	}

	//not a generic solution, but works for our use case
	addFromBase(base, add, "services")
	addFromBase(base, add, "networks")

	return yaml.Marshal(add)
}

func addFromBase(base map[string]any, add map[string]any, key string) {
	addMap, ok := add[key].(map[string]any)
	if !ok {
		addMap = map[string]any{}
		add[key] = addMap
	}

	baseMap, ok := base[key].(map[string]any)
	if ok {
		for k, v := range baseMap {
			addMap[k] = v
		}
	}
}

func (c *TestCase) readDashboardFile() string {
	content, err := os.ReadFile(c.Dashboard.Path)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	c.Dashboard.Content = c.parseDashboard(content)
	return c.replaceDatasource(content)
}

func (c *TestCase) parseDashboard(content []byte) lint.Dashboard {
	d := lint.Dashboard{}
	err := json.Unmarshal(content, &d)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	return d
}

func (c *TestCase) replaceDatasource(content []byte) string {
	newFile := filepath.Join(c.OutputDir, generatedDashboard)
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		lines[i] = strings.ReplaceAll(line, "${DS_GRAFANACLOUD-GREGORZEITLINGER-PROM}", "prometheus")
	}
	err := os.WriteFile(newFile, []byte(strings.Join(lines, "\n")), 0644)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	abs, err := filepath.Abs(newFile)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	return abs
}
