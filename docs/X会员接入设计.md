# X 会员（Premium）接入设计

> 对照 Avanfinity Developer API `2026-09-30`（`/api/v1`，`X-App-Id` + `X-App-Secret`）里的 **X Direct** 与 **X CDK** 两组接口。  
> 日期：2026-09-30　·　版本：1.0  
> 本文只定设计，不改代码。OpenAI 这条线（`DN-` 本站码、A/B 双绑）见 [本站统一发码 · 双卡台绑定设计](本站统一发码-双卡台绑定设计.md)，本文不改它。

---

## 1. 先定五件事

1. **X 是一条新产品线，不是 OpenAI 的一个新套餐。** 套餐、接收方、金额、状态机全都不一样。不往现有 `CardProvider`、`cardplatform_cdk_codes`、公开兑换页里塞。
2. **对外仍然只卖本站码。** 新前缀 `DNX-`，客户只看到本站码。上游 X CDK、卡号、账单链接都不出站。
3. **两个通道，各挂各的卡台账户。** X CDK 通道和 X 直充通道分别绑定一个卡台账户，可以是两个不同的 App、不同账户，甚至不同站点。两者互不回落。
4. **一码一台一通道。** 发码时就写死走哪个通道、哪个账户，兑换过程中不切台、不换通道。X 的每一步都可能动钱，不能照搬 `DN-` 那套「preview 阶段切台」。
5. **没有 X Webhook，结果全靠轮询。** 文档里 23 类 Webhook 事件没有一条是 X 的。凡是结果不确定的（超时、5xx、408），一律锁住这张码并查原订单，绝不新建订单重付。

---

## 2. 两个通道差在哪

| 点 | X CDK | X 直充 |
| --- | --- | --- |
| 接口 | `POST /x-direct/cdks/generate`；兑换走 `/public/x-cdk/{preview,preflight,redeem,result}` | `POST /x-direct/orders/prepare` → `POST /x-direct/orders/{id}/confirm` → `GET /x-direct/orders/{id}` |
| 发码时 | 免费生成上游码，快照费用和上限，不扣钱 | 上游没有任何资源，只在本站生成码 |
| 谁出钱 | 兑换时从发行者钱包出：卡片注资 + 开卡/充值费 + X 服务费 | 服务费从钱包扣；X 官方金额从本站指定的那张卡（`cardId`）直接扣 |
| 花费上限 | 上游按每张码锁死：`maxWalletDebitUsd`、`maxOfficialAmountMinor`、`currency` | 上游不管，本站自己把关：prepare 拿到报价后比对上限，超了就 cancel |
| 付款步骤 | `redeem` 调两次：第一次只注资到 `funded`，第二次才派发 X 付款 | `confirm` 一次 |
| 报价有效期 | 可用原设备、原请求 ID、原接收方刷新 preflight | 30 分钟；过期返回 `QUOTE_EXPIRED`，服务费版本变了返回 `QUOTE_CHANGED` |
| 频率限制 | 公开接口**按来源 IP**：preview 60、preflight 10、redeem 10、result 120 次/分钟 | prepare 每账户每分钟最多 5 个新报价 |
| 鉴权 | 发码、导出、撤销要 App 凭证 + IP 白名单；兑换四步不要凭证 | 全部要凭证；prepare / confirm / cancel 还要 IP 白名单 |
| 退路 | 未派发资金的码可 `revoke` | `prepared` / `review_required` 的报价可 `cancel` |
| 余款 | 卡里剩下的钱留在发行者卡上，不自动退回钱包 | 本来就是本站自己的卡 |

套餐两边一样，固定 6 个：`premium_3m / 6m / 12m`、`premium_plus_3m / 6m / 12m`。接收方都是 X 用户名（去掉 `@` 后 1–15 位字母、数字、下划线），不要 Session、Cookie，也不传卡号。

---

## 3. 「两个卡台」怎么建模

**凭证继续放 `card_platform_accounts`。** 新增协议 `avanfinity-api-v1`：App ID 放 `cred_public`，App Secret 放 `cred_secret`。

**账户加能力标记。** 新列 `capabilities`，取值为逗号分隔的 `openai`、`x_cdk`、`x_direct`。老数据迁移时统一补成 `openai`。

> 必须同时改：`db.ActiveDualIssueAccounts()` 和 `provider.LoadRegistry()` 只取带 `openai` 能力的账户。否则 OpenAI 双发会打到一个只开了 X 权限的 key 上，发码直接失败或熔断。

### X 账户要和 OpenAI 链路完全隔开

现有代码里，以下几处都会扫到「所有 active 账户」。X 账户走 `/api/v1` + App ID 鉴权，被旧逻辑扫到都会出错：

| 位置 | 不隔开会怎样 |
| --- | --- |
| `PrimaryCardPlatformAccount` / `ensurePrimaryAccount` | 主台停用时会按优先级回落，可能把 X 账户选成主台，老码转发打到 X 账户 |
| `ActiveDualIssueAccounts`（双发、`cardplatform_cdk.go`、`site_dual_bind.go`） | 双发选中 X 账户，发码失败 |
| `plansync.syncOnce` / `CircuitProbeAccounts` | 每轮用 `/openapi/v1` + `X-API-Key` 同步 X 账户，连续失败 5 次打开熔断，后台一直报红 |
| `AdminPingCardPlatform` | 测试连接探的是旧路径，X 账户永远显示不通 |
| `buildProvider` | 不认识新协议，报「未知卡台协议」，出现在 Skipped 里 |

做法：加 `db.ListOpenAIAccounts()`，上面这些地方全部改用它。`ListCardPlatformAccounts()` 只留给卡台接入页列表和按 ID 查询。X 账户不参与主台、优先级、熔断，健康状态只用 `last_ok_at` / `last_error` 显示。

### 接一个 X 账户的步骤

1. **在 Avanfinity 建 App**：开 X 直充或 X CDK 权限，把服务器出口 IP 填进白名单，钱包充值；直充还要一张有余额的卡。
2. **在后台「卡台」页添加卡台**（页面设计见 9.5）：先选用途「X 会员 · Avanfinity」，填地址、App ID、App Secret，勾 X CDK / X 直充。X 没有 webhook，这一类不显示回调、主台、顺序、熔断。
3. **测试连接**（按顺序，哪一步挂就停在哪一步，并直接说原因）：
   - `GET /balance`：通了说明地址和凭证对，顺带显示余额；
   - `GET /x-direct/plans`：通了说明 App 开了 X 权限，顺带拉套餐；
   - `GET /cards`：列出可用的卡，供通道选择付款卡或注资卡；
   - 写接口白名单：直充用「试报价」（prepare 后立刻 cancel）；CDK 可选「测试发码」（生成 1 张极小上限的码后立即 revoke）。返回 IP 不在白名单时，直接显示本机出口 IP，提示去 App 上添加。
4. **在同一个卡台的「付款卡」页签选卡**，再到「X 会员 → 通道设置」填套餐上限、打开通道。一个用途同一时间只绑一个卡台；给另一个卡台勾上同一用途时，提示「要把这个通道切过去吗」。

同一个 Avanfinity 账户如果既卖 OpenAI 又卖 X，要建两行：旧协议一行，新协议一行。两种凭证不同，不要混在一行里。

**通道配置单独一张表** `x_channels`，每个通道一行：

| 列 | 说明 |
| --- | --- |
| `channel` | `x_cdk` / `x_direct`，主键 |
| `account_id` | 绑定的卡台账户；两个通道可以不同 |
| `enabled` | 通道开关 |
| `card_id` | 直充：付款卡；CDK：注资卡（和 `auto_card_*` 二选一） |
| `auto_card_product` / `auto_card_first_name` / `auto_card_last_name` | CDK 自动开卡用 |
| `updated_at` | |

**每套餐的上限** 放 `x_plan_limits`（`channel` + `plan` 为主键）：`enabled`、`currency`、`max_official_amount_minor`、`max_service_fee_usd`（直充用）、`max_wallet_debit_usd`、`funding_amount_usd`（CDK 用）。

**第一期不做双绑和跨通道回落。** 以后要做，只允许在「任何报价、任何注资之前」切换，规则同 OpenAI 的 preview 阶段。

---

## 4. 数据

不复用 `cardplatform_cdk_codes`：状态机不同，金额是外币最小单位加 USD 字符串，混进去会污染 OpenAI 的查询和对账。

### `x_codes` 本站 X 码

| 列 | 说明 |
| --- | --- |
| `id`、`code` | `DNX-<8hex>-<8hex>-<8hex>`，唯一 |
| `plan`、`channel`、`account_id` | 发码时写死 |
| `status` | 见第 5 节 |
| `upstream_cdk_id` | CDK 通道：上游 UUID |
| `upstream_code_enc`、`upstream_code_prefix` | CDK 通道：完整上游码加密保存，不进任何 API 响应和日志 |
| `currency`、`max_official_amount_minor`、`max_wallet_debit_e4`、`funding_amount_e4`、`service_fee_e4`、`pricing_version` | 发行快照 |
| `device_token` | CDK 通道：本站为这张码生成的 64 位 hex，逐字复用 |
| `batch_id`、`idempotency_key`、`note`、`created_by`、`created_at`、`disabled_at` | |

### `x_redemptions` 兑换尝试

一张码通常只有一条；只有上游明确允许重来（见第 5 节）才会新增。

| 列 | 说明 |
| --- | --- |
| `x_code_id`、`channel`、`account_id`、`recipient` | |
| `client_request_id`、`idempotency_key` | **调用上游之前先落库**，重试时原样复用 |
| `upstream_order_id` | 直充：prepare 返回的订单 UUID |
| `upstream_status`、`amount_minor`、`currency`、`estimated_usd`、`service_fee_e4`、`pricing_version` | 最新快照 |
| `payment_attempted`、`funding_dispatched`、`payment_dispatched` | 判断能否放回码的依据 |
| `invoice_urls_enc` | 官方账单链接含访问凭据，加密保存，只给后台看 |
| `error_code`、`message`、`next_poll_at`、`poll_count`、`created_at`、`updated_at`、`finished_at` | |

### 另外两张小表

- `x_batches`：批次 ID、套餐、通道、账户、数量、备注、上游幂等键、创建人、时间。发码页「最近批次」和兑换记录的备注搜索都靠它。
- `x_quote_samples`：通道、套餐、币种、`amountMinor`、`estimatedUsd`、服务费、`pricingVersion`、来源（真实兑换 / 试报价）、时间。上限表的「最近实测」取这里。

**金额统一用整数：** USD 用万分之一美元（`_e4`，上游最多 4 位小数），外币用上游给的 `amountMinor`。不用浮点数。

---

## 5. 状态机

本站码状态（`x_codes.status`）：

```
unused ──报价──▶ quoted ──确认──▶ (CDK: funding ─▶ funded ─▶) paying ─▶ completed
   ▲                │                                        │
   │                └── ineligible / 失败且确认没动钱 ─────┐  ├─▶ paid_pending_delivery ─▶ completed
   └─────────────────────── 放回可用 ◀───────────────────┘  └─▶ review_required / requires_action / uncertain（锁码）
```

上游状态到本站的映射：

| 上游状态 | 本站 | 码能否再用 | 客户看到 |
| --- | --- | --- | --- |
| `prepared` | `quoted` | 否（占用中） | 请确认开通 |
| `funding` / `funded` / `processing` / `paying` | 同名 | 否 | 开通中 |
| `completed` | `completed` | 否 | 已开通 |
| `paid_pending_delivery` | 同名，继续轮询 | 否 | 已付款，等待到账 |
| `review_required` / `requires_action` | 同名，进异常队列 | 否 | 处理中，请联系客服 |
| `ineligible` | 见下 | 条件放回 | 该账号暂不能开通 |
| `failed_precharge` | 见下 | 条件放回 | 开通失败，可重试 |
| `cancelled`（直充） | 见下 | 条件放回 | — |
| `revoked`（CDK） | `disabled` | 否 | 卡密已失效 |
| 本站超时 / 5xx / 408 | `uncertain` | 否，只查不重发 | 处理中 |

**只有这三种情况能把码放回 `unused`：**

1. 报价阶段明确返回 `ineligible`，并且 `payment_attempted`、`funding_dispatched` 都是 false。
2. CDK 通道返回 `canRetryPreflight` **严格为 true**。这时同一设备生成新的 `clientRequestId` 重新预检，旧尝试记为结束。
3. 直充订单 `cancelled`，并且 `paymentAttempted = false`。

其他情况一律锁住，交给轮询和人工处理。

---

## 6. 流程

### 6.1 管理端发码

**CDK 通道：**

1. 选套餐、数量（上游每批最多 50 张），上限默认取 `x_plan_limits`。
2. 先写批次行和 `Idempotency-Key`，再写 pending 本站码。
3. 调 `POST /x-direct/cdks/generate`，从响应里取完整上游码，逐张绑定后激活。
4. 响应丢了：用**同一个幂等键和同样的参数**重放（返回 `replayed=true`），不换键。个别码缺全文时用 `GET /x-direct/cdks/{id}/export` 补取。

**直充通道：** 只生成本站码，上游不调用。上限按发码当时的配置写进码行。

两种都复用现有 xlsx 导出。

### 6.2 客户兑换（新页面 `/x`）

1. **输入卡密。** 本站按 `DNX-` 前缀识别。CDK 通道调 `/public/x-cdk/preview`；直充通道只查本地。页面显示套餐。
2. **输入 X 用户名并报价。** 前端先校验格式。
   - CDK：`preflight(code, deviceToken, clientRequestId, recipient)`。
   - 直充：`prepare(recipient, plan, cardId, clientRequestId)`，拿到报价后本站比对 `amountMinor` 和 `serviceFee` 是否在上限内，超了立刻 `cancel`，对客户显示「暂时无法开通」。
   - 客户只看到「为 @xxx 开通 X Premium 3 个月」。外币金额和服务费是本站成本，只在后台显示。
3. **客户点「确认开通」。**
   - 直充：`confirm`，把报价里的 `expectedAmountMinor`、`currency`、`expectedServiceFee` 原样回传。
   - CDK：第一次 `redeem` 注资；轮询到 `funded` 后，本站自动发第二次 `redeem` 派发付款。客户点的「确认开通」就当作文档要求的「用户明确继续」，上游的 `maxOfficialAmountMinor` 兜底。如果第二次返回报价变化，按原设备、原请求 ID、原接收方刷新 preflight 后再发。
4. **等结果。** 前端每 3–5 秒查本站 `/api/v1/public/x/result`，后台轮询也在推进。
5. **成功页** 显示「已为 @xxx 开通」。账单链接不给客户。

本站公开接口单独一组 `/api/v1/public/x/*`，不改 `/api/v1/public/cdk/*`。

### 6.3 后台轮询

- 每 15 秒扫一次 `x_redemptions` 里未结束且 `next_poll_at` 已到的记录。
- 直充查 `GET /x-direct/orders/{id}`，CDK 查 `POST /public/x-cdk/result`。result 是只读的，不会自动推进付款。
- 退避：5 秒、10 秒、30 秒、1 分钟、5 分钟，之后每 5 分钟一次。
- 超过 2 小时仍未结束：转 `review_required`，并用现有 Telegram 通知告警。
- 全局限速：result 每分钟不超过 100 次，给客户实时查询留余量。

---

## 7. 钱和上限

- **币种先实测再定。** X 官方报价是本地币（文档示例是 BDT），跟账户和卡有关。CDK 发码时填的 `currency` 如果和实际报价不一致，兑换会被拒，整批码作废。上线前用真实凭证各跑一次直充 prepare（不 confirm，之后 cancel）和 CDK preflight，拿到真实币种和金额再定上限。
- **未兑换的 X CDK 是负债。** 发码不扣钱，钱包余额到兑换时才检查。后台显示「未兑张数 × 每张计划支出（`totalWalletDebitUsd`）」和钱包余额的对比，不够时告警。兑换时遇到 `422` / `SPENDABLE_BALANCE_INSUFFICIENT`：码保持锁定、可重试，不判客户失败。
- **直充卡余额。** 用 `GET /cards/{id}` 看余额，低于阈值告警。自动充卡（`recharge-quote` / `recharge`）放第二期。
- **付款开关。** `GET /x-direct/plans` 返回 `paymentsEnabled = false` 或某套餐 `enabled = false` 时，公开页对应套餐停止兑换，已报价的不再确认。
- **成本入账。** 每笔兑换记录 `estimatedUsd`、服务费，以及 CDK 的 `fundingFeeUsd`、`openFeeUsd`，在对账页可看。CDK 注资后没付成的钱留在卡上，对账页单独列出。

---

## 8. 限流与白名单（重点）

- **公开 X CDK 接口按来源 IP 计数。** 客户经本站代理后，来源 IP 都是服务器，所以全站合计每分钟最多 10 次 preflight、10 次 redeem。一单要 redeem 两次，高峰大约 **5 单/分钟**。
- **直充 prepare 每账户每分钟 5 个新报价。**
- 对策：
  1. 本站兑换请求进队列，遇到 429 等 60 秒（`Retry-After`）后**用原标识**重试，不换请求 ID。
  2. 量大时开多个账户分流。
  3. 找 Avanfinity 确认服务器 IP 能否提额。
  4. 不让浏览器直连上游：那样会把上游码暴露给客户。
- **写接口要 IP 白名单。** 服务器出口 IP 要填到 App 上。现有卡台接入页的「出口 IP」卡片按账户显示即可。

---

## 9. 页面设计

可点击原型：Cursor 画布 `x-member-pages.canvas.tsx`。页面里的金额、账户、卡号都是示例数据。

### 9.1 三条总原则

1. **客户只认卡密。** 通道、卡台、外币金额、服务费一律不给客户看。码的状态、开通给谁、进度都从卡密推出来。
2. **每条失败文案先回答「卡密还能不能用」。** 能用就说「卡密没有被消耗，可以换账号再试」；锁住就说「不要重复提交，系统在核实」。
3. **运营先看出问题的单。** 后台默认打开「待处理」，每条都写清楚发生了什么、码现在是什么状态、下一步点哪个按钮。

### 9.2 客户兑换页 `/x`

结构沿用现有公开页：顶部「返回首页 · 语言 · 明暗」，下面一个四步进度条：**卡密 → X 账号 → 确认 → 开通**。

| 步骤 | 页面内容 | 背后发生的事 |
| --- | --- | --- |
| 卡密 | 一个输入框。从兑换链接 `/x?code=…` 打开时自动填好 | 按 `DNX-` 识别；CDK 通道调 preview，直充只查本地。已兑过的码直接跳到进度或结果 |
| X 账号 | 顶部显示「卡密有效 · X Premium+ · 12 个月」；一个用户名输入框，下面实时显示「将开通给 @xxx」 | 前端和后端都做同一套规范化：去掉 `@`、`x.com/`、`twitter.com/`、链接参数，只留用户名，再校验 1–15 位。点「下一步」才报价，不边输边查 |
| 确认 | 只放两件事：套餐和账号，外加「在 X 上打开核对」链接；一句提醒「开通后不能更换账号，也不能退回卡密」 | 报价已拿到并通过上限检查；超上限时本站取消报价，这一步显示「暂时无法开通」 |
| 开通 | 三段进度：已确认 → 付款中 → 开通；「可以关闭页面，之后输入同一张卡密就能回到这里」 | 直充 confirm；CDK 两次 redeem 由后台自动推进。前端每 3–5 秒读本站状态 |

另外四种结果页：

- **排队：** 「前面还有 N 位，约 1 分钟」，自动继续。对应上游 429。
- **成功：** 「已为 @xxx 开通 X Premium+ · 12 个月」，按钮「打开 X 查看」「再兑一张」。
- **账号不符合（ineligible）：** 说明常见原因（已是 Premium、账号受限、刚改过用户名），明确写「卡密没有被消耗」，按钮回到填账号。
- **结果不确定：** 「正在核实付款结果，卡密已锁定，请不要重复提交」，超过 30 分钟联系客服，附一键复制卡密。

**进度跟着卡密走，不跟着浏览器走。** 设备标识和请求标识都存在服务器上，所以客户换手机、换浏览器，输入同一张卡密都能回到当前进度。这一点比现有 OpenAI 兑换页（进度存在浏览器 sessionStorage）好用，也更安全。

**入口和互相跳转：**

- 首页服务卡片加「X Premium 开通」。
- `/recharge` 输入 `DNX-` 码自动跳到 `/x?code=…`；`/x` 输入 `DN-` 或旧码自动跳回 `/recharge?code=…`。客户买错页面也能走通。
- 「卡密状态查询」`/history` 支持 `DNX-`：显示套餐、状态、开通给的账号（中间几位打码）。
- `/x` 不加进现有「单张 / 批量 / 查询 / 账单」切换条：那四个都是 ChatGPT 的功能，混进去会误导。

### 9.3 后台「X 会员」`/ops/x`

在后台导航「兑换对账」后面加一项「X 会员」。页面顶部固定一条**通道状态条**，下面三个页签：**发码 / 兑换记录 / 通道设置**。

**通道状态条**（两块并排，三个页签都显示）：

- X CDK：状态点 + 账户名；一根条显示「未兑负债 / 钱包余额」和覆盖倍数；未兑张数。
- X 直充：状态点 + 账户名；付款卡尾号和余额对比告警线；服务费钱包余额；X 付款开关。
- 任一项异常（卡余额低于告警线、付款开关关闭、凭证失效）时，状态点变色并写出原因。

**发码页签**，从上到下：

1. **选套餐：** 2 行 × 3 列卡片（Premium / Premium+ × 3、6、12 个月）。每张卡片直接写「每张最多 $xx」（CDK）或「实测 ≈ $xx」（直充），运营不用自己换算外币。未配置上限或付款开关关闭的套餐置灰并写原因。
2. **选通道：** 「X CDK（推荐）/ X 直充」二选一，下面一句话说明区别。默认值取通道设置。
3. **数量和备注：** 数量快捷键 1 / 10 / 50 / 100，上限 200（后端按上游每批 50 张拆开）。备注写客户名或渠道，之后在兑换记录里可以搜。
4. **汇总行和生成按钮：** 「10 张 Premium · 3 个月 · 每张最多 $18.00 · 合计最多 $180.00」，下面一句说明钱什么时候、从哪里扣。发完后未兑负债会超过钱包余额时显示黄色提醒，但不拦：发码本身不扣钱。
5. **生成结果：** 默认主按钮是「复制兑换链接」（`https://站点/x?code=…`，客户点开就填好），另有「复制卡密」「导出 Excel」。
6. **最近批次：** 时间、套餐、通道、已兑 / 总数、备注、导出。

**兑换记录页签：**

- 顶部筛选：全部 / **待处理**（默认）/ 进行中 / 已完成 / 失败已退回 / 未使用，每项带数量；右侧一个搜索框，同时匹配卡密、X 用户名、批次备注。
- 左边列表：卡密前缀、套餐、开通给、状态、成本、时间。右边详情面板，点「查看」切换。
- 详情面板：
  - 待处理原因（黄色提示框），例如「第二次付款请求超时，已自动查询 14 次，上游仍是 paying。卡密已锁定，不会重付」。
  - 基本信息：套餐、通道和账户、开通给、批次备注。
  - 本站成本：X 官方金额（外币 ≈ 美元）、服务费、开卡费、注资额。
  - 处理过程时间线：本站记录的每一步和时间。
  - 操作按钮，只出现当前状态下安全的操作：

    | 状态 | 可用操作 |
    | --- | --- |
    | 结果不确定 / 上游人工审核 | 立即重新查询；标记已人工处理（必须填备注，写审计日志） |
    | 钱包余额不足 | 充值后重试（沿用原请求标识）；作废本站码 |
    | 进行中 | 立即重新查询 |
    | 未使用 | 复制兑换链接；作废（CDK 通道同时撤销上游码） |
    | 失败已退回 | 复制兑换链接 |
    | 已完成 | 查看官方账单 |

  - 「排障信息」默认折叠：上游 ID、请求标识、原始上游状态。
- 「重新开放给客户」只在上游明确确认没动钱时出现，其余情况不提供。

**通道设置页签：**

- **两个通道并排**，每块只有：启用开关；**只读**显示用的卡台（带状态点）和卡；「在卡台查看」「换卡」两个按钮，直接跳到「卡台」页里对应卡台的对应页签。卡台、凭证、卡片只在「卡台」页改。
- **每套餐花费上限表：** 列为套餐、启用、**最近实测报价**（金额和多久以前）、CDK 每张最多花费、CDK 卡片注资、直充最多花费。
  - 「试报价」：用测试账号在两个通道各报一次价然后立即取消，不花钱，结果写进「最近实测」。
  - 「按实测 +15% 填入」：一键把上限填成实测价加 15%，保存前可以逐格改。
  - 上限按美元填，本站按实测币种换算。提示：已发出的 CDK 按发码当时的上限执行，改这里不影响它们。
- **告警：** 钱包低于多少美元、付款卡低于多少美元、单子卡住超过多少分钟；开关「同时发到 Telegram」，复用现有通知配置。
**总览页：** 加一块「X 会员」小卡：今日开通数、进行中、待处理数（点击进入待处理列表）。

### 9.5 后台「卡台」页（重做，替代卡台接入 / 选卡配置 / Webhook 事件）

**现在的问题：** 卡台接入页从上到下平铺：出口 IP、代理换码、只对主台生效的「一键检测 / 连通 / 价格 / 余额」、多卡台账户卡片（每张卡片里又塞回调 URL、Secret、最近事件）、底部三张主台摘要卡。选卡和回调事件又各是一页，进去还要先在顶部选账户。结果是：看不出哪台有问题；同样的操作有「主台版」和「按账户版」两套；改一台要在三页之间来回跳。

**导航：** 「卡台接入」「选卡配置」「Webhook 事件」三项合成一项「卡台」，路径 `/ops/platforms/:id?tab=…`。旧地址跳转：`/ops/integration` → `/ops/platforms`；`/ops/card-selection?account_id=N` → `/ops/platforms/N?tab=cards`；`/ops/webhooks` → 主台的 `?tab=webhook`。

**布局：** 左边卡台列表，右边详情。

- **页头：** 标题「卡台」；右上角固定出口 IP 和复制按钮（全站只这一处）；「添加卡台」。
- **需要处理汇总：** 有问题时页头下方出现一条黄条，每个问题一行，写清哪台、什么问题，带一个直达按钮，比如「去配置回调」「测连通」，点了跳到那台的对应页签。没问题时不出现。来源包括：Webhook Secret 未配、熔断打开、凭证 401、白名单 403、余额低于告警线。
- **左侧列表按用途分组：**
  - 「OpenAI 发码」：各卡台一行，显示状态点、名称、主台或备台、余额或「需要处理」。组末放「发码策略」。
  - 「X 会员」：各卡台一行，显示状态点、名称、用途（X CDK / X 直充）、余额。
  - 「其他」：「未归属回调」（带条数）、「代理换码」。
- **右侧详情：** 标题行包括名称、状态、角色或用途、协议，以及「测连通」；有问题时下面紧跟一条说明；再往下是页签：

  | | OpenAI 卡台 | X 卡台 |
  | --- | --- | --- |
  | 概览 | 可消费余额（含保证金说明）、服务费、连通和熔断、挂在这台的未用卡密数、近 24h 兑换和失败；「一键检测」「同步套餐」 | 钱包余额（CDK 显示未兑负债覆盖倍数）、付款方式、通道状态；四步测连通 |
  | 第二个页签 | **选卡**：原选卡配置页的内容（产品在线、自动选卡顺序、本站策略、卡健康、拉黑卡），跟着左边选中的卡台走 | **付款卡**：直充选付款卡，CDK 选自动开卡或固定卡；每张卡显示余额和状态，冻结的不能选；卡余额告警线 |
  | 第三个页签 | **回调**：回调地址 + 复制、Webhook Secret、这台最近的回调 | **调用记录**：本站调上游写接口的时间、接口、状态码、说明（X 没有回调，排查白名单、限流靠这个） |
  | 凭证 | 名称、地址、API Key；底部停用（写明停用后多少张未用卡密会改走其他台） | 名称、地址、App ID、App Secret、用途开关；底部停用（写明对客户的影响） |

- **发码策略**（只针对 OpenAI）：双绑开关、允许单台降级；「顺序和主台」改成可拖动的列表，排第一的就是主台，不再手填优先级数字、也不再单独勾「设为主台」。提示换主台不影响历史老码（固定 legacy owner）。
- **添加卡台：** 先选用途：OpenAI · SpaceX 旧 OpenAPI / OpenAI · Avanfinity / X 会员 · Avanfinity，表单只显示该类要的字段。新加的 OpenAI 卡台默认排最后、不做主台。按钮是「保存并测连通」。
- **原来只对主台生效的「一键检测 / 连通 / 价格 / 余额」按钮和底部三张摘要卡**：去掉，统一成每个卡台概览里的同一套。
- **代理换码**：和卡台无关，挪到「其他」。

**后端影响：** 基本是前端重组。需要补的接口：按账户查余额和服务费（目前 `/cardplatform/balance`、`/plans` 只查主台）；卡台顺序批量保存（写 `priority` 和 `is_primary_default`）；「需要处理」汇总；X 卡台的调用记录。

### 9.4 页面对后端的额外要求

这些是为了上面的交互必须补的，已计入 P1 / P2：

| 能力 | 用在哪 | 实现 |
| --- | --- | --- |
| 用户名规范化 | 客户页实时提示、后端校验 | 前后端同一套规则，后端为准 |
| 排队位置 | 客户页「前面还有 N 位」 | 本站兑换队列按提交时间排，返回位置和预计等待 |
| 凭卡密恢复进度 | 客户换设备继续看 | `/public/x/result` 只凭卡密返回状态；按 IP 限频防止撞码 |
| 批次与备注 | 发码结果、最近批次、搜索 | 新表 `x_batches`（批次 ID、套餐、通道、数量、备注、创建人） |
| 实测报价记录 | 发码页「实测 ≈」、上限表 | 新表 `x_quote_samples`（通道、套餐、币种、`amountMinor`、`estimatedUsd`、服务费、时间），每次真实报价和试报价都记 |
| 试报价 | 通道设置 | 直充：prepare 后立即 cancel；CDK：发 1 张码 → preflight → revoke |
| 人工处理 | 兑换记录 | 「标记已人工处理」写 `admin_audit_logs`，必须带备注 |
| 状态聚合 | 通道状态条、总览小卡 | 一个接口返回两个通道的余额、卡余额、未兑负债、待处理数，前端 30 秒刷新 |

---

## 10. 代码落位

```
backend/internal/avanfinity/      新协议 HTTP 客户端：App-Id/Secret、Idempotency-Key、{code:200,data} 外壳、errorCode 解析
                                  （以后 OpenAI 的 avanfinity-2026-08 adapter 也用它，替掉 registry.go 里借用旧客户端的那段）
backend/internal/xmember/         通道选择、状态机、上限校验、轮询器
backend/internal/db/x_codes.go    x_codes / x_redemptions / x_batches / x_quote_samples / x_channels / x_plan_limits
backend/internal/handler/x_public.go   /api/v1/public/x/*（preview、quote、confirm、result）
backend/internal/handler/x_admin.go    /api/v1/admin/x/*（overview、issue、batches、records、actions、channels、limits、test-quote）
frontend/src/lib/xHandle.ts                 用户名规范化（带单测，和后端同规则）
frontend/src/views/user/XRedeemView.vue     /x
frontend/src/views/admin/XMemberView.vue    /ops/x 外壳：通道状态条 + 页签
frontend/src/views/admin/x/XIssueTab.vue、XRecordsTab.vue、XChannelsTab.vue
```

现有页面要改的地方：`HomeView.vue`（入口卡片）、`RechargeView.vue`（`DNX-` 跳转）、`CDKStatusView.vue`（支持 `DNX-`）、`AdminLayout.vue`（导航）、`AdminDashboard.vue`（X 小卡）。`CardIntegration.vue`、`CardSelectionConfig.vue`、`WebhookEvents.vue` 拆成组件，装进新的 `views/admin/platforms/PlatformsView.vue`（左侧列表 + 右侧详情页签）。

---

## 11. 落地顺序

| 阶段 | 内容 | 验收 |
| --- | --- | --- |
| P0 | `avanfinity` 客户端；账户能力标记；「卡台」页重做（合并卡台接入 / 选卡配置 / Webhook 事件，旧地址跳转）；`/ops/x` 外壳 + 通道状态条 + 通道设置页签（含测试连接、试报价、上限表）；`ListOpenAIAccounts` 隔离（主台、双发、同步、熔断、测试连接） | 用真实凭证测试连接全绿；试报价拿到真实币种并写进「最近实测」；OpenAI 发码不受影响 |
| P1 | X CDK 通道：发码页签、客户兑换页 `/x`（含排队、四种结果页、凭卡密恢复）、后台轮询、兑换记录页签和待处理操作、首页入口、`/recharge` 与 `/x` 互相跳转 | 发 1 张 `DNX-` 码，用兑换链接走完到 `completed`；中途关掉浏览器、换设备输入卡密能接着看；断网后靠轮询恢复，没有重复扣款 |
| P2 | X 直充通道：发码可选直充、确认前上限检查、直充卡余额告警 | 报价超上限会自动取消；confirm 超时后查原订单，不重付 |
| P3 | `/history` 支持 `DNX-`、总览小卡、直充卡自动充值、多账户分流 | — |

先做 CDK 的原因：花费上限由上游按每张码锁死，没派发的码能撤销，出问题时损失有上界。直充的上限完全靠本站自己校验。

---

## 12. 风险

- **币种或上限设错**，整批 CDK 兑换不了。P0 先实测。
- **公开接口按 IP 限流**，高峰要排队。
- **未兑 CDK 是负债**，钱包不够时兑换会卡住。
- **没有 Webhook**，轮询漏单或上游长时间不结束。靠超时告警和异常队列兜底。
- **上游码和账单链接都是凭据**，加密存储，不进日志、不进 API 响应。
- **CDK 注资后没付成**，钱留在发行者卡上，不回钱包，对账要显示出来。

---

## 13. 已确认（2026-09-30）

1. **账户：** X CDK 和 X 直充各用一个 Avanfinity 账户，而且这两个都不是 OpenAI 现用的 A/B 台。所以两个通道各配各的 `account_id`，且这两个账户都不带 `openai` 能力，必须被 OpenAI 双发排除（第 3 节）。
2. **售卖方式：** 只卖本站 `DNX-` 码，客户在本站兑换页开通。上游 X CDK 不外发。
3. **CDK 两次 redeem：** 客户点一次「确认开通」，后台自动走完注资和付款两步。
