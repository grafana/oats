package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoteGrafanaURLFromExplicitGCXConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gcx.yaml")
	if err := os.WriteFile(path, []byte(`current-context: remote
contexts:
  remote:
    grafana:
      server: https://grafana.example.test/
  other:
    grafana:
      server: http://other.example.test
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GCX_CONFIG", path)

	got, err := remoteGrafanaURL("remote")
	if err != nil {
		t.Fatalf("remoteGrafanaURL: %v", err)
	}
	if got != "https://grafana.example.test" {
		t.Fatalf("remoteGrafanaURL = %q, want trimmed server URL", got)
	}

	got, err = remoteGrafanaURL("other")
	if err != nil {
		t.Fatalf("remoteGrafanaURL other: %v", err)
	}
	if got != "http://other.example.test" {
		t.Fatalf("remoteGrafanaURL other = %q", got)
	}
}

func TestRemoteGrafanaURLUsesCurrentContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gcx.yaml")
	if err := os.WriteFile(path, []byte(`current-context: local
contexts:
  local:
    grafana:
      server: http://127.0.0.1:3000
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GCX_CONFIG", path)

	got, err := remoteGrafanaURL("")
	if err != nil {
		t.Fatalf("remoteGrafanaURL: %v", err)
	}
	if got != "http://127.0.0.1:3000" {
		t.Fatalf("remoteGrafanaURL = %q", got)
	}
}

func TestRemoteGrafanaURLRejectsMissingServerInExplicitConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gcx.yaml")
	if err := os.WriteFile(path, []byte("contexts:\n  remote: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GCX_CONFIG", path)

	if _, err := remoteGrafanaURL("remote"); err == nil {
		t.Fatal("expected missing grafana.server error")
	}
}

func TestRemoteGrafanaURLWithoutConfigIsOptional(t *testing.T) {
	t.Setenv("GCX_CONFIG", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())

	got, err := remoteGrafanaURL("remote")
	if err != nil {
		t.Fatalf("remoteGrafanaURL without config: %v", err)
	}
	if got != "" {
		t.Fatalf("remoteGrafanaURL without config = %q, want empty", got)
	}
}
