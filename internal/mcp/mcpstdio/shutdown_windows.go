//go:build windows

package mcpstdio

import "os"

// signalProcess 在 Windows 上终止进程。
// Windows 无 SIGTERM 语义，两种模式均调用 Kill()（等效 TerminateProcess）。
func signalProcess(proc *os.Process, _ bool) error {
	return proc.Kill()
}
