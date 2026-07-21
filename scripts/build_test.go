package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildScripts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("build scripts require bash")
	}
	root := filepath.Dir(mustCallerFile(t))

	versionCmd := exec.Command("bash", filepath.Join(root, "gcx-version.sh"))
	versionCmd.Dir = filepath.Dir(root)
	versionOutput, err := versionCmd.Output()
	if err != nil {
		t.Fatalf("gcx-version.sh: %v", err)
	}
	version := strings.TrimSpace(string(versionOutput))
	if version == "" || strings.HasPrefix(version, "v") {
		t.Fatalf("gcx-version.sh returned invalid version %q", version)
	}

	output := filepath.Join(t.TempDir(), "oats")
	projectRoot := filepath.Dir(root)
	buildCmd := exec.Command("mise", "-C", projectRoot, "run", "build", "--", output)
	buildCmd.Dir = projectRoot
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("mise run build: %v\n%s", err, output)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatalf("built oats: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("built oats is not executable: mode %s", info.Mode())
	}
}

func mustCallerFile(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return file
}
