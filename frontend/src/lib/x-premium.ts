export function isXPremiumPlan(value: string): boolean {
  return /^(?:x_)?(?:basic|premium|premium_plus)_(?:monthly|yearly)$/.test(String(value || '').toLowerCase())
}
export function xPremiumCredential(raw: string): string {
  if (!raw || raw.length > 32768 || /\0/.test(raw)) return ''
  try {
    const v = JSON.parse(raw)
    const email = String(v.billing_email || v.billingEmail || '')
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) return ''
    if (typeof v.cookieHeader === 'string' && /auth_token=/.test(v.cookieHeader) && /ct0=/.test(v.cookieHeader) && !/[\r\n\0]/.test(v.cookieHeader)) return raw.trim()
    return /^[a-zA-Z0-9._%~-]{16,512}$/.test(v.auth_token || '') && /^[a-zA-Z0-9._%~-]{16,512}$/.test(v.ct0 || '') ? raw.trim() : ''
  } catch { return '' }
}
