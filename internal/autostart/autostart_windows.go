package autostart

import (
	"os/exec"
	"syscall"
)

const runKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

func reg(args ...string) error {
	cmd := exec.Command("reg", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Run()
}

func Enable(exe string) error {
	return reg("add", runKey, "/v", appName, "/t", "REG_SZ", "/d", `"`+exe+`" --silent`, "/f")
}

func Disable() error {
	reg("delete", runKey, "/v", appName, "/f")
	return nil
}
