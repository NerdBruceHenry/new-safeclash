package app

import (
	"os/exec"
	"syscall"
)

// Pdeathsig：本程序退出时内核自动结束核心进程。
func setupChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
func killStaleCore(Paths) {}
