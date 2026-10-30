package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"safeclash/internal/sysproxy"
)

// Paths 是所有本地数据的位置。目录权限 0700，文件 0600，仅当前用户可读。
type Paths struct {
	Root     string // 数据根目录
	Settings string // settings.json
	Profiles string // 订阅原文
	Work     string // mihomo 工作目录（运行配置、geo 数据、缓存）
	CoreBin  string // mihomo 可执行文件
	CoreMeta string // 已安装核心的版本与哈希记录
	Instance string // 单实例信息（端口 + 令牌）
	Log      string
}

func NewPaths(root string) (Paths, error) {
	if root == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return Paths{}, err
		}
		root = filepath.Join(base, "SafeClash")
	}
	exe := "mihomo"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	p := Paths{
		Root:     root,
		Settings: filepath.Join(root, "settings.json"),
		Profiles: filepath.Join(root, "profiles"),
		Work:     filepath.Join(root, "work"),
		CoreBin:  filepath.Join(root, "core", exe),
		CoreMeta: filepath.Join(root, "core", "core.json"),
		Instance: filepath.Join(root, "instance.json"),
		Log:      filepath.Join(root, "safeclash.log"),
	}
	for _, d := range []string{root, p.Profiles, p.Work, filepath.Dir(p.CoreBin)} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return p, err
		}
	}
	return p, nil
}

type Profile struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	URL           string    `json:"url,omitempty"` // 为空表示本地导入
	UserAgent     string    `json:"user_agent,omitempty"`
	IntervalHours int       `json:"interval_hours"` // 0 = 不自动更新
	UpdatedAt     time.Time `json:"updated_at"`
	Upload        int64     `json:"upload"`
	Download      int64     `json:"download"`
	Total         int64     `json:"total"`
	Expire        int64     `json:"expire"`
	LastError     string    `json:"last_error,omitempty"`
}

type Settings struct {
	MixedPort           int       `json:"mixed_port"`
	AllowLan            bool      `json:"allow_lan"`
	SystemProxy         bool      `json:"system_proxy"`
	Tun                 bool      `json:"tun"`
	Mode                string    `json:"mode"`
	LogLevel            string    `json:"log_level"`
	IPv6                bool      `json:"ipv6"`
	Bypass              []string  `json:"bypass"`
	AutoStart           bool      `json:"auto_start"`
	AutoConnect         bool      `json:"auto_connect"` // 启动应用时自动启动核心
	AutoUpdate          bool      `json:"auto_update"`
	UpdateIntervalHours int       `json:"update_interval_hours"`
	UpdateViaProxy      bool      `json:"update_via_proxy"`
	LastUpdateCheck     time.Time `json:"last_update_check"`
	ActiveProfile       string    `json:"active_profile"`
	Profiles            []Profile `json:"profiles"`
}

func defaultSettings() Settings {
	return Settings{
		MixedPort:           7897,
		Mode:                "rule",
		LogLevel:            "info",
		Bypass:              sysproxy.DefaultBypass(),
		AutoConnect:         true,
		AutoUpdate:          true,
		UpdateIntervalHours: 6,
		UpdateViaProxy:      true,
	}
}

// Store 线程安全地读写 settings.json。
type Store struct {
	mu   sync.Mutex
	path string
	s    Settings
}

func LoadStore(path string) (*Store, error) {
	st := &Store{path: path, s: defaultSettings()}
	b, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, &st.s); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return st, nil
}

func (st *Store) Get() Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	c := st.s
	c.Profiles = append([]Profile(nil), st.s.Profiles...)
	c.Bypass = append([]string(nil), st.s.Bypass...)
	return c
}

// Update 在锁内修改设置并原子写盘。
func (st *Store) Update(fn func(*Settings)) (Settings, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	fn(&st.s)
	b, err := json.MarshalIndent(st.s, "", "  ")
	if err != nil {
		return st.s, err
	}
	return st.s, writeFileAtomic(st.path, b, 0o600)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
