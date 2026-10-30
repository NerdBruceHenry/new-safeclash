package app

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const evilSub = `
port: 1080
allow-lan: true
bind-address: "*"
external-controller: 0.0.0.0:9090
secret: ""
external-ui: ui
listeners:
  - {name: open, type: mixed, port: 10000, listen: 0.0.0.0}
dns:
  enable: true
  listen: 0.0.0.0:53
proxies:
  - {name: a, type: ss, server: 1.2.3.4, port: 443, cipher: aes-128-gcm, password: x}
rules:
  - MATCH,a
`

func TestSanitizeStripsDangerousKeys(t *testing.T) {
	s := defaultSettings()
	out, err := Sanitize([]byte(evilSub), s, "127.0.0.1:5555", "sekret")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["allow-lan"] != false || m["external-controller"] != "127.0.0.1:5555" || m["secret"] != "sekret" {
		t.Fatalf("overrides not applied: %v", m)
	}
	for _, k := range []string{"port", "listeners", "external-ui", "bind-address"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s should be removed", k)
		}
	}
	if _, ok := m["dns"].(map[string]any)["listen"]; ok {
		t.Error("dns.listen should be removed")
	}
	if m["mixed-port"] != 7897 {
		t.Errorf("mixed-port = %v", m["mixed-port"])
	}
}

func TestSanitizeTun(t *testing.T) {
	s := defaultSettings()
	s.Tun = true
	out, err := Sanitize([]byte("proxies: []\n"), s, "127.0.0.1:1", "x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "auto-route: true") || !strings.Contains(string(out), "fake-ip") {
		t.Fatalf("tun/dns missing:\n%s", out)
	}
}

func TestSanitizeRejectsNonClash(t *testing.T) {
	if _, err := Sanitize([]byte("c3M6Ly9hYmM="), defaultSettings(), "a", "b"); err == nil {
		t.Fatal("base64 subscription should be rejected")
	}
}
