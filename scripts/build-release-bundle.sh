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
cd "$ROOT/backend"
# 产物名字是 cdk-bundle-linux-amd64.tgz，必须显式交叉编译。
# 不设 GOOS/GOARCH 时，在 macOS 上会把 Mach-O 二进制打进这个包，
# 上传和一键更新都报成功，生产机重启才报 Exec format error。
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w -X github.com/danew/cdk-recharge-system/internal/handler.BuildVersion=${VER}" \
  -o "$ROOT/dist/cdk-recharge" ./cmd/server
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
