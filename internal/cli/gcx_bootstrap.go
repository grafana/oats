package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

var gcxReleaseBaseURL = "https://github.com/grafana/gcx/releases/download"

var gcxHTTPClient = &http.Client{Timeout: 10 * time.Minute}

func defaultGCXDownloadPolicy() string {
	// A mise-managed environment is expected to install its declared tools and
	// should not silently reach out to GitHub if that setup is incomplete. The
	// OATS_GCX_DOWNLOAD/--gcx-download override remains available when desired.
	if isMiseEnvironment() {
		return "never"
	}
	return "auto"
}

func isMiseEnvironment() bool {
	if os.Getenv("MISE_CONFIG_ROOT") != "" || os.Getenv("MISE_PROJECT_ROOT") != "" {
		return true
	}
	if executable, err := os.Executable(); err == nil && isMiseInstallPath(executable) {
		return true
	}
	_, err := exec.LookPath("mise")
	return err == nil
}

func isMiseInstallPath(path string) bool {
	// Handle both separators regardless of the host OS. This keeps paths from
	// another platform (for example, a Windows path in a Unix-side test) from
	// being misclassified; filepath.ToSlash only normalizes the host separator.
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
	for i := 0; i+1 < len(parts); i++ {
		if (parts[i] == "mise" || parts[i] == ".mise") && parts[i+1] == "installs" {
			return true
		}
	}
	return false
}

func resolveDefaultGCX(fs *pflag.FlagSet, gcxBin string) (string, error) {
	policy, err := fs.GetString("gcx-download")
	if err != nil {
		return "", err
	}
	policy = strings.ToLower(strings.TrimSpace(policy))
	if policy != "auto" && policy != "never" {
		return "", fmt.Errorf("invalid --gcx-download %q (want auto or never)", policy)
	}

	if _, err := exec.LookPath(gcxBin); err == nil {
		// An explicit --gcx is an intentional override, so do not second-guess
		// its version. The embedded minimum protects only the implicit PATH lookup
		// from accidentally selecting an older local installation.
		if fs.Changed("gcx") || MinimumGCXVersion == "" {
			return gcxBin, nil
		}

		installedVersion := gcxVersion(gcxBin)
		if gcxVersionAtLeast(installedVersion, MinimumGCXVersion) {
			return gcxBin, nil
		}
		if policy == "never" {
			if installedVersion == "" {
				return "", fmt.Errorf("gcx on PATH did not report a version (minimum %s; automatic download is disabled)", MinimumGCXVersion)
			}
			return "", fmt.Errorf("gcx on PATH is %s, but oats requires at least %s (automatic download is disabled; install a newer gcx or set --gcx to override)", installedVersion, MinimumGCXVersion)
		}

		if installedVersion == "" {
			fmt.Fprintf(os.Stderr, "gcx on PATH did not report a parseable version; downloading minimum gcx %s\n", MinimumGCXVersion)
		} else {
			fmt.Fprintf(os.Stderr, "gcx on PATH is %s; downloading minimum gcx %s\n", installedVersion, MinimumGCXVersion)
		}
		cacheDir, err := fs.GetString("cache-dir")
		if err != nil {
			return "", err
		}
		return bootstrapGCX(MinimumGCXVersion, cacheDir)
	}

	if fs.Changed("gcx") {
		return "", fmt.Errorf("gcx binary %q was not found (set --gcx to its path)", gcxBin)
	}
	if policy == "never" {
		return "", fmt.Errorf("gcx was not found on PATH and automatic download is disabled (install gcx, set --gcx, or use --gcx-download auto)")
	}
	if MinimumGCXVersion == "" {
		return "", fmt.Errorf("gcx was not found on PATH and this oats build has no embedded minimum gcx version (install gcx or pass --gcx-version)")
	}

	fmt.Fprintf(os.Stderr, "gcx was not found on PATH; downloading minimum gcx %s\n", MinimumGCXVersion)
	cacheDir, err := fs.GetString("cache-dir")
	if err != nil {
		return "", err
	}
	return bootstrapGCX(MinimumGCXVersion, cacheDir)
}

var gcxVersionPattern = regexp.MustCompile(`(?:^|[^0-9])v?([0-9]+)\.([0-9]+)\.([0-9]+)(?:-([0-9A-Za-z.-]+))?`)

type parsedGCXVersion struct {
	major      int
	minor      int
	patch      int
	prerelease string
}

func parseGCXVersion(value string) (parsedGCXVersion, bool) {
	match := gcxVersionPattern.FindStringSubmatch(value)
	if len(match) == 0 {
		return parsedGCXVersion{}, false
	}
	major, errMajor := strconv.Atoi(match[1])
	minor, errMinor := strconv.Atoi(match[2])
	patch, errPatch := strconv.Atoi(match[3])
	if errMajor != nil || errMinor != nil || errPatch != nil {
		return parsedGCXVersion{}, false
	}
	return parsedGCXVersion{
		major:      major,
		minor:      minor,
		patch:      patch,
		prerelease: match[4],
	}, true
}

func gcxVersionAtLeast(installed, minimum string) bool {
	got, gotOK := parseGCXVersion(installed)
	want, wantOK := parseGCXVersion(minimum)
	if !gotOK || !wantOK {
		return false
	}
	if got.major != want.major {
		return got.major > want.major
	}
	if got.minor != want.minor {
		return got.minor > want.minor
	}
	if got.patch != want.patch {
		return got.patch > want.patch
	}
	return compareGCXPrerelease(got.prerelease, want.prerelease) >= 0
}

func compareGCXPrerelease(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}

	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		aNumber, aErr := strconv.Atoi(aParts[i])
		bNumber, bErr := strconv.Atoi(bParts[i])
		switch {
		case aErr == nil && bErr == nil && aNumber != bNumber:
			if aNumber < bNumber {
				return -1
			}
			return 1
		case aErr == nil && bErr != nil:
			return -1
		case aErr != nil && bErr == nil:
			return 1
		case aParts[i] != bParts[i]:
			if aParts[i] < bParts[i] {
				return -1
			}
			return 1
		}
	}
	if len(aParts) < len(bParts) {
		return -1
	}
	return 1
}

// bootstrapGCX downloads a verified gcx release into the user's cache and
// returns the executable path. A version is deliberately required here rather
// than resolving "latest", so a command remains reproducible when used in CI.
func bootstrapGCX(version, cacheDir string) (string, error) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" || strings.ContainsAny(version, `/\\`) {
		return "", fmt.Errorf("invalid gcx version %q", version)
	}

	archiveName, err := gcxArchiveName(version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	archiveURL := fmt.Sprintf("%s/v%s/%s", gcxReleaseBaseURL, version, archiveName)
	checksumURL := fmt.Sprintf("%s/v%s/gcx_%s_checksums.txt", gcxReleaseBaseURL, version, version)

	installDir := filepath.Join(cacheDir, "tools", "gcx", version, runtime.GOOS+"_"+runtime.GOARCH)
	executable := "gcx"
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	target := filepath.Join(installDir, executable)
	if info, statErr := os.Stat(target); statErr == nil && !info.IsDir() {
		return target, nil
	}

	archiveBytes, err := downloadGCXAsset(archiveURL)
	if err != nil {
		return "", fmt.Errorf("download gcx %s: %w", version, err)
	}
	checksums, err := downloadGCXAsset(checksumURL)
	if err != nil {
		return "", fmt.Errorf("download gcx %s checksums: %w", version, err)
	}
	if err := verifyGCXChecksum(archiveName, archiveBytes, string(checksums)); err != nil {
		return "", fmt.Errorf("verify gcx %s: %w", version, err)
	}

	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return "", fmt.Errorf("create gcx cache directory: %w", err)
	}
	tmp, err := os.CreateTemp(installDir, ".gcx-*")
	if err != nil {
		return "", fmt.Errorf("create gcx temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	defer cleanup()

	if err := extractGCXExecutable(archiveName, archiveBytes, tmp); err != nil {
		return "", fmt.Errorf("extract gcx %s: %w", version, err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close gcx temporary file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return "", fmt.Errorf("make gcx executable: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		// Another process may have populated the same version while this one
		// downloaded it. Reuse that complete file if it is now present.
		if info, statErr := os.Stat(target); statErr == nil && !info.IsDir() {
			return target, nil
		}
		return "", fmt.Errorf("install gcx: %w", err)
	}
	return target, nil
}

func gcxArchiveName(version, goos, goarch string) (string, error) {
	if goos != "linux" && goos != "darwin" && goos != "windows" {
		return "", fmt.Errorf("gcx releases do not support %s", goos)
	}
	if goarch != "amd64" && goarch != "arm64" {
		return "", fmt.Errorf("gcx releases do not support %s/%s", goos, goarch)
	}
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("gcx_%s_%s_%s%s", version, goos, goarch, ext), nil
}

func downloadGCXAsset(url string) ([]byte, error) {
	resp, err := gcxHTTPClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 100<<20))
}

func verifyGCXChecksum(name string, data []byte, checksums string) error {
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == name {
			digest := sha256.Sum256(data)
			if !strings.EqualFold(hex.EncodeToString(digest[:]), fields[0]) {
				return fmt.Errorf("checksum mismatch for %s", name)
			}
			return nil
		}
	}
	return fmt.Errorf("checksum for %s not found", name)
}

func extractGCXExecutable(archiveName string, data []byte, dst io.Writer) error {
	if strings.HasSuffix(archiveName, ".zip") {
		return extractGCXZip(data, dst)
	}
	return extractGCXTarGz(data, dst)
}

func extractGCXTarGz(data []byte, dst io.Writer) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeReg && (filepath.Base(header.Name) == "gcx" || filepath.Base(header.Name) == "gcx.exe") {
			_, err = io.Copy(dst, tr)
			return err
		}
	}
	return fmt.Errorf("gcx executable not found in archive")
}

func extractGCXZip(data []byte, dst io.Writer) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, file := range reader.File {
		if filepath.Base(file.Name) != "gcx.exe" && filepath.Base(file.Name) != "gcx" {
			continue
		}
		r, err := file.Open()
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(dst, r)
		closeErr := r.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	return fmt.Errorf("gcx executable not found in archive")
}
