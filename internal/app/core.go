package app

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"safeclash/internal/sign"
)

// CoreMeta 记录已安装核心的版本和哈希。每次启动前都会重新校验哈希，
// 发现二进制被替换就拒绝运行。
type CoreMeta struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Source  string `json:"source"` // "signed-update" 或 "manual-import"
}

type Core struct {
	paths Paths

	mu         sync.Mutex
	cmd        *exec.Cmd
	done       chan struct{}
	controller string
	secret     string
	lastErr    string
	logs       ring
	onExit     func() // 核心意外退出时调用
}

func NewCore(p Paths) *Core { return &Core{paths: p, logs: ring{max: 500}} }

func (c *Core) Meta() (CoreMeta, error) {
	var m CoreMeta
	b, err := os.ReadFile(c.paths.CoreMeta)
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(b, &m)
}

// Verify 检查核心二进制存在且与记录的哈希一致。
func (c *Core) Verify() (CoreMeta, error) {
	m, err := c.Meta()
	if err != nil {
		return m, errors.New("尚未安装核心，请在「设置」中检查更新以下载经签名的核心")
	}
	sum, err := sign.FileSHA256(c.paths.CoreBin)
	if err != nil {
		return m, fmt.Errorf("核心文件不可读：%v", err)
	}
	if !strings.EqualFold(sum, m.SHA256) {
		return m, errors.New("核心文件哈希与记录不符，可能已被篡改，已拒绝运行。请重新下载核心")
	}
	return m, nil
}

// Install 把已校验过的新核心原子替换到位并记录元数据。
func (c *Core) Install(src, version, source string) error {
	sum, err := sign.FileSHA256(src)
	if err != nil {
		return err
	}
	if err := os.Chmod(src, 0o755); err != nil {
		return err
	}
	if err := replaceFile(src, c.paths.CoreBin); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(CoreMeta{Version: version, SHA256: sum, Source: source}, "", "  ")
	return writeFileAtomic(c.paths.CoreMeta, b, 0o600)
}

func (c *Core) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cmd != nil
}

func (c *Core) Controller() (addr, secret string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.controller, c.secret
}

func (c *Core) LastError() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}

func (c *Core) setErr(err error) error {
	c.mu.Lock()
	if err != nil {
		c.lastErr = err.Error()
	} else {
		c.lastErr = ""
	}
	c.mu.Unlock()
	return err
}

// Test 用 mihomo -t 检查配置是否合法。
func (c *Core) Test(cfg []byte) error {
	if _, err := c.Verify(); err != nil {
		return nil // 没有核心时跳过，启动时再报错
	}
	f := filepath.Join(c.paths.Work, "test.yaml")
	if err := os.WriteFile(f, cfg, 0o600); err != nil {
		return err
	}
	defer os.Remove(f)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.paths.CoreBin, "-d", c.paths.Work, "-f", f, "-t")
	setupChild(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("配置校验失败：%s", lastLines(string(out), 5))
	}
	return nil
}

// Start 用 build 生成的配置启动核心。build 收到本次随机生成的控制地址与密钥。
func (c *Core) Start(build func(controller, secret string) ([]byte, error)) error {
	c.Stop()
	if _, err := c.Verify(); err != nil {
		return c.setErr(err)
	}
	port, err := freePort()
	if err != nil {
		return c.setErr(err)
	}
	controller := fmt.Sprintf("127.0.0.1:%d", port)
	secret := randHex(32)
	cfg, err := build(controller, secret)
	if err != nil {
		return c.setErr(err)
	}
	cfgPath := filepath.Join(c.paths.Work, "config.yaml")
	if err := writeFileAtomic(cfgPath, cfg, 0o600); err != nil {
		return c.setErr(err)
	}

	cmd := exec.Command(c.paths.CoreBin, "-d", c.paths.Work, "-f", cfgPath)
	setupChild(cmd)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return c.setErr(fmt.Errorf("启动核心失败：%v", err))
	}
	afterStart(cmd)
	os.WriteFile(filepath.Join(c.paths.Work, "core.pid"), []byte(fmt.Sprint(cmd.Process.Pid)), 0o600)
	done := make(chan struct{})
	go func() {
		s := bufio.NewScanner(pr)
		s.Buffer(make([]byte, 64*1024), 1024*1024)
		for s.Scan() {
			c.logs.add(s.Text())
		}
	}()
	go func() {
		cmd.Wait()
		pw.Close()
		c.mu.Lock()
		unexpected := c.cmd == cmd
		if unexpected {
			c.cmd = nil
			c.lastErr = "核心意外退出：" + lastLines(strings.Join(c.logs.snapshot(), "\n"), 3)
		}
		c.mu.Unlock()
		close(done)
		if unexpected && c.onExit != nil {
			c.onExit()
		}
	}()

	c.mu.Lock()
	c.cmd, c.done, c.controller, c.secret, c.lastErr = cmd, done, controller, secret, ""
	c.mu.Unlock()

	// 等待控制接口就绪
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return c.setErr(errors.New("核心启动后立即退出：" + lastLines(strings.Join(c.logs.snapshot(), "\n"), 5)))
		default:
		}
		if c.ping() == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	c.Stop()
	return c.setErr(errors.New("核心启动超时"))
}

func (c *Core) ping() error {
	req, _ := c.Request(context.Background(), "GET", "/version", nil)
	if req == nil {
		return errors.New("not running")
	}
	resp, err := (&http.Client{Timeout: time.Second}).Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

// Request 构造一个发往核心控制接口的请求（已带认证）。
func (c *Core) Request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	addr, secret := c.Controller()
	if addr == "" {
		return nil, errors.New("核心未运行")
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+addr+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	return req, nil
}

// Patch 修改运行中核心的配置（如模式）。
func (c *Core) Patch(fields map[string]any) error {
	if !c.Running() {
		return nil
	}
	b, _ := json.Marshal(fields)
	req, err := c.Request(context.Background(), "PATCH", "/configs", strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("核心返回 %d：%s", resp.StatusCode, msg)
	}
	return nil
}

func (c *Core) Stop() {
	c.mu.Lock()
	cmd, done := c.cmd, c.done
	c.cmd, c.controller, c.secret = nil, "", ""
	c.mu.Unlock()
	if cmd == nil {
		return
	}
	stopProcess(cmd)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		<-done
	}
	os.Remove(filepath.Join(c.paths.Work, "core.pid"))
}

func (c *Core) Logs() []string { return c.logs.snapshot() }

// LogMark 与 LogsSince 配合使用，只读取某一时刻之后的核心日志。
func (c *Core) LogMark() int { return c.logs.count() }

func (c *Core) LogsSince(mark int) []string { return c.logs.since(mark) }

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// replaceFile 用 src 替换 dst；Windows 下正在运行的文件不能覆盖，但可以改名。
func replaceFile(src, dst string) error {
	old := dst + ".old"
	os.Remove(old)
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			return err
		}
	}
	if err := os.Rename(src, dst); err != nil {
		os.Rename(old, dst)
		return err
	}
	os.Remove(old) // Windows 上可能失败，下次启动再清理
	return nil
}

type ring struct {
	mu    sync.Mutex
	max   int
	lines []string
	total int // 累计写入的行数
}

func (r *ring) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.total++
	r.lines = append(r.lines, s)
	if len(r.lines) > r.max {
		r.lines = r.lines[len(r.lines)-r.max:]
	}
}

// since 返回累计第 n 行之后写入的日志。
func (r *ring) since(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := min(r.total-n, len(r.lines))
	return append([]string(nil), r.lines[len(r.lines)-k:]...)
}

func (r *ring) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.total
}

func (r *ring) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}
