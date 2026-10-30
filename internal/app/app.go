// Package app 是 SafeClash 的主体：管理核心进程、订阅、系统代理、更新和本地 Web 界面。
package app

import (
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"safeclash/internal/autostart"
	"safeclash/internal/sysproxy"
)

type App struct {
	paths Paths
	store *Store
	core  *Core
	upd   updater
	log   *log.Logger

	tunMu sync.Mutex
	tunSt TunStatus

	proxyMu     sync.Mutex
	proxyActive bool // 本进程是否设置过系统代理

	quit     chan struct{}
	quitOnce sync.Once
	shutdown func() // 关闭 HTTP 服务
}

func New(paths Paths, logger *log.Logger) (*App, error) {
	st, err := LoadStore(paths.Settings)
	if err != nil {
		return nil, err
	}
	a := &App{paths: paths, store: st, core: NewCore(paths), log: logger, quit: make(chan struct{})}
	a.core.onExit = func() { a.applySystemProxy(false) }
	killStaleCore(paths)
	// 上次若异常退出，系统代理可能仍指向已不存在的核心，导致无法上网，先清除
	if st.Get().SystemProxy {
		sysproxy.Clear()
	}
	if exe, err := os.Executable(); err == nil {
		os.Remove(exe + ".old") // 清理上次自更新留下的旧文件
	}
	os.Remove(paths.CoreBin + ".old")
	return a, nil
}

// ImportCore 手动导入一个本地 mihomo 二进制（用于开发版或离线环境）。
func (a *App) ImportCore(path string) error {
	tmp := a.paths.CoreBin + ".import"
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	out, err := exec.Command(tmp, "-v").CombinedOutput()
	if err != nil {
		os.Remove(tmp)
		return errors.New("该文件不是可运行的 mihomo 核心")
	}
	ver := "unknown"
	if f := strings.Fields(string(out)); len(f) >= 3 {
		ver = f[2]
	}
	return a.core.Install(tmp, ver, "manual-import")
}

// Run 启动后台任务；若设置了自动连接则启动核心。
func (a *App) Run() {
	s := a.store.Get()
	if s.AutoConnect && s.ActiveProfile != "" {
		if err := a.StartCore(); err != nil {
			a.log.Printf("自动启动核心失败：%v", err)
		}
	}
	go a.scheduler()
}

func (a *App) buildConfig(controller, secret string) ([]byte, error) {
	s := a.store.Get()
	if s.ActiveProfile == "" {
		return nil, errors.New("请先在「订阅」中添加并选择一个配置")
	}
	raw, err := os.ReadFile(a.profilePath(s.ActiveProfile))
	if err != nil {
		return nil, errors.New("当前配置文件丢失，请重新更新订阅")
	}
	return Sanitize(raw, s, controller, secret)
}

func (a *App) StartCore() error {
	mark := a.core.LogMark()
	a.setTun(TunStatus{})
	if err := a.core.Start(a.buildConfig); err != nil {
		a.applySystemProxy(false)
		return err
	}
	if err := a.applySystemProxy(a.store.Get().SystemProxy); err != nil {
		return err
	}
	if a.store.Get().Tun {
		a.checkTun(mark)
	}
	return nil
}

func (a *App) StopCore() {
	a.applySystemProxy(false)
	a.core.Stop()
	a.setTun(TunStatus{})
}

// applySystemProxy 只在核心运行时开启系统代理，避免核心停止后断网。
func (a *App) applySystemProxy(on bool) error {
	a.proxyMu.Lock()
	defer a.proxyMu.Unlock()
	s := a.store.Get()
	if on && a.core.Running() {
		a.proxyActive = true
		return sysproxy.Set("127.0.0.1", s.MixedPort, s.Bypass)
	}
	if !a.proxyActive {
		return nil
	}
	a.proxyActive = false
	return sysproxy.Clear()
}

// SettingsPatch 是界面可修改的设置项，nil 表示不变。
type SettingsPatch struct {
	MixedPort           *int     `json:"mixed_port"`
	AllowLan            *bool    `json:"allow_lan"`
	SystemProxy         *bool    `json:"system_proxy"`
	Tun                 *bool    `json:"tun"`
	Mode                *string  `json:"mode"`
	LogLevel            *string  `json:"log_level"`
	IPv6                *bool    `json:"ipv6"`
	Bypass              []string `json:"bypass"`
	AutoStart           *bool    `json:"auto_start"`
	AutoConnect         *bool    `json:"auto_connect"`
	AutoUpdate          *bool    `json:"auto_update"`
	UpdateIntervalHours *int     `json:"update_interval_hours"`
	UpdateViaProxy      *bool    `json:"update_via_proxy"`
}

func (a *App) ApplySettings(p SettingsPatch) error {
	if p.MixedPort != nil && (*p.MixedPort < 1024 || *p.MixedPort > 65535) {
		return errors.New("端口需在 1024-65535 之间")
	}
	if p.Mode != nil && *p.Mode != "rule" && *p.Mode != "global" && *p.Mode != "direct" {
		return errors.New("未知模式")
	}
	if p.LogLevel != nil && !oneOf(*p.LogLevel, "debug", "info", "warning", "error", "silent") {
		return errors.New("未知日志级别")
	}
	if p.AutoStart != nil {
		exe, _ := os.Executable()
		exe, _ = filepath.EvalSymlinks(exe)
		var err error
		if *p.AutoStart {
			err = autostart.Enable(exe)
		} else {
			err = autostart.Disable()
		}
		if err != nil {
			return err
		}
	}
	old := a.store.Get()
	s, err := a.store.Update(func(s *Settings) {
		set(&s.MixedPort, p.MixedPort)
		set(&s.AllowLan, p.AllowLan)
		set(&s.SystemProxy, p.SystemProxy)
		set(&s.Tun, p.Tun)
		set(&s.Mode, p.Mode)
		set(&s.LogLevel, p.LogLevel)
		set(&s.IPv6, p.IPv6)
		set(&s.AutoStart, p.AutoStart)
		set(&s.AutoConnect, p.AutoConnect)
		set(&s.AutoUpdate, p.AutoUpdate)
		set(&s.UpdateIntervalHours, p.UpdateIntervalHours)
		set(&s.UpdateViaProxy, p.UpdateViaProxy)
		if p.Bypass != nil {
			s.Bypass = p.Bypass
		}
	})
	if err != nil {
		return err
	}
	if !a.core.Running() {
		return nil
	}
	// 需要重启核心的设置
	if s.MixedPort != old.MixedPort || s.AllowLan != old.AllowLan || s.Tun != old.Tun || s.IPv6 != old.IPv6 {
		if err := a.StartCore(); err != nil {
			return err
		}
		if st := a.TunStatus(); s.Tun && !st.Active {
			return errors.New("核心已启动，但 TUN 未生效：" + st.Error)
		}
		return nil
	}
	if s.Mode != old.Mode || s.LogLevel != old.LogLevel {
		if err := a.core.Patch(map[string]any{"mode": s.Mode, "log-level": s.LogLevel}); err != nil {
			return err
		}
	}
	if s.SystemProxy != old.SystemProxy || p.Bypass != nil {
		return a.applySystemProxy(s.SystemProxy)
	}
	return nil
}

// Quit 停止核心、清除系统代理并退出。
func (a *App) Quit() { a.quitThen(nil) }

// quitThen 释放所有资源后执行 then，最后才通知主循环退出，
// 保证 then（如启动新版本）在进程结束前完成。
func (a *App) quitThen(then func()) {
	a.quitOnce.Do(func() {
		a.StopCore()
		os.Remove(a.paths.Instance)
		if a.shutdown != nil {
			a.shutdown()
		}
		if then != nil {
			then()
		}
		close(a.quit)
	})
}

func (a *App) Done() <-chan struct{} { return a.quit }

// Restart 用于自更新后以相同参数启动新版本。
func (a *App) Restart(exe string) {
	a.quitThen(func() {
		args := []string{"--silent"}
		for _, arg := range os.Args[1:] {
			if arg != "--silent" && arg != "-silent" {
				args = append(args, arg)
			}
		}
		if err := exec.Command(exe, args...).Start(); err != nil {
			a.log.Printf("重启失败：%v", err)
		}
	})
}

func set[T any](dst *T, v *T) {
	if v != nil {
		*dst = *v
	}
}

func oneOf(s string, opts ...string) bool {
	for _, o := range opts {
		if s == o {
			return true
		}
	}
	return false
}
