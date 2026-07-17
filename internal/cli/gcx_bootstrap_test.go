package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestGCXArchiveName(t *testing.T) {
	tests := []struct {
		goos, goarch, want string
	}{
		{"linux", "amd64", "gcx_0.4.3_linux_amd64.tar.gz"},
		{"darwin", "arm64", "gcx_0.4.3_darwin_arm64.tar.gz"},
		{"windows", "amd64", "gcx_0.4.3_windows_amd64.zip"},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.goarch, func(t *testing.T) {
			got, err := gcxArchiveName("0.4.3", tt.goos, tt.goarch)
			if err != nil {
				t.Fatalf("gcxArchiveName: %v", err)
			}
			if got != tt.want {
				t.Fatalf("gcxArchiveName = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGCXArchiveNameRejectsUnsupportedPlatform(t *testing.T) {
	if _, err := gcxArchiveName("0.4.3", "freebsd", "amd64"); err == nil {
		t.Fatal("expected unsupported platform error")
	}
	if _, err := gcxArchiveName("0.4.3", "linux", "386"); err == nil {
		t.Fatal("expected unsupported architecture error")
	}
}

func TestVerifyGCXChecksum(t *testing.T) {
	data := []byte("gcx")
	checksums := "deadbeef  other.tar.gz\n" + sha256Hex(data) + "  gcx_0.4.3_linux_amd64.tar.gz\n"
	if err := verifyGCXChecksum("gcx_0.4.3_linux_amd64.tar.gz", data, checksums); err != nil {
		t.Fatalf("verifyGCXChecksum: %v", err)
	}
	if err := verifyGCXChecksum("gcx_0.4.3_linux_amd64.tar.gz", []byte("bad"), checksums); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}

func TestExtractGCXTarGz(t *testing.T) {
	want := []byte("fake-gcx")
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gz)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: "gcx",
		Mode: 0o755,
		Size: int64(len(want)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(want); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	var got bytes.Buffer
	if err := extractGCXTarGz(archive.Bytes(), &got); err != nil {
		t.Fatalf("extractGCXTarGz: %v", err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("extracted %q, want %q", got.Bytes(), want)
	}
}

func TestBootstrapGCXRejectsUnsafeVersion(t *testing.T) {
	if _, err := bootstrapGCX("../latest", t.TempDir()); err == nil {
		t.Fatal("expected invalid version error")
	}
}

func TestBootstrapGCXDownloadsAndCaches(t *testing.T) {
	version := "0.4.3"
	archiveName := "gcx_0.4.3_linux_amd64.tar.gz"
	archiveData := gcxTarGz(t, "fake-gcx")
	checksums := fmt.Sprintf("%x  %s\n", sha256.Sum256(archiveData), archiveName)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0.4.3/" + archiveName:
			_, _ = w.Write(archiveData)
		case "/v0.4.3/gcx_0.4.3_checksums.txt":
			_, _ = io.WriteString(w, checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldBaseURL := gcxReleaseBaseURL
	oldClient := gcxHTTPClient
	gcxReleaseBaseURL = server.URL
	gcxHTTPClient = server.Client()
	t.Cleanup(func() {
		gcxReleaseBaseURL = oldBaseURL
		gcxHTTPClient = oldClient
	})

	cacheDir := t.TempDir()
	got, err := bootstrapGCX("v"+version, cacheDir)
	if err != nil {
		t.Fatalf("bootstrapGCX: %v", err)
	}
	want := filepath.Join(cacheDir, "tools", "gcx", version, "linux_amd64", "gcx")
	if got != want {
		t.Fatalf("bootstrapGCX path = %q, want %q", got, want)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read cached gcx: %v", err)
	}
	if string(data) != "fake-gcx" {
		t.Fatalf("cached gcx = %q, want fake-gcx", data)
	}

	server.Close()
	if gotAgain, err := bootstrapGCX(version, cacheDir); err != nil || gotAgain != got {
		t.Fatalf("cached bootstrap = %q, %v; want %q, nil", gotAgain, err, got)
	}
}

func TestBootstrapGCXRejectsChecksumMismatch(t *testing.T) {
	archiveName := "gcx_0.4.3_linux_amd64.tar.gz"
	archiveData := gcxTarGz(t, "fake-gcx")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0.4.3/"+archiveName {
			_, _ = w.Write(archiveData)
			return
		}
		_, _ = io.WriteString(w, "00000000  "+archiveName+"\n")
	}))
	defer server.Close()

	oldBaseURL := gcxReleaseBaseURL
	oldClient := gcxHTTPClient
	gcxReleaseBaseURL = server.URL
	gcxHTTPClient = server.Client()
	t.Cleanup(func() {
		gcxReleaseBaseURL = oldBaseURL
		gcxHTTPClient = oldClient
	})

	if _, err := bootstrapGCX("0.4.3", t.TempDir()); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}

func TestExtractGCXZip(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	file, err := writer.Create("bin/gcx.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(file, "fake-gcx"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	var got bytes.Buffer
	if err := extractGCXZip(archive.Bytes(), &got); err != nil {
		t.Fatalf("extractGCXZip: %v", err)
	}
	if got.String() != "fake-gcx" {
		t.Fatalf("extracted %q, want fake-gcx", got.String())
	}
}

func gcxTarGz(t *testing.T, contents string) []byte {
	t.Helper()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gz)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: "gcx",
		Mode: 0o755,
		Size: int64(len(contents)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write([]byte(contents)); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func sha256Hex(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
