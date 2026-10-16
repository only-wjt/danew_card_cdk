// 付款地区显示名。地区清单本身只认卡台下发的 payment_regions，这里只做国家码 → 中文名。
const REGION_NAMES: Record<string, string> = {
  PH: '菲律宾', US: '美国', JP: '日本', CL: '智利', EG: '埃及', IN: '印度', KR: '韩国', NG: '尼日利亚', TR: '土耳其',
}

export type PaymentRegion = { country: string; currency: string }

export function regionLabel(code: string): string {
  return REGION_NAMES[code] || code
}

/** 解析 /admin/cardplatform/plans 返回的 payment_regions；卡台不支持地区时为空数组。 */
export function parsePaymentRegions(raw: unknown): PaymentRegion[] {
  if (!Array.isArray(raw)) return []
  return raw
    .map((r: any) => ({ country: String(r?.country || ''), currency: String(r?.currency || '') }))
    .filter((r) => r.country && r.currency)
}
