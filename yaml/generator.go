package yaml

import (
	"bytes"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/onsi/gomega"
	"go.yaml.in/yaml/v3"
)

//go:embed docker-compose-docker-lgtm-template.yml
var lgtmTemplate []byte

func CreateDockerComposeFile(r *Runner) string {
	p := filepath.Join(r.testCase.OutputDir, "docker-compose.yml")
	content := getContent(r)
	err := os.WriteFile(p, content, 0644)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	return p
}

func getContent(r *Runner) []byte {
	c := r.testCase
	compose := c.Definition.DockerCompose
	slog.Info("using docker-compose", "lgtm-version", r.Settings.LgtmVersion)

	vars := map[string]any{}
	vars["ApplicationPort"] = c.PortConfig.ApplicationPort
	vars["GrafanaHTTPPort"] = c.PortConfig.GrafanaHTTPPort
	vars["PrometheusHTTPPort"] = c.PortConfig.PrometheusHTTPPort
	vars["LokiHTTPPort"] = c.PortConfig.LokiHTTPPort
	vars["TempoHTTPPort"] = c.PortConfig.TempoHTTPPort
	vars["PyroscopeHttpPort"] = c.PortConfig.PyroscopeHttpPort
	vars["LgtmVersion"] = r.Settings.LgtmVersion
	vars["LgtmLogSettings"] = r.Settings.LgtmLogSettings

	// Overrides to make tests faster by exporting telemetry data more frequently
	vars["OTEL_BLRP_SCHEDULE_DELAY"] = "5000"
	vars["OTEL_BSP_SCHEDULE_DELAY"] = "5000"
	vars["OTEL_METRIC_EXPORT_INTERVAL"] = "5000"

	env := os.Environ()

	for k, v := range vars {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	env = append(env, compose.Environment...)

	t := template.Must(template.New("docker-compose").Parse(string(lgtmTemplate)))

	buf := bytes.NewBufferString("")
	err := t.Execute(buf, vars)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	name := filepath.FromSlash("./docker-compose-docker-lgtm-template.yml")
	generated, err := filepath.Abs(strings.TrimSuffix(name, filepath.Ext(name)) + "-generated.yml")
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	_ = os.WriteFile(generated, buf.Bytes(), 0644)
	defer func(name string) {
		_ = os.Remove(name)
	}(generated)
	files := []string{generated}
	for _, filename := range compose.Files {
		t = template.Must(template.ParseFiles(filename))
		addbuf := bytes.NewBufferString("")
		err = t.Execute(addbuf, vars)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		name, err := filepath.Abs(strings.TrimSuffix(filename, filepath.Ext(filename)) + "-generated.yml")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = os.WriteFile(name, addbuf.Bytes(), 0644)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		defer func(name string) {
			_ = os.Remove(name)
		}(name)
		files = append(files, name)
	}

	// uses docker compose to merge templates (multiple -f flags allow service overrides)
	args := buildComposeArgs(files, compose.Files)
	cmd := exec.Command("docker", args...)
	cmd.Env = env
	cmd.Stderr = os.Stderr
	content, err := cmd.Output()
	if err != nil {
		slog.Error("failed to run docker compose", "error", err)
	}
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	return content
}

// buildComposeArgs constructs the docker compose arguments for merging compose
// files. When user compose files are present, --project-directory is set to the
// directory of the first user file so that relative paths resolve correctly.
func buildComposeArgs(generatedFiles []string, userFiles []string) []string {
	args := []string{"compose"}
	if len(userFiles) > 0 {
		args = append(args, "--project-directory", filepath.Dir(userFiles[0]))
	}
	for _, file := range generatedFiles {
		args = append(args, "-f", file)
	}
	args = append(args, "config")
	return args
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

	// not a generic solution, but works for our use case
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
