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
        <div class="flex flex-wrap items-center justify-between gap-3">
          <h2 class="text-xl font-semibold text-ink">{{ t('inspect.previewTitle') }}</h2>
          <div class="flex flex-wrap items-center gap-2">
            <span class="rounded-full px-2.5 py-1 text-xs" style="background: var(--primary-soft); color: var(--primary)">
              {{ t('inspect.okCount', { n: rows.length }) }}
            </span>
            <span
              v-if="passCount || failCount"
              class="rounded-full px-2.5 py-1 text-xs"
              :style="tokenPill(!failCount)"
            >
              {{ t('inspect.checkSummary', { pass: passCount, fail: failCount }) }}
            </span>
            <button
              type="button"
              class="btn-primary !py-1.5 text-sm"
              :disabled="checkingAll || !checkableCount"
              @click="checkAll"
            >
              {{ checkingAll ? t('inspect.checking') : t('inspect.checkAll') }}
            </button>
            <button
              type="button"
              class="btn-secondary !py-1.5 text-sm"
              :disabled="billingAll || !checkableCount"
              @click="checkAllBilling"
            >
              {{ billingAll ? t('inspect.checkingBilling') : t('inspect.checkAllBilling') }}
            </button>
            <button
              type="button"
              class="btn-secondary !py-1.5 text-sm"
              :disabled="refreshingAll || !refreshableCount"
              @click="refreshAll"
            >
              {{ refreshingAll ? t('inspect.refreshing') : t('inspect.refreshAll') }}
            </button>
            <button
              v-if="passCount"
              type="button"
              class="btn-secondary !py-1.5 text-sm"
              @click="copyPassed"
            >
              {{ copiedPassed ? t('inspect.copied') : t('inspect.copyPassed', { n: passCount }) }}
            </button>
            <button
              v-if="failCount"
              type="button"
              class="btn-secondary !py-1.5 text-sm"
              style="color: var(--err, #dc2626)"
              :disabled="checkingAll"
              @click="removeFailed"
            >
              {{ t('inspect.removeFailed', { n: failCount }) }}
            </button>
            <button
              v-if="refreshedCount"
              type="button"
              class="btn-primary !py-1.5 text-sm"
              @click="copyAllRefreshed"
            >
              {{ copiedAll ? t('inspect.copied') : t('inspect.copyAll') }}
            </button>
          </div>
        </div>

        <div v-if="batchError" class="alert alert-error">{{ batchError }}</div>

        <div class="space-y-3">
          <div
            v-for="{ row, key } in items"
            :key="key"
            class="rounded-xl border p-3 space-y-2"
            style="border-color: var(--brd)"
          >
            <div class="flex flex-wrap items-center justify-between gap-2">
              <div class="font-mono text-xs text-ink">{{ row.email || t('convert.noEmail') }}</div>
              <div class="flex flex-wrap gap-1.5 text-[11px]">
                <span class="rounded-full px-2 py-0.5" :style="tokenPill(row.hasAccessToken)">
                  {{ row.hasAccessToken ? t('inspect.hasAT') : t('inspect.missingAT') }}
                </span>
                <span class="rounded-full px-2 py-0.5" :style="tokenPill(row.hasSessionToken)">
                  {{ row.hasSessionToken ? t('inspect.hasST') : t('inspect.missingST') }}
                </span>
                <span
                  v-if="row.hasAccessToken"
                  class="rounded-full px-2 py-0.5"
                  :style="tokenPill(!row.accessExpired)"
                >
                  {{ row.accessExpired ? t('inspect.expired') : t('inspect.valid') }}
                </span>
                <span
                  v-if="status[key] === 'ok'"
                  class="rounded-full px-2 py-0.5"
                  style="background: var(--primary-soft); color: var(--primary)"
                >{{ t('inspect.refreshed') }}</span>
                <span
                  v-if="verdict[key] && !verdict[key]!.error"
                  class="rounded-full px-2 py-0.5 font-medium"
                  :style="tokenPill(Boolean(verdict[key]!.valid))"
                >{{ verdict[key]!.valid ? t('inspect.checkPass') : t('inspect.checkFail') }}</span>
              </div>
            </div>
            <div class="grid sm:grid-cols-3 gap-2 text-xs text-muted">
              <div>{{ t('inspect.plan') }}：{{ row.planType || '—' }}</div>
              <div>{{ t('inspect.expires') }}：{{ formatDisplayDate(row.accessExpiresAt) }}</div>
              <div class="truncate">{{ row.accountId || '—' }}</div>
            </div>
            <p v-if="row.issues.length && status[key] !== 'ok'" class="text-xs" style="color: var(--err, #dc2626)">
              {{ row.issues.join(' · ') }}
            </p>
            <p v-if="errors[key]" class="text-xs" style="color: var(--err, #dc2626)">{{ errors[key] }}</p>
            <div
              v-if="verdict[key]?.valid"
              class="rounded-lg bg-soft px-3 py-2 text-xs text-muted flex flex-wrap gap-x-4 gap-y-1"
            >
              <span>{{ t('inspect.checkEmail') }}：<b class="text-ink font-mono">{{ verdict[key]!.email || '—' }}</b></span>
              <span>{{ t('inspect.currentPlan') }}：<b class="text-ink">{{ verdict[key]!.current_plan ? planLabel(verdict[key]!.current_plan!) : '—' }}</b></span>
            </div>
            <p v-if="verdict[key]?.valid" class="text-[11px] text-subtle">{{ t('inspect.checkPassNote') }}</p>
            <p v-if="verdict[key] && !verdict[key]!.valid" class="text-xs" style="color: var(--err, #dc2626)">
              {{ verdict[key]!.error || verdict[key]!.message || t('inspect.checkFailGeneric') }}
            </p>
            <div v-if="billing[key]?.summary" class="rounded-lg bg-soft px-3 py-2 text-xs text-muted flex flex-wrap gap-x-4 gap-y-1">
              <span>{{ t('inspect.subscription') }}：<b class="text-ink">{{ subscriptionLabel(billing[key]!.summary) }}</b></span>
              <span>{{ billing[key]!.summary?.has_active_subscription ? t('inspect.billingActive') : t('inspect.billingInactive') }}</span>
              <span>{{ t('inspect.billingUntil') }}：{{ billing[key]!.summary?.expires_at || billing[key]!.summary?.renews_at || '—' }}</span>
              <span v-if="billing[key]!.summary?.will_renew != null">
                {{ billing[key]!.summary?.will_renew ? t('inspect.willRenew') : t('inspect.wontRenew') }}
              </span>
            </div>
            <p v-if="billing[key]?.error" class="text-xs" style="color: var(--err, #dc2626)">{{ billing[key]!.error }}</p>
            <div class="flex flex-wrap gap-2">
              <button
                type="button"
                class="btn-primary !py-1 text-xs"
                :disabled="!canCheck(row) || checkBusy[key] || checkingAll"
                @click="checkOne(key)"
              >
                {{ checkBusy[key] ? t('inspect.checking') : t('inspect.check') }}
              </button>
              <button
                type="button"
                class="btn-secondary !py-1 text-xs"
                :disabled="!canCheck(row) || billingBusy[key] || billingAll"
                @click="checkOneBilling(key)"
              >
                {{ billingBusy[key] ? t('inspect.checkingBilling') : t('inspect.checkBilling') }}
              </button>
              <button
                type="button"
                class="btn-secondary !py-1 text-xs"
                :disabled="!row.hasSessionToken || busy[key]"
                @click="refreshOne(key)"
              >
                {{ busy[key] ? t('inspect.refreshing') : t('inspect.refresh') }}
              </button>
              <button
                type="button"
                class="btn-secondary !py-1 text-xs"
                @click="copyText(sessionText(key), key)"
              >
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
import { mapPool } from '../../lib/batch-session'
import {
  checkSessionBilling,
  precheckSession,
  refreshSessions,
  SESSION_TOOL_MAX,
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

const pasteText = ref('')
const accepted = ref<SessionEntry[]>([])
const pasteHint = ref('')
const rows = ref<SessionInspectRow[]>([])
const refreshed = reactive<RowState<Record<string, unknown>>>({})
const errors = reactive<RowState<string>>({})
const status = reactive<RowState<'ok' | 'fail' | ''>>({})
const busy = reactive<RowState<boolean>>({})
const copied = reactive<RowState<boolean>>({})
const copiedAll = ref(false)
const refreshingAll = ref(false)
const checkingAll = ref(false)
const batchError = ref('')
const verdict = reactive<RowState<PrecheckResult>>({})
const checkBusy = reactive<RowState<boolean>>({})
const billing = reactive<RowState<{ summary?: BillingSummary; error?: string }>>({})
const billingBusy = reactive<RowState<boolean>>({})
const billingAll = ref(false)
const copiedPassed = ref(false)

const rowStates: Record<string, unknown>[] = [
  refreshed, errors, status, busy, copied, verdict, checkBusy, billing, billingBusy,
]

function rowKey(row: SessionInspectRow) {
  return `${row.sourceName}#${row.sourcePath}`
}

const items = computed(() => rows.value.map((row) => ({ row, key: rowKey(row) })))
const refreshableCount = computed(() => rows.value.filter((r) => r.hasSessionToken).length)
const checkableCount = computed(() => rows.value.filter(canCheck).length)
const refreshedCount = computed(() => items.value.filter((x) => refreshed[x.key]).length)
const passedKeys = computed(() => items.value.filter((x) => verdict[x.key]?.valid).map((x) => x.key))
const failedKeys = computed(() =>
  items.value
    .filter((x) => {
      const v = verdict[x.key]
      return v && !v.valid && !v.error
    })
    .map((x) => x.key),
)
const passCount = computed(() => passedKeys.value.length)
const failCount = computed(() => failedKeys.value.length)

// 兑换只收含 sessionToken 的 Session，校验也一样；只有 AT 的行交给后端返回明确原因
function canCheck(row: SessionInspectRow) {
  return row.hasAccessToken || row.hasSessionToken
}

function rowOf(key: string) {
  return items.value.find((x) => x.key === key)?.row
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
  batchError.value = ''
}

function tokenPill(ok: boolean) {
  return ok
    ? { background: 'var(--primary-soft)', color: 'var(--primary)' }
    : { background: 'color-mix(in srgb, var(--err, #dc2626) 12%, transparent)', color: 'var(--err, #dc2626)' }
}

async function refreshOne(key: string) {
  const row = rowOf(key)
  if (!row?.hasSessionToken) return
  busy[key] = true
  errors[key] = ''
  batchError.value = ''
  try {
    const results = await refreshSessions([row.sessionRaw], apiMsgs())
    applyResult(key, results[0])
  } catch (e) {
    errors[key] = e instanceof Error ? e.message : t('inspect.refreshFail')
    status[key] = 'fail'
  } finally {
    busy[key] = false
  }
}

async function refreshAll() {
  const targets = items.value.filter((x) => x.row.hasSessionToken).slice(0, SESSION_TOOL_MAX)
  if (!targets.length) {
    batchError.value = t('inspect.noRefreshable')
    return
  }
  refreshingAll.value = true
  batchError.value = ''
  targets.forEach((x) => {
    busy[x.key] = true
    errors[x.key] = ''
  })
  try {
    const results = await refreshSessions(targets.map((x) => x.row.sessionRaw), apiMsgs())
    targets.forEach((x, i) => applyResult(x.key, results[i]))
  } catch (e) {
    batchError.value = e instanceof Error ? e.message : t('inspect.refreshFail')
  } finally {
    targets.forEach((x) => {
      busy[x.key] = false
    })
    refreshingAll.value = false
  }
}

async function checkOne(key: string) {
  const row = rowOf(key)
  if (!row || !canCheck(row)) return
  checkBusy[key] = true
  batchError.value = ''
  try {
    verdict[key] = await precheckSession(row.sessionRaw, checkMsgs())
  } finally {
    checkBusy[key] = false
  }
}

// 逐条串行：后端按 IP 限流，并发只会更快撞上限。被限流就停下，剩下的不再发。
async function checkAll() {
  const targets = items.value
    .filter((x) => canCheck(x.row) && !verdict[x.key]?.valid)
    .slice(0, SESSION_TOOL_MAX)
  if (!targets.length) {
    batchError.value = rows.value.some(canCheck) ? '' : t('inspect.noCheckable')
    return
  }
  checkingAll.value = true
  batchError.value = ''
  try {
    for (const x of targets) {
      // 校验途中被移除的行跳过
      if (!rowOf(x.key)) continue
      checkBusy[x.key] = true
      const res = await precheckSession(x.row.sessionRaw, checkMsgs())
      checkBusy[x.key] = false
      if (rowOf(x.key)) verdict[x.key] = res
      if (res.retryAfter) {
        batchError.value = t('inspect.checkLimitedBatch', { n: res.retryAfter })
        break
      }
    }
  } finally {
    targets.forEach((x) => {
      checkBusy[x.key] = false
    })
    checkingAll.value = false
  }
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

async function copyText(text: string, key: string) {
  try {
    await navigator.clipboard.writeText(text)
    copied[key] = true
    setTimeout(() => {
      copied[key] = false
    }, 1600)
  } catch {
    batchError.value = t('convert.copyFail')
  }
}

async function copyAllRefreshed() {
  const list = items.value
    .map((x) => refreshed[x.key])
    .filter((x): x is Record<string, unknown> => Boolean(x))
  if (!list.length) return
  const payload = list.length === 1 ? list[0] : list
  try {
    await navigator.clipboard.writeText(JSON.stringify(payload, null, 2))
    copiedAll.value = true
    setTimeout(() => {
      copiedAll.value = false
    }, 1600)
  } catch {
    batchError.value = t('convert.copyFail')
  }
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

async function copyPassed() {
  const list = passedKeys.value.map(sessionValue)
  if (!list.length) return
  try {
    await navigator.clipboard.writeText(JSON.stringify(list.length === 1 ? list[0] : list, null, 2))
    copiedPassed.value = true
    setTimeout(() => {
      copiedPassed.value = false
    }, 1600)
  } catch {
    batchError.value = t('convert.copyFail')
  }
}

function billingMsgs() {
  return { down: t('inspect.backendDown'), fail: t('inspect.billingFail') }
}

function subscriptionLabel(summary: BillingSummary | undefined): string {
  const raw = String(summary?.plan_type || summary?.subscription_plan || '').trim()
  return raw ? planLabel(raw) : planLabel('free')
}

async function checkOneBilling(key: string) {
  const row = rowOf(key)
  if (!row || !canCheck(row)) return
  billingBusy[key] = true
  batchError.value = ''
  try {
    billing[key] = await checkSessionBilling(row.sessionRaw, billingMsgs())
  } finally {
    billingBusy[key] = false
  }
}

async function checkAllBilling() {
  const targets = items.value.filter((x) => canCheck(x.row)).slice(0, SESSION_TOOL_MAX)
  if (!targets.length) {
    batchError.value = t('inspect.noCheckable')
    return
  }
  billingAll.value = true
  batchError.value = ''
  targets.forEach((x) => {
    billingBusy[x.key] = true
  })
  try {
    await mapPool(targets, 3, async (x) => {
      const res = await checkSessionBilling(x.row.sessionRaw, billingMsgs())
      if (rowOf(x.key)) billing[x.key] = res
      billingBusy[x.key] = false
    })
  } finally {
    targets.forEach((x) => {
      billingBusy[x.key] = false
    })
    billingAll.value = false
  }
}
</script>
