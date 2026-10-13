#!/bin/sh
set -eu

# 绑定挂载会遮盖镜像中的目录，因此每次启动都准备工作目录内的临时目录。
mkdir -p "$TMPDIR"
exec /usr/local/bin/remote-mcp "$@"
