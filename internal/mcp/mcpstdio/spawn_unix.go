//go:build !windows

package mcpstdio

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr 设置 Unix 平台的进程属性。
// Setpgid=true 使子进程创建独立进程组，
// 避免父进程收到 SIGINT 时连坐杀死子进程。
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}
