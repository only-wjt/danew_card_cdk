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

export type PrecheckResult = {
  valid?: boolean
  reason_code?: string
  message?: string
  email?: string
  current_plan?: string
  /** 网络或服务问题时才有；有 error 表示没拿到结论，可以稍后重试 */
  error?: string
  /** 被限流时的建议等待秒数 */
  retryAfter?: number
}

/** 用卡台凭据预检校验 Session（不需要卡密）。只回结论，不落库。 */
export async function precheckSession(
  session: string,
  messages: { down: string; fail: string; limited: (secs: number) => string },
): Promise<PrecheckResult> {
  let r: Response
  try {
    r = await fetch('/api/v1/public/session/precheck', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ session }),
    })
  } catch {
    return { error: messages.down }
  }
  const data = await r.json().catch(() => ({}))
  if (r.status === 429) {
    const secs = Number(data.retry_after || r.headers.get('Retry-After') || 30)
    return { error: messages.limited(secs), retryAfter: secs }
  }
  if (!r.ok) {
    return { error: r.status === 404 ? messages.down : String(data.error || messages.fail) }
  }
  return data as PrecheckResult
}
