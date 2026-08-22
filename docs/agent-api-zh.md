# 代理开放 API

> 版本 `1.0.0`　·　本文由 `backend/internal/apidocs/agent-openapi.yaml` 自动生成，请勿手工编辑。

面向代理（二次分发）的充值与对账接口。

## 鉴权
所有接口使用 API Key，放在 `Authorization` 头里：

```
Authorization: Bearer ak_live_xxxxxxxxxxxx
```

API Key 在代理门户「API 密钥」页自助创建，**仅在创建时展示一次**，请立即保存。
密钥泄露时到同一页面吊销即可，吊销后立即失效。

## 异步模型
充值是异步的。`POST /agent/recharge` 与 `POST /agent/batch-recharge` 都只做「受理」，
立即返回 `202` 和 `request_id`，实际开通结果通过以下两种方式获取：

1. **Webhook（推荐）**：在门户「设置」页配置回调地址并生成签名密钥，
   每条明细落终态时会推送事件，无需轮询。详见 `#/components/schemas/WebhookEvent`。
2. **轮询**：调用 `GET /agent/recharge/{request_id}` 或 `GET /agent/batch-recharge/{batch_id}`。
   建议间隔不低于 5 秒。

## 幂等
批量接口支持 `Idempotency-Key` 请求头，24 小时内相同 key 直接返回首次创建的批次，
不会重复扣款。**强烈建议每次提交都带上**，网络超时重发时才不会重复下单。

未带该头时，系统仍会按「套餐 + 凭据集合」在 60 秒内做一次误重复提交保护，
但这只兜误双击，不能替代 `Idempotency-Key`。

## 卡密（拿货模式）
代理充值**必须**提交站长线下分配的卡密 `cdk_code`。服务端不会代代理自动发码扣费。
站长在管理后台「CDK 卡密」页发码/入库后，到「代理管理 → 发卡密」划给对应代理。
门户「设置」可查看 `unused_cdk_count`（名下仍可用卡密数量）。
完整卡密列表见 `GET /agent/cdks`（代理门户「我的卡密」页）。

已分配给代理的卡密默认**不允许终端客户在本站公开兑换页自助兑换**（返回 `CDK_AGENT_CHANNEL`），
避免同一张码被双方同时用掉。所以拿到货之后，请由代理统一代客户提交充值。

## 客户查单
代充完成后，终端客户凭卡密可以自助查询，无需联系代理：

| 客户要查什么 | 入口 |
| --- | --- |
| 卡密是否已使用 | `GET /lookup/cdk?code=` |
| 充值进度 | `GET /public/cdk/result-by-code?code=` |
| 账单 / 发票 | 账单查询页填卡密（后台按订单邮箱查，代理无需交出 session） |

代理侧对账仍走 `GET /agent/records` 与 `GET /agent/batch-recharge`。

## 限额
每个代理有三个独立额度，可在门户「设置」页查看当前值：

| 额度 | 含义 | 超限响应 |
| --- | --- | --- |
| `rate_limit_rpm` | 每分钟请求数 | `429 RATE_LIMITED` |
| `max_concurrent_recharge` | 同时在途的**充值明细条数**（单条与批量共用） | `429 CONCURRENCY_LIMIT` |
| `max_batch_items` | 单批最多条数 | `400 BATCH_TOO_LARGE` |

注意 `max_concurrent_recharge` 计的是明细条数而非批次数：若在途已有 8 条、
额度为 10，此时提交 5 条的批次会被整批拒绝，需等在途降到 5 条以内再提交。

## 错误码
所有错误响应形如 `{"error": "人类可读说明", "error_code": "MACHINE_CODE"}`。
**程序分支请判断 `error_code`**，`error` 文案可能随版本调整。

| error_code | HTTP | 含义 | 建议处理 |
| --- | --- | --- | --- |
| `INVALID_REQUEST` | 400 | 请求体不是合法 JSON 或字段缺失 | 修正后重发 |
| `INVALID_ITEM` | 400 | 某条凭据不合法，`error` 里带序号 | 修正该条后重发 |
| `ITEMS_REQUIRED` | 400 | items 为空 | 检查提交逻辑 |
| `CDK_REQUIRED` | 400 | 缺少 `cdk_code` | 使用站长分配的卡密 |
| `CDK_NOT_FOUND` | 400 | 卡密未录入本站 | 联系站长 |
| `CDK_NOT_ASSIGNED` | 400 | 卡密未分配给本代理 | 联系站长发卡 |
| `CDK_WRONG_AGENT` | 403 | 卡密属于其他代理 | 检查是否抄错码 |
| `CDK_PLAN_MISMATCH` | 400 | 卡密套餐与 plan 不一致 | 换对应套餐的卡密 |
| `CDK_UNAVAILABLE` | 400 | 卡密已使用或已禁用 | 换一张 |
| `CDK_DUPLICATE` | 400 | 同批卡密重复 | 去重后重发 |
| `CDK_IN_FLIGHT` | 409 | 卡密正在其他任务中使用 | 等待完成 |
| `BATCH_TOO_LARGE` | 400 | 超出 `max_batch_items` | 拆成多批 |
| `PLAN_NOT_AVAILABLE` | 400 | 套餐不在可售范围或白名单内 | 先查 `/agent/plans` |
| `AGENT_INACTIVE` | 403 | 账号被停用 | 联系管理员 |
| `BATCH_NOT_FOUND` | 404 | 批次不存在或不属于本账号 | 检查 batch_id |
| `DELIVERY_NOT_FOUND` | 404 | 投递记录不存在或当前状态不可重投 | — |
| `RATE_LIMITED` | 429 | 超出每分钟请求数 | 退避后重试 |
| `CONCURRENCY_LIMIT` | 429 | 超出在途条数额度 | 等在途任务完成再提交 |
| `BALANCE_QUERY_FAILED` | 502 | 上游余额查询失败 | 可直接重试 |
| `CARD_PLATFORM_UNCONFIGURED` | 503 | 卡台未配置 | 联系管理员 |
| `INTERNAL` | 500 | 服务端异常 | 重试；持续失败请联系管理员 |

⚠️ 遇到 `429` 和 `502` 可以安全重试；**遇到明细状态 `unknown` 绝不要重新提交同一账号**，
上游可能已扣款，重复提交会造成双倍扣费。

## 接口一览

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/agent/batch-recharge` | 批次列表 |
| `POST` | `/agent/batch-recharge` | 批量充值 |
| `GET` | `/agent/batch-recharge/{batch_id}` | 批次详情 |
| `GET` | `/agent/batch-recharge/{batch_id}/export` | 导出批次对账表 |
| `GET` | `/agent/cdks` | 我的卡密库存 |
| `GET` | `/agent/plans` | 查询可售套餐 |
| `POST` | `/agent/recharge` | 单条充值 |
| `GET` | `/agent/recharge/{request_id}` | 查询单条充值状态 |
| `GET` | `/agent/records` | 兑换记录查询 |
| `POST` | `/agent/records/search-session` | 按 session 检索记录 |
| `GET` | `/agent/webhooks/deliveries` | 回调投递日志 |
| `POST` | `/agent/webhooks/deliveries/{id}/retry` | 手动重投回调 |

## 接口详情

### `GET /agent/batch-recharge` 批次列表

倒序列出本账号的全部批次，单条充值产生的批次也在其中。

**参数**

| 名称 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `page` | query | 否 | integer | 页码，从 1 开始 |
| `page_size` | query | 否 | integer | 每页条数，最大 100 |

**响应**

| 状态码 | 说明 |
| --- | --- |
| `200` | OK |
| `401` | API Key 缺失、无效或已吊销 |

---

### `POST /agent/batch-recharge` 批量充值

一次提交多条充值任务，立即返回 `batch_id` 及每条的 `request_id`。

执行链路与单条完全一致，服务端内部按小并发逐条处理。
提交前请确认：

- 每条 `items[]` **必须带 `cdk_code`**（站长线下分配给你的卡密）
- 条数不超过 `max_batch_items`
- 在途条数 + 本批条数不超过 `max_concurrent_recharge`，否则整批被拒
- 强烈建议带上 `Idempotency-Key`

**参数**

| 名称 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `Idempotency-Key` | header | 否 | string | 幂等键，建议用 UUID。24 小时内重复提交同一 key 直接返回首次的批次。 |

**请求体**

```json
{
  "items": [
    {
      "cdk_code": "CDK-AAAA-BBBB-CCCC",
      "client_reference": "order-2001",
      "mode": "session",
      "session": "eyJhbGciOi..."
    },
    {
      "cdk_code": "CDK-DDDD-EEEE-FFFF",
      "client_reference": "order-2002",
      "email": "user@example.com",
      "gpt_password": "ChatGPT 登录密码",
      "mode": "mailbox",
      "password": "邮箱密码"
    }
  ],
  "plan": "plus"
}
```

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `items` | object[] | 是 |  |
| `plan` | string | 是 |  |

**响应**

| 状态码 | 说明 |
| --- | --- |
| `200` | 命中幂等，返回原批次（未重复扣款） |
| `202` | 已受理 |
| `400` | 请求参数有误 |
| `401` | API Key 缺失、无效或已吊销 |
| `403` | 账号被停用 |
| `429` | 超出频率或在途条数额度 |
| `502` | 上游余额查询失败 |
| `503` | 卡台未配置 |

---

### `GET /agent/batch-recharge/{batch_id}` 批次详情

返回批次概览与全部明细。轮询建议间隔不低于 5 秒。

**参数**

| 名称 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `batch_id` | path | 是 | string |  |

**响应**

| 状态码 | 说明 |
| --- | --- |
| `200` | OK |
| `401` | API Key 缺失、无效或已吊销 |
| `404` | 资源不存在 |

---

### `GET /agent/batch-recharge/{batch_id}/export` 导出批次对账表

返回 `.xlsx` 对账表（不含凭据）。

**参数**

| 名称 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `batch_id` | path | 是 | string |  |
| `scope` | query | 否 | string (枚举) |  |

**响应**

| 状态码 | 说明 |
| --- | --- |
| `200` | Excel 文件 |
| `401` | API Key 缺失、无效或已吊销 |
| `404` | 资源不存在 |

---

### `GET /agent/cdks` 我的卡密库存

列出站长分配给本代理的卡密（含完整码），用于门户查看与复制。
默认按「未使用优先」排序。

**参数**

| 名称 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `status` | query | 否 | string (枚举) | 按库存状态筛选；不传则返回全部 |
| `plan` | query | 否 | string | 套餐 key |
| `code` | query | 否 | string | 卡密前缀或片段 |
| `page` | query | 否 | integer |  |
| `page_size` | query | 否 | integer |  |

**响应**

| 状态码 | 说明 |
| --- | --- |
| `200` | OK |
| `401` | API Key 缺失、无效或已吊销 |

---

### `GET /agent/plans` 查询可售套餐

返回「卡台在售 ∩ 该代理白名单」的套餐列表。下单前应先取此列表，不要硬编码套餐 key。

**响应**

| 状态码 | 说明 |
| --- | --- |
| `200` | OK |
| `401` | API Key 缺失、无效或已吊销 |
| `502` | 卡台档位暂不可用 |

---

### `POST /agent/recharge` 单条充值

受理一条充值任务，立即返回 `request_id`。开通结果通过 webhook 或轮询获取。

**须提交站长分配给你的 `cdk_code`**，服务端不会代你自动发码。

**请求体**

```json
{
  "account": {
    "email": "user@example.com",
    "gpt_password": "ChatGPT 登录密码",
    "mode": "mailbox",
    "password": "邮箱密码"
  },
  "cdk_code": "CDK-YYYY-YYYY-YYYY",
  "client_reference": "order-1002",
  "plan": "plus"
}
```

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `account` | Credential | 是 |  |
| `cdk_code` | string | 是 | 站长分配给你的完整卡密 |
| `client_reference` | string | 否 | 你自己的业务单号，会原样带回并出现在 webhook 与对账导出里 |
| `plan` | string | 是 |  |

**响应**

| 状态码 | 说明 |
| --- | --- |
| `200` | 命中去重，返回原任务（未重复扣款） |
| `202` | 已受理 |
| `400` | 请求参数有误 |
| `401` | API Key 缺失、无效或已吊销 |
| `403` | 账号被停用 |
| `429` | 超出频率或在途条数额度 |

---

### `GET /agent/recharge/{request_id}` 查询单条充值状态

**参数**

| 名称 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `request_id` | path | 是 | string |  |

**响应**

| 状态码 | 说明 |
| --- | --- |
| `200` | OK |
| `401` | API Key 缺失、无效或已吊销 |
| `404` | 资源不存在 |

---

### `GET /agent/records` 兑换记录查询

按邮箱、卡密、状态、套餐筛选。

**按 session 检索请改用 `POST /agent/records/search-session`**：
session 明文放进 query string 会落进访问日志和浏览器历史。

**参数**

| 名称 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `email` | query | 否 | string | 账号邮箱，模糊匹配 |
| `cdk` | query | 否 | string | 卡密，前缀或模糊匹配 |
| `status` | query | 否 | string (枚举) |  |
| `plan` | query | 否 | string | 套餐 key，精确匹配 |
| `page` | query | 否 | integer | 页码，从 1 开始 |
| `page_size` | query | 否 | integer | 每页条数，最大 100 |

**响应**

| 状态码 | 说明 |
| --- | --- |
| `200` | OK |
| `401` | API Key 缺失、无效或已吊销 |

---

### `POST /agent/records/search-session` 按 session 检索记录

session 只以哈希形式存储与比对，服务端不留明文，也不会回显。
用 POST 是为了避免明文出现在 URL 里。

**请求体**

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `page` | integer | 否 |  |
| `page_size` | integer | 否 |  |
| `session` | string | 是 |  |

**响应**

| 状态码 | 说明 |
| --- | --- |
| `200` | OK |
| `400` | 请求参数有误 |
| `401` | API Key 缺失、无效或已吊销 |

---

### `GET /agent/webhooks/deliveries` 回调投递日志

查看每条事件的投递状态、重试次数与最近一次错误，便于排查回调没收到的原因。

**参数**

| 名称 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `page` | query | 否 | integer | 页码，从 1 开始 |
| `page_size` | query | 否 | integer | 每页条数，最大 100 |

**响应**

| 状态码 | 说明 |
| --- | --- |
| `200` | OK |
| `401` | API Key 缺失、无效或已吊销 |

---

### `POST /agent/webhooks/deliveries/{id}/retry` 手动重投回调

仅对 `status = failed` 的记录有效，重投后重新进入自动重试队列。

**参数**

| 名称 | 位置 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | path | 是 | integer |  |

**响应**

| 状态码 | 说明 |
| --- | --- |
| `200` | 已重新入队 |
| `401` | API Key 缺失、无效或已吊销 |
| `404` | 资源不存在 |

---

## Webhook 回调

推送到你回调地址的事件体（`POST`，`Content-Type: application/json`）。

### 请求头
| 头 | 说明 |
| --- | --- |
| `X-Webhook-Id` | 事件唯一 ID，等于 body 里的 `event_id`，**用它做幂等去重** |
| `X-Webhook-Event` | 事件类型 |
| `X-Webhook-Timestamp` | Unix 秒级时间戳，参与签名 |
| `X-Signature` | `hex(HMAC-SHA256(secret, timestamp + "." + rawBody))` |

### 验签
用门户「设置」页生成的 webhook 密钥，对 `时间戳 + "." + 原始请求体` 做
HMAC-SHA256，与 `X-Signature` 常量时间比对。务必使用**原始字节**，
不要先反序列化再重新序列化。

建议同时校验 `X-Webhook-Timestamp` 与当前时间相差不超过 5 分钟，以拒绝重放。

```javascript
const crypto = require("crypto");
// express: app.post("/hook", express.raw({ type: "application/json" }), ...)
function verify(req, secret) {
  const ts = req.get("X-Webhook-Timestamp");
  const expect = crypto.createHmac("sha256", secret)
    .update(ts + "." + req.body)  // req.body 是 Buffer
    .digest("hex");
  const got = req.get("X-Signature") || "";
  return got.length === expect.length &&
    crypto.timingSafeEqual(Buffer.from(got), Buffer.from(expect));
}
```

```python
import hmac, hashlib
def verify(raw_body: bytes, timestamp: str, signature: str, secret: str) -> bool:
    expect = hmac.new(secret.encode(),
                      timestamp.encode() + b"." + raw_body,
                      hashlib.sha256).hexdigest()
    return hmac.compare_digest(expect, signature)
```

### 投递与重试
返回 2xx 视为成功；其余状态码或超时（10 秒）会重试，
间隔依次为 30 秒、2 分钟、10 分钟、30 分钟、2 小时，共 6 次。
全部失败后置为 `failed`，可在门户「回调日志」里手动重投。

重试意味着**同一事件可能被投递多次**，请按 `event_id` 幂等处理。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `created_at` | string | 否 |  |
| `data` | object | 否 | `recharge.*` 事件为单条明细（字段同 RechargeRecord 的子集）； `batch.completed` 事件为批次汇总（字段同 BatchSummary 的子集）。 |
| `event_id` | string | 否 |  |
| `event_type` | string (枚举) | 否 |  |

---

## 数据结构

### BatchSummary

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `batch_id` | string | 否 |  |
| `created_at` | string | 否 |  |
| `failed` | integer | 否 |  |
| `message` | string | 否 |  |
| `pending` | integer | 否 |  |
| `plan` | string | 否 |  |
| `running` | integer | 否 |  |
| `skipped` | integer | 否 |  |
| `source` | string | 否 |  |
| `status` | string (枚举) | 否 | paused 表示因余额不足等致命错误中止，剩余条目未提交 |
| `success` | integer | 否 |  |
| `total` | integer | 否 |  |
| `unknown` | integer | 否 |  |
| `updated_at` | string | 否 |  |

### Credential

ChatGPT 账号凭据 + 卡密。批量接口每条须带 `cdk_code`；单条接口在顶层传 `cdk_code`。

凭据二选一：

- `mode = session`：只需 `session`
- `mode = mailbox`：需要 `email` + `password`，建议同时给 `gpt_password`

凭据仅用于本次开通，服务端不回显；session 只存哈希。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `cdk_code` | string | 否 | 站长分配给你的完整卡密（批量接口必填） |
| `email` | string | 否 | mode = mailbox 时必填 |
| `email_password` | string | 否 | 邮箱密码；留空时回落到 password |
| `gpt_password` | string | 否 | ChatGPT 登录密码 |
| `mode` | string (枚举) | 是 |  |
| `password` | string | 否 | mode = mailbox 时必填，邮箱密码 |
| `session` | string | 否 | mode = session 时必填 |

### Error

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `error` | string | 否 | 人类可读的错误说明，用于展示 |
| `error_code` | string | 否 | 机器可判断的错误码，程序分支请用这个字段而非 error 文案。取值见文档开头「错误码」一节。 |

### ItemStatus

明细状态机。终态为 `success` / `failed` / `skipped` / `unknown`。

- `pending`：已受理，排队中
- `issuing` / `preparing` / `submitted` / `processing`：处理中
- `success`：开通完成
- `failed`：开通失败
- `skipped`：账号已是目标套餐，未扣款
- `unknown`：上游结果不确定。**切勿重新提交同一账号**，
  请联系管理员核对，重复提交可能造成双倍扣款。

取值：`pending`、`issuing`、`preparing`、`submitted`、`processing`、`success`、`failed`、`skipped`、`unknown`

### RechargeRecord

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `account_email` | string | 否 |  |
| `batch_id` | string | 否 |  |
| `cdk_prefix` | string | 否 | 内部凭证前缀，仅用于人工核对 |
| `client_reference` | string | 否 | 你提交时带的业务单号 |
| `created_at` | string | 否 |  |
| `cred_mode` | string (枚举) | 否 |  |
| `message` | string | 否 | 人类可读说明，不要用于程序判断 |
| `plan` | string | 否 |  |
| `request_id` | string | 否 | 该条充值的唯一标识，对账以此为准 |
| `seq` | integer | 否 |  |
| `source` | string (枚举) | 否 |  |
| `status` | ItemStatus | 否 |  |
| `updated_at` | string | 否 |  |
| `upstream_order_id` | string | 否 |  |

### RecordPage

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `list` | RechargeRecord[] | 否 |  |
| `page` | integer | 否 |  |
| `page_size` | integer | 否 |  |
| `total` | integer | 否 |  |

### WebhookDelivery

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `attempts` | integer | 否 |  |
| `batch_id` | string | 否 |  |
| `created_at` | string | 否 |  |
| `delivered_at` | string | 否 |  |
| `event_id` | string | 否 |  |
| `event_type` | string | 否 |  |
| `id` | integer | 否 |  |
| `last_error` | string | 否 |  |
| `last_status_code` | integer | 否 |  |
| `next_attempt_at` | string | 否 |  |
| `request_id` | string | 否 |  |
| `status` | string (枚举) | 否 |  |

