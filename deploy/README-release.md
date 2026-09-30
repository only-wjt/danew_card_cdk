# 发布包与一键更新

管理后台「一键无痕更新」依赖 GitHub Release 上的预编译包：

- `cdk-bundle-linux-amd64.tgz`（内含 `cdk-recharge` + `web/` + `VERSION`）
- 可选 `cdk-bundle-linux-amd64.tgz.sha256`

## 本地构建

发布包须在 Linux amd64 + gcc 环境构建。SQLite 驱动依赖 CGO，不能使用
`CGO_ENABLED=0`；这种二进制虽为 ELF，却会在初始化数据库时退出。
`build-linux-backend.sh` 生成含 SQLite 的静态程序，并明确拒绝在 macOS 上裸交叉编译。
macOS 可使用仓库 Dockerfile 的 Linux 构建环境。

```bash
./scripts/build-release-bundle.sh 1.2.4
```

## 发布到 GitHub

```bash
git tag v1.2.4 && git push origin v1.2.4
gh release create v1.2.4 cdk-bundle-linux-amd64.tgz cdk-bundle-linux-amd64.tgz.sha256 --generate-notes
```

## 启用 GitHub Actions（可选）

将 `deploy/github-release.workflow.yml` 复制为仓库内：

`.github/workflows/release.yml`

推送该文件需要 PAT 勾选 **workflow** 权限。之后 `git tag v*` 会自动构建并上传 bundle。
