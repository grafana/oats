package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/pflag"
)

func TestDefaultGCXDownloadPolicy(t *testing.T) {
	t.Setenv("MISE_CONFIG_ROOT", "/tmp/mise")
	if got := defaultGCXDownloadPolicy(); got != "never" {
		t.Fatalf("defaultGCXDownloadPolicy with mise = %q, want never", got)
	}
	t.Setenv("MISE_CONFIG_ROOT", "")
	t.Setenv("MISE_PROJECT_ROOT", "")
	t.Setenv("PATH", t.TempDir())
	if got := defaultGCXDownloadPolicy(); got != "auto" {
		t.Fatalf("defaultGCXDownloadPolicy without mise = %q, want auto", got)
	}

	miseDir := t.TempDir()
	miseBin := filepath.Join(miseDir, "mise")
	if err := os.WriteFile(miseBin, []byte("mise"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", miseDir)
	if got := defaultGCXDownloadPolicy(); got != "never" {
		t.Fatalf("defaultGCXDownloadPolicy with mise on PATH = %q, want never", got)
	}
}

func TestIsMiseInstallPath(t *testing.T) {
	for _, path := range []string{
		"/home/gregor/.local/share/mise/installs/aqua-grafana-gcx/0.4.3/gcx",
		"/workspace/.mise/installs/aqua-grafana-oats/0.7.0/oats",
		`C:\Users\gregor\AppData\Local\mise\installs\oats\0.7.0\oats.exe`,
	} {
		if !isMiseInstallPath(path) {
			t.Errorf("isMiseInstallPath(%q) = false, want true", path)
		}
	}
	if isMiseInstallPath("/usr/local/bin/oats") {
		t.Error("isMiseInstallPath(/usr/local/bin/oats) = true, want false")
	}
}

func TestResolveDefaultGCXUsesEmbeddedVersion(t *testing.T) {
	cacheDir := t.TempDir()
	target := filepath.Join(cacheDir, "tools", "gcx", "0.4.3", runtime.GOOS+"_"+runtime.GOARCH, "gcx")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("cached gcx"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldVersion := DefaultGCXVersion
	DefaultGCXVersion = "0.4.3"
	t.Cleanup(func() { DefaultGCXVersion = oldVersion })

	fs := gcxRuntimeFlags(cacheDir, "auto")
	got, err := resolveDefaultGCX(fs, "oats-gcx-test-not-on-path")
	if err != nil {
		t.Fatalf("resolveDefaultGCX: %v", err)
	}
	if got != target {
		t.Fatalf("resolveDefaultGCX = %q, want %q", got, target)
	}
}

func TestResolveDefaultGCXRejectsDisabledDownload(t *testing.T) {
	fs := gcxRuntimeFlags(t.TempDir(), "never")
	if _, err := resolveDefaultGCX(fs, "oats-gcx-test-not-on-path"); err == nil {
		t.Fatal("expected disabled download error")
	}
}

func TestResolveDefaultGCXRequiresEmbeddedVersion(t *testing.T) {
	oldVersion := DefaultGCXVersion
	DefaultGCXVersion = ""
	t.Cleanup(func() { DefaultGCXVersion = oldVersion })

	fs := gcxRuntimeFlags(t.TempDir(), "auto")
	if _, err := resolveDefaultGCX(fs, "oats-gcx-test-not-on-path"); err == nil {
		t.Fatal("expected missing embedded version error")
	}
}

func TestResolveDefaultGCXRejectsInvalidPolicy(t *testing.T) {
	fs := gcxRuntimeFlags(t.TempDir(), "sometimes")
	if _, err := resolveDefaultGCX(fs, "oats-gcx-test-not-on-path"); err == nil {
		t.Fatal("expected invalid policy error")
	}
}

func gcxRuntimeFlags(cacheDir, downloadPolicy string) *pflag.FlagSet {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("gcx-download", downloadPolicy, "")
	fs.String("cache-dir", cacheDir, "")
	return fs
}
