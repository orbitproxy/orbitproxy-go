package proclife

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const maxArchiveBytes = 256 << 20

// InstallFromURL downloads a machine artifact (tar.gz or zip) and replaces the current executable.
func InstallFromURL(ctx context.Context, downloadURL string) error {
	if strings.TrimSpace(downloadURL) == "" {
		return fmt.Errorf("download url is empty")
	}
	exePath, err := currentExecutable()
	if err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "orbitproxy-update-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	archivePath, err := downloadArchive(ctx, downloadURL, tmpDir)
	if err != nil {
		return err
	}
	binPath, err := extractOrbitproxyBinary(archivePath, tmpDir)
	if err != nil {
		return err
	}
	return replaceExecutable(binPath, exePath)
}

func downloadArchive(ctx context.Context, downloadURL, tmpDir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("build download request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download update: http %d", resp.StatusCode)
	}

	path := filepath.Join(tmpDir, "artifact.bin")
	out, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer out.Close()

	if _, err := io.Copy(out, io.LimitReader(resp.Body, maxArchiveBytes+1)); err != nil {
		return "", fmt.Errorf("write update archive: %w", err)
	}
	info, err := out.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() > maxArchiveBytes {
		return "", fmt.Errorf("update archive exceeds %d bytes", maxArchiveBytes)
	}
	return path, nil
}

func extractOrbitproxyBinary(archivePath, destDir string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	head := make([]byte, 4)
	n, _ := io.ReadFull(f, head)
	if _, err := f.Seek(0, 0); err != nil {
		return "", err
	}

	if n >= 2 && head[0] == 0x1f && head[1] == 0x8b {
		return extractTarGz(f, destDir)
	}
	if n >= 2 && head[0] == 'P' && head[1] == 'K' {
		_ = f.Close()
		return extractZip(archivePath, destDir)
	}
	return "", fmt.Errorf("unsupported update archive format")
}

func extractTarGz(r io.Reader, destDir string) (string, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return "", fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if !isOrbitproxyBinaryName(hdr.Name) {
			continue
		}
		if err := rejectUnsafePath(hdr.Name); err != nil {
			return "", err
		}
		outPath := filepath.Join(destDir, "orbitproxy")
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(out, io.LimitReader(tr, maxArchiveBytes)); err != nil {
			_ = out.Close()
			return "", err
		}
		if err := out.Close(); err != nil {
			return "", err
		}
		return outPath, nil
	}
	return "", fmt.Errorf("archive did not contain orbitproxy binary")
}

func extractZip(archivePath, destDir string) (string, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("zip: %w", err)
	}
	defer zr.Close()

	for _, file := range zr.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if !isOrbitproxyBinaryName(file.Name) {
			continue
		}
		if err := rejectUnsafePath(file.Name); err != nil {
			return "", err
		}
		rc, err := file.Open()
		if err != nil {
			return "", err
		}
		outPath := filepath.Join(destDir, "orbitproxy.exe")
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			_ = rc.Close()
			return "", err
		}
		_, copyErr := io.Copy(out, io.LimitReader(rc, maxArchiveBytes))
		_ = rc.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		return outPath, nil
	}
	return "", fmt.Errorf("archive did not contain orbitproxy binary")
}

func rejectUnsafePath(name string) error {
	cleaned := filepath.Clean(name)
	if strings.Contains(cleaned, "..") || filepath.IsAbs(cleaned) {
		return fmt.Errorf("unsafe archive path %q", name)
	}
	return nil
}
