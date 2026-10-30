package sysproxy

import (
	"os/exec"
	"strconv"
	"strings"
)

func hideWindow(*exec.Cmd) {}

func DefaultBypass() []string {
	return []string{"localhost", "127.0.0.0/8", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
}

// Linux 仅支持 GNOME 系列（gsettings）。其他桌面请手动设置 HTTP/SOCKS 代理。
func Set(host string, port int, bypass []string) error {
	p := strconv.Itoa(port)
	quoted := make([]string, len(bypass))
	for i, b := range bypass {
		quoted[i] = "'" + strings.ReplaceAll(b, "'", "") + "'"
	}
	cmds := [][]string{
		{"set", "org.gnome.system.proxy.http", "host", host},
		{"set", "org.gnome.system.proxy.http", "port", p},
		{"set", "org.gnome.system.proxy.https", "host", host},
		{"set", "org.gnome.system.proxy.https", "port", p},
		{"set", "org.gnome.system.proxy.socks", "host", host},
		{"set", "org.gnome.system.proxy.socks", "port", p},
		{"set", "org.gnome.system.proxy", "ignore-hosts", "[" + strings.Join(quoted, ", ") + "]"},
		{"set", "org.gnome.system.proxy", "mode", "manual"},
	}
	for _, c := range cmds {
		if err := run("gsettings", c...); err != nil {
			return err
		}
	}
	return nil
}

func Clear() error {
	return run("gsettings", "set", "org.gnome.system.proxy", "mode", "none")
}
