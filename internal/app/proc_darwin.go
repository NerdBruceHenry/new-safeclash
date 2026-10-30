package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func setupChild(*exec.Cmd) {}

// macOS 没有 Pdeathsig/Job Object。若上次异常退出遗留了核心进程，
// 根据 pid 文件结束它（只结束确实是 mihomo 的进程）。
func killStaleCore(p Paths) {
	b, err := os.ReadFile(filepath.Join(p.Work, "core.pid"))
	if err != nil {
		return
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid <= 0 {
		return
	}
	out, _ := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if strings.Contains(string(out), p.CoreBin) || strings.HasSuffix(strings.TrimSpace(string(out)), "mihomo") {
		syscall.Kill(pid, syscall.SIGTERM)
	}
}
