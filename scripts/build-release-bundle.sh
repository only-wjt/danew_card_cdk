#!/usr/bin/env bash
# 构建管理后台一键更新用的预编译包 cdk-bundle-linux-amd64.tgz
# 用法：./scripts/build-release-bundle.sh [version]
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VER="${1:-}"
if [[ -z "$VER" ]]; then
  VER="$(tr -d ' \n' < "$ROOT/VERSION" 2>/dev/null || echo 0.0.0)"
fi
VER="${VER#v}"
echo "$VER" > "$ROOT/VERSION"
mkdir -p "$ROOT/dist/web"
echo "==> go build v$VER (linux/amd64)"
# SQLite 需要 CGO。CGO_ENABLED=0 的 ELF 能启动，但初始化数据库时会退出。
bash "$ROOT/scripts/build-linux-backend.sh" "$ROOT/dist/cdk-recharge" "$VER"
if command -v file >/dev/null 2>&1; then
  if ! file "$ROOT/dist/cdk-recharge" | grep -q "ELF 64-bit.*x86-64"; then
    echo "产物不是 linux/amd64 ELF，拒绝打包：$(file "$ROOT/dist/cdk-recharge")"
    exit 1
  fi
fi
echo "==> frontend build"
cd "$ROOT/frontend"
if [[ -f package-lock.json ]]; then npm ci; else npm install; fi
npm run build
rm -rf "$ROOT/dist/web"
mkdir -p "$ROOT/dist/web"
cp -a dist/. "$ROOT/dist/web/"
cp "$ROOT/VERSION" "$ROOT/dist/VERSION"
cd "$ROOT/dist"
tar -czf "$ROOT/cdk-bundle-linux-amd64.tgz" cdk-recharge web VERSION
sha256sum "$ROOT/cdk-bundle-linux-amd64.tgz" | tee "$ROOT/cdk-bundle-linux-amd64.tgz.sha256"
ls -lh "$ROOT/cdk-bundle-linux-amd64.tgz"*
echo "==> done. 上传到 GitHub Release 资产："
echo "    gh release upload v${VER} cdk-bundle-linux-amd64.tgz cdk-bundle-linux-amd64.tgz.sha256 --clobber"
