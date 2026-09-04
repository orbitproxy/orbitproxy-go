//go:build windows

package mcpstdio

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr 设置 Windows 平台的进程属性。
// CREATE_NEW_PROCESS_GROUP 使子进程不继承父进程的 console signal（Ctrl+C 等）。
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
