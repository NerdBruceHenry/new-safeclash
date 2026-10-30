// SafeClash：一个可完全自行审计、自行构建、自行签名更新的 mihomo 图形管理器。
package main

import (
	"embed"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"safeclash/internal/app"
)

//go:embed web
var webFS embed.FS

func main() {
	silent := flag.Bool("silent", false, "启动时不打开浏览器（开机自启使用）")
	dataDir := flag.String("data-dir", "", "数据目录（默认为系统用户配置目录下的 SafeClash）")
	importCore := flag.String("import-core", "", "导入本地 mihomo 二进制并信任它，然后退出")
	showVer := flag.Bool("version", false, "显示版本")
	flag.Parse()

	if *showVer {
		fmt.Println("SafeClash", app.Version)
		return
	}
	paths, err := app.NewPaths(*dataDir)
	if err != nil {
		fatal(err)
	}
	logf, err := os.OpenFile(paths.Log, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		fatal(err)
	}
	logger := log.New(io.MultiWriter(os.Stderr, logf), "", log.LstdFlags)

	a, err := app.New(paths, logger)
	if err != nil {
		fatal(err)
	}
	if *importCore != "" {
		if err := a.ImportCore(*importCore); err != nil {
			fatal(err)
		}
		fmt.Println("核心已导入")
		return
	}

	// 单实例：已在运行则只打开界面
	if inst, ok := app.FindRunning(paths); ok {
		if !*silent {
			openBrowser(inst.URL())
		}
		return
	}

	sub, _ := fs.Sub(webFS, "web")
	inst, err := a.Serve(http.FS(sub))
	if err != nil {
		fatal(err)
	}
	logger.Printf("SafeClash %s 已启动，界面地址 http://127.0.0.1:%d", app.Version, inst.Port)
	a.Run()
	if !*silent {
		openBrowser(inst.URL())
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
		a.Quit()
	case <-a.Done():
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "错误：", err)
	os.Exit(1)
}
