#!/usr/bin/env bash
# 构建 SafeClash 本体（所有平台）。
# 用法: scripts/build-app.sh <版本> <owner/repo> <输出目录>
set -euo pipefail
VERSION="$1"; REPO="$2"; OUT="$3"
PUB="$(tr -d '[:space:]' < signing.pub)"
mkdir -p "$OUT"
LD="-s -w -buildid= -X safeclash/internal/app.Version=$VERSION -X safeclash/internal/app.UpdateRepo=$REPO -X safeclash/internal/app.PublicKey=$PUB"
for target in windows/amd64 windows/arm64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
  os="${target%/*}"; arch="${target#*/}"
  ext=""; extra=""
  [ "$os" = windows ] && { ext=".exe"; extra="-H windowsgui"; }
  echo "构建 SafeClash $os/$arch"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "$LD $extra" -o "$OUT/safeclash-$os-$arch$ext" .
done
