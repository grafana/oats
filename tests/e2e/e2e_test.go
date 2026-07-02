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
	RemoteOTLPHTTP string
	GCX            string
	GCXConfig      string
	OATS           string
}

var shared sharedEnv

func TestMain(m *testing.M) {
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "find repo root: %v\n", err)
		os.Exit(1)
	}
	shared.RepoRoot = root
	if runtime.GOOS != "windows" {
		if err := setupSharedEnv(&shared); err != nil {
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
	tmp, err := os.MkdirTemp("", "oats-e2e-")
	if err != nil {
		return err
	}
	env.TempDir = tmp
	env.Project = fmt.Sprintf("oatse2e%d", os.Getpid())
	env.ComposeFile = filepath.Join(tmp, "docker-compose.yml")
	const composeBody = `services:
  lgtm:
    image: docker.io/grafana/otel-lgtm:latest
    ports:
      - "3000:3000"
      - "4317:4317"
      - "4318:4318"
      - "3200:3200"
      - "4040:4040"
      - "9090:9090"
`
	if err := os.WriteFile(env.ComposeFile, []byte(composeBody), 0o644); err != nil {
		return err
	}
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
	const gcxConfig = `current-context: local
contexts:
  local:
    grafana:
      server: http://localhost:3000
      user: admin
      password: admin
      org-id: 1
      auth-method: basic
    datasources:
      prometheus: prometheus
      loki: loki
      tempo: tempo
      pyroscope: pyroscope
`
	if err := os.WriteFile(env.GCXConfig, []byte(gcxConfig), 0o600); err != nil {
		return err
	}
	up := exec.Command("docker", "compose", "-p", env.Project, "-f", env.ComposeFile, "up", "-d")
	up.Stdout = os.Stdout
	up.Stderr = os.Stderr
	if err := up.Run(); err != nil {
		return fmt.Errorf("start shared lgtm: %w", err)
	}
	env.RemoteOTLPHTTP = "http://localhost:4318"
	if err := waitForHTTP("http://localhost:3000/api/health", 2*time.Minute); err != nil {
		return err
	}
	return nil
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
	for _, f := range filters {
		if strings.Contains(path, f) {
			return true
		}
	}
	return false
}

func runCase(t *testing.T, root, dir string) {
	t.Helper()

	spec := loadCaseFile(t, filepath.Join(dir, "test.yaml"))
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
	for _, rel := range []string{"verify.sh", "setup.sh"} {
		path := filepath.Join(filesDir, rel)
		if _, err := os.Stat(path); err == nil {
			if chmodErr := os.Chmod(path, 0o755); chmodErr != nil {
				t.Fatalf("chmod %s: %v", path, chmodErr)
			}
		}
	}
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
