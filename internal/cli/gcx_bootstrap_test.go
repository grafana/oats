package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
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

func sha256Hex(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
