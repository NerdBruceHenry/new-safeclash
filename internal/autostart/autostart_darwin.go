package autostart

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
)

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "com.safeclash.app.plist")
}

func Enable(exe string) error {
	var b strings.Builder
	xml.EscapeText(&b, []byte(exe))
	content := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>com.safeclash.app</string>
  <key>ProgramArguments</key><array><string>` + b.String() + `</string><string>--silent</string></array>
  <key>RunAtLoad</key><true/>
</dict></plist>
`
	os.MkdirAll(filepath.Dir(plistPath()), 0o755)
	return os.WriteFile(plistPath(), []byte(content), 0o644)
}

func Disable() error {
	err := os.Remove(plistPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
