#!/bin/sh
# node_modules 是 compose 的 named volume,只在首次创建时从镜像拷贝;之后镜像里
# 重新 npm ci 不会同步到卷里。这里按 package.json + 锁文件哈希判断,变了就重装。
set -eu

stamp=node_modules/.grasp-deps-hash
want="$(cat package.json package-lock.json | sha256sum | cut -d' ' -f1)"
have="$(cat "$stamp" 2>/dev/null || true)"

if [ "$want" != "$have" ]; then
  echo "dependencies changed; running npm ci..."
  npm ci --no-audit --no-fund
  echo "$want" > "$stamp"
fi

exec "$@"
