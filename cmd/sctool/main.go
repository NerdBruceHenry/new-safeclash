// sctool 是发布辅助工具：生成签名密钥、生成并签名更新清单、校验清单。
//
//	sctool keygen
//	sctool manifest -dir dist -app-version 1.0.0 -core-version v1.19.33 [-security] [-notes ...]
//	sctool sign dist/manifest.json        (私钥从环境变量 SIGNING_KEY 读取)
//	sctool verify -pub <公钥> dist/manifest.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"safeclash/internal/app"
	"safeclash/internal/sign"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "keygen":
		pub, seed, err := sign.GenerateKey()
		check(err)
		fmt.Println("公钥（提交到仓库 signing.pub，可公开）：")
		fmt.Println(pub)
		fmt.Println("\n私钥（只放进 GitHub Secret SIGNING_KEY，并离线备份，绝不要提交）：")
		fmt.Println(seed)
	case "manifest":
		fs := flag.NewFlagSet("manifest", flag.ExitOnError)
		dir := fs.String("dir", "dist", "构建产物目录")
		appVer := fs.String("app-version", "", "")
		coreVer := fs.String("core-version", "", "")
		security := fs.Bool("security", false, "本次包含安全修复")
		notes := fs.String("notes", "", "")
		fs.Parse(os.Args[2:])
		writeManifest(*dir, *appVer, *coreVer, *security, *notes)
	case "sign":
		data, err := os.ReadFile(os.Args[2])
		check(err)
		sig, err := sign.Sign(os.Getenv("SIGNING_KEY"), data)
		check(err)
		check(os.WriteFile(os.Args[2]+".sig", []byte(sig), 0o644))
	case "verify":
		fs := flag.NewFlagSet("verify", flag.ExitOnError)
		pub := fs.String("pub", "", "公钥")
		fs.Parse(os.Args[2:])
		file := fs.Arg(0)
		data, err := os.ReadFile(file)
		check(err)
		sig, err := os.ReadFile(file + ".sig")
		check(err)
		check(sign.Verify(*pub, data, string(sig)))
		var m app.Manifest
		check(json.Unmarshal(data, &m))
		for key, as := range m.Assets {
			sum, err := sign.FileSHA256(filepath.Join(filepath.Dir(file), as.File))
			check(err)
			if sum != as.SHA256 {
				check(fmt.Errorf("%s 哈希不符", key))
			}
		}
		fmt.Println("签名与所有文件哈希校验通过")
	default:
		usage()
	}
}

// 文件名约定：safeclash-<os>-<arch>[.exe] 与 mihomo-<os>-<arch>[.exe]
func writeManifest(dir, appVer, coreVer string, security bool, notes string) {
	m := app.Manifest{
		AppVersion: appVer, CoreVersion: coreVer, GoVersion: runtime.Version(),
		BuiltAt: time.Now().UTC().Format(time.RFC3339), Security: security, Notes: notes,
		Assets: map[string]app.Asset{},
	}
	entries, err := os.ReadDir(dir)
	check(err)
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".exe")
		var kind string
		switch {
		case strings.HasPrefix(name, "safeclash-"):
			kind = "app-" + strings.TrimPrefix(name, "safeclash-")
		case strings.HasPrefix(name, "mihomo-"):
			kind = "core-" + strings.TrimPrefix(name, "mihomo-")
		default:
			continue
		}
		path := filepath.Join(dir, e.Name())
		sum, err := sign.FileSHA256(path)
		check(err)
		info, _ := e.Info()
		m.Assets[kind] = app.Asset{File: e.Name(), SHA256: sum, Size: info.Size()}
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	check(os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o644))
	fmt.Printf("已写入 %d 个文件的清单\n", len(m.Assets))
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: sctool keygen | manifest | sign <file> | verify -pub <key> <file>")
	os.Exit(2)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}
