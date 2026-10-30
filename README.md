# SafeClash

一个**可以完全自己审计、自己构建、自己签名更新**的 Clash 图形客户端（Windows / macOS / Linux）。

- 代理内核：开源的 [mihomo](https://github.com/MetaCubeX/mihomo)（Clash Verge 用的也是它），**由你自己的 GitHub Actions 从官方源码编译**，不使用任何人预编译的二进制。
- 本程序：约 2000 行 Go + 一个原生 JS 页面，只有一个第三方依赖（`gopkg.in/yaml.v3`），一个下午就能读完。
- 信任根只有一个：**你自己的 ed25519 私钥**。客户端只安装被这把私钥签名的更新。

## 功能

| 功能 | 说明 |
|---|---|
| 订阅管理 | 添加 Clash 订阅链接 / 粘贴本地 YAML；显示流量与到期时间；按间隔自动更新 |
| 代理 | 规则 / 全局 / 直连模式；策略组切换节点；分组测速 |
| 系统代理 | Windows（注册表 + WinINet 刷新）、macOS（networksetup）、Linux GNOME（gsettings） |
| TUN 模式 | 接管全部流量（需以管理员 / root 身份运行） |
| 连接 / 日志 | 实时连接列表、单个或全部断开；实时日志；实时网速 |
| 开机自启 | 仅当前用户，无需管理员权限 |
| 自动更新 | 定期检查；校验签名与哈希后自动替换核心和程序本体并重启 |

## 安全设计

**1. 不信任订阅**（`internal/app/sanitize.go`）
机场下发的配置会被强制改写：删除 `external-controller`、`external-ui`、`listeners`、`tunnels`、`allow-lan`、`dns.listen`、`geox-url` 等字段，防止恶意订阅向局域网/公网开放端口或暴露控制接口。代理端口只监听 `127.0.0.1`。

**2. 核心控制接口**
每次启动随机端口 + 随机 256 位密钥，只监听 127.0.0.1，密钥只写在权限 0600 的配置文件中，界面拿不到。

**3. 本地界面**
- 只监听 127.0.0.1 随机端口，用一次性 URL 令牌换取 HttpOnly + SameSite=Strict Cookie。
- 校验 `Host`（防 DNS 重绑定）和 `Origin`（防其他网页跨站调用）。
- 严格 CSP，不加载任何外部资源；节点名等外部数据只用 `textContent` 渲染，防 XSS。
- 界面对核心 API 的访问走白名单，无法通过界面修改 `allow-lan` 等敏感配置。

**4. 更新链路**（`internal/app/updater.go`）
```
manifest.json ──ed25519 签名校验（内置你的公钥）──> 每个文件 SHA-256 校验 ──> 试运行 ──> 原子替换
```
- 清单被篡改、被别的私钥签名、文件被替换：全部拒绝（已测试）。
- **防降级**：即使签名有效，也拒绝比当前版本旧的清单。
- **防本地篡改**：每次启动核心前重新校验其哈希，与记录不符就拒绝运行。

**5. 无遥测**
本程序只会访问：你的订阅地址、你自己仓库的 GitHub Releases。没有统计、没有崩溃上报、没有第三方 CDN。

**6. 退出与崩溃**
核心意外退出时自动清除系统代理（不会断网）；Windows 用 Job Object、Linux 用 Pdeathsig 保证本程序崩溃时核心随之退出；macOS 下次启动时清理残留进程。

## 自动安全更新是怎么运作的

`.github/workflows/watch-upstream.yml` **每天**在你的仓库里自动运行：

1. mihomo 发布了新版本 → 更新 `core-version.txt`
2. Go 发布了安全补丁（标准库漏洞）→ 用新工具链重新编译
3. `govulncheck` 发现**本程序**依赖有漏洞 → 自动升级到修复版本并跑测试
4. `govulncheck` 发现 **mihomo 源码的依赖**有可修复漏洞 → 构建时自动升级这些依赖（`scripts/build-core.sh`）
5. mihomo 发布官方安全公告 → 在你的仓库开 issue 提醒

有任何一项变化，就自动提升版本号、构建、签名、发布。你电脑上的 SafeClash 按设置的间隔（默认 6 小时）检查，自动下载、校验、安装。含安全修复的版本会在界面上红色提示。

另外 Dependabot 每周检查依赖和 Actions 版本并提 PR。

## 一次性配置（约 10 分钟）

**1. 建一个公开（Public）GitHub 仓库**并推送本代码。
客户端不登录，直接从 Releases 下载更新，私有仓库的发布文件会返回 404。公开仓库不影响安全：安全依赖你的私钥，而不是代码保密，GitHub Secret 在公开仓库中同样不会泄露。
注意隐私：公开后任何人都能看到你的 GitHub 用户名和提交记录。建议在 Settings → Emails 勾选 “Keep my email addresses private” 和 “Block command line pushes that expose my email”，提交时使用 GitHub 提供的 noreply 邮箱。

**2. 生成签名密钥**（在你自己的电脑上执行）：
```bash
go run ./cmd/sctool keygen
```
- 公钥 → 覆盖写入仓库根目录 `signing.pub` 并提交。
- 私钥 → 仓库 Settings → Secrets and variables → Actions → New secret，名称 `SIGNING_KEY`。**同时离线备份**（如密码管理器）。私钥丢了只能重新装一次客户端；私钥泄露则必须换钥并重装。

**3. 发布第一个版本**：
```bash
git tag v0.1.0
git push origin v0.1.0
```
等待 Actions 完成后，在 Releases 页下载对应平台的文件：

| 平台 | 文件 |
|---|---|
| Windows | `safeclash-windows-amd64.exe` |
| Mac（M 系列） | `safeclash-darwin-arm64` |
| Mac（Intel） | `safeclash-darwin-amd64` |

**4. 首次运行**：打开后浏览器会自动打开控制界面 → 「设置」→「立即更新」下载签名核心 → 「订阅」添加链接 → 点「启动」→ 打开「系统代理」。
再次双击程序即可重新打开界面；在「设置」底部点「退出」完全关闭。

> 发布产物附带 `SHA256SUMS.txt`。首次下载后，可以用 `go run ./cmd/sctool verify -pub "$(cat signing.pub)" manifest.json`（需与清单文件放在同一目录）离线验证全部文件。

### 平台说明
- **Windows**：首次运行 SmartScreen 会提示“未知发布者”（没有购买代码签名证书），点“更多信息 → 仍要运行”。TUN 模式需右键“以管理员身份运行”。
- **macOS**：浏览器下载的文件需执行 `chmod +x safeclash-darwin-arm64 && xattr -d com.apple.quarantine safeclash-darwin-arm64`。TUN 模式需 `sudo` 运行。
- **Linux**：系统代理仅支持 GNOME；其他桌面请手动把代理设为 `127.0.0.1:7897`。

## 本地开发

```bash
go test ./...
go build -o safeclash .
./safeclash --import-core /path/to/mihomo   # 开发版没有内置公钥，可手动导入核心
./safeclash
```
数据目录：Windows `%AppData%\SafeClash`，macOS `~/Library/Application Support/SafeClash`，Linux `~/.config/SafeClash`。

## 与 Clash Verge 相比尚未实现的
- 系统托盘图标（需要 cgo，目前用浏览器界面 + 再次双击打开）
- TUN 的后台服务模式（目前需以管理员身份运行整个程序）
- 配置覆写 / 脚本扩展、规则编辑器
- macOS 的 .app 打包与公证、Windows 安装包

## 需要知道的局限
- **私钥就是一切**：GitHub Secret 能被有仓库写权限的人通过修改 workflow 读取。不要添加协作者，并为 GitHub 账号开启两步验证。来自他人 fork 的 PR 无法读取 Secret。
- 自动修补只能升级**依赖**到已修复版本；mihomo **自身代码**的漏洞需等上游发布修复（工作流会第一时间自动跟进）。
- GitHub 会在仓库 60 天无活动后暂停定时工作流；收到邮件提醒时点一下 “Enable” 即可。
- mihomo 采用 GPL-3.0 许可，自用没有问题；若要分发给他人，需同时提供源码。
