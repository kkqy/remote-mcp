#!/usr/bin/env bash
set -euo pipefail
# 从任何工作目录调用，产物统一写入仓库 dist 目录。
cd "$(dirname "$0")/.."
for target_os in linux windows; do
  for target_arch in amd64 arm64; do
    destination="dist/${target_os}-${target_arch}"
    mkdir -p "$destination"
    suffix=""
    if [[ "$target_os" == "windows" ]]; then suffix=".exe"; fi
    for command in remote-mcp remote-mcp-transfer; do
      CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -o "$destination/$command$suffix" "./cmd/$command"
    done
  done
done
