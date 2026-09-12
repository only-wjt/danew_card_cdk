<template>
  <div class="min-h-screen py-12">
    <div class="max-w-5xl mx-auto px-6">
      <div class="mb-10 flex flex-col sm:flex-row sm:items-start sm:justify-between gap-4 animate-slideInUp">
        <div>
          <router-link to="/" class="app-link mb-4 inline-block text-sm">{{ t('common.back') }}</router-link>
          <h1 class="text-3xl font-bold text-ink mb-1">{{ t('cdkLookup.title') }}</h1>
          <p class="text-muted">{{ t('cdkLookup.subtitle') }}</p>
        </div>
        <div class="flex items-center gap-3">
          <LanguageToggle />
          <ThemeToggle />
        </div>
      </div>

      <RedeemModeTabs />

      <div class="card animate-slideInUp space-y-5">
        <div class="flex gap-2">
          <button
            type="button"
            class="btn-secondary !py-1.5 flex-1"
            :class="{ 'ring-2': mode === 'single' }"
            @click="setMode('single')"
          >
            {{ t('cdkLookup.modeSingle') }}
          </button>
          <button
            type="button"
            class="btn-secondary !py-1.5 flex-1"
            :class="{ 'ring-2': mode === 'batch' }"
            @click="setMode('batch')"
          >
            {{ t('cdkLookup.modeBatch') }}
          </button>
        </div>

        <template v-if="mode === 'single'">
          <h2 class="text-xl font-bold text-ink">{{ t('cdkLookup.queryTitle') }}</h2>
          <div class="rounded-xl bg-soft p-4 text-sm text-muted">
            {{ t('cdkLookup.queryHint') }}
          </div>
          <div class="form-group">
            <label>{{ t('cdkLookup.cdkLabel') }}</label>
            <input
              v-model="cdkCode"
              type="text"
              :placeholder="t('cdkLookup.cdkPlaceholder')"
              class="input mono"
              @keyup.enter="query"
            />
          </div>
          <button
            @click="query"
            :disabled="!cdkCode.trim() || querying"
            class="btn-primary w-full disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <span v-if="!querying">{{ t('cdkLookup.queryBtn') }}</span>
            <span v-else class="flex items-center justify-center gap-2">
              <span class="spinner"></span>{{ t('common.querying') }}
            </span>
          </button>
        </template>

        <template v-else>
          <h2 class="text-xl font-bold text-ink">{{ t('cdkLookup.modeBatch') }}</h2>
          <div class="rounded-xl bg-soft p-4 text-sm text-muted">
            {{ t('cdkLookup.batchHint', { max: LOOKUP_BATCH_MAX }) }}
          </div>
          <div class="form-group">
            <label>{{ t('cdkLookup.batchLabel') }}</label>
            <textarea
              v-model="batchText"
              rows="8"
              class="input mono text-sm min-h-[160px]"
              :placeholder="t('cdkLookup.batchPlaceholder')"
            />
            <p class="text-xs text-muted mt-1">
              {{ t('cdkLookup.batchRecognized', { n: parsedCodes.length }) }}
              <span v-if="parsedCodes.length > LOOKUP_BATCH_MAX" class="text-warn">
                · {{ t('cdkLookup.batchTruncated', { max: LOOKUP_BATCH_MAX }) }}
              </span>
            </p>
          </div>
          <button
            @click="queryBatch"
            :disabled="parsedCodes.length === 0 || querying"
            class="btn-primary w-full disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <span v-if="!querying">{{ t('cdkLookup.batchQueryBtn') }}</span>
            <span v-else class="flex items-center justify-center gap-2">
              <span class="spinner"></span>{{ t('common.querying') }}
            </span>
          </button>
        </template>

        <p v-if="error" class="text-sm" style="color: var(--err)">{{ error }}</p>
      </div>

      <div v-if="mode === 'single' && result" class="mt-8 card animate-slideInUp">
        <h2 class="text-xl font-bold text-ink mb-6">{{ t('cdkLookup.resultTitle') }}</h2>
        <div class="rounded-xl bg-soft p-6 space-y-4">
          <div class="flex justify-between items-center gap-3">
            <span class="text-muted shrink-0">{{ t('cdkLookup.cdkCode') }}</span>
            <span class="font-mono text-ink text-right break-all">{{ result.cdk_code }}</span>
          </div>
          <div class="flex justify-between items-center">
            <span class="text-muted">{{ t('cdkLookup.useStatus') }}</span>
            <span class="font-semibold" :style="{ color: statusColor(result.status) }">{{ statusText(result.status) }}</span>
          </div>
          <div class="flex justify-between items-center gap-3">
            <span class="text-muted shrink-0">{{ t('cdkLookup.rechargeEmail') }}</span>
            <span class="font-mono text-ink text-right break-all">
              {{ result.account_email || t('cdkLookup.emailEmpty') }}
            </span>
          </div>
          <div v-if="result.plan" class="flex justify-between items-center">
            <span class="text-muted">{{ t('cdkLookup.plan') }}</span>
            <span class="text-ink">{{ result.plan }}</span>
          </div>
          <div v-if="result.used_at" class="flex justify-between items-center text-sm">
            <span class="text-muted">{{ t('cdkLookup.usedAt') }}</span>
            <span class="text-muted">{{ result.used_at }}</span>
          </div>
        </div>

        <div
          v-if="result.status === 'failed'"
          class="alert alert-error mt-6"
        >{{ result.message || t('cdkLookup.msgFailed') }}</div>
        <div
          v-else-if="result.used"
          class="alert alert-success mt-6"
        >{{ result.message || t('cdkLookup.msgUsed') }}</div>
        <div
          v-else-if="result.status === 'processing'"
          class="alert alert-info mt-6"
        >{{ result.message || t('cdkLookup.msgProcessing') }}</div>
        <div
          v-else-if="result.status === 'disabled' || result.status === 'expired'"
          class="alert mt-6"
          style="background: var(--warn-soft); color: var(--warn); border-color: var(--warn)"
        >{{ result.message }}</div>
        <div
          v-else
          class="alert alert-info mt-6"
        >{{ result.message || t('cdkLookup.msgUnused') }}</div>

        <div class="flex flex-col sm:flex-row gap-3 mt-6">
          <button
            v-if="result.can_resubmit"
            class="btn-primary flex-1"
            @click="goRedeem(result.cdk_code)"
          >{{ t('cdkLookup.resubmit') }}</button>
          <button
            v-else-if="result.status === 'unused'"
            class="btn-primary flex-1"
            @click="goRedeem(result.cdk_code)"
          >{{ t('cdkLookup.goRedeem') }}</button>
          <button @click="reset" class="btn-secondary flex-1">{{ t('cdkLookup.queryOther') }}</button>
        </div>
      </div>

      <div v-if="mode === 'batch' && batchResults.length" class="mt-8 card animate-slideInUp space-y-4">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <h2 class="text-xl font-bold text-ink">{{ t('cdkLookup.resultTitle') }}</h2>
          <div class="flex flex-wrap gap-2">
            <button type="button" class="btn-secondary !py-1.5 !px-3 text-sm" @click="copyUsedEmails">
              {{ t('cdkLookup.copyEmails') }}
            </button>
            <button type="button" class="btn-secondary !py-1.5 !px-3 text-sm" @click="exportCsv">
              {{ t('cdkLookup.exportCsv') }}
            </button>
          </div>
        </div>
        <div class="grid grid-cols-2 sm:grid-cols-5 gap-2 text-center text-sm">
          <div class="rounded-lg py-2" style="background: color-mix(in srgb, var(--good, #16a34a) 12%, transparent)">
            {{ t('cdkLookup.summaryUsed', { n: batchSummary.used }) }}
          </div>
          <div class="rounded-lg py-2" style="background: color-mix(in srgb, var(--err, #dc2626) 12%, transparent)">
            {{ t('cdkLookup.summaryFailed', { n: batchSummary.failed }) }}
          </div>
          <div class="rounded-lg bg-soft py-2">
            {{ t('cdkLookup.summaryUnused', { n: batchSummary.unused }) }}
          </div>
          <div class="rounded-lg py-2" style="background: color-mix(in srgb, var(--primary) 12%, transparent)">
            {{ t('cdkLookup.summaryProcessing', { n: batchSummary.processing }) }}
          </div>
          <div class="rounded-lg py-2" style="background: color-mix(in srgb, var(--warn, #d97706) 12%, transparent)">
            {{ t('cdkLookup.summaryUnknown', { n: batchSummary.unknown }) }}
          </div>
        </div>
        <div class="overflow-x-auto rounded-xl border bd">
          <table class="w-full text-sm">
            <thead>
              <tr class="bg-soft text-left text-muted">
                <th class="px-3 py-2 font-medium">#</th>
                <th class="px-3 py-2 font-medium">{{ t('cdkLookup.cdkCode') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('cdkLookup.useStatus') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('cdkLookup.rechargeEmail') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('cdkLookup.plan') }}</th>
                <th class="px-3 py-2 font-medium">{{ t('cdkLookup.usedAt') }}</th>
                <th class="px-3 py-2 font-medium"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, i) in batchResults" :key="row.cdk_code + i" class="border-t bd">
                <td class="px-3 py-2 text-muted tabular-nums">{{ i + 1 }}</td>
                <td class="px-3 py-2 font-mono break-all">{{ row.cdk_code }}</td>
                <td class="px-3 py-2 font-semibold whitespace-nowrap" :style="{ color: statusColor(row.status) }">
                  {{ statusText(row.status) }}
                </td>
                <td class="px-3 py-2 font-mono break-all">{{ row.account_email || t('cdkLookup.emailEmpty') }}</td>
                <td class="px-3 py-2">{{ row.plan || t('cdkLookup.emailEmpty') }}</td>
                <td class="px-3 py-2 text-muted whitespace-nowrap">{{ row.used_at || t('cdkLookup.emailEmpty') }}</td>
                <td class="px-3 py-2 text-right whitespace-nowrap">
                  <button
                    v-if="row.can_resubmit"
                    type="button"
                    class="btn-primary !py-1 !px-3 text-xs"
                    @click="goRedeem(row.cdk_code)"
                  >{{ t('cdkLookup.resubmit') }}</button>
                  <button
                    v-else-if="row.status === 'unused'"
                    type="button"
                    class="btn-secondary !py-1 !px-3 text-xs"
                    @click="goRedeem(row.cdk_code)"
                  >{{ t('cdkLookup.goRedeem') }}</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import ThemeToggle from '../../components/ThemeToggle.vue'
import LanguageToggle from '../../components/LanguageToggle.vue'
import RedeemModeTabs from '../../components/RedeemModeTabs.vue'
import { parseCdks } from '../../lib/batch-session'
import { copyToClipboard } from '../../lib/clipboard'
import { toast } from '../../lib/ui'

const { t } = useI18n({ useScope: 'global' })
const router = useRouter()

const LOOKUP_BATCH_MAX = 100

interface CDKStatusResult {
  cdk_code: string
  status: string
  used: boolean
  can_resubmit?: boolean
  account_email?: string
  plan?: string
  used_at?: string
  notes?: string
  message?: string
}

const mode = ref<'single' | 'batch'>('single')
const cdkCode = ref('')
const batchText = ref('')
const querying = ref(false)
const error = ref('')
const result = ref<CDKStatusResult | null>(null)
const batchResults = ref<CDKStatusResult[]>([])

const parsedCodes = computed(() => parseCdks(batchText.value))

const batchSummary = computed(() => {
  const s = { used: 0, failed: 0, unused: 0, processing: 0, unknown: 0 }
  for (const row of batchResults.value) {
    if (row.status === 'used') s.used++
    else if (row.status === 'failed') s.failed++
    else if (row.status === 'unused') s.unused++
    else if (row.status === 'processing') s.processing++
    else s.unknown++
  }
  return s
})

function statusText(status: string) {
  const key = `cdkLookup.status.${status}`
  const label = t(key)
  return label === key ? status : label
}

function statusColor(status: string) {
  if (status === 'used') return 'var(--good, #16a34a)'
  if (status === 'failed') return 'var(--err, #dc2626)'
  if (status === 'unused') return 'var(--ink)'
  if (status === 'processing') return 'var(--primary)'
  if (status === 'disabled' || status === 'expired' || status === 'unknown') return 'var(--warn, #d97706)'
  return 'var(--ink)'
}

function goRedeem(code: string) {
  router.push({ path: '/recharge', query: { cdk: code } })
}

async function query() {
  const code = cdkCode.value.trim()
  if (!code) return
  querying.value = true
  error.value = ''
  result.value = null
  try {
    const r = await fetch(`/api/v1/lookup/cdk?code=${encodeURIComponent(code)}`)
    const data = await r.json().catch(() => ({}))
    if (!r.ok) {
      error.value = data.error || data.message || t('cdkLookup.errNotFound')
      return
    }
    result.value = data as CDKStatusResult
  } catch {
    error.value = t('cdkLookup.errNetwork')
  } finally {
    querying.value = false
  }
}

async function queryBatch() {
  const codes = parsedCodes.value.slice(0, LOOKUP_BATCH_MAX)
  if (!codes.length) {
    error.value = t('cdkLookup.batchEmpty')
    return
  }
  querying.value = true
  error.value = ''
  batchResults.value = []
  try {
    const r = await fetch('/api/v1/lookup/cdk/batch', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ codes }),
    })
    const data = await r.json().catch(() => ({}))
    if (!r.ok) {
      error.value = data.error || data.message || t('cdkLookup.errNetwork')
      return
    }
    batchResults.value = Array.isArray(data.results) ? data.results : []
  } catch {
    error.value = t('cdkLookup.errNetwork')
  } finally {
    querying.value = false
  }
}

async function copyUsedEmails() {
  const emails = Array.from(
    new Set(
      batchResults.value
        .map((row) => String(row.account_email || '').trim())
        .filter(Boolean),
    ),
  )
  if (!emails.length) {
    toast(t('cdkLookup.emailEmpty'), 'warn')
    return
  }
  const ok = await copyToClipboard(emails.join('\n'))
  toast(ok ? t('cdkLookup.copied') : t('cdkLookup.copyFail'), ok ? 'ok' : 'err')
}

function exportCsv() {
  const header = ['cdk_code', 'status', 'can_resubmit', 'account_email', 'plan', 'used_at', 'message']
  const lines = [header.join(',')]
  for (const row of batchResults.value) {
    const cells = [
      row.cdk_code,
      row.status,
      row.can_resubmit ? '1' : '0',
      row.account_email || '',
      row.plan || '',
      row.used_at || '',
      row.message || '',
    ].map((v) => `"${String(v).replace(/"/g, '""')}"`)
    lines.push(cells.join(','))
  }
  const blob = new Blob([`\uFEFF${lines.join('\n')}`], { type: 'text/csv;charset=utf-8' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = `cdk-lookup-${new Date().toISOString().slice(0, 10)}.csv`
  a.click()
  URL.revokeObjectURL(a.href)
}

function setMode(next: 'single' | 'batch') {
  mode.value = next
  error.value = ''
}

function reset() {
  result.value = null
  error.value = ''
  cdkCode.value = ''
}
</script>

<style scoped>
.text-warn { color: var(--warn, #d97706); }
</style>
