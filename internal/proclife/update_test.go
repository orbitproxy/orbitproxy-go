package proclife

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTarGzOrbitproxyBinary(t *testing.T) {
	t.Parallel()

	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gz)
	body := []byte("fake-binary")
	hdr := &tar.Header{
		Name: "orbitproxy",
		Mode: 0755,
		Size: int64(len(body)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "orbitproxy.tar.gz")
	if err := os.WriteFile(archivePath, raw.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	binPath, err := extractOrbitproxyBinary(archivePath, dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "fake-binary" {
		t.Fatalf("extracted %q", got)
	}
}

func TestRejectUnsafePath(t *testing.T) {
	t.Parallel()
	if err := rejectUnsafePath("../orbitproxy"); err == nil {
		t.Fatal("expected unsafe path error")
	}
}
