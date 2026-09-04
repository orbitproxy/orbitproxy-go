//go:build windows

package proclife

import (
	"fmt"
	"io"
	"os"
)

func replaceExecutable(newPath, exePath string) error {
	oldPath := staleOldPath(exePath)
	_ = os.Remove(oldPath)
	if err := os.Rename(exePath, oldPath); err != nil {
		return fmt.Errorf("rename running binary: %w", err)
	}
	if err := copyFile(newPath, exePath, 0o755); err != nil {
		_ = os.Rename(oldPath, exePath)
		return err
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}
