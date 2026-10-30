package sysproxy

import (
	"os/exec"
	"strconv"
	"strings"
)

func hideWindow(*exec.Cmd) {}

func DefaultBypass() []string {
	return []string{"localhost", "127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "*.local", "169.254.0.0/16"}
}

// services 返回所有已启用的网络服务（Wi-Fi、Ethernet 等）。
func services() ([]string, error) {
	out, err := output("networksetup", "-listallnetworkservices")
	if err != nil {
		return nil, err
	}
	var s []string
	for i, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if i == 0 || line == "" || strings.HasPrefix(line, "*") { // 第一行是说明，* 开头为已禁用
			continue
		}
		s = append(s, line)
	}
	return s, nil
}

func Set(host string, port int, bypass []string) error {
	svcs, err := services()
	if err != nil {
		return err
	}
	p := strconv.Itoa(port)
	for _, s := range svcs {
		for _, kind := range []string{"-setwebproxy", "-setsecurewebproxy", "-setsocksfirewallproxy"} {
			if err := run("networksetup", kind, s, host, p); err != nil {
				return err
			}
		}
		if err := run("networksetup", append([]string{"-setproxybypassdomains", s}, bypass...)...); err != nil {
			return err
		}
	}
	return nil
}

func Clear() error {
	svcs, err := services()
	if err != nil {
		return err
	}
	var last error
	for _, s := range svcs {
		for _, kind := range []string{"-setwebproxystate", "-setsecurewebproxystate", "-setsocksfirewallproxystate"} {
			if err := run("networksetup", kind, s, "off"); err != nil {
				last = err
			}
		}
	}
	return last
}
