//go:build !windows

package app

import (
	"os/exec"
	"syscall"
)

func afterStart(*exec.Cmd) {}

func stopProcess(cmd *exec.Cmd) { cmd.Process.Signal(syscall.SIGTERM) }
