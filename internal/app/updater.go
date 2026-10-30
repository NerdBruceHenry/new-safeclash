package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"safeclash/internal/sign"
)

// 以下变量在构建时通过 -ldflags -X 注入（见 .github/workflows/release.yml）。
var (
	Version    = "0.0.0-dev"
	UpdateRepo = "" // 你自己的 GitHub 仓库，如 "yourname/safeclash"
	PublicKey  = "" // 你的 ed25519 公钥（base64）
	UpdateURL  = "" // 可选：自定义下载地址前缀（自建镜像），默认使用 GitHub Releases
)

// Manifest 是每次发布附带的、经你私钥签名的清单。
type Manifest struct {
	AppVersion  string           `json:"app_version"`
	CoreVersion string           `json:"core_version"`
	GoVersion   string           `json:"go_version"`
	BuiltAt     string           `json:"built_at"`
	Security    bool             `json:"security"` // 本次发布包含安全修复
	Notes       string           `json:"notes"`
	Assets      map[string]Asset `json:"assets"`
}

type Asset struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type UpdateStatus struct {
	Checking      bool      `json:"checking"`
	LastCheck     time.Time `json:"last_check"`
	LastError     string    `json:"last_error,omitempty"`
	LatestApp     string    `json:"latest_app,omitempty"`
	LatestCore    string    `json:"latest_core,omitempty"`
	AppAvailable  bool      `json:"app_available"`
	CoreAvailable bool      `json:"core_available"`
	Security      bool      `json:"security"`
	Notes         string    `json:"notes,omitempty"`
	Message       string    `json:"message,omitempty"`
}

type updater struct {
	mu       sync.Mutex
	status   UpdateStatus
	manifest *Manifest
	busy     sync.Mutex
}

func platformKey(kind string) string { return kind + "-" + runtime.GOOS + "-" + runtime.GOARCH }

func releaseURL(file string) string {
	if UpdateURL != "" {
		return strings.TrimSuffix(UpdateURL, "/") + "/" + file
	}
	return "https://github.com/" + UpdateRepo + "/releases/latest/download/" + file
}

func (a *App) UpdateStatus() UpdateStatus {
	a.upd.mu.Lock()
	defer a.upd.mu.Unlock()
	return a.upd.status
}

func (a *App) setUpd(fn func(*UpdateStatus)) {
	a.upd.mu.Lock()
	fn(&a.upd.status)
	a.upd.mu.Unlock()
}

// CheckUpdate 下载并校验签名清单，判断是否有新版本。
func (a *App) CheckUpdate() (*Manifest, error) {
	if UpdateRepo == "" || PublicKey == "" {
		return nil, errors.New("这是开发版构建，未配置更新仓库和公钥，自动更新不可用")
	}
	a.setUpd(func(s *UpdateStatus) { s.Checking = true })
	m, err := a.fetchManifest()
	a.store.Update(func(s *Settings) { s.LastUpdateCheck = time.Now() })
	a.setUpd(func(s *UpdateStatus) {
		s.Checking, s.LastCheck = false, time.Now()
		if err != nil {
			s.LastError = err.Error()
			return
		}
		s.LastError = ""
		meta, _ := a.core.Meta()
		s.LatestApp, s.LatestCore, s.Security, s.Notes = m.AppVersion, m.CoreVersion, m.Security, m.Notes
		s.AppAvailable = compareVersions(m.AppVersion, Version) > 0
		s.CoreAvailable = m.CoreVersion != meta.Version || meta.Source != "signed-update"
	})
	if err != nil {
		return nil, err
	}
	a.upd.mu.Lock()
	a.upd.manifest = m
	a.upd.mu.Unlock()
	return m, nil
}

func (a *App) fetchManifest() (*Manifest, error) {
	body, err := a.download(releaseURL("manifest.json"), 1<<20)
	if err != nil {
		return nil, fmt.Errorf("获取更新清单失败：%v", err)
	}
	sig, err := a.download(releaseURL("manifest.json.sig"), 4096)
	if err != nil {
		return nil, fmt.Errorf("获取清单签名失败：%v", err)
	}
	if err := sign.Verify(PublicKey, body, string(sig)); err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	// 防回滚：签名有效也不接受比当前更旧的应用版本
	if compareVersions(m.AppVersion, Version) < 0 && !strings.Contains(Version, "dev") {
		return nil, fmt.Errorf("清单版本 %s 低于当前版本 %s，已拒绝（防降级攻击）", m.AppVersion, Version)
	}
	return &m, nil
}

func (a *App) download(url string, limit int64) ([]byte, error) {
	resp, err := a.httpClient(10 * time.Minute).Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("文件超过大小限制")
	}
	return b, nil
}

// fetchAsset 下载清单中的文件到临时路径，并校验 SHA-256。
func (a *App) fetchAsset(m *Manifest, key, dstDir string) (string, error) {
	as, ok := m.Assets[key]
	if !ok {
		return "", fmt.Errorf("本次发布没有 %s 的文件", key)
	}
	data, err := a.download(releaseURL(as.File), 200<<20)
	if err != nil {
		return "", fmt.Errorf("下载 %s 失败：%v", as.File, err)
	}
	tmp := filepath.Join(dstDir, ".download-"+randHex(4))
	if err := os.WriteFile(tmp, data, 0o700); err != nil {
		return "", err
	}
	sum, _ := sign.FileSHA256(tmp)
	if !strings.EqualFold(sum, as.SHA256) {
		os.Remove(tmp)
		return "", fmt.Errorf("%s 哈希校验失败，已丢弃", as.File)
	}
	return tmp, nil
}

// ApplyUpdate 先更新核心，再更新应用本身（应用更新后会自动重启）。
func (a *App) ApplyUpdate() error {
	if !a.upd.busy.TryLock() {
		return errors.New("正在更新中")
	}
	defer a.upd.busy.Unlock()
	m, err := a.CheckUpdate()
	if err != nil {
		return err
	}
	st := a.UpdateStatus()
	if st.CoreAvailable {
		if err := a.updateCore(m); err != nil {
			a.setUpd(func(s *UpdateStatus) { s.LastError = err.Error() })
			return err
		}
	}
	if st.AppAvailable {
		if err := a.updateSelf(m); err != nil {
			a.setUpd(func(s *UpdateStatus) { s.LastError = err.Error() })
			return err
		}
	}
	a.setUpd(func(s *UpdateStatus) { s.CoreAvailable = false; s.Message = "已是最新版本" })
	return nil
}

func (a *App) updateCore(m *Manifest) error {
	tmp, err := a.fetchAsset(m, platformKey("core"), filepath.Dir(a.paths.CoreBin))
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	// 确认新核心能正常运行
	cmd := exec.Command(tmp, "-v")
	setupChild(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("新核心无法运行：%v %s", err, out)
	}
	wasRunning := a.core.Running()
	a.core.Stop()
	if err := a.core.Install(tmp, m.CoreVersion, "signed-update"); err != nil {
		return err
	}
	a.log.Printf("核心已更新到 %s", m.CoreVersion)
	if wasRunning {
		return a.StartCore()
	}
	return nil
}

func (a *App) updateSelf(m *Manifest) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	tmp, err := a.fetchAsset(m, platformKey("app"), filepath.Dir(exe))
	if err != nil {
		return err
	}
	if err := replaceFile(tmp, exe); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("替换程序失败：%v", err)
	}
	a.log.Printf("应用已更新到 %s，正在重启", m.AppVersion)
	go a.Restart(exe)
	return nil
}

// scheduler 后台定期检查订阅和更新。
func (a *App) scheduler() {
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	time.Sleep(30 * time.Second) // 等启动完成
	for {
		a.autoUpdateProfiles()
		s := a.store.Get()
		interval := time.Duration(max(s.UpdateIntervalHours, 1)) * time.Hour
		if UpdateRepo != "" && time.Since(s.LastUpdateCheck) > interval {
			if s.AutoUpdate {
				if err := a.ApplyUpdate(); err != nil {
					a.log.Printf("自动更新失败：%v", err)
				}
			} else if _, err := a.CheckUpdate(); err != nil {
				a.log.Printf("检查更新失败：%v", err)
			}
		}
		select {
		case <-tick.C:
		case <-a.quit:
			return
		}
	}
}

// compareVersions 比较 "v1.2.3" 风格的版本号，忽略预发布后缀。
func compareVersions(x, y string) int {
	px, py := parseVersion(x), parseVersion(y)
	for i := 0; i < 3; i++ {
		if px[i] != py[i] {
			if px[i] > py[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

func parseVersion(v string) [3]int {
	var r [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "-")
	for i, p := range strings.SplitN(v, ".", 3) {
		r[i], _ = strconv.Atoi(p)
	}
	return r
}
