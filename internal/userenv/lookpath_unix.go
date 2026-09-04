//go:build unix

package userenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func lookPath(file, pathEnv string) (string, error) {
	if strings.Contains(file, "/") {
		return exec.LookPath(file)
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, file)
		if isExecutable(candidate) {
			return candidate, nil
		}
	}
	return "", &exec.Error{Name: file, Err: exec.ErrNotFound}
}

func isExecutable(file string) bool {
	info, err := os.Stat(file)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}
