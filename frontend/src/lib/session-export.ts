import type { ImportedSession } from './batch-session'

/** Ported from coderwfz/GptSessionCpaSub2api (sub2api / CPA / Cockpit). Runs in the browser. */
export const CONVERT_MAX = 2000

export type ExportFormat = 'sub2api' | 'cpa' | 'cockpit'

export type JsonObject = Record<string, any>

export interface ConvertedAccount {
  sourceName: string
  sourcePath: string
  email: string
  name: string
  planType?: string
  expiresAt?: string
  cpa: JsonObject
  cockpit: JsonObject
  sub2apiAccount: JsonObject
}

export interface SessionInspectRow {
  sourceName: string
  sourcePath: string
  email: string
  planType?: string
  accountId?: string
  hasAccessToken: boolean
  hasSessionToken: boolean
  accessExpiresAt?: string
  accessExpired: boolean
  sessionRaw: string
  issues: string[]
}

export interface ConvertIssue {
  sourceName: string
  path: string
  reason: string
  sessionRaw?: string
  canRefresh?: boolean
}

export interface ConvertResult {
  converted: ConvertedAccount[]
  skipped: ConvertIssue[]
}

export interface SessionSource {
  value: JsonObject
  sourceName: string
  path: string
}

function isPlainObject(value: unknown): value is JsonObject {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function firstNonEmpty(...values: unknown[]): string | undefined {
  for (const value of values) {
    if (typeof value === 'string' && value.trim() !== '') return value.trim()
  }
  return undefined
}

function decodeBase64Url(value: string): string {
  const normalized = value.replace(/-/g, '+').replace(/_/g, '/')
  const padded = normalized.padEnd(Math.ceil(normalized.length / 4) * 4, '=')
  const binary = atob(padded)
  const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0))
  return new TextDecoder().decode(bytes)
}

function bytesToBase64Url(bytes: Uint8Array): string {
  let binary = ''
  for (let index = 0; index < bytes.length; index += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(index, index + 0x8000))
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}

function encodeBase64UrlJson(value: unknown): string {
  return bytesToBase64Url(new TextEncoder().encode(JSON.stringify(value)))
}

function parseJwtPayload(token: unknown): JsonObject | undefined {
  if (typeof token !== 'string' || token.trim() === '') return undefined
  const segments = token.split('.')
  if (segments.length < 2) return undefined
  try {
    const parsed = JSON.parse(decodeBase64Url(segments[1]))
    return isPlainObject(parsed) ? parsed : undefined
  } catch {
    return undefined
  }
}

function getOpenAIAuthSection(payload: JsonObject | undefined): JsonObject {
  if (!isPlainObject(payload)) return {}
  const auth = payload['https://api.openai.com/auth']
  return isPlainObject(auth) ? auth : {}
}

function getOpenAIProfileSection(payload: JsonObject | undefined): JsonObject {
  if (!isPlainObject(payload)) return {}
  const profile = payload['https://api.openai.com/profile']
  return isPlainObject(profile) ? profile : {}
}

function normalizeTimestamp(value: unknown): string | undefined {
  if (value instanceof Date && !Number.isNaN(value.getTime())) return value.toISOString()
  if (typeof value === 'number' && Number.isFinite(value)) {
    const milliseconds = value > 1e11 ? value : value * 1000
    const date = new Date(milliseconds)
    return Number.isNaN(date.getTime()) ? undefined : date.toISOString()
  }
  if (typeof value !== 'string' || value.trim() === '') return undefined
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString()
}

function timestampFromUnixSeconds(value: unknown): string | undefined {
  const numeric = Number(value)
  if (!Number.isFinite(numeric)) return undefined
  const date = new Date(numeric * 1000)
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString()
}

function epochSecondsFromValue(value: unknown): number {
  if (value === undefined || value === null || value === '') return 0
  const numeric = Number(value)
  if (Number.isFinite(numeric)) return Math.trunc(numeric > 1e11 ? numeric / 1000 : numeric)
  const parsed = Date.parse(String(value))
  return Number.isFinite(parsed) ? Math.trunc(parsed / 1000) : 0
}

function buildSyntheticCodexIdToken(
  email: string | undefined,
  accountId: string | undefined,
  planType: string | undefined,
  userId: string | undefined,
  expiresAt: string | undefined,
): string | undefined {
  if (!accountId) return undefined
  const now = Math.trunc(Date.now() / 1000)
  const authInfo: JsonObject = { chatgpt_account_id: accountId }
  const expires = epochSecondsFromValue(expiresAt) || now + 90 * 24 * 60 * 60
  if (planType) authInfo.chatgpt_plan_type = planType
  if (userId) {
    authInfo.chatgpt_user_id = userId
    authInfo.user_id = userId
  }
  const payload: JsonObject = {
    iat: now,
    exp: expires,
    'https://api.openai.com/auth': authInfo,
  }
  if (email) payload.email = email
  return `${encodeBase64UrlJson({ alg: 'none', typ: 'JWT', cpa_synthetic: true })}.${encodeBase64UrlJson(payload)}.`
}

function getExpiresIn(expiresAt: string | undefined, now = new Date()): number | undefined {
  if (!expiresAt) return undefined
  const expiresMs = new Date(expiresAt).getTime()
  if (Number.isNaN(expiresMs)) return undefined
  return Math.max(0, Math.floor((expiresMs - now.getTime()) / 1000))
}

function stripUnavailable(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(stripUnavailable).filter((item) => item !== undefined)
  }
  if (isPlainObject(value)) {
    const entries = Object.entries(value)
      .map(([key, item]) => [key, stripUnavailable(item)] as const)
      .filter(([, item]) => item !== undefined)
    return entries.length ? Object.fromEntries(entries) : undefined
  }
  if (value === undefined || value === null || value === '') return undefined
  return value
}

function toEmailKey(email: string | undefined): string | undefined {
  if (typeof email !== 'string') return undefined
  return email
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '')
}

function nestedAccountId(record: JsonObject): string | undefined {
  const accounts = record.accounts
  if (!isPlainObject(accounts)) return undefined
  const def = (accounts.default || Object.values(accounts)[0]) as unknown
  if (!isPlainObject(def)) return undefined
  const acc = def.account
  return firstNonEmpty(
    isPlainObject(acc) ? acc.account_id : undefined,
    isPlainObject(acc) ? acc.id : undefined,
    def.account_id,
    def.id,
  )
}

function nestedPlanType(record: JsonObject): string | undefined {
  const accounts = record.accounts
  if (!isPlainObject(accounts)) return undefined
  const def = (accounts.default || Object.values(accounts)[0]) as unknown
  if (!isPlainObject(def)) return undefined
  const acc = isPlainObject(def.account) ? def.account : undefined
  const ent = isPlainObject(def.entitlement) ? def.entitlement : undefined
  return pickPlanType(
    acc?.plan_type,
    acc?.planType,
    def.plan_type,
    def.planType,
    ent?.subscription_plan,
    ent?.plan_type,
    ent?.has_active_subscription === true ? 'plus' : undefined,
  )
}

/** Session 的 account.planType 经常仍是 free；付费信号优先，并把 ChatGPT 套餐名收成 plus/pro。 */
function normalizePlanName(value: unknown): string | undefined {
  if (typeof value !== 'string' || !value.trim()) return undefined
  const raw = value.trim()
  const key = raw.toLowerCase().replace(/[\s_-]+/g, '')
  if (key === 'free' || key === 'chatgptfreeplan' || key === 'unknown') return 'free'
  if (key.includes('20x') || key.includes('pro20')) return raw
  if (key.includes('chatgptpro') || key === 'pro') return 'pro'
  if (key.includes('chatgptplus') || key === 'plus') return 'plus'
  if (key.includes('team')) return 'team'
  if (key.includes('business') || key.includes('enterprise')) return raw
  return raw
}

function pickPlanType(...candidates: unknown[]): string | undefined {
  const values = candidates.map(normalizePlanName).filter((v): v is string => Boolean(v))
  return values.find((v) => v !== 'free') || values[0]
}

function accessTokenOf(item: JsonObject): string | undefined {
  return firstNonEmpty(
    item.accessToken,
    item.access_token,
    isPlainObject(item.token) ? item.token.accessToken : undefined,
    isPlainObject(item.token) ? item.token.access_token : undefined,
    isPlainObject(item.credentials) ? item.credentials.access_token : undefined,
    isPlainObject(item.credentials) ? item.credentials.accessToken : undefined,
  )
}

function sessionTokenOf(item: JsonObject): string | undefined {
  return firstNonEmpty(
    item.sessionToken,
    item.session_token,
    isPlainObject(item.token) ? item.token.sessionToken : undefined,
    isPlainObject(item.token) ? item.token.session_token : undefined,
    isPlainObject(item.credentials) ? item.credentials.session_token : undefined,
    isPlainObject(item.credentials) ? item.credentials.sessionToken : undefined,
  )
}

function isSessionLike(item: JsonObject): boolean {
  return Boolean(accessTokenOf(item))
}

function isInspectLike(item: JsonObject): boolean {
  return Boolean(accessTokenOf(item) || sessionTokenOf(item))
}

export function collectSessionLikeObjects(
  value: unknown,
  sourceName = 'pasted-json',
  match: (item: JsonObject) => boolean = isSessionLike,
): SessionSource[] {
  const found: SessionSource[] = []
  const visited = new WeakSet<object>()

  function visit(item: unknown, path: string) {
    if (Array.isArray(item)) {
      item.forEach((child, index) => visit(child, `${path}[${index}]`))
      return
    }
    if (!isPlainObject(item)) return
    if (visited.has(item)) return
    visited.add(item)
    if (match(item)) {
      found.push({ value: item, sourceName, path })
      return
    }
    for (const [key, child] of Object.entries(item)) {
      if (key === 'accessToken' || key === 'access_token' || key === 'sessionToken') continue
      visit(child, `${path}.${key}`)
    }
  }

  visit(value, '$')
  return found
}

function stripCodeFence(text: string): string {
  const t = text.trim()
  const m = t.match(/^```(?:json)?\s*([\s\S]*?)\s*```$/i)
  return m ? m[1].trim() : t
}

function extractJsonChunks(text: string): string[] {
  const chunks: string[] = []
  let i = 0
  const n = text.length
  while (i < n) {
    while (i < n && text[i] !== '{' && text[i] !== '[') i++
    if (i >= n) break
    const open = text[i]
    const close = open === '{' ? '}' : ']'
    let depth = 0
    let inStr = false
    let escape = false
    let j = i
    for (; j < n; j++) {
      const ch = text[j]
      if (inStr) {
        if (escape) {
          escape = false
          continue
        }
        if (ch === '\\') {
          escape = true
          continue
        }
        if (ch === '"') inStr = false
        continue
      }
      if (ch === '"') {
        inStr = true
        continue
      }
      if (ch === open) depth++
      else if (ch === close) {
        depth--
        if (depth === 0) {
          chunks.push(text.slice(i, j + 1))
          i = j + 1
          break
        }
      }
    }
    if (j >= n) break
  }
  return chunks
}

function parseMaybeJson(text: string): unknown | undefined {
  try {
    return JSON.parse(text)
  } catch {
    return undefined
  }
}

function parseSources(
  text: string,
  sourceName: string,
  match: (item: JsonObject) => boolean,
): SessionSource[] {
  const raw = stripCodeFence(text)
  if (!raw) return []

  const found: SessionSource[] = []
  const seen = new WeakSet<object>()

  function add(sources: SessionSource[]) {
    for (const src of sources) {
      if (seen.has(src.value)) continue
      seen.add(src.value)
      found.push(src)
    }
  }

  const whole = parseMaybeJson(raw)
  if (whole !== undefined) add(collectSessionLikeObjects(whole, sourceName, match))

  if (!found.length) {
    for (const chunk of extractJsonChunks(raw)) {
      const parsed = parseMaybeJson(chunk)
      if (parsed !== undefined) add(collectSessionLikeObjects(parsed, sourceName, match))
    }
  }

  if (!found.length) {
    raw.split(/\r?\n/).forEach((line, index) => {
      const trimmed = line.trim()
      if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return
      const parsed = parseMaybeJson(trimmed)
      if (parsed !== undefined) {
        add(
          collectSessionLikeObjects(parsed, sourceName, match).map((src) => ({
            ...src,
            path: `$line[${index + 1}]${src.path === '$' ? '' : src.path.slice(1)}`,
          })),
        )
      }
    })
  }

  return found
}

export function parseSessionSources(text: string, sourceName = 'pasted-json'): SessionSource[] {
  return parseSources(text, sourceName, isSessionLike)
}

export function convertSession(
  record: unknown,
  options: { now?: Date; sourceName?: string; sourcePath?: string } = {},
): ConvertedAccount {
  if (!isPlainObject(record)) throw new Error('session 不是 JSON 对象')

  const accessToken = accessTokenOf(record)
  if (!accessToken) throw new Error('缺少 accessToken')

  const token = isPlainObject(record.token) ? record.token : undefined
  const credentials = isPlainObject(record.credentials) ? record.credentials : undefined
  const user = isPlainObject(record.user) ? record.user : undefined
  const account = isPlainObject(record.account) ? record.account : undefined

  const sessionToken = sessionTokenOf(record)
  const refreshToken = firstNonEmpty(
    record.refreshToken,
    record.refresh_token,
    token?.refreshToken,
    token?.refresh_token,
    credentials?.refresh_token,
  )
  const inputIdToken = firstNonEmpty(
    record.idToken,
    record.id_token,
    token?.idToken,
    token?.id_token,
    credentials?.id_token,
  )

  const payload = parseJwtPayload(accessToken)
  const idPayload = parseJwtPayload(inputIdToken)
  const auth = getOpenAIAuthSection(payload)
  const idAuth = getOpenAIAuthSection(idPayload)
  const profile = getOpenAIProfileSection(payload)
  const expiresAt = firstNonEmpty(
    payload ? timestampFromUnixSeconds(payload.exp) : undefined,
    normalizeTimestamp(record.expires),
    normalizeTimestamp(record.expired),
    normalizeTimestamp(record.expires_at),
    normalizeTimestamp(credentials?.expires_at),
  )
  const email = firstNonEmpty(
    user?.email,
    record.email,
    credentials?.email,
    isPlainObject(record.extra) ? record.extra.email : undefined,
    profile.email,
    idPayload?.email,
    payload?.email,
  )
  const accountId = firstNonEmpty(
    account?.id,
    account?.account_id,
    record.account_id,
    credentials?.chatgpt_account_id,
    auth.chatgpt_account_id,
    idAuth.chatgpt_account_id,
    nestedAccountId(record),
  )
  const userId = firstNonEmpty(
    user?.id,
    record.user_id,
    credentials?.chatgpt_user_id,
    auth.chatgpt_user_id,
    auth.user_id,
    idAuth.chatgpt_user_id,
    idAuth.user_id,
  )
  const planType = pickPlanType(
    auth.chatgpt_plan_type,
    idAuth.chatgpt_plan_type,
    credentials?.plan_type,
    nestedPlanType(record),
    account?.planType,
    account?.plan_type,
    record.plan_type,
  )
  const now = options.now || new Date()
  const exportedAt = normalizeTimestamp(now)
  const expiresIn = getExpiresIn(expiresAt, now)
  const sourceName = firstNonEmpty(options.sourceName, 'pasted-json') || 'pasted-json'
  const name = firstNonEmpty(email, sourceName, 'ChatGPT Account') || 'ChatGPT Account'
  const syntheticIdToken = !inputIdToken
    ? buildSyntheticCodexIdToken(email, accountId, planType, userId, expiresAt)
    : undefined
  const idToken = firstNonEmpty(inputIdToken, syntheticIdToken)

  const cpa = stripUnavailable({
    type: 'codex',
    account_id: accountId,
    chatgpt_account_id: accountId,
    email,
    name,
    plan_type: planType,
    chatgpt_plan_type: planType,
    id_token: idToken,
    id_token_synthetic: Boolean(syntheticIdToken) || undefined,
    access_token: accessToken,
    refresh_token: refreshToken,
    session_token: sessionToken,
    last_refresh: exportedAt,
    expired: expiresAt,
    disabled: Boolean(record.disabled) || undefined,
  })
  const sub2apiAccount = stripUnavailable({
    name: firstNonEmpty(name, email, sourceName, 'ChatGPT Account'),
    platform: 'openai',
    type: 'oauth',
    concurrency: 10,
    priority: 1,
    credentials: {
      access_token: accessToken,
      chatgpt_account_id: accountId,
      chatgpt_user_id: userId,
      email,
      expires_at: expiresAt,
      expires_in: expiresIn,
      plan_type: planType,
    },
    extra: {
      email,
      email_key: toEmailKey(email),
      name,
      auth_provider: firstNonEmpty(record.authProvider, record.auth_provider),
      source: 'chatgpt_web_session',
      last_refresh: exportedAt,
    },
  })

  const cockpit: JsonObject = {
    type: 'codex',
    id_token: idToken,
    access_token: accessToken,
    refresh_token: refreshToken || '',
    account_id: accountId,
    last_refresh: exportedAt,
    email,
    expired: expiresAt,
    account_note: firstNonEmpty(
      record.account_note,
      record.accountInfo,
      record.account_info,
      record.note,
      record.notes,
      record.remark,
    ),
  }

  if (!isPlainObject(cpa) || !isPlainObject(sub2apiAccount)) {
    throw new Error('无法生成导出对象')
  }

  return {
    sourceName,
    sourcePath: options.sourcePath || '$',
    email: email || '',
    name,
    planType,
    expiresAt,
    cpa,
    cockpit,
    sub2apiAccount,
  }
}

function convertSources(sources: SessionSource[], now: Date): ConvertResult {
  const converted: ConvertedAccount[] = []
  const skipped: ConvertIssue[] = []
  const limited = sources.slice(0, CONVERT_MAX)
  if (sources.length > CONVERT_MAX) {
    skipped.push({
      sourceName: 'batch',
      path: '$',
      reason: `超过上限 ${CONVERT_MAX} 条，已截断`,
    })
  }
  limited.forEach((item, index) => {
    try {
      converted.push(
        convertSession(item.value, {
          now,
          sourceName: item.sourceName,
          sourcePath: item.path || `$[${index}]`,
        }),
      )
    } catch (error) {
      skipped.push({
        sourceName: item.sourceName,
        path: item.path,
        reason: error instanceof Error ? error.message : '无法转换',
        sessionRaw: JSON.stringify(item.value),
        canRefresh: Boolean(sessionTokenOf(item.value)),
      })
    }
  })
  return { converted, skipped }
}

export function inspectSessionsFromText(text: string, sourceName = 'pasted-json'): SessionInspectRow[] {
  const raw = stripCodeFence(text)
  if (!raw) return []

  const compact = raw.replace(/\s+/g, '')
  if (!raw.startsWith('{') && !raw.startsWith('[') && compact.split('.').length >= 5) {
    return [
      {
        sourceName,
        sourcePath: '$',
        email: '',
        hasAccessToken: false,
        hasSessionToken: true,
        accessExpired: false,
        sessionRaw: raw,
        issues: ['仅有 sessionToken，可尝试刷新 accessToken'],
      },
    ]
  }

  return parseSources(raw, sourceName, isInspectLike).map((src) => inspectSessionRecord(src))
}

function inspectSessionRecord(src: SessionSource): SessionInspectRow {
  const record = src.value
  const accessToken = accessTokenOf(record)
  const sessionToken = sessionTokenOf(record)
  const payload = parseJwtPayload(accessToken)
  const auth = getOpenAIAuthSection(payload)
  const profile = getOpenAIProfileSection(payload)
  const user = isPlainObject(record.user) ? record.user : undefined
  const account = isPlainObject(record.account) ? record.account : undefined
  const email =
    firstNonEmpty(user?.email, record.email, profile.email, payload?.email) || ''
  const planType = pickPlanType(
    auth.chatgpt_plan_type,
    nestedPlanType(record),
    account?.planType,
    account?.plan_type,
    record.plan_type,
  )
  const accountId = firstNonEmpty(
    account?.id,
    account?.account_id,
    record.account_id,
    auth.chatgpt_account_id,
    nestedAccountId(record),
  )
  const accessExpiresAt = firstNonEmpty(
    payload ? timestampFromUnixSeconds(payload.exp) : undefined,
    normalizeTimestamp(record.expires),
  )
  const accessExpired = Boolean(
    accessExpiresAt && new Date(accessExpiresAt).getTime() <= Date.now(),
  )
  const issues: string[] = []
  if (!accessToken) issues.push('缺少 accessToken')
  if (!sessionToken) issues.push('缺少 sessionToken，无法刷新')
  if (accessExpired) issues.push('accessToken 已过期')
  if (!email) issues.push('未解析到邮箱')
  return {
    sourceName: src.sourceName,
    sourcePath: src.path,
    email,
    planType,
    accountId,
    hasAccessToken: Boolean(accessToken),
    hasSessionToken: Boolean(sessionToken),
    accessExpiresAt,
    accessExpired,
    sessionRaw: JSON.stringify(record),
    issues,
  }
}

export interface SessionEntry {
  key: string
  email: string
  raw: string
}

export function sessionEntryKey(raw: string, email = ''): string {
  const text = String(raw || '').trim()
  try {
    const parsed = JSON.parse(text) as unknown
    if (isPlainObject(parsed)) {
      const at = accessTokenOf(parsed)
      const st = sessionTokenOf(parsed)
      if (at) return `at:${at.slice(-32)}`
      if (st) return `st:${st.slice(-32)}`
    }
  } catch {
    /* bare token */
  }
  if (email.trim()) return `em:${email.trim().toLowerCase()}`
  return `raw:${text.slice(-48)}`
}

export function collectSessionEntries(text: string): SessionEntry[] {
  return inspectSessionsFromText(text).map((row) => ({
    key: sessionEntryKey(row.sessionRaw, row.email),
    email: row.email,
    raw: row.sessionRaw,
  }))
}

export function convertSessionsFromText(text: string, sourceName = 'pasted-json', now = new Date()): ConvertResult {
  const trimmed = stripCodeFence(text)
  if (!trimmed) return { converted: [], skipped: [] }
  let sources = parseSessionSources(trimmed, sourceName)
  if (!sources.length) sources = parseSources(trimmed, sourceName, isInspectLike)
  const result = convertSources(sources, now)
  if (!sources.length) {
    result.skipped.push({
      sourceName,
      path: '$',
      reason: '未找到包含 accessToken 的 session 对象',
      sessionRaw: trimmed,
      canRefresh: sessionRawCanRefresh(trimmed),
    })
  }
  return result
}

export function convertImportedSessions(sessions: ImportedSession[], now = new Date()): ConvertResult {
  if (!sessions.length) return { converted: [], skipped: [] }
  const sources: SessionSource[] = []
  const skipped: ConvertIssue[] = []

  sessions.forEach((row, index) => {
    const path = row.source || `$import[${index}]`
    const raw = String(row.session || '').trim()
    if (raw.startsWith('{') || raw.startsWith('[')) {
      const parsed = parseMaybeJson(raw)
      if (parsed !== undefined) {
        const found = collectSessionLikeObjects(parsed, path, isInspectLike)
        if (found.length) {
          sources.push(
            ...found.map((src) => ({
              ...src,
              value: enrichImportedRecord(src.value, row),
            })),
          )
          return
        }
        if (isPlainObject(parsed)) {
          sources.push({
            value: enrichImportedRecord(parsed, row),
            sourceName: path,
            path,
          })
          return
        }
      }
    }
    if (row.accessToken) {
      sources.push({
        value: enrichImportedRecord({}, row),
        sourceName: path,
        path,
      })
      return
    }
    skipped.push({
      sourceName: path,
      path,
      reason: '无法解析为 Session JSON（需要 accessToken）',
      sessionRaw: raw || undefined,
      canRefresh: sessionRawCanRefresh(raw || row.session),
    })
  })

  const converted = convertSources(sources, now)
  return { converted: converted.converted, skipped: [...skipped, ...converted.skipped] }
}

function enrichImportedRecord(record: JsonObject, row: ImportedSession): JsonObject {
  const next: JsonObject = { ...record }
  if (!firstNonEmpty(next.email, isPlainObject(next.user) ? next.user.email : undefined) && row.email) {
    next.email = row.email
  }
  if (!accessTokenOf(next) && row.accessToken) next.accessToken = row.accessToken
  if (!firstNonEmpty(next.sessionToken, next.session_token) && row.session && !row.session.trim().startsWith('{')) {
    next.sessionToken = row.session
  }
  if (row.accountId && !firstNonEmpty(isPlainObject(next.account) ? next.account.id : undefined, next.account_id)) {
    next.account = isPlainObject(next.account) ? { ...next.account, id: next.account.id || row.accountId } : { id: row.accountId }
  }
  return next
}

export function mergeConvertResults(...parts: ConvertResult[]): ConvertResult {
  const seen = new Set<string>()
  const converted: ConvertedAccount[] = []
  const skipped: ConvertIssue[] = []
  for (const part of parts) {
    for (const item of part.converted) {
      const key = String(item.cpa.access_token || item.email || item.sourcePath)
      if (key && seen.has(key)) continue
      if (key) seen.add(key)
      converted.push(item)
    }
    skipped.push(...part.skipped)
  }
  return { converted, skipped }
}

export function buildExportDocument(
  converted: ConvertedAccount[],
  format: ExportFormat,
  now = new Date(),
): unknown {
  if (format === 'cpa') {
    return converted.length === 1 ? converted[0].cpa : converted.map((item) => item.cpa)
  }
  if (format === 'cockpit') {
    return converted.length === 1 ? converted[0].cockpit : converted.map((item) => item.cockpit)
  }
  return {
    exported_at: normalizeTimestamp(now),
    proxies: [],
    accounts: converted.map((item) => item.sub2apiAccount),
  }
}

export function exportFilename(format: ExportFormat, count: number, now = new Date()): string {
  const pad = (value: number) => String(value).padStart(2, '0')
  const stamp = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}_${pad(now.getHours())}-${pad(now.getMinutes())}-${pad(now.getSeconds())}`
  const n = Math.max(1, count)
  return `${format}_${n}account${n === 1 ? '' : 's'}_${stamp}.json`
}

export function sessionRawCanRefresh(raw: string): boolean {
  const t = String(raw || '').trim()
  if (!t) return false
  if (!t.startsWith('{') && !t.startsWith('[') && t.split('.').length >= 5) return true
  try {
    const parsed = JSON.parse(t) as unknown
    if (isPlainObject(parsed)) return Boolean(sessionTokenOf(parsed))
    if (Array.isArray(parsed)) return parsed.some((item) => isPlainObject(item) && sessionTokenOf(item))
  } catch {
    /* ignore */
  }
  return false
}

export function isTimestampExpired(value: string | undefined): boolean {
  if (!value) return false
  const ms = new Date(value).getTime()
  return Number.isFinite(ms) && ms <= Date.now()
}

export function convertedRefreshRaw(row: ConvertedAccount): string | undefined {
  const sessionToken = firstNonEmpty(row.cpa.session_token, row.cpa.sessionToken)
  if (!sessionToken) return undefined
  return JSON.stringify({
    email: row.email,
    accessToken: row.cpa.access_token,
    sessionToken,
  })
}

export function formatDisplayDate(value: string | undefined): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const pad = (item: number) => String(item).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}
