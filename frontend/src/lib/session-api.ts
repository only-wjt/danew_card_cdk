export const SESSION_TOOL_MAX = 20

export type SessionRefreshRow = {
  ok?: boolean
  error?: string
  email?: string
  session?: Record<string, unknown>
}

export type BillingSummary = {
  plan_type?: string
  subscription_plan?: string
  has_active_subscription?: boolean
  expires_at?: string
  renews_at?: string
  will_renew?: boolean | null
}

function backendError(status: number, fallback: string, down: string): string {
  if (status === 404 || status >= 500) return down
  return fallback
}

export async function refreshSessions(
  sessions: string[],
  messages: { down: string; fail: string },
): Promise<SessionRefreshRow[]> {
  if (!sessions.length) return []
  let r: Response
  try {
    r = await fetch('/api/v1/public/session/refresh', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sessions: sessions.slice(0, SESSION_TOOL_MAX) }),
    })
  } catch {
    throw new Error(messages.down)
  }
  const data = await r.json().catch(() => ({}))
  if (!r.ok) {
    throw new Error(backendError(r.status, String(data.error || messages.fail), messages.down))
  }
  return Array.isArray(data.results) ? data.results : []
}

export async function checkSessionBilling(
  tokenInput: string,
  messages: { down: string; fail: string },
): Promise<{ summary?: BillingSummary; error?: string }> {
  let r: Response
  try {
    r = await fetch('/api/v1/public/billing/check', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token_input: tokenInput }),
    })
  } catch {
    return { error: messages.down }
  }
  const data = await r.json().catch(() => ({}))
  if (!r.ok) {
    return { error: backendError(r.status, String(data.error || messages.fail), messages.down) }
  }
  return { summary: data.summary || {} }
}

export function planLabel(summary: BillingSummary | undefined): string {
  const raw = String(summary?.plan_type || summary?.subscription_plan || '').trim()
  if (!raw || raw === 'free') return 'free'
  return raw.replace('chatgpt', 'ChatGPT ').replace(/_/g, ' ')
}
