#!/bin/sh
# 开发镜像与 dev-entrypoint.sh 共用的依赖安装。先用 .npmrc 的镜像源，失败后按
# 同一份锁文件与 integrity 改从官方源重装；两次都限制超时与重试，与
# server/Dockerfile 的 web-builder 相同，单个 tarball 卡住不会拖住整次构建。
set -eu

official=https://registry.npmjs.org
set -- --no-audit --no-fund --replace-registry-host=always \
  --fetch-retries=2 --fetch-retry-maxtimeout=30000 --fetch-timeout=120000

if npm ci "$@"; then
  exit 0
fi
echo "npm ci from the configured registry failed; retrying from $official" >&2
npm ci "$@" --registry="$official"
