package proclife

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const oldSuffix = ".old"

func currentExecutable() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		return exePath, nil
	}
	return resolved, nil
}

func restartArgs(exePath string) []string {
	args := append([]string{exePath}, os.Args[1:]...)
	return args
}

func isOrbitproxyBinaryName(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	return base == "orbitproxy" || base == "orbitproxy.exe"
}

func staleOldPath(exePath string) string {
	return exePath + oldSuffix
}

// RemoveStaleOldBinary deletes a leftover <exe>.old from a previous Windows update.
func RemoveStaleOldBinary() {
	exePath, err := currentExecutable()
	if err != nil {
		return
	}
	_ = os.Remove(staleOldPath(exePath))
}
