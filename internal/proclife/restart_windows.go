//go:build windows

package proclife

import (
	"fmt"
	"os"
	"os/exec"
)

// Restart starts a new process with the same argv and exits the current one.
func Restart() error {
	exePath, err := currentExecutable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exePath, os.Args[1:]...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn %s: %w", exePath, err)
	}
	os.Exit(0)
	return nil
}
