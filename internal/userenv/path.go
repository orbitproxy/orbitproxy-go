package userenv

import (
	"os"
	"os/exec"
	"sync"
)

var (
	once sync.Once
	path string
)

// Capture loads the current user's toolchain PATH once (nvm / fnm / volta).
// It fills HOME when missing so nvm lookup works under systemd.
func Capture() {
	once.Do(func() {
		EnsureHome()
		path = captureToolchainPATH()
	})
}

// PATH is the PATH that MCP preflight and stdio spawn should use.
// Falls back to this process's PATH when no toolchain init files exist.
func PATH() string {
	Capture()
	if path != "" {
		return path
	}
	return os.Getenv("PATH")
}

// LookPath resolves a command using PATH(), not this process's PATH.
func LookPath(file string) (string, error) {
	if file == "" {
		return "", &exec.Error{Name: file, Err: exec.ErrNotFound}
	}
	return lookPath(file, PATH())
}

func resetForTest() {
	once = sync.Once{}
	path = ""
	resetNpmRootForTest()
}
