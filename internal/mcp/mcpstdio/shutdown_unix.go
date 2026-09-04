//go:build !windows

package mcpstdio

import (
	"os"
	"syscall"
)

// signalProcess 向进程发送信号。
// kill=false 发送 SIGTERM（可被捕获的优雅终止）；
// kill=true 发送 SIGKILL（不可捕获的强制终止）。
func signalProcess(proc *os.Process, kill bool) error {
	if kill {
		return proc.Signal(syscall.SIGKILL)
	}
	return proc.Signal(syscall.SIGTERM)
}
