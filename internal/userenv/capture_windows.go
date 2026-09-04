//go:build windows

package userenv

import "os"

func captureToolchainPATH() string {
	return os.Getenv("PATH")
}
