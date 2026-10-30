package app

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// TunStatus 反映 TUN 是否真正生效，而不只是设置里勾选了。
type TunStatus struct {
	Checked bool   `json:"checked"` // 本次启动已完成检测
	Active  bool   `json:"active"`  // 核心报告虚拟网卡已创建
	Error   string `json:"error,omitempty"`
}

func (a *App) TunStatus() TunStatus {
	a.tunMu.Lock()
	defer a.tunMu.Unlock()
	return a.tunSt
}

func (a *App) setTun(st TunStatus) {
	a.tunMu.Lock()
	a.tunSt = st
	a.tunMu.Unlock()
}

// mihomo 创建虚拟网卡成功或失败时各打印一行日志；失败时还会把 tun.enable 改回 false。
const (
	tunOKLog  = "[TUN] Tun adapter listening at"
	tunErrLog = "Start TUN listening error:"
)

// checkTun 在核心启动后确认 TUN 是否真的生效。mark 为启动前的日志位置。
func (a *App) checkTun(mark int) {
	st := a.waitTun(mark, 10*time.Second)
	st.Checked = true
	if !st.Active {
		if st.Error == "" {
			st.Error = "核心未报告 TUN 已启动"
		}
		if !isElevated() {
			st.Error += "。" + elevationHint()
		}
		a.log.Printf("TUN 未生效：%s", st.Error)
	}
	a.setTun(st)
}

func (a *App) waitTun(mark int, timeout time.Duration) TunStatus {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if st, ok := parseTunLogs(a.core.LogsSince(mark)); ok {
			return st
		}
		if !a.core.Running() {
			return TunStatus{Error: "核心已退出"}
		}
		time.Sleep(200 * time.Millisecond)
	}
	// 日志里没有结论（例如日志级别为 error/silent），以核心当前配置为准
	return TunStatus{Active: a.coreTunEnabled()}
}

// parseTunLogs 从核心日志判断 TUN 结果；ok 为 false 表示还没有结论。
func parseTunLogs(lines []string) (TunStatus, bool) {
	for _, l := range lines {
		if i := strings.Index(l, tunErrLog); i >= 0 {
			return TunStatus{Error: strings.Trim(l[i+len(tunErrLog):], ` "`)}, true
		}
		if strings.Contains(l, tunOKLog) {
			return TunStatus{Active: true}, true
		}
	}
	return TunStatus{}, false
}

func (a *App) coreTunEnabled() bool {
	req, err := a.core.Request(context.Background(), "GET", "/configs", nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var cfg struct {
		Tun struct {
			Enable bool `json:"enable"`
		} `json:"tun"`
	}
	return json.NewDecoder(resp.Body).Decode(&cfg) == nil && cfg.Tun.Enable
}

func elevationHint() string {
	switch runtime.GOOS {
	case "windows":
		return "当前不是管理员权限：请先退出 SafeClash，再右键程序选择「以管理员身份运行」"
	case "darwin":
		return "当前不是 root 权限：请在终端用 sudo 运行 SafeClash"
	default:
		return "当前不是 root 权限：请用 sudo 运行，或为程序授予 CAP_NET_ADMIN"
	}
}
