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
