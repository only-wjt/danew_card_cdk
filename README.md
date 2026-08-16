# CDK 充值系统

一个现代化的 CDK 卡密兑换系统：管理员在卡台批量发码，买家拿到卡密后在本站输入并提交 ChatGPT 凭据，由卡台自动开卡并开通订阅。本站是卡台 OpenAPI 的 BFF，不托管资金账户。

## 官方入口

| 类型 | 链接 |
|------|------|
| **卡台官网** | [https://spacexcard.com](https://spacexcard.com) |
| **官方频道** | [https://t.me/spacex_card_visa](https://t.me/spacex_card_visa) |
| **官方群聊 2 群** | [https://t.me/spacex_card2](https://t.me/spacex_card2) |

## 主要特性

✨ **用户端（无需注册，凭卡密使用）**
- 卡密预览与实时套餐服务费展示
- ChatGPT 凭据预检（session / 邮箱两种模式）
- 一键兑换 + 进度轮询到终态
- 凭卡密查询兑换状态与充值邮箱
- 粘贴 session 查订阅与账单

⚙️ **管理端（`/ops`）**
- 卡台批量发码，完整码落本站库随时找回
- CDK 禁用 / 解禁、兑换订单对账
- 管理员批量充值（界面不出现卡密）
- 自动选卡优先级、卡台套餐状态同步
- 站点品牌 / 皮肤设置、Webhook 事件、操作审计日志

🔒 **技术特性**
- JWT 认证
- SQLite 单文件数据库（无需外部数据库服务）
- 启动时自动建表 / 迁移，零手工 DDL
- 精确的小数点计算
- 完整的审计日志

## 技术栈

### 前端
- Vue 3.4+
- Vite 5.0+
- TypeScript
- Tailwind CSS 3.4
- Pinia (状态管理)
- Axios (HTTP 客户端)
- Chart.js (数据可视化)

### 后端
- Go 1.24+
- Gin Web Framework
- SQLite（`mattn/go-sqlite3`，需要 cgo）
- 原生 `database/sql`（未使用 ORM）

## 快速开始

### 前置要求
- Docker 和 Docker Compose
- Node.js 18+ (本地开发前端)
- Go 1.24+ (本地开发后端，编译时需 `CGO_ENABLED=1` 和 C 编译器)
- 无需 PostgreSQL / Redis：数据库是单个 SQLite 文件，进程启动时自动创建

### 使用 Docker Compose 运行

```bash
# 先把根目录 Caddyfile 里的 :YOUR_DOMAIN 换成真实域名，无域名则改成 :80
docker compose up -d --build

# 只有 Caddy 对外映射 80/443，后端容器不暴露端口，统一从 Caddy 进
# 访问应用: https://你的域名 （或 http://本机IP）
# 数据库: SQLite 文件，容器内 /app/data/cdk_recharge.db（对应命名卷 cdk_data）
```

### 本地开发

**后端**
```bash
cd backend
go mod download
# go-sqlite3 依赖 cgo，必须开启；默认使用 ../data/cdk_recharge.db
CGO_ENABLED=1 go run ./cmd/server/main.go

# 想换库文件位置时用 DB_PATH 覆盖
DB_PATH=/tmp/cdk.db CGO_ENABLED=1 go run ./cmd/server/main.go
```

**前端**
```bash
cd frontend
npm install
npm run dev
```

## 项目结构

```
cdk-recharge-system/
├── frontend/              # Vue 3 前端应用
│   ├── src/
│   │   ├── api/          # API 调用
│   │   ├── views/        # 页面视图
│   │   ├── components/   # 可复用组件
│   │   ├── stores/       # Pinia 状态管理
│   │   ├── router/       # Vue Router 路由
│   │   └── types/        # TypeScript 类型
│   ├── package.json
│   └── vite.config.ts
│
├── backend/               # Go 后端应用
│   ├── cmd/server/       # 主程序入口
│   ├── internal/
│   │   ├── db/           # SQLite 连接、建表与迁移
│   │   ├── handler/      # HTTP 请求处理（业务逻辑也写在这里）
│   │   ├── server/       # 路由注册 + 中间件
│   │   ├── cardplatform/ # 卡台 OpenAPI 客户端
│   │   ├── auth/         # JWT 密钥与 claims
│   │   ├── config/       # 配置管理
│   │   ├── gptcheck/     # ChatGPT 订阅 / 账单查询
│   │   ├── notify/       # Telegram 通知
│   │   └── plansync/     # 卡台套餐状态后台同步
│   └── go.mod
│
├── deploy/               # 部署脚本、systemd 单元、环境变量模板
├── docs/                 # 卡台接入与 OpenAPI 中文文档
├── Caddyfile             # 反向代理配置（compose 挂载）
├── docker-compose.yml    # Docker 编排配置
└── Makefile             # 构建脚本
```

## 环境变量配置

### 后端

后端只读进程环境变量，**不会自动加载 `.env` / `.env.local`**（`config.Load()` 与各 handler 都直接调 `os.Getenv`）。
本地开发请在启动 shell 里 `export`，容器部署用 compose 的 `environment` / `env_file`，systemd 部署见 `deploy/app.env.example`。

```env
# 服务
SERVER_HOST=0.0.0.0
SERVER_PORT=8080
SERVER_MODE=debug          # release 模式会强制校验 JWT_SECRET

# 数据库（SQLite 单文件；不设则用 ../data/cdk_recharge.db，相对 backend 目录）
DB_PATH=/app/data/cdk_recharge.db

# 前端静态资源目录；留空则只跑 API
WEB_DIR=/app/web

# 认证：release 模式必须为 >=16 位随机串
JWT_SECRET=change-me-to-random-32chars-min

# 安装：wizard=等 Web 安装向导（默认）；auto=启动时建管理员
INSTALL_MODE=wizard
SETUP_BOOTSTRAP_TOKEN=     # 留空则启动日志打印一次性随机 token
SETUP_ALLOW_CIDRS=         # 限制 POST /api/v1/setup/bootstrap 的来源 IP，逗号分隔；留空为不限
ADMIN_USERNAME=admin       # 仅 auto 模式或显式设 ADMIN_PASSWORD 时生效
ADMIN_PASSWORD=

# 网络
TRUSTED_PROXIES=127.0.0.1  # 逗号分隔；不设则不信任任何代理头
CORS_ALLOWED_ORIGINS=      # 逗号分隔

# 卡台 OpenAPI
CARD_API_BASE=
CARD_API_KEY=

# 可选：Telegram 通知、账单上游、后台热更新
TELEGRAM_BOT_TOKEN=
TELEGRAM_CHAT_ID=
ACCOUNTHUB_BASE_URL=
UPDATE_ENABLED=1
CDK_GITHUB_REPO=
GITHUB_TOKEN=
CDK_INSTALL_DIR=
CDK_APP_VERSION=
```

> 历史遗留：`config.go` 里仍会读取 `DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME/DB_SSLMODE`、
> `DB_MAX_*`、`REDIS_*`、`JWT_EXPIRATION_HOURS`，但这些值没有任何使用方——
> `db.Init()` 只认 `DB_PATH`，连接池参数写死在代码里，JWT 有效期固定 24 小时，项目也没有引入任何 Redis 客户端。设置它们不会有任何效果。

## 数据库 Schema

SQLite 单文件库，所有表由 `backend/internal/db/db.go` 的 `createTables()` 在启动时 `CREATE TABLE IF NOT EXISTS` 建好，
无需手工执行 DDL，也没有独立的 migration 工具。

### 核心表结构

**cd_keys** - 本站 CDK 激活码表
- id, code, plan_type (pro / 5x), status, created_at, used_at, expires_at, description

**cardplatform_cdk_codes** - 卡台直充 CDK 完整码（卡台列表只回前缀，靠本表补全）
- upstream_id, code, code_prefix, plan, fee_amount_minor, status, created_at

**recharge_tasks** - 充值任务
- id, task_id, cdk_code, session_json, account_email, task_status, created_at, updated_at, completed_at, notes

**billing_records** - 账单记录
- id, session_hash, account_email, subscription_status, plan_type, billing_amount, currency, next_billing_date, created_at, updated_at

**cdk_session_bindings** - 卡密 ↔ redemption_token ↔ session 绑定（账单页凭卡密查询）
- cdk_code, session_payload, redemption_token, updated_at

**admin_users** - 管理员账号
- id, username, password_hash (bcrypt), display_name, is_active, created_at, updated_at

**admin_audit_logs** - 管理员操作审计
- id, username, action, detail, ip, created_at

**site_settings** - 站点 key-value 配置（品牌/皮肤/安装锁/安装令牌哈希）
- key, value, updated_at

**webhook_events** - 卡台回调事件（按 idem_key 幂等入库）
- id, event_type, idem_key, payload, created_at

**card_selection_rules** - 自动选卡优先级规则
- id, sort_order, plan_key, display_name, bin_prefix, channel, enabled, created_at

**plan_status_cache** - 卡台套餐状态缓存（后台每 3 分钟同步）
- plan_key, label, online, service_fee_usd_minor, synced_at

**card_product_cache** - 卡台实体产品缓存
- product_code, issuer, bin, network, issuing_area, scene, card_group, description, bin_heads, enabled, suspended_at, synced_at

**admin_recharge_batches** - 管理员批量充值批次头（管理端界面不出现卡密）
- batch_id (PK), operator, plan, total, status (running / done / paused), fingerprint, message, created_at, updated_at
- fingerprint = 操作员 + 套餐 + 全部凭据标识的哈希，配合 `idx_arb_fingerprint` 做 60 秒内误双击去重；`idx_arb_created` 供列表按时间倒序

**admin_recharge_items** - 批次内的单条充值明细（只落非敏感字段）
- id, batch_id, seq, client_request_id (UNIQUE), plan, cred_mode (session / mailbox), account_email, cdk_code, card_id（建表保留，当前无写入路径）, redemption_token, upstream_order_id, status, message, created_at, updated_at
- 明细状态机与批次状态是两套：pending → issuing → preparing → submitted → processing → success / failed / skipped，另有 unknown —— redeem 结果不确定（超时或网络中断），可能已经扣款，只能向上游轮询确认，严禁重新提交
- 没有 session / 邮箱密码列，凭据只在执行 goroutine 的内存里流转；cdk_code 与 redemption_token 也不随管理接口返回。唯一索引 `idx_ari_batch_seq(batch_id, seq)` 保证批内序号不重，`idx_ari_status` 供进程重启后捞出未终态明细继续对齐

## API 端点

全部路由集中注册在 `backend/internal/server/http.go` 的 `setupRoutes()`，API 前缀统一为 `/api/v1`。
下面路径均省略该前缀。管理接口（`/admin/*`、`/stats/*`）需要 JWT + 管理员校验，
认证方式是 `Authorization: Bearer <token>` 或登录时下发的 HttpOnly cookie；
走 cookie 的写操作还需带 `X-CSRF-Token`（与 `csrf_token` cookie 双提交校验），Bearer 方式不受此限制。

### 探活
- `GET /health` - 健康检查（不在 `/api/v1` 下）
- `GET /api/v1` - API 可用性探活

### 认证 (`/api/v1/auth/*`)

后台只有管理员账号，只认用户名 + 密码（`admin_users` 表没有 email 字段，也没有注册接口）。

- `POST /auth/admin/login` - 管理员登录，body `{"username":"...","password":"..."}`，返回 JWT + `csrf_token` 并写入 cookie
- `POST /auth/admin/logout` - 退出登录（清 cookie）
- `GET /auth/admin/me` - 当前登录管理员信息（`user_id` / `username` / `is_admin`）
- `POST /auth/admin/change-password` - 修改自己的密码

### 首次安装 (`/api/v1/setup/*`)
- `GET /setup/status` - 是否已安装、当前 `INSTALL_MODE`
- `POST /setup/bootstrap` - 创建首个管理员，仅一次；需请求头 `X-Setup-Token`；已安装后返回 410

### 公开接口（无需登录）
- `GET /public/site` - 站点品牌 / 皮肤配置
- `GET /public/cdk/plans` - 可兑换套餐及实时服务费
- `POST /public/cdk/preview` - 卡密预览（成功时本站记录 code ↔ redemption_token）
- `POST /public/cdk/preflight` - 兑换前预检（会绑定 session，供账单页凭卡密查询）
- `POST /public/cdk/redeem` - 提交兑换
- `GET /public/cdk/result?token=` - 按 redemption_token 查兑换进度
- `GET /public/cdk/result-by-code?code=` - 按卡密反查绑定的 token 后查进度
- `GET /lookup/cdk?code=` - 卡密状态查询（是否已用 + 充值邮箱，不返回 token / session）
- `GET /lookup/task?code=` - 同上，兼容旧路径
- `POST /public/billing/check` - 粘贴 session 查 ChatGPT 订阅与账单
- `POST /billing/check` - 同上，兼容旧路径

### Webhook
- `POST /webhooks/cardplatform` - 卡台回调（按 idem_key 幂等入库），在卡台开发者页配置为 `https://你的域名/api/v1/webhooks/cardplatform`

### 管理 (`/api/v1/admin/*`、`/api/v1/stats/*`)
- `GET /stats/system` - 系统统计
- `GET /admin/system/version` - 本机版本 + GitHub 最新 release
- `GET /admin/system/update/status` / `POST /admin/system/update` - 一键热更新
- `GET /admin/cardplatform/ping` / `plans` / `balance` - 卡台连通性、实时套餐价格、账户余额
- `GET /admin/cardplatform/cdks` - 卡台 CDK 列表（上游只回码前缀）
- `GET /admin/cardplatform/cdks/stored` - 本站已存完整码列表
- `POST /admin/cardplatform/cdks` - 发码
- `POST /admin/cardplatform/cdks/store` - 完整码回填入库
- `POST /admin/cardplatform/cdks/batch-disable` / `batch-enable` - 批量禁用 / 解禁
- `POST /admin/cardplatform/cdks/:id/disable` / `:id/enable` - 单个禁用 / 解禁
- `GET /admin/cardplatform/cdk-orders` / `cdk-orders/:id` - CDK 订单列表 / 详情
- `POST /admin/cardplatform/batch-recharge` - 创建管理员批量充值批次（后端自动发码即兑换）
- `GET /admin/cardplatform/batch-recharge` / `batch-recharge/:batch_id` - 批次列表 / 明细
- `POST /admin/cardplatform/batch-recharge/:batch_id/retry` - 只与上游对齐状态，不会重复扣款
- `DELETE /admin/cardplatform/cards/:id` - 删除绑定卡片
- `GET /admin/webhooks/events` - Webhook 事件列表
- `GET /admin/network/egress` - 本机出口 IP（用于卡台 API 白名单）
- `GET /admin/audit-logs` - 管理员操作审计日志
- `GET /admin/settings` / `PUT /admin/settings` - 站点设置（品牌 / 皮肤 / 卡台密钥）
- `GET /admin/card-selection/rules` / `PUT /admin/card-selection/rules` - 自动选卡优先级规则
- `GET /admin/card-selection/plan-status` - 套餐状态缓存
- `POST /admin/card-selection/sync` - 立即同步卡台套餐状态

### 后台页面路径

前端后台基础路径是 `/ops`（见 `frontend/src/router/index.ts`）：

- `/ops/login` - 管理员登录页
- `/ops/setup` - 首次安装向导
- `/ops` - 后台首页，下挂 `cdkeys` / `orders` / `appearance` / `integration` / `audit` / `webhooks` / `card-selection`
- `/admin` 及 `/admin/*` 保留为重定向到 `/ops`，`/auth/login` 重定向到 `/ops/login`

## 充值流程

没有用户注册，也没有「管理员审核充值申请」这一步。本站只是卡台 CDK 接口的 BFF：
浏览器只打本站 `/api/v1/public/cdk/*`，本站转发卡台，开卡与订阅开通全部发生在卡台侧。

### 用户兑换（`/recharge`，无需登录）

```
1. 管理员发码：POST /admin/cardplatform/cdks
   卡台按套餐（plus / pro_5x / pro_20x）扣服务费，完整码只在发码响应返回一次
   → 本站写入 cardplatform_cdk_codes 兜底，之后卡台列表只回 code_prefix
   ↓
2. 用户输入卡密：POST /public/cdk/preview
   转发卡台 preview 拿 redemption_token
   → 本站记 code ↔ token（cdk_session_bindings）
   ↓
3. 用户提交 ChatGPT 凭据：POST /public/cdk/preflight
   credential 支持 mode=session（session token）或 mode=mailbox（邮箱 + 密码）
   卡台校验后回 preflight_token
   → 本站把 session 绑到卡密，供账单页凭卡密查询
   ↓
4. 提交兑换：POST /public/cdk/redeem
   带 redemption_token + preflight_token + client_request_id（幂等）
   卡台自动用发码方名下资金开卡并开通订阅
   ↓
5. 轮询进度：GET /public/cdk/result?token=
   或 GET /public/cdk/result-by-code?code=（凭卡密反查本站绑定的 token）
   终态：completed / declined / failed_precharge / cancelled
   review、pending 期间严禁重复提交，只能继续轮询
```

卡台侧另有回调 `POST /webhooks/cardplatform`，按 `idem_key` 幂等写入 `webhook_events`，与轮询构成双通道。
公开的 `GET /lookup/cdk?code=` 只回卡密是否已用和充值邮箱，不返回 token / session。

### 管理员批量充值（`/ops`，界面上不出现卡密）

`POST /admin/cardplatform/batch-recharge` 受理一批 ChatGPT 凭据后立即返回 `batch_id`，
后台 goroutine 对每条自动走完「发码 → preview → preflight → redeem → 轮询 result」，
卡密退化为不可见的内部记账凭证。批次与明细落 `admin_recharge_batches` / `admin_recharge_items`，
凭据只在内存流转、绝不落库；redeem 结果不确定时只标 `unknown` 并继续轮询，绝不重试。
提交前会用卡台 `spendable_balance` 做余额预检，余额不足即中止整批。

### 关于 `cd_keys` / `recharge_tasks`

这两张表是早期「本站自己发码 + 本地任务」流程留下的。当前路由表里没有注册对应接口，
只有 `GET /lookup/cdk` 还会读它们来补历史数据，实际业务已全部走卡台。

## 开发指南

### 添加新的 API 端点

1. 在 `backend/internal/handler/` 中创建处理器（业务逻辑目前也写在 handler 里，没有单独的 service 层）
2. 在 `backend/internal/server/http.go` 的 `setupRoutes()` 中注册路由，按需挂 `JWTAuthMiddleware()` / `AdminAuthMiddleware()`
3. 在 `frontend/src/lib/api.ts` 中添加对应的 API 调用

### 创建新的前端页面

1. 在 `frontend/src/views/` 中创建 Vue 组件
2. 在 `frontend/src/router/index.ts` 中添加路由
3. 导入必要的 stores 和 API 调用

## 测试

仓库目前还没有写测试用例（`backend/` 下没有任何 `_test.go`，`frontend/package.json` 也没有 test 脚本）。

```bash
# 后端：编译 + 空跑测试（需 CGO_ENABLED=1）
cd backend && go build ./... && go test ./...

# 前端：目前只有 lint
cd frontend && npm run lint
```

## 部署

### Docker 镜像构建

`docker-compose.yml` 用了 `env_file: - path:` 等新语法，需要 Compose V2（`docker compose`），旧的 `docker-compose` 命令会解析失败。

```bash
docker compose build
```

### 生产环境部署

仓库里没有单独的部署长文，可直接用 `deploy/` 下的现成资源：

**方式一：Docker 一键脚本（推荐）**
```bash
./deploy/docker-deploy.sh --domain 你的域名
# 不带 --domain 则以 :80 纯 IP 方式跑
```
脚本会装好 Docker / compose、把代码放到 `/opt/cdk-recharge`（`--dir` 可改）、
生成随机 `JWT_SECRET` 写进 `.env`（`INSTALL_MODE=wizard`）、按域名生成 `Caddyfile`，最后 `compose up -d`。

**方式二：手工 compose**
```bash
# 先把根目录 Caddyfile 里的 :YOUR_DOMAIN 换成真实域名，无域名则改成 :80
make docker-up        # 首次会自动生成含 JWT_SECRET 的 .env
# 等价于 docker compose up -d --build
```
Caddy 占 80/443，后端容器内监听 8080；数据库在命名卷 `cdk_data`（容器内 `/app/data/cdk_recharge.db`）。

**方式三：裸机 systemd**

用 `deploy/cdk-recharge.service`（`EnvironmentFile=/opt/cdk-recharge/app.env`）配合
`deploy/app.env.example` 作为环境变量模板，反代配置参考 `deploy/Caddyfile.snippet`。

**发布包与后台一键更新**

管理后台的 `POST /admin/system/update` 依赖 GitHub Release 上的预编译包，
构建与发布步骤见 `deploy/README-release.md`，CI 模板见 `deploy/github-release.workflow.yml`。

部署完成后首次访问会进入 `/ops/setup` 安装向导，用启动日志里打印的一次性 Setup Token 创建管理员。

## 常见问题

**Q: 如何重置数据库？**

数据库就是一个 SQLite 文件，删掉它即可，下次启动会重新建表。

```bash
# 本地开发（默认路径）
rm data/cdk_recharge.db

# Docker：删除承载 /app/data 的命名卷
docker compose down -v
docker compose up -d
```

**Q: 如何查看日志？**

compose 里只有两个服务：`cdk`（Go 后端 + 内置前端静态资源）和 `caddy`（反代）。

```bash
docker compose logs -f cdk
docker compose logs -f caddy
```

**Q: 默认管理员账户是什么？**

没有内置默认账户。默认 `INSTALL_MODE=wizard`：首次启动时后端会在日志里打印一次性 Setup Token，
打开 `/ops/setup`、用该 token 走安装向导创建首个管理员（用户名 + 密码，无邮箱）。
`INSTALL_MODE=auto` 或显式设置 `ADMIN_PASSWORD` 时，启动阶段直接建号；未设密码则日志打印一次随机密码。
安装完成后 setup token 立即作废，`POST /api/v1/setup/bootstrap` 返回 410。

## 贡献

欢迎提交 Issue 和 Pull Request！

## License

MIT License
