//go:build windows

package userenv

import "os/exec"

func lookPath(file, _ string) (string, error) {
	return exec.LookPath(file)
}
