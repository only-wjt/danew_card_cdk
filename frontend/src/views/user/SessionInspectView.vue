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
          <div class="flex flex-wrap gap-2">
            <span class="rounded-full px-2.5 py-1 text-xs" style="background: var(--primary-soft); color: var(--primary)">
              {{ t('inspect.okCount', { n: rows.length }) }}
            </span>
            <button
              type="button"
              class="btn-secondary !py-1.5 text-sm"
              :disabled="checkingAll || !checkableCount"
              @click="checkAllBilling"
            >
              {{ checkingAll ? t('inspect.checkingBilling') : t('inspect.checkAllBilling') }}
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
            v-for="(row, i) in rows"
            :key="i"
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
                  v-if="status[i] === 'ok'"
                  class="rounded-full px-2 py-0.5"
                  style="background: var(--primary-soft); color: var(--primary)"
                >{{ t('inspect.refreshed') }}</span>
              </div>
            </div>
            <div class="grid sm:grid-cols-3 gap-2 text-xs text-muted">
              <div>{{ t('inspect.plan') }}：{{ row.planType || '—' }}</div>
              <div>{{ t('inspect.expires') }}：{{ formatDisplayDate(row.accessExpiresAt) }}</div>
              <div class="truncate">{{ row.accountId || '—' }}</div>
            </div>
            <p v-if="row.issues.length && status[i] !== 'ok'" class="text-xs" style="color: var(--err, #dc2626)">
              {{ row.issues.join(' · ') }}
            </p>
            <p v-if="errors[i]" class="text-xs" style="color: var(--err, #dc2626)">{{ errors[i] }}</p>
            <div v-if="billing[i]?.summary" class="rounded-lg bg-soft px-3 py-2 text-xs text-muted flex flex-wrap gap-x-4 gap-y-1">
              <span>{{ t('inspect.plan') }}：<b class="text-ink">{{ planLabel(billing[i]!.summary) }}</b></span>
              <span>{{ billing[i]!.summary?.has_active_subscription ? t('inspect.billingActive') : t('inspect.billingInactive') }}</span>
              <span>{{ t('inspect.billingUntil') }}：{{ billing[i]!.summary?.expires_at || billing[i]!.summary?.renews_at || '—' }}</span>
            </div>
            <p v-if="billing[i]?.error" class="text-xs" style="color: var(--err, #dc2626)">{{ billing[i]!.error }}</p>
            <div class="flex flex-wrap gap-2">
              <button
                type="button"
                class="btn-secondary !py-1 text-xs"
                :disabled="!canCheck(row) || billingBusy[i]"
                @click="checkOneBilling(i)"
              >
                {{ billingBusy[i] ? t('inspect.checkingBilling') : t('inspect.checkBilling') }}
              </button>
              <button
                type="button"
                class="btn-secondary !py-1 text-xs"
                :disabled="!row.hasSessionToken || busy[i]"
                @click="refreshOne(i)"
              >
                {{ busy[i] ? t('inspect.refreshing') : t('inspect.refresh') }}
              </button>
              <button
                v-if="refreshed[i]"
                type="button"
                class="btn-secondary !py-1 text-xs"
                @click="copyText(JSON.stringify(refreshed[i], null, 2), i)"
              >
                {{ copied[i] ? t('inspect.copied') : t('inspect.copySession') }}
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
import { mapPool } from '../../lib/batch-session'
import {
  checkSessionBilling,
  planLabel,
  refreshSessions,
  SESSION_TOOL_MAX,
  type BillingSummary,
} from '../../lib/session-api'
import {
  collectSessionEntries,
  formatDisplayDate,
  inspectSessionsFromText,
  type SessionEntry,
  type SessionInspectRow,
} from '../../lib/session-export'

const { t } = useI18n({ useScope: 'global' })

const pasteText = ref('')
const accepted = ref<SessionEntry[]>([])
const pasteHint = ref('')
const rows = ref<SessionInspectRow[]>([])
const refreshed = reactive<Record<number, Record<string, unknown> | undefined>>({})
const errors = reactive<Record<number, string>>({})
const status = reactive<Record<number, 'ok' | 'fail' | ''>>({})
const busy = reactive<Record<number, boolean>>({})
const copied = reactive<Record<number, boolean>>({})
const copiedAll = ref(false)
const refreshingAll = ref(false)
const checkingAll = ref(false)
const batchError = ref('')
const billing = reactive<Record<number, { summary?: BillingSummary; error?: string }>>({})
const billingBusy = reactive<Record<number, boolean>>({})

const refreshableCount = computed(() => rows.value.filter((r) => r.hasSessionToken).length)
const checkableCount = computed(() => rows.value.filter(canCheck).length)
const refreshedCount = computed(() => Object.values(refreshed).filter(Boolean).length)

function canCheck(row: SessionInspectRow) {
  return row.hasAccessToken || row.hasSessionToken
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

function rebuildRows() {
  const fromList = accepted.value.flatMap((item) => inspectSessionsFromText(item.raw, item.key))
  const leftover = collectSessionEntries(pasteText.value).filter((e) => !accepted.value.some((a) => a.key === e.key))
  const extra = leftover.flatMap((e) => inspectSessionsFromText(e.raw, e.key))
  rows.value = [...fromList, ...extra]
  for (const key of Object.keys(refreshed)) delete refreshed[Number(key)]
  for (const key of Object.keys(errors)) delete errors[Number(key)]
  for (const key of Object.keys(status)) delete status[Number(key)]
  for (const key of Object.keys(billing)) delete billing[Number(key)]
  for (const key of Object.keys(billingBusy)) delete billingBusy[Number(key)]
  batchError.value = ''
}

function tokenPill(ok: boolean) {
  return ok
    ? { background: 'var(--primary-soft)', color: 'var(--primary)' }
    : { background: 'color-mix(in srgb, var(--err, #dc2626) 12%, transparent)', color: 'var(--err, #dc2626)' }
}

async function refreshOne(index: number) {
  const row = rows.value[index]
  if (!row?.hasSessionToken) return
  busy[index] = true
  errors[index] = ''
  batchError.value = ''
  try {
    const results = await refreshSessions([row.sessionRaw], apiMsgs())
    applyResult(index, results[0])
  } catch (e) {
    errors[index] = e instanceof Error ? e.message : t('inspect.refreshFail')
    status[index] = 'fail'
  } finally {
    busy[index] = false
  }
}

async function refreshAll() {
  const targets = rows.value
    .map((row, index) => ({ row, index }))
    .filter((x) => x.row.hasSessionToken)
    .slice(0, SESSION_TOOL_MAX)
  if (!targets.length) {
    batchError.value = t('inspect.noRefreshable')
    return
  }
  refreshingAll.value = true
  batchError.value = ''
  targets.forEach((x) => {
    busy[x.index] = true
    errors[x.index] = ''
  })
  try {
    const results = await refreshSessions(targets.map((x) => x.row.sessionRaw), apiMsgs())
    targets.forEach((x, i) => applyResult(x.index, results[i]))
  } catch (e) {
    batchError.value = e instanceof Error ? e.message : t('inspect.refreshFail')
  } finally {
    targets.forEach((x) => {
      busy[x.index] = false
    })
    refreshingAll.value = false
  }
}

async function checkOneBilling(index: number) {
  const row = rows.value[index]
  if (!row || !canCheck(row)) return
  billingBusy[index] = true
  batchError.value = ''
  try {
    billing[index] = await checkSessionBilling(row.sessionRaw, {
      down: t('inspect.backendDown'),
      fail: t('inspect.billingFail'),
    })
  } finally {
    billingBusy[index] = false
  }
}

async function checkAllBilling() {
  const targets = rows.value
    .map((row, index) => ({ row, index }))
    .filter((x) => canCheck(x.row))
    .slice(0, SESSION_TOOL_MAX)
  if (!targets.length) {
    batchError.value = t('inspect.noCheckable')
    return
  }
  checkingAll.value = true
  batchError.value = ''
  targets.forEach((x) => {
    billingBusy[x.index] = true
  })
  try {
    await mapPool(targets, 3, async (x) => {
      billing[x.index] = await checkSessionBilling(x.row.sessionRaw, {
        down: t('inspect.backendDown'),
        fail: t('inspect.billingFail'),
      })
    })
  } catch (e) {
    batchError.value = e instanceof Error ? e.message : t('inspect.billingFail')
  } finally {
    targets.forEach((x) => {
      billingBusy[x.index] = false
    })
    checkingAll.value = false
  }
}

function applyResult(index: number, item: { ok?: boolean; error?: string; session?: Record<string, unknown> } | undefined) {
  if (!item?.ok || !item.session) {
    status[index] = 'fail'
    errors[index] = item?.error || t('inspect.refreshFail')
    return
  }
  refreshed[index] = item.session
  status[index] = 'ok'
  errors[index] = ''
  const next = inspectSessionsFromText(JSON.stringify(item.session))[0]
  if (next) {
    const copy = rows.value.slice()
    copy[index] = { ...next, sourcePath: rows.value[index].sourcePath }
    rows.value = copy
  }
}

async function copyText(text: string, index: number) {
  try {
    await navigator.clipboard.writeText(text)
    copied[index] = true
    setTimeout(() => {
      copied[index] = false
    }, 1600)
  } catch {
    batchError.value = t('convert.copyFail')
  }
}

async function copyAllRefreshed() {
  const list = rows.value
    .map((_, i) => refreshed[i])
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
</script>
