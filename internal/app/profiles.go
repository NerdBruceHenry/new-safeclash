package app

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxProfileSize = 20 << 20

func (a *App) profilePath(id string) string {
	return filepath.Join(a.paths.Profiles, id+".yaml")
}

func validID(id string) bool {
	if len(id) != 16 {
		return false
	}
	for _, r := range id {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// AddProfile 添加订阅（url 非空）或本地配置（content 非空）。
func (a *App) AddProfile(name, rawURL, content string) (Profile, error) {
	p := Profile{ID: randHex(8), Name: strings.TrimSpace(name), IntervalHours: 12}
	if rawURL != "" {
		u, err := url.Parse(strings.TrimSpace(rawURL))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return p, errors.New("订阅链接无效")
		}
		p.URL = u.String()
		if p.Name == "" {
			p.Name = u.Hostname()
		}
		data, err := a.fetchProfile(&p)
		if err != nil {
			return p, err
		}
		content = string(data)
	} else {
		p.IntervalHours = 0
		if p.Name == "" {
			p.Name = "本地配置"
		}
	}
	if err := a.validateProfile([]byte(content)); err != nil {
		return p, err
	}
	if err := os.WriteFile(a.profilePath(p.ID), []byte(content), 0o600); err != nil {
		return p, err
	}
	p.UpdatedAt = time.Now()
	_, err := a.store.Update(func(s *Settings) {
		s.Profiles = append(s.Profiles, p)
		if s.ActiveProfile == "" {
			s.ActiveProfile = p.ID
		}
	})
	return p, err
}

func (a *App) validateProfile(data []byte) error {
	cfg, err := Sanitize(data, a.store.Get(), "127.0.0.1:0", "x")
	if err != nil {
		return err
	}
	return a.core.Test(cfg)
}

// UpdateProfile 重新下载订阅。若它是当前配置且核心在运行，则重新加载。
func (a *App) UpdateProfile(id string) error {
	p, ok := a.findProfile(id)
	if !ok {
		return errors.New("配置不存在")
	}
	if p.URL == "" {
		return errors.New("本地配置无需更新")
	}
	data, err := a.fetchProfile(&p)
	if err == nil {
		err = a.validateProfile(data)
	}
	if err == nil {
		err = os.WriteFile(a.profilePath(id), data, 0o600)
	}
	a.store.Update(func(s *Settings) {
		for i := range s.Profiles {
			if s.Profiles[i].ID == id {
				if err != nil {
					s.Profiles[i].LastError = err.Error()
				} else {
					p.LastError, p.UpdatedAt = "", time.Now()
					s.Profiles[i] = p
				}
			}
		}
	})
	if err != nil {
		return err
	}
	if a.store.Get().ActiveProfile == id && a.core.Running() {
		return a.StartCore()
	}
	return nil
}

func (a *App) fetchProfile(p *Profile) ([]byte, error) {
	ua := p.UserAgent
	if ua == "" {
		ua = "clash.meta"
	}
	req, err := http.NewRequest("GET", p.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
	resp, err := a.httpClient(60 * time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载订阅失败：%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("下载订阅失败：HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxProfileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxProfileSize {
		return nil, errors.New("订阅内容过大")
	}
	parseUserInfo(resp.Header.Get("Subscription-Userinfo"), p)
	return data, nil
}

// parseUserInfo 解析 "upload=1; download=2; total=3; expire=4"。
func parseUserInfo(h string, p *Profile) {
	for _, part := range strings.Split(h, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		switch k {
		case "upload":
			p.Upload = n
		case "download":
			p.Download = n
		case "total":
			p.Total = n
		case "expire":
			p.Expire = n
		}
	}
}

func (a *App) findProfile(id string) (Profile, bool) {
	for _, p := range a.store.Get().Profiles {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}

func (a *App) SelectProfile(id string) error {
	if _, ok := a.findProfile(id); !ok {
		return errors.New("配置不存在")
	}
	a.store.Update(func(s *Settings) { s.ActiveProfile = id })
	if a.core.Running() {
		return a.StartCore()
	}
	return nil
}

func (a *App) DeleteProfile(id string) error {
	if !validID(id) {
		return errors.New("配置不存在")
	}
	if a.store.Get().ActiveProfile == id && a.core.Running() {
		return errors.New("不能删除正在使用的配置，请先切换或停止")
	}
	os.Remove(a.profilePath(id))
	_, err := a.store.Update(func(s *Settings) {
		out := s.Profiles[:0]
		for _, p := range s.Profiles {
			if p.ID != id {
				out = append(out, p)
			}
		}
		s.Profiles = out
		if s.ActiveProfile == id {
			s.ActiveProfile = ""
			if len(out) > 0 {
				s.ActiveProfile = out[0].ID
			}
		}
	})
	return err
}

func (a *App) SetProfileInterval(id string, hours int) error {
	if hours < 0 {
		hours = 0
	}
	_, err := a.store.Update(func(s *Settings) {
		for i := range s.Profiles {
			if s.Profiles[i].ID == id {
				s.Profiles[i].IntervalHours = hours
			}
		}
	})
	return err
}

// autoUpdateProfiles 由调度器调用，更新到期的订阅。
func (a *App) autoUpdateProfiles() {
	for _, p := range a.store.Get().Profiles {
		if p.URL != "" && p.IntervalHours > 0 && time.Since(p.UpdatedAt) > time.Duration(p.IntervalHours)*time.Hour {
			if err := a.UpdateProfile(p.ID); err != nil {
				a.log.Printf("自动更新订阅 %s 失败：%v", p.Name, err)
			}
		}
	}
}

// httpClient 在核心运行且开启「经代理更新」时走本地代理，否则直连。
func (a *App) httpClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 15 * time.Second}
	s := a.store.Get()
	if s.UpdateViaProxy && a.core.Running() {
		u, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", s.MixedPort))
		tr.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Timeout: timeout, Transport: tr}
}
