# 卡网发码接口（cardnet）

给卡网服务端调用的发码接口。码由本站主台发出并落本站库，买家在本站兑换、查单。

- Base：`https://<本站域名>/api/v1/cardnet`
- 鉴权：`Authorization: Bearer cn_live_...`（管理端「卡网对接」创建客户端时生成，只显示一次）
- 只能服务端调用；客户端可配置 IP 白名单
- 每个客户端：允许的套餐、允许的付款地区（未配置时只允许默认地区 PH；不传 `payment_country` 等同 PH）、每日发码额度（北京时间自然日）
- 已成功的请求用同一个键重放总能拿回原码，即使之后收紧了授权；授权只约束新请求和尚未发出的请求
- 本站不向卡网计费，只记录每张码的卡台服务费

## 产品与档位键

| 产品 | 档位键 | 付款地区 |
|---|---|---|
| ChatGPT | 原样，如 `plus`、`pro_20x`、`credit500` | 受客户端「允许地区」约束；不传 = PH |
| Grok | **必须带 `grok_` 前缀**，如 `grok_plus_monthly`、`grok_premium_yearly` | **固定 US**：只能不传或传 `US`；不受「允许地区」约束 |

- 档位键以 `/overview` 下发的为准，不要自己拼；
- 裸键 `premium_monthly` / `premium_yearly` 是 X 的键，Grok 必须用 `grok_premium_monthly`；
- **X 订阅不开放给卡网**：`x_*`、`basic_*`、`premium_*` 一律 422 `PLAN_NOT_SELLABLE`；
- 每日额度 ChatGPT 与 Grok 共用。

## 错误与重试约定

失败响应：`{"error_code": "...", "message": "..."}`（发码接口另带 `request_id`、`status`）。

| HTTP | 含义 | 调用方处理 |
|---|---|---|
| 200 | 成功 | — |
| 400 / 401 / 403 / 404 / 422 | 明确拒绝，**未发码** | 不要用同一个键重试；修正后换新键 |
| 409 / 429 | 并发 / 限流 | 稍后用**同一个键**重试 |
| 5xx | 结果待定，可能已发码 | **必须用同一个键**重试，不能换键 |

## GET /overview

```json
{
  "issuable": true,
  "platform": {"account_id": 1, "name": "ZovoCard", "status": "active", "circuit": "closed", "supports_payment_country": true},
  "balance": {"balance": "100.00", "spendable_balance": "80.00", "account_reserve_amount": "20.00"},
  "plans": [
    {"key": "plus", "product": "gpt", "label": "Plus", "purchasable": true, "service_fee_usd_minor": 150, "sort_order": 10,
     "checkout_currency": "", "checkout_amount_minor": 0},
    {"key": "grok_premium_monthly", "product": "grok", "label": "Grok Premium 月付", "purchasable": true,
     "service_fee_usd_minor": 100, "sort_order": 2, "checkout_currency": "USD", "checkout_amount_minor": 3000}
  ],
  "payment_regions": [{"country": "PH", "currency": "PHP"}, {"country": "US", "currency": "USD"}],
  "products": {
    "gpt":  {"available": true, "payment_regions": [{"country": "PH", "currency": "PHP"}, {"country": "US", "currency": "USD"}]},
    "grok": {"available": true, "payment_regions": [{"country": "US", "currency": "USD"}], "fixed_payment_country": "US"}
  },
  "client": {"allowed_plans": ["plus", "grok_premium_monthly"], "allowed_countries": ["US"], "daily_code_limit": 200, "issued_today": 37},
  "checked_at": "2026-10-17T09:00:00Z"
}
```

- `plans` 只含「卡台可售」且「本客户端被允许」的套餐，先 ChatGPT 后 Grok，不含 X。`product` 为 `gpt` / `grok`；
- `payment_regions` 只代表 **ChatGPT** 的地区；各产品的地区与可用状态看 `products`；
- `products.<p>.available=false` 时带 `message`（如「卡台 Grok 套餐暂不可读」），该产品档位不出现在 `plans`，其它产品不受影响；
- `issuable=false` 时 `message` 说明原因。

## POST /cdks/issue

请求头 `Idempotency-Key`：必填，1–128 位 `[A-Za-z0-9._:-]`，作用域为本客户端。

```json
{"plan": "plus", "count": 1, "payment_country": "PH", "external_order_no": "DJ2026...", "external_ref": "item:1:chunk:0"}
```

- `plan`：见「产品与档位键」；Grok 旧键（`grok_lite_monthly` 等）会换成新键，响应里的 `plan` 是新键
- `count`：1–200
- `payment_country`：可选，空 = 卡台默认地区（ChatGPT 为 PH，Grok 为 US）；Grok 传 US 以外的值返回 422 `COUNTRY_UNSUPPORTED`
- `external_order_no` / `external_ref`：可选，仅用于检索与审计

成功：

```json
{
  "request_id": "CNR20261017a1b2c3d4e5f6",
  "status": "succeeded",
  "plan": "plus",
  "payment_country": "PH",
  "count": 1,
  "issued": [{"id": 4821, "code": "GPTD-....", "plan": "plus", "payment_country": "PH", "fee_amount_minor": 150}],
  "fee_total_minor": 150,
  "replayed": false
}
```

- 同一个键、同样参数再次调用返回同一批码，`replayed=true`
- 同一个键、不同参数：422 `IDEMPOTENCY_KEY_REUSED`
- `issued[].id` 用于作废

| HTTP | error_code | 说明 |
|---|---|---|
| 400 | `INVALID_PARAMS` | 参数或 Idempotency-Key 不合法 |
| 401 | `INVALID_API_KEY` | Key 无效或客户端停用 |
| 403 | `IP_NOT_ALLOWED` / `PLAN_NOT_ALLOWED` / `COUNTRY_NOT_ALLOWED` | 未授权 |
| 403 | `DAILY_LIMIT_EXCEEDED` | 超当日额度 |
| 422 | `PLAN_NOT_SELLABLE` | 卡台不可售；X 键；裸 Grok 键（缺 `grok_` 前缀）；主台不支持 Grok |
| 422 | `COUNTRY_UNSUPPORTED` | 卡台不支持该地区；Grok 传了 US 以外的地区 |
| 422 | `UPSTREAM_REJECTED` | 卡台明确拒绝（如余额不足），`message` 带卡台原文 |
| 422 | `MANUAL_FAILED` | 管理员人工判定失败，可换新键重发 |
| 422 | `IDEMPOTENCY_KEY_REUSED` | 同键不同参 |
| 409 | `REQUEST_IN_FLIGHT` | 同一个键正在处理 |
| 429 | `RATE_LIMITED` | 每客户端 120 次/分钟 |
| 502 | `UPSTREAM_COUNT_MISMATCH` / `UPSTREAM_PLAN_MISMATCH` / `CODE_CONFLICT` | 卡台结果异常（张数不符 / Grok 档位不符 / 码冲突），待本站人工核对；核对后同键重试可拿到结果 |
| 503 | `UPSTREAM_PENDING` / `ISSUING_UNAVAILABLE` | 结果待定 / 卡台暂不可用，同键重试 |

## POST /cdks/revoke

作废本客户端发出、尚未兑换的码（例如订单退款）。

```json
{"ids": [4821, 4822], "reason": "order_refunded", "external_order_no": "DJ2026..."}
```

```json
{"revoked": [4821], "failed": [{"id": 4822, "reason": "CONSUMED", "message": "CDK_CONSUMED CDK 已使用"}]}
```

- 每次最多 100 个；已作废的再作废视为成功
- 优先在卡台删除（未使用的购买码退回服务费），删除失败再停用
- `failed[].reason`：`CONSUMED`（已兑换）、`RESERVED`（兑换进行中）、`NOT_FOUND`（不存在或不属于本客户端）、`UPSTREAM_ERROR`
