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
	if got := defaultGCXDownloadPolicy(); got != gcxDownloadPolicyNever {
		t.Fatalf("defaultGCXDownloadPolicy with mise = %q, want %s", got, gcxDownloadPolicyNever)
	}
	t.Setenv("MISE_CONFIG_ROOT", "")
	t.Setenv("MISE_PROJECT_ROOT", "")
	t.Setenv("PATH", t.TempDir())
	if got := defaultGCXDownloadPolicy(); got != gcxDownloadPolicyAuto {
		t.Fatalf("defaultGCXDownloadPolicy without mise = %q, want %s", got, gcxDownloadPolicyAuto)
	}

	miseDir := t.TempDir()
	miseBin := filepath.Join(miseDir, "mise")
	if err := os.WriteFile(miseBin, []byte("mise"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", miseDir)
	if got := defaultGCXDownloadPolicy(); got != gcxDownloadPolicyNever {
		t.Fatalf("defaultGCXDownloadPolicy with mise on PATH = %q, want %s", got, gcxDownloadPolicyNever)
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

	oldVersion := MinimumGCXVersion
	MinimumGCXVersion = "0.4.3"
	t.Cleanup(func() { MinimumGCXVersion = oldVersion })

	fs := gcxRuntimeFlags(cacheDir, gcxDownloadPolicyAuto)
	got, err := resolveDefaultGCX(fs, "oats-gcx-test-not-on-path")
	if err != nil {
		t.Fatalf("resolveDefaultGCX: %v", err)
	}
	if got != target {
		t.Fatalf("resolveDefaultGCX = %q, want %q", got, target)
	}
}

func TestResolveDefaultGCXRejectsDisabledDownload(t *testing.T) {
	fs := gcxRuntimeFlags(t.TempDir(), gcxDownloadPolicyNever)
	if _, err := resolveDefaultGCX(fs, "oats-gcx-test-not-on-path"); err == nil {
		t.Fatal("expected disabled download error")
	}
}

func TestResolveDefaultGCXRequiresEmbeddedVersion(t *testing.T) {
	oldVersion := MinimumGCXVersion
	MinimumGCXVersion = ""
	t.Cleanup(func() { MinimumGCXVersion = oldVersion })

	fs := gcxRuntimeFlags(t.TempDir(), gcxDownloadPolicyAuto)
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

func TestGCXVersionAtLeast(t *testing.T) {
	tests := []struct {
		installed string
		minimum   string
		want      bool
	}{
		{installed: "gcx version 0.4.4 built from abc", minimum: "0.4.3", want: true},
		{installed: "v0.4.3", minimum: "0.4.3", want: true},
		{installed: "gcx version 0.4.2", minimum: "0.4.3", want: false},
		{installed: "gcx version 0.5.0", minimum: "0.4.3", want: true},
		{installed: "gcx version 0.4.3-rc.1", minimum: "0.4.3", want: false},
		{installed: "SNAPSHOT", minimum: "0.4.3", want: false},
	}
	for _, tt := range tests {
		if got := gcxVersionAtLeast(tt.installed, tt.minimum); got != tt.want {
			t.Errorf("gcxVersionAtLeast(%q, %q) = %v, want %v", tt.installed, tt.minimum, got, tt.want)
		}
	}
}

func TestResolveDefaultGCXUsesSufficientPathVersion(t *testing.T) {
	oldVersion := MinimumGCXVersion
	MinimumGCXVersion = "0.4.3"
	t.Cleanup(func() { MinimumGCXVersion = oldVersion })

	gcxBin := fakeGCXOnPath(t, "0.4.3")
	fs := gcxRuntimeFlags(t.TempDir(), gcxDownloadPolicyAuto)
	got, err := resolveDefaultGCX(fs, gcxBin)
	if err != nil {
		t.Fatalf("resolveDefaultGCX: %v", err)
	}
	if got != gcxBin {
		t.Fatalf("resolveDefaultGCX = %q, want %q", got, gcxBin)
	}
}

func TestResolveDefaultGCXFallsBackForOlderPathVersion(t *testing.T) {
	cacheDir := t.TempDir()
	target := filepath.Join(cacheDir, "tools", "gcx", "0.4.3", runtime.GOOS+"_"+runtime.GOARCH, "gcx")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("cached gcx"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldVersion := MinimumGCXVersion
	MinimumGCXVersion = "0.4.3"
	t.Cleanup(func() { MinimumGCXVersion = oldVersion })

	gcxBin := fakeGCXOnPath(t, "0.4.2")
	fs := gcxRuntimeFlags(cacheDir, gcxDownloadPolicyAuto)
	got, err := resolveDefaultGCX(fs, gcxBin)
	if err != nil {
		t.Fatalf("resolveDefaultGCX: %v", err)
	}
	if got != target {
		t.Fatalf("resolveDefaultGCX = %q, want cached minimum %q", got, target)
	}
}

func TestResolveDefaultGCXRejectsOlderPathVersionWhenDownloadsDisabled(t *testing.T) {
	oldVersion := MinimumGCXVersion
	MinimumGCXVersion = "0.4.3"
	t.Cleanup(func() { MinimumGCXVersion = oldVersion })

	gcxBin := fakeGCXOnPath(t, "0.4.2")
	fs := gcxRuntimeFlags(t.TempDir(), gcxDownloadPolicyNever)
	if _, err := resolveDefaultGCX(fs, gcxBin); err == nil {
		t.Fatal("expected minimum version error")
	}
}

func TestResolveDefaultGCXExplicitPathOverridesMinimum(t *testing.T) {
	oldVersion := MinimumGCXVersion
	MinimumGCXVersion = "0.4.3"
	t.Cleanup(func() { MinimumGCXVersion = oldVersion })

	gcxBin := fakeGCXOnPath(t, "0.4.2")
	fs := gcxRuntimeFlags(t.TempDir(), gcxDownloadPolicyNever)
	if err := fs.Set("gcx", gcxBin); err != nil {
		t.Fatal(err)
	}
	got, err := resolveDefaultGCX(fs, gcxBin)
	if err != nil {
		t.Fatalf("resolveDefaultGCX: %v", err)
	}
	if got != gcxBin {
		t.Fatalf("resolveDefaultGCX = %q, want %q", got, gcxBin)
	}
}

func gcxRuntimeFlags(cacheDir, downloadPolicy string) *pflag.FlagSet {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("gcx", "gcx", "")
	fs.String("gcx-download", downloadPolicy, "")
	fs.String("cache-dir", cacheDir, "")
	return fs
}

func fakeGCXOnPath(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	name := "gcx"
	contents := "#!/bin/sh\nprintf 'gcx version " + version + " built from test\\n'\n"
	permissions := 0o755
	if runtime.GOOS == "windows" {
		name += ".cmd"
		contents = "@echo off\r\necho gcx version " + version + " built from test\r\n"
		permissions = 0o644
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), os.FileMode(permissions)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return "gcx"
}
