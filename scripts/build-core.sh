#!/usr/bin/env bash
# 从 mihomo 官方源码构建核心，并自动把有已知漏洞的依赖升级到已修复版本。
# 用法: scripts/build-core.sh <mihomo-tag> <输出目录>
set -euo pipefail
TAG="$1"
OUT="$(realpath -m "$2")"
VULNFIX="$(realpath "$(dirname "$0")")/vulnfix.sh"
WORK="$(mktemp -d)"
mkdir -p "$OUT"

git clone --quiet --depth 1 --branch "$TAG" https://github.com/MetaCubeX/mihomo.git "$WORK/src"
cd "$WORK/src"
echo "mihomo 源码提交: $(git rev-parse HEAD)" | tee "$OUT/core-source.txt"

go install golang.org/x/vuln/cmd/govulncheck@latest
GOVULN="$(go env GOPATH)/bin/govulncheck"

# 把有漏洞的第三方依赖升级到已修复版本（标准库漏洞通过使用最新 Go 工具链修复）
fixes=$("$GOVULN" -format json ./... 2>/dev/null | "$VULNFIX")
if [ -n "$fixes" ]; then
  go mod tidy
  echo "自动修补依赖漏洞:"; echo "$fixes"
  echo "$fixes" > "$OUT/core-patches.txt"
fi
echo "--- 修补后漏洞扫描 ---"
"$GOVULN" ./... | tee "$OUT/core-govulncheck.txt" || echo "::warning::mihomo 仍存在无法自动修复的漏洞，详见 core-govulncheck.txt"

LDFLAGS="-s -w -buildid= -X github.com/metacubex/mihomo/constant.Version=$TAG -X github.com/metacubex/mihomo/constant.BuildTime=$(git log -1 --format=%cI)"
for target in windows/amd64 windows/arm64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
  os="${target%/*}"; arch="${target#*/}"
  ext=""; [ "$os" = windows ] && ext=".exe"
  echo "构建 mihomo $os/$arch"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -tags with_gvisor -ldflags "$LDFLAGS" -o "$OUT/mihomo-$os-$arch$ext" .
done
rm -rf "$WORK"
