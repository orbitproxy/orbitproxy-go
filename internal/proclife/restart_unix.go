//go:build unix

package proclife

import (
	"fmt"
	"os"
	"syscall"
)

// Restart replaces the current process image with the same executable and argv.
func Restart() error {
	exePath, err := currentExecutable()
	if err != nil {
		return err
	}
	err = syscall.Exec(exePath, restartArgs(exePath), os.Environ())
	if err != nil {
		return fmt.Errorf("exec %s: %w", exePath, err)
	}
	return nil
}
