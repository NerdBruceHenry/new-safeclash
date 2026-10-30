package app

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// 订阅由第三方提供，不可信。以下字段可能被用来暴露控制接口、
// 向局域网/公网开放入站端口或替换数据源，一律删除，由本程序统一接管。
var strippedKeys = []string{
	"external-controller", "external-controller-tls", "external-controller-unix",
	"external-controller-pipe", "external-controller-cors", "external-ui",
	"external-ui-name", "external-ui-url", "external-doh-server", "secret",
	"port", "socks-port", "redir-port", "tproxy-port", "mixed-port",
	"listeners", "tunnels", "ss-config", "vmess-config", "tuic-server",
	"bind-address", "allow-lan", "skip-auth-prefixes", "lan-allowed-ips", "lan-disallowed-ips",
	"geox-url", "tun", "interface-name", "routing-mark",
}

// Sanitize 解析订阅配置，删除危险字段，并写入本程序控制的参数。
func Sanitize(raw []byte, s Settings, controller, secret string) ([]byte, error) {
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("不是有效的 YAML：%v", err)
	}
	if m == nil {
		return nil, errors.New("配置为空")
	}
	_, hasProxies := m["proxies"]
	_, hasProviders := m["proxy-providers"]
	if !hasProxies && !hasProviders {
		return nil, errors.New("不是 Clash/mihomo 格式的配置（缺少 proxies 或 proxy-providers）。请使用机场提供的 Clash 订阅链接")
	}
	for _, k := range strippedKeys {
		delete(m, k)
	}

	m["mixed-port"] = s.MixedPort
	m["allow-lan"] = s.AllowLan
	if s.AllowLan {
		m["bind-address"] = "*"
	}
	m["external-controller"] = controller
	m["secret"] = secret
	m["mode"] = s.Mode
	m["log-level"] = s.LogLevel
	m["ipv6"] = s.IPv6
	m["unified-delay"] = true
	m["profile"] = map[string]any{"store-selected": true, "store-fake-ip": true}

	if s.Tun {
		m["tun"] = map[string]any{
			"enable":                true,
			"stack":                 "mixed",
			"auto-route":            true,
			"auto-detect-interface": true,
			"strict-route":          true,
			"dns-hijack":            []string{"any:53", "tcp://any:53"},
		}
		dns, _ := m["dns"].(map[string]any)
		if dns == nil {
			dns = map[string]any{
				"enhanced-mode":  "fake-ip",
				"fake-ip-range":  "198.18.0.1/16",
				"nameserver":     []string{"https://223.5.5.5/dns-query", "https://1.12.12.12/dns-query"},
				"fake-ip-filter": []string{"*.lan", "*.local", "localhost.ptlogin2.qq.com"},
			}
		}
		dns["enable"] = true
		delete(dns, "listen") // 不开放 DNS 入站端口
		m["dns"] = dns
	} else if dns, ok := m["dns"].(map[string]any); ok {
		delete(dns, "listen")
	}
	return yaml.Marshal(m)
}
