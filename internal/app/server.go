package app

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Instance 写在 instance.json 中，供第二次启动时找到正在运行的实例。
type Instance struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

func (i Instance) URL() string {
	return "http://127.0.0.1:" + strconv.Itoa(i.Port) + "/?token=" + i.Token
}

// FindRunning 返回已在运行的实例（若有）。
func FindRunning(p Paths) (Instance, bool) {
	var inst Instance
	b, err := os.ReadFile(p.Instance)
	if err != nil || json.Unmarshal(b, &inst) != nil || inst.Port == 0 {
		return inst, false
	}
	req, _ := http.NewRequest("GET", "http://127.0.0.1:"+strconv.Itoa(inst.Port)+"/api/ping", nil)
	req.AddCookie(&http.Cookie{Name: "sc_token", Value: inst.Token})
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return inst, false
	}
	resp.Body.Close()
	return inst, resp.StatusCode == 200
}

// Serve 在 127.0.0.1 的随机端口上启动界面服务，只监听本机。
func (a *App) Serve(web http.FileSystem) (Instance, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Instance{}, err
	}
	inst := Instance{Port: ln.Addr().(*net.TCPAddr).Port, Token: randHex(32), PID: os.Getpid()}
	b, _ := json.Marshal(inst)
	if err := writeFileAtomic(a.paths.Instance, b, 0o600); err != nil {
		ln.Close()
		return inst, err
	}
	srv := &http.Server{Handler: a.handler(inst, web), ReadHeaderTimeout: 10 * time.Second}
	a.shutdown = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}
	go srv.Serve(ln)
	return inst, nil
}

func (a *App) handler(inst Instance, web http.FileSystem) http.Handler {
	mux := http.NewServeMux()
	static := http.FileServer(web)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if t := r.URL.Query().Get("token"); t != "" {
			if subtle.ConstantTimeCompare([]byte(t), []byte(inst.Token)) == 1 {
				http.SetCookie(w, &http.Cookie{Name: "sc_token", Value: inst.Token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			}
			http.Redirect(w, r, "/", http.StatusFound) // 去掉地址栏里的令牌
			return
		}
		static.ServeHTTP(w, r)
	})
	mux.Handle("GET /assets/", static)

	api := http.NewServeMux()
	api.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	api.HandleFunc("GET /api/status", a.apiStatus)
	api.HandleFunc("POST /api/core/start", func(w http.ResponseWriter, r *http.Request) { reply(w, nil, a.StartCore()) })
	api.HandleFunc("POST /api/core/stop", func(w http.ResponseWriter, r *http.Request) { a.StopCore(); reply(w, nil, nil) })
	api.HandleFunc("GET /api/core/logs", func(w http.ResponseWriter, r *http.Request) { reply(w, a.core.Logs(), nil) })
	api.HandleFunc("POST /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var p SettingsPatch
		if err := decode(r, &p); err != nil {
			reply(w, nil, err)
			return
		}
		reply(w, nil, a.ApplySettings(p))
	})
	api.HandleFunc("POST /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name, URL, Content string }
		if err := decode(r, &req); err != nil {
			reply(w, nil, err)
			return
		}
		p, err := a.AddProfile(req.Name, req.URL, req.Content)
		reply(w, p, err)
	})
	api.HandleFunc("POST /api/profiles/{id}/update", func(w http.ResponseWriter, r *http.Request) { reply(w, nil, a.UpdateProfile(r.PathValue("id"))) })
	api.HandleFunc("POST /api/profiles/{id}/select", func(w http.ResponseWriter, r *http.Request) { reply(w, nil, a.SelectProfile(r.PathValue("id"))) })
	api.HandleFunc("POST /api/profiles/{id}/interval", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Hours int }
		if err := decode(r, &req); err != nil {
			reply(w, nil, err)
			return
		}
		reply(w, nil, a.SetProfileInterval(r.PathValue("id"), req.Hours))
	})
	api.HandleFunc("DELETE /api/profiles/{id}", func(w http.ResponseWriter, r *http.Request) { reply(w, nil, a.DeleteProfile(r.PathValue("id"))) })
	api.HandleFunc("GET /api/profiles/{id}/content", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !validID(id) {
			reply(w, nil, errors.New("配置不存在"))
			return
		}
		b, err := os.ReadFile(a.profilePath(id))
		reply(w, string(b), err)
	})
	api.HandleFunc("POST /api/update/check", func(w http.ResponseWriter, r *http.Request) {
		_, err := a.CheckUpdate()
		reply(w, a.UpdateStatus(), err)
	})
	api.HandleFunc("POST /api/update/apply", func(w http.ResponseWriter, r *http.Request) { reply(w, a.UpdateStatus(), a.ApplyUpdate()) })
	api.HandleFunc("POST /api/quit", func(w http.ResponseWriter, r *http.Request) {
		reply(w, nil, nil)
		go func() { time.Sleep(200 * time.Millisecond); a.Quit() }()
	})
	api.Handle("/api/mihomo/", a.coreProxy())

	mux.Handle("/api/", a.guard(inst, api))
	return securityHeaders(inst, mux)
}

// guard 校验 Cookie 令牌和 Origin，防止网页跨站调用本地接口。
func (a *App) guard(inst Instance, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("sc_token")
		if err != nil || subtle.ConstantTimeCompare([]byte(c.Value), []byte(inst.Token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != "http://127.0.0.1:"+strconv.Itoa(inst.Port) {
			http.Error(w, "bad origin", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// securityHeaders 校验 Host（防 DNS 重绑定）并设置严格的 CSP。
func securityHeaders(inst Instance, next http.Handler) http.Handler {
	host := "127.0.0.1:" + strconv.Itoa(inst.Port)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host {
			http.Error(w, "bad host", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// 界面只能调用核心接口的这些部分；修改核心配置必须走 /api/settings。
var coreAllow = map[string][]string{
	"GET":    {"/version", "/proxies", "/group", "/providers", "/connections", "/logs", "/traffic", "/memory", "/rules", "/configs"},
	"PUT":    {"/proxies/", "/providers/"},
	"DELETE": {"/connections"},
}

func (a *App) coreProxy() http.Handler {
	rp := &httputil.ReverseProxy{
		FlushInterval: -1, // 日志、流量是流式接口
		Rewrite: func(pr *httputil.ProxyRequest) {
			addr, secret := a.core.Controller()
			path := strings.TrimPrefix(pr.In.URL.EscapedPath(), "/api/mihomo")
			u, _ := url.Parse("http://" + addr + path)
			u.RawQuery = pr.In.URL.RawQuery
			pr.Out.URL = u
			pr.Out.Host = addr
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Del("Origin")
			pr.Out.Header.Set("Authorization", "Bearer "+secret)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.core.Running() {
			http.Error(w, "核心未运行", http.StatusServiceUnavailable)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/mihomo")
		if strings.Contains(path, "..") || !allowed(r.Method, path) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		rp.ServeHTTP(w, r)
	})
}

func allowed(method, path string) bool {
	for _, p := range coreAllow[method] {
		if path == p || strings.HasPrefix(path, strings.TrimSuffix(p, "/")+"/") {
			return true
		}
	}
	return false
}

func (a *App) apiStatus(w http.ResponseWriter, r *http.Request) {
	meta, verr := a.core.Verify()
	coreErr := a.core.LastError()
	if verr != nil && coreErr == "" {
		coreErr = verr.Error()
	}
	reply(w, map[string]any{
		"app_version":  Version,
		"update_repo":  UpdateRepo,
		"signed_build": UpdateRepo != "" && PublicKey != "",
		"core":         meta,
		"core_ok":      verr == nil,
		"running":      a.core.Running(),
		"core_error":   coreErr,
		"settings":     a.store.Get(),
		"update":       a.UpdateStatus(),
		"tun":          a.TunStatus(),
		"admin":        isElevated(),
		"data_dir":     a.paths.Root,
	}, nil)
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxProfileSize+4096)).Decode(v)
}

func reply(w http.ResponseWriter, data any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"data": data})
}
