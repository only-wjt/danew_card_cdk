# CDK 充值系统 - 快速启动指南

## ✅ 编译成功！

后端和前端都已编译成功。可以直接本地运行，无需 Docker。

### 文件位置

下面所有路径都相对仓库根目录，请先 `cd` 到你自己 clone 的位置。

- **后端可执行文件**: `backend/cdk-recharge`（Windows 下是 `cdk-recharge.exe`）
- **前端打包文件**: `frontend/dist/` (包含 HTML + CSS + JS)

---

## 📋 前置要求

### 1. 数据库：SQLite，无需任何准备

后端用的是 SQLite 单文件数据库，不需要 PostgreSQL，也不需要 Redis。
`backend/internal/db/db.go` 的 `Init()` 直接打开数据库文件，缺文件会自动创建，表也在启动时自动建好：

- 默认路径 `../data/cdk_recharge.db`（相对 `backend` 目录，即仓库根的 `data/cdk_recharge.db`）
- 想换位置就设环境变量 `DB_PATH`，例如 `DB_PATH=/opt/cdk-recharge/data/cdk_recharge.db`

唯一要保证的是父目录存在且可写：

```bash
mkdir -p data
```

### 2. 编译需要 cgo

`mattn/go-sqlite3` 是 cgo 绑定，`CGO_ENABLED=0` 编出来的二进制启动时会报
`go-sqlite3 requires cgo to work. This is a stub`。编译时确保 `CGO_ENABLED=1` 且本机有 C 编译器（gcc / clang / MinGW-w64）。

### 3. Windows 开发者：注意 32 位 Go 这个坑

如果本机装的是 32 位 Go（`go env GOARCH` 输出 `386`），默认没有可用的 cgo 工具链，
`go build` 会静默退回 `CGO_ENABLED=0`，跑起来就是上面那个 stub 报错。
解决办法是装 MinGW-w64 后交叉编译到 amd64（PowerShell）：

```powershell
# 把 MinGW-w64 的 bin 加进 PATH（路径按自己的安装位置改）
$env:PATH="C:\Users\<你>\AppData\Local\Microsoft\WinGet\Packages\BrechtSanders.WinLibs.POSIX.UCRT_Microsoft.Winget.Source_8wekyb3d8bbwe\mingw64\bin;$env:PATH"
$env:CGO_ENABLED='1'; $env:GOARCH='amd64'; $env:CC='x86_64-w64-mingw32-gcc'
cd backend; go build -o cdk-recharge.exe ./cmd/server
```

`go env GOARCH` 已经是 `amd64` 的机器只需要装好 gcc 并设 `CGO_ENABLED=1`，不用指定 `GOARCH` / `CC`。

---

## 🚀 运行项目

### 方法 1: 直接运行后端 + 前端开发模式

**终端 1 - 启动后端:**
```bash
cd backend            # 相对仓库根目录

# 设置环境变量（后端只读进程环境变量，不会自动加载 .env）
export SERVER_HOST=0.0.0.0
export SERVER_PORT=8080
export SERVER_MODE=debug
export JWT_SECRET=dev-secret-key-at-least-16-chars
# 可选：不设则用 ../data/cdk_recharge.db
export DB_PATH=../data/cdk_recharge.db

# 运行后端
./cdk-recharge

# 或用 Go 运行（改代码后重启即可）
CGO_ENABLED=1 go run ./cmd/server/main.go
```

**终端 2 - 启动前端开发服务器:**
```bash
cd frontend           # 相对仓库根目录
npm run dev
```

**终端 3 - 测试 API:**
```bash
# 检查后端健康状态
curl http://localhost:8080/health

# 预期响应:
# {"message":"Recharge System is running","status":"ok"}
```

### 方法 2: 使用启动脚本
```bash
# 在仓库根目录执行

# 使脚本可执行
chmod +x start-dev.sh

# 运行脚本（会同时启动后端和前端）
./start-dev.sh
```

---

## 🌐 访问应用

启动后，访问以下地址：

- **前端界面**: http://localhost:5173
- **后端 API**: http://localhost:8080/api/v1
- **健康检查**: http://localhost:8080/health

---

## 📊 现在的功能

### ✅ 已完成
- [x] 项目结构搭建、前后端均可编译
- [x] 数据库 Schema + 启动自动建表/迁移（`backend/internal/db/db.go`）
- [x] 完整认证：JWT 签发（`issueAdminSession`）+ 校验（`server/middleware.go`），
      Bearer 或 HttpOnly cookie，cookie 写操作走 CSRF 双提交
- [x] 首次安装向导：`/api/v1/setup/status` + `/setup/bootstrap`，一次性 Setup Token
- [x] 卡台 CDK 全链路：发码 / 列码 / 禁用解禁 / 完整码回填（`handler/cardplatform_cdk.go`）
- [x] 公开兑换 BFF：preview → preflight → redeem → result 轮询
- [x] 卡密状态查询 `GET /api/v1/lookup/cdk`、账单查询 `POST /api/v1/public/billing/check`
- [x] 管理员批量充值（`handler/admin_batch_recharge.go`，含幂等、余额预检、重启对齐）
- [x] 管理后台页面：`/ops` 下 dashboard / cdkeys / orders / appearance / integration / audit /
      webhooks / card-selection（`frontend/src/router/index.ts`）
- [x] 卡台 Webhook 幂等入库、审计日志、站点品牌与皮肤设置、自动选卡规则

### ⏳ 仍未实现 / 已按新方案取消
- [ ] **用户注册和登录** —— 未实现，且已不在方案内。后端只有管理员账号
      （`admin_users`，无 email 字段），没有任何注册接口；前端 `/auth/register`
      是遗留路由，没有对应后端。用户端一律无需登录、凭卡密使用。
- [ ] **用户余额管理** —— 未实现，也不会有。系统里没有用户账户，
      余额概念属于卡台账户（`GET /api/v1/admin/cardplatform/balance`）。
- [ ] **充值申请提交 + 管理员审核** —— 已取消。当前是直连卡台即时兑换，
      没有「待审核 / 批准 / 拒绝」环节，详见 `README.md` 的「充值流程」。
- [ ] 自动化测试 —— `backend/` 下没有任何 `_test.go`，前端也没有 test 脚本。

---

## 🧪 检查数据库

后端启动日志里会打印实际使用的库文件路径（`使用数据库: ...`），照着这个路径查即可。

### 查看已创建的表
```bash
sqlite3 data/cdk_recharge.db ".tables"

# createTables() 在启动时建的 14 张表（.tables 按表名排序输出，会排成多列）:
# admin_audit_logs        admin_recharge_batches  admin_recharge_items
# admin_users             billing_records         card_product_cache
# card_selection_rules    cardplatform_cdk_codes  cd_keys
# cdk_session_bindings    plan_status_cache       recharge_tasks
# site_settings           webhook_events
```

### 抽查数据
```bash
sqlite3 data/cdk_recharge.db "SELECT id, code, plan_type, status FROM cd_keys LIMIT 10;"
sqlite3 data/cdk_recharge.db "SELECT username, is_active FROM admin_users;"
```

没装 `sqlite3` 命令行也不影响服务运行，它只是查库工具。

---

## 📝 环境配置

**后端不会自动加载 `.env` / `.env.local`**：`config.Load()` 和各 handler 都只调 `os.Getenv`，
项目没有引入 godotenv。环境变量必须由启动后端的那个 shell 注入（`export ...` 后再启动，或写进
systemd 的 `EnvironmentFile` / compose 的 `env_file`）。

本地开发实际需要的就这几个：

```env
SERVER_HOST=0.0.0.0
SERVER_PORT=8080
SERVER_MODE=debug
# 不设则用 ../data/cdk_recharge.db（相对 backend 目录）
DB_PATH=../data/cdk_recharge.db
JWT_SECRET=dev-secret-key-at-least-16-chars
# wizard: 打开 /ops/setup 用启动日志里的 Setup Token 创建管理员
INSTALL_MODE=wizard
```

完整变量清单见 `README.md` 的「环境变量配置」，生产模板见 `deploy/app.env.example`。

---

## 🔧 开发工作流

### 修改后端代码
```bash
cd backend
CGO_ENABLED=1 go run ./cmd/server/main.go
# 没有热重载，改完代码需要 Ctrl+C 再重跑（go run 每次会重新编译）
```

### 修改前端代码
```bash
cd frontend
npm run dev
# 修改后会自动热更新
```

---

## 📦 生产部署

构建优化的二进制文件（仍然必须带 cgo，否则 SQLite 驱动是空壳）：
```bash
cd backend
CGO_ENABLED=1 go build -ldflags="-s -w" -o cdk-recharge ./cmd/server
# -s -w 去掉符号表和调试信息，体积明显小于默认构建
```

前后端一起构建可以直接用根目录的 `make build`（前端 `npm ci && npm run build`，
后端带 cgo 输出到 `dist/cdk-recharge`）。完整部署方式见 `README.md` 的「生产环境部署」。

---

## 🐛 故障排除

### 启动报 `go-sqlite3 requires cgo to work. This is a stub`
二进制是在 `CGO_ENABLED=0` 下编出来的。按「前置要求」里的方式带 cgo 重新编译。

### 启动报 `unable to open database file`
`DB_PATH` 指向的目录不存在或没有写权限。SQLite 会自动建文件，但不会自动建目录：
```bash
mkdir -p data          # 默认路径
# 或按自己设的 DB_PATH 创建对应目录
```
另外注意默认路径是相对 `backend` 目录的 `../data/`，从别的工作目录启动二进制时请显式设 `DB_PATH`。

### 前端编译错误
```bash
# 重新安装依赖
cd frontend
rm -rf node_modules package-lock.json
npm install
npm run dev
```

### 后端编译错误
```bash
# 清理 Go 缓存
cd backend
go clean -cache
go mod tidy
go build ./cmd/server/main.go
```

---

## 📚 起步之后

核心业务已经打通，本地跑起来后按这个顺序熟悉系统：

1. **走一遍安装向导** - 启动日志里拿 Setup Token，打开 `/ops/setup` 创建管理员
2. **配置卡台接入** - 后台「卡台接入」填 API Key，再用 `GET /api/v1/admin/cardplatform/ping` 验连通性
   （卡台会按出口 IP 做白名单，`GET /api/v1/admin/network/egress` 可查本机出口 IP）
3. **发一张码** - 后台「CDK 管理」发码，完整码只在发码响应返回一次，本站会落库兜底
4. **跑一次兑换** - 用 `/recharge` 页面走 preview → preflight → redeem → 轮询结果
5. **对账** - 后台「兑换对账」看订单，「Webhook 事件」看卡台回调

完整流程与接口清单见 `README.md` 的「充值流程」和「API 端点」。

---

## 🎯 快速命令

```bash
# 以下命令都在仓库根目录执行

# 启动后端
cd backend && go run ./cmd/server/main.go

# 启动前端
cd frontend && npm run dev

# 构建生产版本
make build

# 查看所有可用命令
make help
```

祝你开发愉快！ 🚀
