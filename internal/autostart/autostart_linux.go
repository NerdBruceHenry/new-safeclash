package autostart

import (
	"os"
	"path/filepath"
	"strings"
)

func desktopPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "autostart", "safeclash.desktop")
}

func Enable(exe string) error {
	quoted := `"` + strings.ReplaceAll(exe, `"`, `\"`) + `"`
	content := "[Desktop Entry]\nType=Application\nName=" + appName + "\nExec=" + quoted + " --silent\nX-GNOME-Autostart-enabled=true\n"
	os.MkdirAll(filepath.Dir(desktopPath()), 0o755)
	return os.WriteFile(desktopPath(), []byte(content), 0o644)
}

func Disable() error {
	err := os.Remove(desktopPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
