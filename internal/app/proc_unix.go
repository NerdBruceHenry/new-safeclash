//go:build !windows

package app

import (
	"os"
	"os/exec"
	"syscall"
)

func afterStart(*exec.Cmd) {}

func stopProcess(cmd *exec.Cmd) { cmd.Process.Signal(syscall.SIGTERM) }

// isElevated 报告当前进程是否以 root 运行（TUN 需要）。
func isElevated() bool { return os.Geteuid() == 0 }
