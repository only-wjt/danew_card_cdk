# CDK 充值系统 - 实现完成总结

## ✅ 已实现功能

### 1. **卡密兑换页 (Recharge)** - `/recharge`
- ✓ 卡密输入与预览（`POST /api/v1/public/cdk/preview`），返回套餐与 `redemption_token`
- ✓ 实时套餐服务费展示（`GET /api/v1/public/cdk/plans`，未配置卡台 Key 时回退文档默认价）
- ✓ ChatGPT 凭据预检（`POST /api/v1/public/cdk/preflight`），支持 `mode=session` 与 `mode=mailbox`
- ✓ 提交兑换（`POST /api/v1/public/cdk/redeem`，带 `client_request_id` 幂等）
- ✓ 进度轮询到终态（`GET /api/v1/public/cdk/result?token=`）
- ✓ 换设备/刷新后凭卡密恢复进度（`GET /api/v1/public/cdk/result-by-code?code=`）
- ✓ 错误提示（卡密无效、已用、已冻结、账号已是目标套餐等，均由卡台判定）

### 2. **卡密状态查询页 (Lookup)** - `/history`
- ✓ 单接口聚合查询 `GET /api/v1/lookup/cdk?code=`（旧路径 `/lookup/task` 仍兼容）
- ✓ 依次汇总 `cd_keys`、`cardplatform_cdk_codes`、`recharge_tasks`、`cdk_session_bindings`
- ✓ 有 `redemption_token` 时再向卡台查一次订单状态做校正
- ✓ 只回是否已用、状态、充值邮箱、套餐、使用时间；**不返回 token / session**

### 3. **账单查询页 (Billing)** - `/billing`
- ✓ 粘贴 session 查 ChatGPT 订阅状态与账单（`POST /api/v1/public/billing/check`）
- ✓ 兑换时绑定过 session 的卡密，可凭卡密直接查（`cdk_session_bindings`）

### 4. **后端API实现** ✅

全部路由注册在 `backend/internal/server/http.go` 的 `setupRoutes()`，前缀 `/api/v1`：

```
公开（无需登录）
✓ GET  /api/v1/public/cdk/plans            - 可兑换套餐与实时服务费
✓ POST /api/v1/public/cdk/preview          - 卡密预览，拿 redemption_token
✓ POST /api/v1/public/cdk/preflight        - 凭据预检，拿 preflight_token
✓ POST /api/v1/public/cdk/redeem           - 提交兑换
✓ GET  /api/v1/public/cdk/result           - 按 token 查进度
✓ GET  /api/v1/public/cdk/result-by-code   - 按卡密反查 token 后查进度
✓ GET  /api/v1/lookup/cdk                  - 卡密状态查询
✓ POST /api/v1/public/billing/check        - session 查订阅与账单
✓ POST /api/v1/webhooks/cardplatform       - 卡台回调（按 idem_key 幂等入库）

管理（JWT + 管理员）
✓ POST /api/v1/auth/admin/login            - 管理员登录（用户名 + 密码）
✓ GET  /api/v1/stats/system                - 系统统计（CDK 数量以卡台为准）
✓ POST /api/v1/admin/cardplatform/cdks     - 卡台发码
✓ GET  /api/v1/admin/cardplatform/cdks     - 卡台 CDK 列表（本站补全完整码）
✓ POST /api/v1/admin/cardplatform/batch-recharge - 管理员批量充值
✓ GET  /api/v1/admin/cardplatform/cdk-orders     - 兑换订单对账
```

### 5. **数据库实现** ✅
- ✓ SQLite 单文件数据库（WAL + busy_timeout，默认 `../data/cdk_recharge.db`）
- ✓ 启动时 `createTables()` 自动建 14 张表并跑迁移，无需手工 DDL
- ✓ `cardplatform_cdk_codes` 表（卡台完整码兜底，上游列表只回前缀）
- ✓ `recharge_tasks` 表（早期本地任务流程遗留，现仅被卡密查询读取）
- ✓ `cdk_session_bindings` 表（卡密 ↔ redemption_token ↔ session）
- ✓ `admin_recharge_batches` / `admin_recharge_items` 表（管理员批量充值批次与明细）
- ✓ `admin_users` / `admin_audit_logs` / `site_settings` / `webhook_events` 等支撑表

### 6. **前端特性** ✅
- ✓ Vue 3 + Vite + TypeScript + Tailwind CSS
- ✓ 用户端页面：首页、`/recharge`、`/batch`、`/history`、`/billing`
- ✓ 管理端统一挂在 `/ops`（不用 `/admin`，降低扫路径风险，旧路径保留重定向）
- ✓ 响应式布局、Loading 与错误状态处理
- ✓ Pinia 管理登录态，401/403 自动登出（卡台代理接口豁免，避免上游鉴权错误踢人）

## 📊 系统特点

### 套餐类型
- **plus** / **pro_5x** / **pro_20x**，三者服务费默认 $1 / $5 / $10（美分 100 / 500 / 1000）
- 配置卡台 API Key 后按账户实时价返回，拿不到实时价时回退文档默认价
- 服务费在**发码时**扣，兑换时的开卡 / 充值 / 订阅实付由发码方账户承担
- `cd_keys.plan_type` 里的 `pro` / `5x` / `20x` 是早期本地发码留下的取值

### 状态管理
- **卡密（`cd_keys`）**：active / used / disabled / expired
- **卡台兑换订单**：queued、running（开通中）→ review、pending（待对账，**不得重试**）
  → completed（成功）/ declined / failed_precharge / cancelled（终态失败）
- **批量充值明细**：pending → issuing → preparing → submitted → processing
  → success / failed / skipped / unknown

### 幂等与资金安全
- ✓ `client_request_id` 后端生成，落库唯一索引，卡台侧同 ID 返回原订单
- ✓ 同操作员 + 同套餐 + 同批凭据 60 秒内指纹去重，防误双击重复扣款
- ✓ redeem 结果不确定（网络异常）只标 `unknown` 并轮询确认，**绝不重试**
- ✓ 批量充值前用卡台 `spendable_balance` 预检，余额不足立即中止整批
- ✓ 进程重启后自动把在途明细与上游状态对齐（`ResumeInFlightBatchRecharges`）

## 🚀 技术栈

### 后端
- **Go 1.24** + Gin Web框架
- **SQLite** 数据库（`mattn/go-sqlite3`，需 cgo）
- 原生 `database/sql`，未使用 ORM
- 管理端 JWT 认证：Bearer 或 HttpOnly cookie，cookie 写操作走 CSRF 双提交

### 前端
- **Vue 3** 组件框架
- **Tailwind CSS** 样式系统
- **Vite** 构建工具
- **TypeScript** 类型系统

## 📝 关键改进点

1. **用户端零账号** - 不注册不登录，凭卡密使用；只有管理端需要登录
2. **本站不碰资金** - 开卡 / 充值 / 订阅实付全在卡台，本站只做 BFF 转发
3. **完整码兜底** - 卡台完整码只在发码响应返回一次，本站落库保证随时找回
4. **凭据最小留存** - 批量充值的 session / 邮箱密码只在内存流转，不落库不进日志
5. **双通道确认** - 轮询 result 之外，卡台 webhook 按 `idem_key` 幂等入库

## 🔄 完整业务流程

```
管理员 → 后台 /ops → 卡台发码（扣服务费）→ 完整码入库
   ↓
分发卡密给买家
   ↓
买家打开 /recharge → 输入卡密 → preview 拿 redemption_token
   ↓
提交 ChatGPT 凭据（session 或邮箱）→ preflight 拿 preflight_token
   ↓
redeem 提交 → 卡台自动开卡并开通订阅
   ↓
轮询 result 到终态（completed / declined / ...）
   ↓
随时可在 /history 凭卡密查状态，在 /billing 查订阅与账单
```

管理员批量充值走的是同一条卡台链路，只是「发码 → preview → preflight → redeem → 轮询」
全部由后端在一个批次里自动完成，管理端界面上不出现卡密。

## 💾 本地运行

### 启动后端
```bash
cd backend
# go-sqlite3 依赖 cgo，必须开启
CGO_ENABLED=1 go build -o cdk-recharge ./cmd/server
# 后端只读进程环境变量，不会加载 .env
export JWT_SECRET=dev-secret-key-at-least-16-chars
./cdk-recharge
```
后端运行在 `http://localhost:8080`，健康检查 `GET /health`。

### 启动前端
```bash
cd frontend
npm install
npm run dev
```
前端运行在 `http://localhost:5173`

## 📈 测试数据

系统**没有预置测试卡密**。真实可兑换的卡密只能通过后台向卡台发码取得。

根目录的 `insert_test_data.sql` 是早期脚本，里面用到的 `key_type` / `amount` /
`created_by_user_id` 等列在当前 `cd_keys` 表结构里已不存在，直接执行会报错，仅作历史留档。

## ✨ 后续可扩展功能

1. **自动化测试** - 目前 `backend/` 下没有任何 `_test.go`
2. **兑换订单本地留存** - 现在对账完全依赖卡台接口，本地无历史快照
3. **统计报表** - 系统统计只有实时计数，缺趋势与导出
4. **监控告警** - 余额不足、批次失败率异常等场景的主动告警
5. **多语言** - 国际化支持

---

**系统状态**: ✅ 核心链路可用
**部署环境**: 本地开发 / Docker Compose / systemd（见 `README.md` 生产环境部署）
**数据持久化**: SQLite 文件存储
