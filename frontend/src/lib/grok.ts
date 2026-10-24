// Grok 卡密：档位识别 + 登录态（grok.com 的 sso cookie）规范化。
//
// 卡台预览回裸档位（premium_monthly / plus_yearly …，不带 grok_ 前缀）；新版卡台另回 product。
// ★2026-10-19 改名★（对齐 xAI 现行 Plus / Premium / Super / Ultra）后，裸 premium_monthly 同时是
// X Premium 的键——有 product 时调用方先补成 grok_premium_monthly 再判；没有时 X 优先（见 plan.ts）。
// 旧键 lite / heavy / monthly / yearly 仍认，显示为新名。
//
// ★Grok 不产预检票★：预检只查账号状态，不回 preflight_token；兑换时改为直接带登录态
// credential（卡台按码上的产品分流，GPT / X 码仍必须带 preflight_token）。

// 带 grok_ 前缀时认全部四档；裸键不认 premium_*（归 X）。
const GROK_PLAN = /^(?:grok_(?:(?:plus|premium|super|ultra|lite|heavy)_)?|(?:(?:plus|super|ultra|lite|heavy)_)?)(?:monthly|yearly)$/

export function isGrokPlan(value: string): boolean {
  return GROK_PLAN.test(String(value || '').trim().toLowerCase())
}

// 改名前的旧裸键 → 新裸键。★不含 plus_*★：它现在就是新键（$8 的 Grok Plus）。
const LEGACY: Record<string, string> = {
  lite_monthly: 'plus_monthly', lite_yearly: 'plus_yearly',
  monthly: 'premium_monthly', yearly: 'premium_yearly',
  heavy_monthly: 'ultra_monthly', heavy_yearly: 'ultra_yearly',
}

/** 裸档位（去掉 grok_ 前缀、旧键换新键），用于和预检回来的 currentPlan 比对。 */
export function grokBarePlan(value: string): string {
  const bare = String(value || '').trim().toLowerCase().replace(/^grok_/, '')
  return LEGACY[bare] || bare
}

/** 预检报价表（quotes / promotions）里该档位可能用的键：新裸键、带前缀、改名前的旧裸键。 */
export function grokQuoteKeys(value: string): string[] {
  const bare = grokBarePlan(value)
  return [bare, 'grok_' + bare, ...Object.keys(LEGACY).filter((k) => LEGACY[k] === bare)]
}

const TIER_LABEL: Record<string, string> = { plus: 'Plus', premium: 'Premium', super: 'Super', ultra: 'Ultra' }

export function grokPlanLabel(value: string): string {
  const m = grokBarePlan(value).match(/^(plus|premium|super|ultra)_(monthly|yearly)$/)
  if (!m) return value
  return `Grok ${TIER_LABEL[m[1]]} / ${m[2] === 'yearly' ? '年' : '月'}`
}

const SSO_COOKIE = /(?:^|;\s*)sso(?:-rw)?=[^;\s]+/

/** 已是 cookie 形式（含 sso= / sso-rw=）的 Grok 登录态。批量导入只认这种，避免和 GPT 裸 token 混淆。 */
export function isGrokCredential(raw: string): boolean {
  const s = String(raw || '').trim()
  return !!s && s.length <= 8192 && !/[\r\n\0]/.test(s) && !s.startsWith('{') && SSO_COOKIE.test(s)
}

/**
 * 规范化用户粘贴的 Grok 登录态，无效返回 ''。
 * 接受整段 cookie（含 sso=）或只粘了 sso 的值（补成 sso=<值>）。
 */
export function grokCredential(raw: string): string {
  const s = String(raw || '').trim()
  if (!s || s.length > 8192 || /[\r\n\0]/.test(s)) return ''
  if (isGrokCredential(s)) return s
  // 只粘了值：cookie 值字符集，末尾允许 base64 填充 =
  return /^[A-Za-z0-9._~%+/-]{16,4096}={0,2}$/.test(s) ? 'sso=' + s : ''
}

export interface GrokBenefit {
  /** 产品线：Grok / Cursor / X / 用量 / 计费 */
  name: string
  detail: string
}

// 2026-10-12 起 Grok 按「The X Subscription」售卖：Grok + Cursor + X Premium 打包。
// 卡台不下发权益说明，这里本站维护；xAI 调整档位权益时同步改这张表。
const TIER_BENEFITS: Record<string, GrokBenefit[]> = {
  plus: [
    { name: 'Grok', detail: '对话 · 语音 · Imagine · Build · Grok API' },
    { name: 'Cursor', detail: '可用（仅 Grok 模型）' },
    { name: 'X', detail: 'X 蓝 V 认证 · 广告减少' },
    { name: '用量', detail: '基准用量' },
  ],
  premium: [
    { name: 'Grok', detail: '最新模型 · 更快对话 · 图片/视频生成 · 云端 Agent · Grok Bot' },
    { name: 'Cursor', detail: 'Pro 级权益（与 Grok 共用用量）' },
    { name: 'X', detail: 'X 蓝 V 认证 · 无广告（X Premium+ 权益）' },
    { name: '用量', detail: 'Plus 的 4 倍' },
  ],
  super: [
    { name: 'Grok', detail: 'Premium 全部权益 · 高峰期优先使用模型' },
    { name: 'Cursor', detail: 'Pro 级权益（与 Grok 共用用量）' },
    { name: 'X', detail: 'X 蓝 V 认证 · 无广告（X Premium+ 权益）' },
    { name: '用量', detail: 'Plus 的 15 倍' },
  ],
  ultra: [
    { name: 'Grok', detail: 'Super 全部权益 · 新产品/新模型抢先体验' },
    { name: 'Cursor', detail: 'Pro 级权益（与 Grok 共用用量）' },
    { name: 'X', detail: 'X 蓝 V 认证 · 无广告（X Premium+ 权益）' },
    { name: '用量', detail: 'Plus 的 50 倍' },
  ],
}

/** Grok 档位包含的权益；不是已知 Grok 档位返回空数组。年付与月付权益相同，另加一条计费说明。 */
export function grokPlanBenefits(value: string): GrokBenefit[] {
  const m = grokBarePlan(value).match(/^(plus|premium|super|ultra)_(monthly|yearly)$/)
  if (!m) return []
  const list = TIER_BENEFITS[m[1]]
  return m[2] === 'yearly' ? [...list, { name: '计费', detail: '按年计费（权益同月付）' }] : list
}
