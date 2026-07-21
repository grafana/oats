package fixture

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grafana/oats/casefile"
	"github.com/grafana/oats/discovery"
	"github.com/grafana/oats/testhelpers/container"
)

func TestComposeHelpers(t *testing.T) {
	if got := portString(4318); got != "4318" {
		t.Fatalf("portString = %q", got)
	}
	if got := composeProjectName(discovery.Plan{Name: "A useful/test name"}); !strings.HasPrefix(got, "oats-a-useful-test-name-") {
		t.Fatalf("composeProjectName = %q", got)
	}
	if got := composeProjectName(discovery.Plan{}); !strings.HasPrefix(got, "oats-oats-") {
		t.Fatalf("empty composeProjectName = %q", got)
	}

	plan := discovery.Plan{FixtureSourceDir: "/cases", Fixture: casefile.FixtureConfig{Compose: &casefile.ComposeFixture{File: "compose.yml"}}}
	if got := extraComposeFiles(plan); len(got) != 1 || got[0] != "/cases/compose.yml" {
		t.Fatalf("extraComposeFiles file = %#v", got)
	}
	plan.Fixture.Compose.File = ""
	plan.Fixture.Compose.Files = []string{"base.yml", "override.yml"}
	if got := extraComposeFiles(plan); len(got) != 2 || got[1] != "/cases/override.yml" {
		t.Fatalf("extraComposeFiles files = %#v", got)
	}
	plan.Fixture.Compose.Files = nil
	if got := extraComposeFiles(plan); got != nil {
		t.Fatalf("extraComposeFiles empty = %#v", got)
	}

	rt := Runtime{
		ComposeFiles:     []string{"/cases/compose.yml"},
		ContainerRuntime: "podman",
		ComposeProject:   "oats-case",
		GrafanaURL:       "http://127.0.0.1:3000",
		OTLPHTTP:         "http://127.0.0.1:4318",
		PyroscopeURL:     "http://127.0.0.1:4040",
	}
	env := composeCheckEnv(plan, rt)
	for _, want := range []string{
		"OATS_FIXTURE_TYPE=compose",
		"OATS_CONTAINER_RUNTIME=podman",
		"COMPOSE_PROJECT_NAME=oats-case",
		"COMPOSE_FILE=/cases/compose.yml",
	} {
		if !containsString(env, want) {
			t.Errorf("composeCheckEnv missing %q in %#v", want, env)
		}
	}
	if !containsString(env, "OATS_COMPOSE_FILE_ARGS=-f '/cases/compose.yml'") {
		t.Errorf("composeCheckEnv missing quoted file args in %#v", env)
	}

	noFiles := composeCheckEnv(plan, Runtime{})
	if len(noFiles) != 1 || noFiles[0] != "OATS_FIXTURE_TYPE=compose" {
		t.Fatalf("composeCheckEnv without files = %#v", noFiles)
	}
}

func TestComposePortHelpers(t *testing.T) {
	for input, want := range map[string]bool{
		"8080:80":        true,
		"127.0.0.1:8080": false,
		"8080":           false,
		"8080:0":         true,
		"127.0.0.1:0:80": false,
	} {
		if got := fixedShortPortMapping(input); got != want {
			t.Errorf("fixedShortPortMapping(%q) = %v, want %v", input, got, want)
		}
	}

	for _, tc := range []struct {
		input, host, port string
		wantErr           bool
	}{
		{input: "127.0.0.1:4318", host: "127.0.0.1", port: "4318"},
		{input: "[::1]:4318", host: "::1", port: "4318"},
		{input: "missing-port", wantErr: true},
		{input: "[::1]", wantErr: true},
	} {
		host, port, err := splitComposeHostPort(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("splitComposeHostPort(%q) succeeded", tc.input)
			}
			continue
		}
		if err != nil || host != tc.host || port != tc.port {
			t.Errorf("splitComposeHostPort(%q) = %q, %q, %v", tc.input, host, port, err)
		}
	}
}

func TestFixtureUtilityFunctions(t *testing.T) {
	path, err := writeLocalGCXConfig("http://grafana:3000")
	if err != nil {
		t.Fatalf("writeLocalGCXConfig: %v", err)
	}
	defer func() { _ = os.Remove(path) }()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "server: http://grafana:3000") {
		t.Fatalf("unexpected gcx config: %s", data)
	}

	port, err := findFreePort()
	if err != nil || port == 0 {
		t.Fatalf("findFreePort = %d, %v", port, err)
	}
	if err := removeIfExists(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatalf("removeIfExists missing = %v", err)
	}

	var calls []string
	cleanup := chainCleanup(func() error {
		calls = append(calls, "first")
		return nil
	}, func() error {
		calls = append(calls, "second")
		return errors.New("cleanup failed")
	})
	if err := cleanup(); err == nil || strings.Join(calls, ",") != "first,second" {
		t.Fatalf("chainCleanup = %v calls=%v", err, calls)
	}
	if err := chainCleanup(nil, nil)(); err != nil {
		t.Fatalf("empty chainCleanup = %v", err)
	}

	if err := startFixture(&fakeHandle{}); err != nil {
		t.Fatalf("startFixture: %v", err)
	}
	if err := startFixture(struct{ Handle }{}); err == nil {
		t.Fatal("startFixture accepted a non-startable handle")
	}
	if err := startFixture(&fakeHandle{upErr: errors.New("up failed")}); err == nil {
		t.Fatal("startFixture swallowed startup error")
	}

	if err := WaitForReady(discovery.Plan{Fixture: casefile.FixtureConfig{Remote: &casefile.RemoteFixture{}}}, Runtime{}); err != nil {
		t.Fatalf("WaitForReady remote: %v", err)
	}
}

func TestComposeAndReadinessHelpers(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(bin, "docker")
	if err := os.WriteFile(command, []byte(`#!/bin/sh
case "$*" in
*exec*) printf 'grafana-token\n' ;;
*) printf '127.0.0.1:5432\n' ;;
esac
`), 0o700); err != nil {
		t.Fatal(err)
	}
	kubectl := filepath.Join(bin, "kubectl")
	if err := os.WriteFile(kubectl, []byte("#!/bin/sh\nprintf 'k3d-token\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	files := []string{"compose.yml"}
	if got, err := composePort(container.Docker, files, nil, "lgtm", "3000"); err != nil || got != "5432" {
		t.Fatalf("composePort = %q, %v", got, err)
	}
	plan := discovery.Plan{
		FixtureSourceDir: dir,
		Fixture:          casefile.FixtureConfig{Compose: &casefile.ComposeFixture{Template: "none", File: "compose.yml"}},
	}
	if got, err := readComposeGrafanaToken(plan, container.Docker); err != nil || got != "grafana-token\n" {
		t.Fatalf("readComposeGrafanaToken = %q, %v", got, err)
	}
	if got, err := readK3DGrafanaToken(); err != nil || got != "k3d-token\n" {
		t.Fatalf("readK3DGrafanaToken = %q, %v", got, err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if err := waitForHTTP(server.URL, time.Second); err != nil {
		t.Fatalf("waitForHTTP: %v", err)
	}
	if err := WaitForReady(discovery.Plan{Fixture: casefile.FixtureConfig{Compose: &casefile.ComposeFixture{}}}, Runtime{
		GrafanaURL: server.URL,
		OTLPHTTP:   server.URL,
	}); err != nil {
		t.Fatalf("WaitForReady compose: %v", err)
	}
	if _, _, err := StartWithOptions(context.Background(), discovery.Plan{
		Fixture: casefile.FixtureConfig{Remote: &casefile.RemoteFixture{Endpoint: "remote"}},
	}, Options{ContainerRuntime: "podman"}); err == nil || !strings.Contains(err.Error(), "k3d fixtures require Docker") {
		t.Fatalf("StartWithOptions Podman non-Compose error = %v", err)
	}
}
