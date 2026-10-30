package sysproxy

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

const inetKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func DefaultBypass() []string {
	return []string{"localhost", "127.*", "10.*", "172.16.*", "172.17.*", "172.18.*", "172.19.*", "172.2*", "172.30.*", "172.31.*", "192.168.*", "<local>"}
}

func Set(host string, port int, bypass []string) error {
	if err := run("reg", "add", inetKey, "/v", "ProxyServer", "/t", "REG_SZ", "/d", host+":"+strconv.Itoa(port), "/f"); err != nil {
		return err
	}
	if err := run("reg", "add", inetKey, "/v", "ProxyOverride", "/t", "REG_SZ", "/d", strings.Join(bypass, ";"), "/f"); err != nil {
		return err
	}
	if err := run("reg", "add", inetKey, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "1", "/f"); err != nil {
		return err
	}
	refresh()
	return nil
}

func Clear() error {
	err := run("reg", "add", inetKey, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f")
	refresh()
	return err
}

// refresh 通知 WinINet 设置已变更，让浏览器立即生效。
func refresh() {
	p := syscall.NewLazyDLL("wininet.dll").NewProc("InternetSetOptionW")
	if p.Find() != nil {
		return
	}
	p.Call(0, 39, 0, 0) // INTERNET_OPTION_SETTINGS_CHANGED
	p.Call(0, 37, 0, 0) // INTERNET_OPTION_REFRESH
}
