//go:build unix

package proclife

import (
	"fmt"
	"io"
	"os"
)

func replaceExecutable(newPath, exePath string) error {
	dest := exePath + ".new"
	if err := copyFile(newPath, dest, 0o755); err != nil {
		return err
	}
	if err := os.Rename(dest, exePath); err != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("replace %s: %w", exePath, err)
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
	if err := out.Chmod(mode); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
