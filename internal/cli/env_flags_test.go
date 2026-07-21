package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func TestApplyEnvFlags(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.Duration("timeout", 30*time.Second, "timeout")
	fs.String("gcx", "gcx", "gcx binary")
	fs.String("gcx-download", gcxDownloadPolicyAuto, "gcx download policy")
	fs.Bool("no-cache", false, "disable cache")

	t.Setenv("OATS_TIMEOUT", "2s")
	t.Setenv("OATS_GCX", "/opt/tools/gcx")
	t.Setenv("OATS_GCX_DOWNLOAD", gcxDownloadPolicyNever)
	t.Setenv("OATS_NO_CACHE", "true")

	if err := applyEnvFlags(fs); err != nil {
		t.Fatalf("applyEnvFlags: %v", err)
	}

	if got := flagDur(fs, "timeout"); got != 2*time.Second {
		t.Fatalf("timeout = %s, want 2s", got)
	}
	if got := flagStr(fs, "gcx"); got != "/opt/tools/gcx" {
		t.Fatalf("gcx = %q, want /opt/tools/gcx", got)
	}
	if got := flagStr(fs, "gcx-download"); got != gcxDownloadPolicyNever {
		t.Fatalf("gcx-download = %q, want %s", got, gcxDownloadPolicyNever)
	}
	if !flagBool(fs, "no-cache") {
		t.Fatal("no-cache = false, want true")
	}
	if !fs.Lookup("timeout").Changed {
		t.Fatal("environment-supplied flag should be marked changed")
	}
}

func TestApplyEnvFlagsKeepGCXFlagsIndependent(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.Duration("timeout", 30*time.Second, "timeout")
	fs.String("gcx", "gcx", "gcx binary")
	fs.String("gcx-version", "", "gcx version")

	if err := fs.Parse([]string{"--timeout", "3s", "--gcx-version", "0.4.3"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Setenv("OATS_TIMEOUT", "2s")
	t.Setenv("OATS_GCX", "/opt/tools/gcx")

	if err := applyEnvFlags(fs); err != nil {
		t.Fatalf("applyEnvFlags: %v", err)
	}

	if got := flagDur(fs, "timeout"); got != 3*time.Second {
		t.Fatalf("timeout = %s, want command-line value 3s", got)
	}
	if got := flagStr(fs, "gcx"); got != "/opt/tools/gcx" {
		t.Fatalf("gcx = %q, want environment value", got)
	}
	if got := flagStr(fs, "gcx-version"); got != "0.4.3" {
		t.Fatalf("gcx-version = %q, want command-line value 0.4.3", got)
	}
}

func TestApplyEnvFlagsInvalidValue(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.Duration("timeout", 30*time.Second, "timeout")
	t.Setenv("OATS_TIMEOUT", "not-a-duration")

	err := applyEnvFlags(fs)
	if err == nil || !strings.Contains(err.Error(), "invalid OATS_TIMEOUT") {
		t.Fatalf("applyEnvFlags error = %v, want invalid OATS_TIMEOUT", err)
	}
}

func TestFlagEnvName(t *testing.T) {
	if got := flagEnvName("absent-timeout"); got != "OATS_ABSENT_TIMEOUT" {
		t.Fatalf("flagEnvName = %q, want OATS_ABSENT_TIMEOUT", got)
	}
}
