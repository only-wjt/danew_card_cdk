<template>
  <div class="min-h-screen py-12">
    <div class="max-w-3xl mx-auto px-6 space-y-6">
      <div class="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-4">
        <div>
          <router-link to="/" class="app-link mb-4 inline-block text-sm">{{ t('common.backHome') }}</router-link>
          <h1 class="text-3xl font-bold text-ink">{{ t('inspect.title') }}</h1>
          <p class="text-sm text-muted mt-1">{{ t('inspect.subtitle') }}</p>
        </div>
        <div class="flex gap-2">
          <LanguageToggle />
          <ThemeToggle />
        </div>
      </div>

      <div class="card space-y-4">
        <div class="rounded-xl bg-soft p-4 text-sm text-muted">
          {{ t('inspect.hintPrefix') }}
          <a class="app-link underline" href="https://chatgpt.com/api/auth/session" target="_blank" rel="noopener">
            chatgpt.com/api/auth/session
          </a>
          {{ t('inspect.hintSuffix') }}
        </div>
        <div class="form-group">
          <label>{{ t('inspect.pasteLabel') }}</label>
          <textarea
            v-model="pasteText"
            class="input h-40 font-mono text-xs"
            :placeholder="t('inspect.pastePlaceholder')"
          />
          <div class="mt-2 flex flex-wrap items-center gap-2">
            <button type="button" class="btn-secondary !py-1.5 text-sm" :disabled="!pasteText.trim()" @click="commitPaste(true)">
              {{ t('convert.addToList') }}
            </button>
            <span v-if="pasteHint" class="text-xs text-muted">{{ pasteHint }}</span>
          </div>
        </div>
        <div v-if="accepted.length" class="rounded-xl bg-soft p-3 space-y-2">
          <div class="flex items-center justify-between gap-2">
            <div class="text-sm font-medium text-ink">{{ t('convert.listTitle', { n: accepted.length }) }}</div>
            <button type="button" class="text-xs hover:underline" style="color: var(--err, #dc2626)" @click="clearAccepted">
              {{ t('convert.clearList') }}
            </button>
          </div>
          <div v-for="item in accepted" :key="item.key" class="flex items-center justify-between gap-2 text-xs font-mono text-muted">
            <span class="truncate">{{ item.email || t('convert.noEmail') }}</span>
            <button type="button" class="app-link shrink-0" @click="removeAccepted(item.key)">{{ t('convert.remove') }}</button>
          </div>
        </div>
        <p class="text-xs text-subtle">{{ t('inspect.notice') }}</p>
      </div>

      <div v-if="pasteText.trim() && !rows.length && !accepted.length" class="alert alert-error">
        {{ t('inspect.noSessions') }}
      </div>

      <div v-if="rows.length" class="card space-y-4">
        <div class="space-y-2">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div class="flex flex-wrap items-center gap-2">
              <h2 class="text-xl font-semibold text-ink">{{ t('inspect.previewTitle') }}</h2>
              <span class="rounded-full px-2.5 py-0.5 text-xs" :style="pillStyle('neutral')">
                {{ t('inspect.okCount', { n: rows.length }) }}
              </span>
              <span v-if="passCount" class="rounded-full px-2.5 py-0.5 text-xs" :style="pillStyle('ok')">
                {{ t('inspect.passCount', { n: passCount }) }}
              </span>
              <span v-if="failCount" class="rounded-full px-2.5 py-0.5 text-xs" :style="pillStyle('err')">
                {{ t('inspect.failCount', { n: failCount }) }}
              </span>
            </div>
            <div class="flex flex-wrap items-center gap-2">
              <button v-if="passCount" type="button" class="btn-secondary !py-1.5 text-sm" @click="copyPassed">
                {{ copiedPassed ? t('inspect.copied') : t('inspect.copyPassed', { n: passCount }) }}
              </button>
              <button v-if="refreshedCount" type="button" class="btn-secondary !py-1.5 text-sm" @click="copyAllRefreshed">
                {{ copiedAll ? t('inspect.copied') : t('inspect.copyAll') }}
              </button>
              <button
                v-if="failCount"
                type="button"
                class="btn-secondary !py-1.5 text-sm"
                style="color: var(--err, #dc2626)"
                :disabled="Boolean(pending)"
                @click="removeFailed"
              >
                {{ t('inspect.removeFailed', { n: failCount }) }}
              </button>
            </div>
          </div>
          <p class="text-xs text-subtle">{{ pending ? t('inspect.oneAtATimeBusy') : t('inspect.oneAtATime') }}</p>
        </div>

        <div v-if="pageError" class="alert alert-error">{{ pageError }}</div>

        <div class="space-y-3">
          <div
            v-for="{ row, key } in items"
            :key="key"
            data-session-row
            class="rounded-xl border p-4 space-y-3"
            :style="{ borderColor: cardBorder(key) }"
          >
            <div class="flex flex-wrap items-start justify-between gap-2">
              <div class="min-w-0">
                <div class="font-mono text-sm text-ink truncate" :title="row.accountId || ''">
                  {{ row.email || t('convert.noEmail') }}
                </div>
                <div class="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted">
                  <span>{{ t('inspect.plan') }}：{{ row.planType ? planLabel(row.planType) : '—' }}</span>
                  <span v-if="row.hasAccessToken">
                    {{ t('inspect.expires') }}：{{ formatDisplayDate(row.accessExpiresAt) }}
                    <span v-if="row.accessExpired">（{{ t('inspect.expired') }}）</span>
                  </span>
                  <span v-else>{{ t('inspect.missingAT') }}</span>
                  <span :style="row.hasSessionToken ? undefined : { color: 'var(--err, #dc2626)' }">
                    {{ row.hasSessionToken ? t('inspect.hasST') : t('inspect.missingST') }}
                  </span>
                </div>
              </div>
              <div class="flex flex-wrap gap-1.5 text-xs">
                <span v-if="status[key] === 'ok'" class="rounded-full px-2.5 py-0.5" :style="pillStyle('neutral')">
                  {{ t('inspect.refreshed') }}
                </span>
                <span class="rounded-full px-2.5 py-0.5 font-medium" :style="pillStyle(rowTone(key))">
                  {{ rowStatusText(key) }}
                </span>
              </div>
            </div>

            <p v-if="rowHint(row, key)" class="text-xs" :style="rowHint(row, key)!.style">{{ rowHint(row, key)!.text }}</p>

            <div v-if="verdict[key]?.valid" class="rounded-lg px-3 py-2 text-xs space-y-1" :style="boxStyle('ok')">
              <div class="flex flex-wrap gap-x-4 gap-y-1">
                <span>{{ t('inspect.checkEmail') }}：<b class="font-mono">{{ verdict[key]!.email || '—' }}</b></span>
                <span>{{ t('inspect.currentPlan') }}：<b>{{ verdict[key]!.current_plan ? planLabel(verdict[key]!.current_plan!) : '—' }}</b></span>
              </div>
              <div class="opacity-80">{{ t('inspect.checkPassNote') }}</div>
            </div>
            <div
              v-else-if="verdict[key]"
              class="rounded-lg px-3 py-2 text-xs"
              :style="boxStyle(verdict[key]!.error ? 'warn' : 'err')"
            >
              {{ verdict[key]!.error || verdict[key]!.message || t('inspect.checkFailGeneric') }}
            </div>

            <div
              v-if="billing[key]?.summary"
              class="rounded-lg bg-soft px-3 py-2 text-xs text-muted flex flex-wrap gap-x-4 gap-y-1"
            >
              <span>{{ t('inspect.subscription') }}：<b class="text-ink">{{ subscriptionLabel(billing[key]!.summary) }}</b></span>
              <span>{{ billing[key]!.summary?.has_active_subscription ? t('inspect.billingActive') : t('inspect.billingInactive') }}</span>
              <span>{{ t('inspect.billingUntil') }}：{{ billing[key]!.summary?.expires_at || billing[key]!.summary?.renews_at || '—' }}</span>
              <span v-if="billing[key]!.summary?.will_renew != null">
                {{ billing[key]!.summary?.will_renew ? t('inspect.willRenew') : t('inspect.wontRenew') }}
              </span>
            </div>
            <div v-else-if="billing[key]?.error" class="rounded-lg px-3 py-2 text-xs" :style="boxStyle('warn')">
              {{ billing[key]!.error }}
            </div>

            <p v-if="errors[key]" class="text-xs" style="color: var(--err, #dc2626)">{{ errors[key] }}</p>

            <div class="flex flex-wrap gap-2">
              <button
                type="button"
                class="btn-primary !py-1 text-xs"
                :disabled="!row.hasSessionToken || Boolean(pending)"
                @click="checkOne(key)"
              >
                {{ isPending(key, 'check') ? t('inspect.checking') : t('inspect.check') }}
              </button>
              <button
                type="button"
                class="btn-secondary !py-1 text-xs"
                :disabled="!canCheckBilling(row) || Boolean(pending)"
                @click="checkOneBilling(key)"
              >
                {{ isPending(key, 'billing') ? t('inspect.checkingBilling') : t('inspect.checkBilling') }}
              </button>
              <button
                type="button"
                class="btn-secondary !py-1 text-xs"
                :disabled="!row.hasSessionToken || Boolean(pending)"
                @click="refreshOne(key)"
              >
                {{ isPending(key, 'refresh') ? t('inspect.refreshing') : t('inspect.refresh') }}
              </button>
              <button type="button" class="btn-secondary !py-1 text-xs" @click="copyText(sessionText(key), key)">
                {{ copied[key] ? t('inspect.copied') : t('inspect.copySession') }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import LanguageToggle from '../../components/LanguageToggle.vue'
import ThemeToggle from '../../components/ThemeToggle.vue'
import { planLabel } from '../../lib/plan'
import {
  checkSessionBilling,
  precheckSession,
  refreshSessions,
  type BillingSummary,
  type PrecheckResult,
} from '../../lib/session-api'
import {
  collectSessionEntries,
  formatDisplayDate,
  inspectSessionsFromText,
  type SessionEntry,
  type SessionInspectRow,
} from '../../lib/session-export'

const { t } = useI18n({ useScope: 'global' })

// 每行的状态都按行 key 存，不按下标：增删行、再贴一条都不会把别的行的结果对错位置。
// 行 key = 列表条目 key（来自 sessionEntryKey）+ 在原文里的路径，刷新后保持不变。
type RowState<T> = Record<string, T | undefined>
type Action = 'check' | 'billing' | 'refresh'
type Tone = 'ok' | 'err' | 'warn' | 'neutral'

const pasteText = ref('')
const accepted = ref<SessionEntry[]>([])
const pasteHint = ref('')
const rows = ref<SessionInspectRow[]>([])
const refreshed = reactive<RowState<Record<string, unknown>>>({})
const errors = reactive<RowState<string>>({})
const status = reactive<RowState<'ok' | 'fail' | ''>>({})
const copied = reactive<RowState<boolean>>({})
const verdict = reactive<RowState<PrecheckResult>>({})
const billing = reactive<RowState<{ summary?: BillingSummary; error?: string }>>({})
const copiedAll = ref(false)
const copiedPassed = ref(false)
const pageError = ref('')
// 整页同一时间只跑一个请求（校验 / 查订阅 / 刷新都算），后端也按 IP 只放一条校验
const pending = ref<{ key: string; action: Action } | null>(null)

const rowStates: Record<string, unknown>[] = [refreshed, errors, status, copied, verdict, billing]

function rowKey(row: SessionInspectRow) {
  return `${row.sourceName}#${row.sourcePath}`
}

const items = computed(() => rows.value.map((row) => ({ row, key: rowKey(row) })))
const refreshedCount = computed(() => items.value.filter((x) => refreshed[x.key]).length)
const passedKeys = computed(() => items.value.filter((x) => verdict[x.key]?.valid).map((x) => x.key))
const failedKeys = computed(() => items.value.filter((x) => isFailed(x.key)).map((x) => x.key))
const passCount = computed(() => passedKeys.value.length)
const failCount = computed(() => failedKeys.value.length)

// 有结论的未通过；网络错误、限流（有 error）不算，可以再点一次
function isFailed(key: string) {
  const v = verdict[key]
  return Boolean(v && !v.valid && !v.error)
}

// 订阅查询用 accessToken 也能查；校验和兑换一样必须有 sessionToken
function canCheckBilling(row: SessionInspectRow) {
  return row.hasAccessToken || row.hasSessionToken
}

function rowOf(key: string) {
  return items.value.find((x) => x.key === key)?.row
}

function isPending(key: string, action: Action) {
  return pending.value?.key === key && pending.value.action === action
}

async function runExclusive(key: string, action: Action, fn: () => Promise<void>) {
  if (pending.value) return
  pending.value = { key, action }
  pageError.value = ''
  try {
    await fn()
  } finally {
    pending.value = null
  }
}

// 用主题的状态色，深色模式和各皮肤都跟着走
const TONES: Record<Tone, { bg: string; fg: string }> = {
  ok: { bg: 'color-mix(in srgb, var(--good, #2f8f6b) 12%, transparent)', fg: 'var(--good, #2f8f6b)' },
  err: { bg: 'color-mix(in srgb, var(--err, #c0392b) 10%, transparent)', fg: 'var(--err, #c0392b)' },
  warn: { bg: 'color-mix(in srgb, var(--warn, #b87514) 12%, transparent)', fg: 'var(--warn, #b87514)' },
  neutral: { bg: 'var(--primary-soft)', fg: 'var(--primary)' },
}

function pillStyle(tone: Tone) {
  return { background: TONES[tone].bg, color: TONES[tone].fg }
}

function boxStyle(tone: Tone) {
  return { background: TONES[tone].bg, color: TONES[tone].fg }
}

function rowTone(key: string): Tone {
  if (isPending(key, 'check')) return 'neutral'
  const v = verdict[key]
  if (!v) return 'neutral'
  if (v.error) return 'warn'
  return v.valid ? 'ok' : 'err'
}

function rowStatusText(key: string) {
  if (isPending(key, 'check')) return t('inspect.checking')
  const v = verdict[key]
  if (!v) return t('inspect.notChecked')
  if (v.error) return t('inspect.checkRetry')
  return v.valid ? t('inspect.checkPass') : t('inspect.checkFail')
}

function cardBorder(key: string) {
  const v = verdict[key]
  if (v?.valid) return 'color-mix(in srgb, var(--good, #2f8f6b) 35%, var(--brd))'
  if (v && !v.error) return 'color-mix(in srgb, var(--err, #c0392b) 35%, var(--brd))'
  return 'var(--brd)'
}

// 本地解析出的提示按影响分级：没有 sessionToken 才真的用不了；
// AT 过期在有 sessionToken 时不影响校验（会自动续期）；有了校验结论就以结论为准，不再提示。
function rowHint(row: SessionInspectRow, key: string): { text: string; style: Record<string, string> } | null {
  const muted = { color: 'var(--ink-2)' }
  if (!row.hasSessionToken) return { text: t('inspect.hintNoST'), style: { color: 'var(--err, #c0392b)' } }
  const v = verdict[key]
  if (v && !v.error) return null
  if (!row.hasAccessToken) return { text: t('inspect.hintOnlyST'), style: muted }
  if (row.accessExpired) return { text: t('inspect.hintATExpired'), style: muted }
  if (!row.email) return { text: t('inspect.hintNoEmail'), style: muted }
  return null
}

function checkMsgs() {
  return {
    down: t('inspect.backendDown'),
    fail: t('inspect.checkFailGeneric'),
    limited: (secs: number) => t('inspect.checkLimited', { n: secs }),
  }
}

function apiMsgs() {
  return { down: t('inspect.backendDown'), fail: t('inspect.refreshFail') }
}

function billingMsgs() {
  return { down: t('inspect.backendDown'), fail: t('inspect.billingFail') }
}

let timer: ReturnType<typeof setTimeout> | undefined
watch(pasteText, () => {
  clearTimeout(timer)
  timer = setTimeout(() => commitPaste(false), 200)
})

function commitPaste(clearBox: boolean) {
  const entries = collectSessionEntries(pasteText.value)
  if (entries.length) {
    let added = 0
    const next = accepted.value.slice()
    for (const entry of entries) {
      if (next.some((item) => item.key === entry.key)) continue
      next.push(entry)
      added += 1
    }
    accepted.value = next
    pasteHint.value = added ? t('convert.addedMsg', { n: added }) : t('convert.alreadyMsg')
    if (clearBox && added) pasteText.value = ''
  } else {
    pasteHint.value = pasteText.value.trim() ? '' : pasteHint.value
  }
  rebuildRows()
}

function removeAccepted(key: string) {
  accepted.value = accepted.value.filter((item) => item.key !== key)
  rebuildRows()
}

function clearAccepted() {
  accepted.value = []
  pasteHint.value = ''
  rebuildRows()
}

// 未通过的行可能还在输入框里没加入列表：先收进列表并清空输入框，再一起移除，
// 否则输入框里的内容下次一解析又会出现。
function removeFailed() {
  const failed = new Set(failedKeys.value.map((key) => rowOf(key)?.sourceName))
  if (!failed.size) return
  commitPaste(true)
  // 输入框里的条目早就在列表里时 commitPaste 不会清空它，这里补上
  if (collectSessionEntries(pasteText.value).some((e) => failed.has(e.key))) pasteText.value = ''
  accepted.value = accepted.value.filter((item) => !failed.has(item.key))
  rebuildRows()
}

function rebuildRows() {
  const fromList = accepted.value.flatMap((item) => inspectSessionsFromText(item.raw, item.key))
  const seen = new Set(accepted.value.map((a) => a.key))
  const leftover = collectSessionEntries(pasteText.value).filter((e) => {
    if (seen.has(e.key)) return false
    seen.add(e.key)
    return true
  })
  const extra = leftover.flatMap((e) => inspectSessionsFromText(e.raw, e.key))
  rows.value = [...fromList, ...extra]
  // 只清掉已经不在列表里的行，留下来的行结果保留
  const live = new Set(rows.value.map(rowKey))
  for (const state of rowStates) {
    for (const key of Object.keys(state)) {
      if (!live.has(key)) delete state[key]
    }
  }
  pageError.value = ''
}

function checkOne(key: string) {
  const row = rowOf(key)
  if (!row?.hasSessionToken) return
  return runExclusive(key, 'check', async () => {
    const res = await precheckSession(row.sessionRaw, checkMsgs())
    if (rowOf(key)) verdict[key] = res
  })
}

function checkOneBilling(key: string) {
  const row = rowOf(key)
  if (!row || !canCheckBilling(row)) return
  return runExclusive(key, 'billing', async () => {
    const res = await checkSessionBilling(row.sessionRaw, billingMsgs())
    if (rowOf(key)) billing[key] = res
  })
}

function refreshOne(key: string) {
  const row = rowOf(key)
  if (!row?.hasSessionToken) return
  errors[key] = ''
  return runExclusive(key, 'refresh', async () => {
    try {
      const results = await refreshSessions([row.sessionRaw], apiMsgs())
      applyResult(key, results[0])
    } catch (e) {
      errors[key] = e instanceof Error ? e.message : t('inspect.refreshFail')
      status[key] = 'fail'
    }
  })
}

function applyResult(key: string, item: { ok?: boolean; error?: string; session?: Record<string, unknown> } | undefined) {
  const row = rowOf(key)
  if (!row) return
  if (!item?.ok || !item.session) {
    status[key] = 'fail'
    errors[key] = item?.error || t('inspect.refreshFail')
    return
  }
  refreshed[key] = item.session
  status[key] = 'ok'
  errors[key] = ''
  // Session 换了，旧的校验结论不再对应它
  delete verdict[key]
  // 写回列表（还在输入框里的行顺手加入列表），之后再贴新内容重建时不会退回旧 Session。
  // 列表条目 key 不变，行 key 也就不变，刷新标记和查订阅结果都还在。
  const raw = JSON.stringify(item.session)
  const next = inspectSessionsFromText(raw, row.sourceName)[0]
  const email = next?.email || row.email
  const list = accepted.value.slice()
  const at = list.findIndex((a) => a.key === row.sourceName)
  if (at >= 0) list[at] = { ...list[at], raw, email }
  else list.push({ key: row.sourceName, email, raw })
  accepted.value = list
  if (next) {
    rows.value = rows.value.map((r) => (rowKey(r) === key ? { ...next, sourcePath: row.sourcePath } : r))
  }
}

async function writeClipboard(text: string, flag: () => void, unflag: () => void) {
  try {
    await navigator.clipboard.writeText(text)
    flag()
    setTimeout(unflag, 1600)
  } catch {
    pageError.value = t('convert.copyFail')
  }
}

function copyText(text: string, key: string) {
  return writeClipboard(text, () => (copied[key] = true), () => (copied[key] = false))
}

function copyAllRefreshed() {
  const list = items.value
    .map((x) => refreshed[x.key])
    .filter((x): x is Record<string, unknown> => Boolean(x))
  if (!list.length) return
  const payload = list.length === 1 ? list[0] : list
  return writeClipboard(JSON.stringify(payload, null, 2), () => (copiedAll.value = true), () => (copiedAll.value = false))
}

// 优先用刷新后的 Session，没刷新过就用原文
function sessionValue(key: string): unknown {
  if (refreshed[key]) return refreshed[key]
  const raw = rowOf(key)?.sessionRaw || ''
  try {
    return JSON.parse(raw)
  } catch {
    return raw
  }
}

function sessionText(key: string): string {
  const value = sessionValue(key)
  return typeof value === 'string' ? value : JSON.stringify(value, null, 2)
}

function copyPassed() {
  const list = passedKeys.value.map(sessionValue)
  if (!list.length) return
  const payload = JSON.stringify(list.length === 1 ? list[0] : list, null, 2)
  return writeClipboard(payload, () => (copiedPassed.value = true), () => (copiedPassed.value = false))
}

function subscriptionLabel(summary: BillingSummary | undefined): string {
  const raw = String(summary?.plan_type || summary?.subscription_plan || '').trim()
  return raw ? planLabel(raw) : planLabel('free')
}
</script>
