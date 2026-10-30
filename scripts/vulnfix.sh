#!/usr/bin/env bash
# 从标准输入读取 govulncheck -format json 的结果，在当前 Go 模块中
# 把每个有漏洞的第三方模块升级到它所有修复版本中最高的那个。
# 只升级、不降级：降级会连带把其他模块拉回到有漏洞的旧版本。
# 标准输出打印实际升级的 模块@版本。
set -euo pipefail
jq -r 'select(.finding) | .finding | select(.fixed_version != null and .trace[0].module != "stdlib") | "\(.trace[0].module) \(.fixed_version)"' \
  | sort -u | sort -k1,1 -k2,2V \
  | awk '{ max[$1] = $2 } END { for (m in max) print m, max[m] }' | sort \
  | while read -r mod want; do
      cur=$(go list -m -f '{{.Version}}' "$mod" 2>/dev/null || true)
      if [ -n "$cur" ] && [ "$(printf '%s\n%s\n' "$cur" "$want" | sort -V | tail -n1)" = "$cur" ]; then
        continue
      fi
      go get "$mod@$want" >&2
      echo "$mod@$want"
    done
