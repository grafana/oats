package fixture

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/oats/casefile"
	"github.com/grafana/oats/discovery"
	"github.com/grafana/oats/testhelpers"
	"github.com/grafana/oats/testhelpers/container"
)

const (
	lgtmComposeService = "lgtm"
)

func portString(port int) string {
	return strconv.Itoa(port)
}

func startCompose(plan discovery.Plan, engine container.Engine) (Handle, Runtime, error) {
	compose := plan.Fixture.Compose
	composeFiles, cleanup, err := resolveComposeFiles(plan.FixtureSourceDir, compose)
	if err != nil {
		return nil, Runtime{}, err
	}
	project := composeProjectName(plan)
	suiteEnv := append([]string(nil), compose.Env...)
	suiteEnv = append(suiteEnv, "COMPOSE_PROJECT_NAME="+project)
	suite, err := newComposeSuite(composeFiles, suiteEnv, engine)
	if err != nil {
		if cleanup != nil {
			_ = cleanup()
		}
		return nil, Runtime{}, err
	}
	if err := startSuiteFixture(suite); err != nil {
		if cleanup != nil {
			_ = cleanup()
		}
		return nil, Runtime{}, err
	}
	// fail tears the started suite (and any file cleanup) down before returning.
	fail := func(err error) (Handle, Runtime, error) {
		_ = suite.Close()
		if cleanup != nil {
			_ = cleanup()
		}
		return nil, Runtime{}, err
	}
	grafanaPort, err := lookupComposePort(engine, composeFiles, suiteEnv, lgtmComposeService, portString(testhelpers.GrafanaHTTPPort))
	if err != nil {
		return fail(err)
	}
	otlpPort, err := lookupComposePort(engine, composeFiles, suiteEnv, lgtmComposeService, portString(testhelpers.OTLPHTTPPort))
	if err != nil {
		return fail(err)
	}
	pyroscopePort, err := lookupComposePort(engine, composeFiles, suiteEnv, lgtmComposeService, portString(testhelpers.PyroscopeHTTPPort))
	if err != nil {
		return fail(err)
	}
	rt := Runtime{
		GrafanaURL:       "http://" + testhelpers.LocalhostIPv4 + ":" + grafanaPort,
		OTLPHTTP:         "http://" + testhelpers.LocalhostIPv4 + ":" + otlpPort,
		PyroscopeURL:     "http://" + testhelpers.LocalhostIPv4 + ":" + pyroscopePort,
		ComposeFiles:     composeFiles,
		ComposeProject:   project,
		ContainerRuntime: string(engine),
	}
	// When the fixture manages the app (app_service + app_port), resolve the host
	// port docker published for it. This lets the app bind an ephemeral host port
	// (127.0.0.1::<port>) instead of a fixed one, which is what makes app-seed
	// suites parallel-safe.
	if plan.Fixture.HasManagedApp() {
		appPort, portErr := lookupComposePort(engine, composeFiles, suiteEnv, compose.AppService, strconv.Itoa(compose.AppPort))
		if portErr != nil {
			return fail(fmt.Errorf("resolve app host port for service %q: %w", compose.AppService, portErr))
		}
		p, convErr := strconv.Atoi(appPort)
		if convErr != nil {
			return fail(fmt.Errorf("invalid app host port %q for service %q: %w", appPort, compose.AppService, convErr))
		}
		rt.AppHostPort = p
	}
	cfg, cfgErr := writeLocalGCXConfig(rt.GrafanaURL)
	if cfgErr != nil {
		return fail(fmt.Errorf("write local gcx config: %w", cfgErr))
	}
	rt.GCXConfig = cfg
	cleanup = chainCleanup(func() error { return removeIfExists(cfg) }, cleanup)
	rt.CustomCheckEnv = composeCheckEnv(plan, rt)
	rt.ParallelSafe, rt.ParallelDisabled = SupportsParallel(plan)
	return composeFixture{suite: suite, cleanup: cleanup}, rt, nil
}

func resolveComposeFiles(sourceDir string, compose *casefile.ComposeFixture) ([]string, func() error, error) {
	var files []string
	var cleanup func() error
	template := compose.EffectiveTemplate()
	switch template {
	case "lgtm":
		f, err := writeBuiltinLGTMCompose(sourceDir)
		if err != nil {
			return nil, nil, err
		}
		files = append(files, f)
		cleanup = func() error { return os.Remove(f) }
	case "none":
		// Bring-your-own-stack: no builtin is booted, only the user's file/files.
	default:
		return nil, nil, fmt.Errorf("unsupported compose template %q", compose.Template)
	}
	switch {
	case compose.File != "":
		files = append(files, filepath.Join(sourceDir, compose.File))
	case len(compose.Files) > 0:
		for _, file := range compose.Files {
			files = append(files, filepath.Join(sourceDir, file))
		}
	case template == "none":
		return nil, nil, fmt.Errorf("compose template=none requires file or files")
	}
	return files, cleanup, nil
}

func writeBuiltinLGTMCompose(sourceDir string) (string, error) {
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(sourceDir, ".oats.lgtm.*.compose.yml")
	if err != nil {
		return "", err
	}
	path := f.Name()
	body := `services:
  lgtm:
    image: ${LGTM_IMAGE:-docker.io/grafana/otel-lgtm:latest}
    ports:
      - "` + testhelpers.LocalhostIPv4 + `::` + portString(testhelpers.GrafanaHTTPPort) + `"
      - "` + testhelpers.LocalhostIPv4 + `::` + portString(testhelpers.OTLPGRPCPort) + `"
      - "` + testhelpers.LocalhostIPv4 + `::` + portString(testhelpers.OTLPHTTPPort) + `"
      - "` + testhelpers.LocalhostIPv4 + `::` + portString(testhelpers.TempoHTTPPort) + `"
      - "` + testhelpers.LocalhostIPv4 + `::` + portString(testhelpers.PyroscopeHTTPPort) + `"
      - "` + testhelpers.LocalhostIPv4 + `::` + portString(testhelpers.PrometheusHTTPPort) + `"
`
	if _, err := f.WriteString(body); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func composeCheckEnv(plan discovery.Plan, rt Runtime) []string {
	files := rt.ComposeFiles
	if len(files) == 0 {
		return []string{"OATS_FIXTURE_TYPE=compose"}
	}
	return []string{
		"OATS_FIXTURE_TYPE=compose",
		"OATS_CONTAINER_RUNTIME=" + rt.ContainerRuntime,
		"COMPOSE_PROJECT_NAME=" + rt.ComposeProject,
		"COMPOSE_FILE=" + strings.Join(files, string(os.PathListSeparator)),
		"OATS_COMPOSE_FILE_ARGS=" + composeFileArgs(files),
		"OATS_GRAFANA_URL=" + rt.GrafanaURL,
		"OATS_OTLP_HTTP=" + rt.OTLPHTTP,
		"OATS_PYROSCOPE_URL=" + rt.PyroscopeURL,
	}
}

func composeFileArgs(files []string) string {
	var parts []string
	for _, f := range files {
		parts = append(parts, "-f", shellQuote(f))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func composeProjectName(plan discovery.Plan) string {
	name := strings.ToLower(plan.Name)
	if name == "" {
		name = "oats"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "oats"
	}
	if len(slug) > 32 {
		slug = slug[:32]
	}
	return fmt.Sprintf("oats-%s-%d", slug, time.Now().UnixNano())
}

func extraComposeFiles(plan discovery.Plan) []string {
	compose := plan.Fixture.Compose
	switch {
	case compose.File != "":
		return []string{filepath.Join(plan.FixtureSourceDir, compose.File)}
	case len(compose.Files) > 0:
		files := make([]string, 0, len(compose.Files))
		for _, file := range compose.Files {
			files = append(files, filepath.Join(plan.FixtureSourceDir, file))
		}
		return files
	default:
		return nil
	}
}

func composeFilePublishesFixedHostPorts(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	lines := strings.Split(string(data), "\n")
	inPorts := false
	portsIndent := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if inPorts && indent <= portsIndent {
			inPorts = false
		}
		if strings.HasPrefix(trimmed, "ports:") {
			inPorts = true
			portsIndent = indent
			continue
		}
		if !inPorts {
			continue
		}
		if strings.Contains(trimmed, "published:") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "published:"))
			if value != "" && value != "0" {
				return true, nil
			}
			continue
		}
		if !strings.HasPrefix(trimmed, "-") {
			continue
		}
		value := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "-")), `"'`)
		if value == "" {
			continue
		}
		if fixedShortPortMapping(value) {
			return true, nil
		}
	}
	return false, nil
}

func fixedShortPortMapping(value string) bool {
	if !strings.Contains(value, ":") {
		return false
	}
	parts := strings.Split(value, ":")
	if len(parts) < 2 {
		return false
	}
	hostPart := strings.Trim(parts[len(parts)-2], "[]")
	if _, err := strconv.Atoi(hostPart); err == nil && hostPart != "0" {
		return true
	}
	return false
}

func readComposeGrafanaToken(plan discovery.Plan, engine container.Engine) (string, error) {
	compose := plan.Fixture.Compose
	files, _, err := resolveComposeFiles(plan.FixtureSourceDir, compose)
	if err != nil {
		return "", err
	}
	args := engine.ComposeArgs()
	for _, f := range files {
		args = append(args, "-f", f)
	}
	args = append(args, "exec", "-T", lgtmComposeService, "sh", "-c", "cat /tmp/grafana-sa-token 2>/dev/null || true")
	cmd := exec.Command(engine.Binary(), args...)
	cmd.Env = append(cmd.Environ(), compose.Env...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func composePort(engine container.Engine, files []string, env []string, service, containerPort string) (string, error) {
	args := engine.ComposeArgs()
	for _, f := range files {
		args = append(args, "-f", f)
	}
	args = append(args, "port", service, containerPort)
	cmd := exec.Command(engine.Binary(), args...)
	cmd.Env = append(cmd.Environ(), env...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	host, port, err := splitComposeHostPort(strings.TrimSpace(string(out)))
	if err != nil {
		return "", err
	}
	if host == "" || port == "" {
		return "", fmt.Errorf("invalid compose port output %q", strings.TrimSpace(string(out)))
	}
	return port, nil
}

func splitComposeHostPort(addr string) (string, string, error) {
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
