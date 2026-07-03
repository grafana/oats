package e2e

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

type caseFile struct {
	Name      string     `yaml:"name"`
	Tags      []string   `yaml:"tags,omitempty"`
	Execution execution  `yaml:"execution,omitempty"`
	Run       runSpec    `yaml:"run,omitempty"`
	Expect    expectSpec `yaml:"expect,omitempty"`
}

type execution struct {
	Mode string `yaml:"mode,omitempty"` // parallel (default) | serial
}

type runSpec struct {
	Command string            `yaml:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
	Shell   string            `yaml:"shell,omitempty"`
}

type expectSpec struct {
	ExitCode          *int     `yaml:"exit_code,omitempty"`
	StdoutContains    []string `yaml:"stdout_contains,omitempty"`
	StdoutNotContains []string `yaml:"stdout_not_contains,omitempty"`
	StderrContains    []string `yaml:"stderr_contains,omitempty"`
}

type placeholders struct {
	RepoRoot       string
	CaseDir        string
	CaseName       string
	RemoteOTLPHTTP string
	GCX            string
	GCXConfig      string
	OATS           string
}

type sharedEnv struct {
	RepoRoot       string
	TempDir        string
	ComposeFile    string
	Project        string
	GrafanaURL     string
	RemoteOTLPHTTP string
	GCX            string
	GCXConfig      string
	OATS           string
}

var (
	shared     sharedEnv
	sharedOnce sync.Once
	sharedErr  error
)

func TestMain(m *testing.M) {
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "find repo root: %v\n", err)
		os.Exit(1)
	}
	shared.RepoRoot = root
	if runtime.GOOS != "windows" {
		if err := prepareLocalTools(&shared); err != nil {
			fmt.Fprintf(os.Stderr, "e2e setup failed: %v\n", err)
			_ = teardownSharedEnv(&shared)
			os.Exit(1)
		}
	}
	code := m.Run()
	if runtime.GOOS != "windows" {
		if err := teardownSharedEnv(&shared); err != nil {
			fmt.Fprintf(os.Stderr, "e2e teardown failed: %v\n", err)
			if code == 0 {
				code = 1
			}
		}
	}
	os.Exit(code)
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find go.mod above %s", wd)
		}
		dir = parent
	}
}

func TestCases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("e2e cases rely on POSIX shell helpers")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is required for e2e")
	}

	root := repoRoot(t)
	caseRoots := discoverCases(t, filepath.Join(root, "tests", "e2e", "cases"))
	filters := splitFilter(os.Getenv("OATS_E2E_FILTER"))

	var serial []string
	var parallel []string
	for _, dir := range caseRoots {
		rel, err := filepath.Rel(filepath.Join(root, "tests", "e2e", "cases"), dir)
		if err != nil {
			t.Fatalf("rel path for %s: %v", dir, err)
		}
		if len(filters) > 0 && !matchesAnyFilter(rel, filters) {
			continue
		}
		spec := loadCaseFile(t, filepath.Join(dir, "test.yaml"))
		if spec.Execution.Mode == "serial" {
			serial = append(serial, dir)
			continue
		}
		parallel = append(parallel, dir)
	}

	for _, dir := range parallel {
		dir := dir
		t.Run(caseName(root, dir), func(t *testing.T) {
			t.Parallel()
			runCase(t, root, dir)
		})
	}

	for _, dir := range serial {
		dir := dir
		t.Run(caseName(root, dir), func(t *testing.T) {
			runCase(t, root, dir)
		})
	}
}

func setupSharedEnv(env *sharedEnv) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return err
	}
	if err := prepareLocalTools(env); err != nil {
		return err
	}
	env.Project = fmt.Sprintf("oatse2e%d", os.Getpid())
	env.ComposeFile = filepath.Join(env.TempDir, "docker-compose.yml")
	const composeBody = `services:
  lgtm:
    image: docker.io/grafana/otel-lgtm:latest
    ports:
      - "127.0.0.1::3000"
      - "127.0.0.1::4318"
`
	if err := os.WriteFile(env.ComposeFile, []byte(composeBody), 0o644); err != nil {
		return err
	}
	up := exec.Command("docker", "compose", "-p", env.Project, "-f", env.ComposeFile, "up", "-d")
	up.Stdout = os.Stdout
	up.Stderr = os.Stderr
	if err := up.Run(); err != nil {
		return fmt.Errorf("start shared lgtm: %w", err)
	}
	grafanaPort, err := dockerComposePort(env.Project, env.ComposeFile, "lgtm", "3000")
	if err != nil {
		return err
	}
	otlpPort, err := dockerComposePort(env.Project, env.ComposeFile, "lgtm", "4318")
	if err != nil {
		return err
	}
	env.GrafanaURL = "http://127.0.0.1:" + grafanaPort
	env.RemoteOTLPHTTP = "http://127.0.0.1:" + otlpPort
	if err := writeGCXConfig(env.GCXConfig, env.GrafanaURL); err != nil {
		return err
	}
	if err := waitForHTTP(env.GrafanaURL+"/api/health", 2*time.Minute); err != nil {
		return err
	}
	if err := waitForHTTP(env.RemoteOTLPHTTP, 2*time.Minute); err != nil {
		return err
	}
	return nil
}

func prepareLocalTools(env *sharedEnv) error {
	if env.TempDir != "" && env.OATS != "" && env.GCX != "" && env.GCXConfig != "" {
		return nil
	}
	tmp, err := os.MkdirTemp("", "oats-e2e-")
	if err != nil {
		return err
	}
	env.TempDir = tmp
	binDir := filepath.Join(tmp, "bin")
	build := exec.Command("bash", "-lc", fmt.Sprintf("./scripts/build-local-tools.sh %q", binDir))
	build.Dir = env.RepoRoot
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("build local tools: %w", err)
	}
	env.OATS = filepath.Join(binDir, "oats")
	env.GCX = filepath.Join(binDir, "gcx")
	env.GCXConfig = filepath.Join(tmp, "gcx.yaml")
	return nil
}

func ensureSharedEnv(t *testing.T) {
	t.Helper()
	sharedOnce.Do(func() {
		sharedErr = setupSharedEnv(&shared)
	})
	if sharedErr != nil {
		t.Fatalf("e2e setup failed: %v", sharedErr)
	}
}

func caseNeedsSharedEnv(t *testing.T, dir string) bool {
	t.Helper()
	matches := false
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "{{REMOTE_OTLP_HTTP}}") {
			matches = true
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && !errors.Is(err, filepath.SkipAll) {
		t.Fatalf("scan %s: %v", dir, err)
	}
	return matches
}

func writeGCXConfig(path, grafanaURL string) error {
	cfg := map[string]any{
		"current-context": "local",
		"contexts": map[string]any{
			"local": map[string]any{
				"grafana": map[string]any{
					"server":      grafanaURL,
					"user":        "admin",
					"password":    "admin",
					"org-id":      1,
					"auth-method": "basic",
				},
				"datasources": map[string]any{
					"prometheus": "prometheus",
					"loki":       "loki",
					"tempo":      "tempo",
					"pyroscope":  "pyroscope",
				},
			},
		},
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func dockerComposePort(project, composeFile, service, containerPort string) (string, error) {
	cmd := exec.Command("docker", "compose", "-p", project, "-f", composeFile, "port", service, containerPort)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker compose port %s %s: %w\n%s", service, containerPort, err, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", fmt.Errorf("docker compose port %s %s: empty output", service, containerPort)
	}
	host, port, err := splitHostPort(out)
	if err != nil {
		return "", fmt.Errorf("docker compose port %s %s: %w", service, containerPort, err)
	}
	if host == "" {
		return "", fmt.Errorf("docker compose port %s %s: missing host in %q", service, containerPort, out)
	}
	return port, nil
}

func splitHostPort(addr string) (string, string, error) {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, "[") {
		end := strings.Index(addr, "]")
		if end < 0 || end+2 > len(addr) || addr[end+1] != ':' {
			return "", "", fmt.Errorf("invalid address %q", addr)
		}
		return addr[1:end], addr[end+2:], nil
	}
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return "", "", fmt.Errorf("invalid address %q", addr)
	}
	return addr[:idx], addr[idx+1:], nil
}

func teardownSharedEnv(env *sharedEnv) error {
	var errs []string
	if env.ComposeFile != "" {
		cmd := exec.Command("docker", "compose", "-p", env.Project, "-f", env.ComposeFile, "down", "-v", "--remove-orphans")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if env.TempDir != "" {
		if err := os.RemoveAll(env.TempDir); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func waitForHTTP(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out waiting for %s", url)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	return shared.RepoRoot
}

func caseName(root, dir string) string {
	rel, err := filepath.Rel(filepath.Join(root, "tests", "e2e", "cases"), dir)
	if err != nil {
		return filepath.Base(dir)
	}
	return filepath.ToSlash(rel)
}

func writeFile(t *testing.T, path, label string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", label, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", label, err)
	}
}

func splitFilter(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func matchesAnyFilter(path string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	matchedInclude := false
	haveInclude := false
	for _, f := range filters {
		if strings.HasPrefix(f, "-") {
			if strings.Contains(path, strings.TrimPrefix(f, "-")) {
				return false
			}
			continue
		}
		haveInclude = true
		if strings.Contains(path, f) {
			matchedInclude = true
		}
	}
	if haveInclude {
		return matchedInclude
	}
	return true
}

func TestMatchesAnyFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		filters []string
		want    bool
	}{
		{name: "no filters matches all", path: "fixture/compose-logs", want: true},
		{name: "positive include match", path: "fixture/compose-logs", filters: []string{"fixture/compose"}, want: true},
		{name: "positive include miss", path: "fixture/k3d-smoke", filters: []string{"fixture/compose"}, want: false},
		{name: "exclude only keeps other cases", path: "fixture/compose-logs", filters: []string{"-fixture/k3d"}, want: true},
		{name: "exclude only drops match", path: "fixture/k3d-smoke", filters: []string{"-fixture/k3d"}, want: false},
		{name: "include plus exclude prefers exclude", path: "fixture/k3d-smoke", filters: []string{"fixture/", "-fixture/k3d"}, want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := matchesAnyFilter(tc.path, tc.filters); got != tc.want {
				t.Fatalf("matchesAnyFilter(%q, %v) = %v, want %v", tc.path, tc.filters, got, tc.want)
			}
		})
	}
}

func runCase(t *testing.T, root, dir string) {
	t.Helper()

	spec := loadCaseFile(t, filepath.Join(dir, "test.yaml"))
	if caseNeedsSharedEnv(t, dir) {
		ensureSharedEnv(t)
	}
	tmp := t.TempDir()
	filesDir := filepath.Join(tmp, "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		t.Fatalf("mkdir files dir: %v", err)
	}

	ph := placeholders{
		RepoRoot:       root,
		CaseDir:        dir,
		CaseName:       spec.Name,
		RemoteOTLPHTTP: shared.RemoteOTLPHTTP,
		GCX:            shared.GCX,
		GCXConfig:      shared.GCXConfig,
		OATS:           shared.OATS,
	}

	copyFiles(t, filepath.Join(dir, "files"), filesDir, ph)

	configPath := filepath.Join(tmp, ".generated-oats.toml")
	if _, err := os.Stat(filepath.Join(filesDir, "oats.toml")); err == nil {
		configPath = filepath.Join(filesDir, "oats.toml")
	} else {
		writeFile(t, configPath, "generated oats.toml", []byte("cases = [\"files/oats.yaml\"]\n\n[meta]\nversion = 2\n"))
	}

	markExecutables(t, filesDir)

	stdout, stderr, exitCode := runCommand(t, spec.Run, ph, tmp, filesDir, configPath)
	assertOutput(t, spec.Expect, stdout, stderr, exitCode)
}

func runCommand(t *testing.T, run runSpec, ph placeholders, cwd, filesDir, configPath string) (string, string, int) {
	t.Helper()

	if strings.TrimSpace(run.Shell) != "" {
		cmd := exec.Command("bash", "-lc", expand(run.Shell, ph))
		cmd.Dir = cwd
		cmd.Env = baseEnv(filesDir, ph)
		cmd.Env = append(cmd.Env, renderEnv(run.Env, ph)...)
		return captureCommand(t, cmd)
	}

	command := strings.TrimSpace(run.Command)
	extraArgs := append([]string(nil), run.Args...)
	args := []string(nil)
	if command == "" {
		command = ph.OATS
		args = []string{
			"--config", configPath,
			"--gcx", ph.GCX,
			"--gcx-context", "local",
			"--no-cache",
			"--timeout", "10s",
			"--interval", "1s",
		}
		args = append(args, extraArgs...)
	} else {
		args = extraArgs
	}
	command = expand(command, ph)
	for i := range args {
		args[i] = expand(args[i], ph)
	}
	cmd := exec.Command(command, args...)
	cmd.Dir = ph.RepoRoot
	cmd.Env = baseEnv(filesDir, ph)
	cmd.Env = append(cmd.Env, renderEnv(run.Env, ph)...)
	return captureCommand(t, cmd)
}

func captureCommand(t *testing.T, cmd *exec.Cmd) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.String(), stderr.String(), exitErr.ExitCode()
	}
	t.Fatalf("run command: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	return "", "", 0
}

func discoverCases(t *testing.T, root string) []string {
	t.Helper()
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == "test.yaml" {
			dirs = append(dirs, filepath.Dir(path))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("discover cases: %v", err)
	}
	return dirs
}

func loadCaseFile(t *testing.T, path string) caseFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var spec caseFile
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if spec.Name == "" {
		t.Fatalf("%s: name is required", path)
	}
	return spec
}

func copyFiles(t *testing.T, srcDir, dstDir string, ph placeholders) {
	t.Helper()
	if _, err := os.Stat(srcDir); err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("stat %s: %v", srcDir, err)
	}
	err := filepath.WalkDir(srcDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dstDir, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, []byte(expand(string(data), ph)), 0o644)
	})
	if err != nil {
		t.Fatalf("copy files: %v", err)
	}
}

func markExecutables(t *testing.T, filesDir string) {
	t.Helper()
	_ = filepath.WalkDir(filesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return err
		}
		if strings.HasSuffix(d.Name(), ".sh") {
			if chmodErr := os.Chmod(path, 0o755); chmodErr != nil {
				t.Fatalf("chmod %s: %v", path, chmodErr)
			}
		}
		return nil
	})
	binDir := filepath.Join(filesDir, "bin")
	_ = filepath.WalkDir(binDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return err
		}
		if chmodErr := os.Chmod(path, 0o755); chmodErr != nil {
			t.Fatalf("chmod %s: %v", path, chmodErr)
		}
		return nil
	})
}

func assertOutput(t *testing.T, expect expectSpec, stdout, stderr string, exitCode int) {
	t.Helper()
	wantExit := 0
	if expect.ExitCode != nil {
		wantExit = *expect.ExitCode
	}
	if exitCode != wantExit {
		t.Fatalf("exit code: got %d want %d\nstdout:\n%s\nstderr:\n%s", exitCode, wantExit, stdout, stderr)
	}
	for _, want := range expect.StdoutContains {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q\nstdout:\n%s\nstderr:\n%s", want, stdout, stderr)
		}
	}
	for _, want := range expect.StdoutNotContains {
		if strings.Contains(stdout, want) {
			t.Fatalf("stdout unexpectedly contains %q\nstdout:\n%s\nstderr:\n%s", want, stdout, stderr)
		}
	}
	for _, want := range expect.StderrContains {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q\nstdout:\n%s\nstderr:\n%s", want, stdout, stderr)
		}
	}
}

func renderEnv(env map[string]string, ph placeholders) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+expand(v, ph))
	}
	return out
}

func baseEnv(filesDir string, ph placeholders) []string {
	env := append([]string(nil), os.Environ()...)
	env = replaceEnv(env, "GCX_CONFIG", ph.GCXConfig)
	binDir := filepath.Join(filesDir, "bin")
	if info, err := os.Stat(binDir); err == nil && info.IsDir() {
		pathValue := os.Getenv("PATH")
		if pathValue == "" {
			pathValue = binDir
		} else {
			pathValue = binDir + string(os.PathListSeparator) + pathValue
		}
		env = replaceEnv(env, "PATH", pathValue)
	}
	return env
}

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, prefix+value)
}

func expand(text string, ph placeholders) string {
	return strings.NewReplacer(
		"{{REPO_ROOT}}", ph.RepoRoot,
		"{{CASE_DIR}}", ph.CaseDir,
		"{{CASE_NAME}}", ph.CaseName,
		"{{REMOTE_OTLP_HTTP}}", ph.RemoteOTLPHTTP,
		"{{GCX}}", ph.GCX,
		"{{GCX_CONFIG}}", ph.GCXConfig,
		"{{OATS}}", ph.OATS,
	).Replace(text)
}
