<template>
  <div class="pb-2 space-y-5">
    <div class="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
      <div>
        <h1 class="text-3xl font-bold text-ink">{{ t('batchRecharge.title') }}</h1>
        <p class="text-sm text-muted mt-2">{{ t('batchRecharge.subtitle') }}</p>
      </div>
      <div class="flex flex-wrap gap-2">
        <el-button :loading="listLoading" @click="loadBatches">{{ t('batchRecharge.refresh') }}</el-button>
      </div>
    </div>

    <!-- ── 创建批次 ── -->
    <div class="card space-y-4">
      <h2 class="text-base font-semibold text-ink">{{ t('batchRecharge.createTitle') }}</h2>

      <div class="grid gap-4 md:grid-cols-2">
        <div class="form-group">
          <label>{{ t('batchRecharge.planLabel') }}</label>
          <div class="flex flex-wrap gap-2">
            <button
              v-for="p in PLANS"
              :key="p.value"
              type="button"
              class="plan-chip"
              :class="{ active: plan === p.value }"
              @click="plan = p.value"
            >
              <span class="plan-name">{{ p.value }}</span>
              <span class="plan-fee">{{ t('batchRecharge.fee') }} {{ usd(p.feeMinor) }}</span>
            </button>
          </div>
          <p class="text-xs text-subtle">{{ t('batchRecharge.planHint') }}</p>
        </div>

        <div class="form-group">
          <label>{{ t('batchRecharge.credMode') }}</label>
          <div class="flex gap-2">
            <button
              type="button"
              class="btn-secondary !py-1.5 flex-1"
              :class="{ 'mode-on': credMode === 'session' }"
              @click="switchMode('session')"
            >
              {{ t('batchRecharge.modeSession') }}
            </button>
            <button
              type="button"
              class="btn-secondary !py-1.5 flex-1"
              :class="{ 'mode-on': credMode === 'mailbox' }"
              @click="switchMode('mailbox')"
            >
              {{ t('batchRecharge.modeMailbox') }}
            </button>
          </div>
          <p class="text-xs text-subtle">{{ t('batchRecharge.maxHint') }}</p>
        </div>
      </div>

      <div v-if="credMode === 'session'" class="form-group">
        <label>{{ t('batchRecharge.sessionLabel') }}</label>
        <input
          ref="sessionFileRef"
          type="file"
          accept=".xlsx,.xls,.csv"
          class="hidden"
          @change="onPickSessionFile"
        />
        <button type="button" class="btn-secondary w-full" :disabled="importing" @click="sessionFileRef?.click()">
          {{ importing ? t('batchRecharge.importing') : t('batchRecharge.sessionPickBtn') }}
        </button>
        <p class="text-xs text-subtle">{{ t('batchRecharge.sessionHint') }}</p>
      </div>

      <div v-else class="form-group">
        <label>{{ t('batchRecharge.mailboxLabel') }}</label>
        <textarea
          v-model="mailboxText"
          rows="5"
          class="input mono text-xs"
          :placeholder="t('batchRecharge.mailboxPlaceholder')"
        />
        <input
          ref="mailboxFileRef"
          type="file"
          accept=".xlsx,.xls,.csv,.txt"
          class="hidden"
          @change="onPickMailboxFile"
        />
        <button type="button" class="btn-secondary w-full" :disabled="importing" @click="mailboxFileRef?.click()">
          {{ importing ? t('batchRecharge.importing') : t('batchRecharge.mailboxImportBtn') }}
        </button>
      </div>

      <p v-if="importMsg" class="text-xs text-muted">{{ importMsg }}</p>

      <div class="flex flex-wrap items-center gap-3 text-sm">
        <span class="text-muted">{{ t('batchRecharge.recognized', { n: itemCount }) }}</span>
        <button v-if="itemCount > 0" type="button" class="btn-ghost !py-1 !px-2 text-xs" @click="clearCreds">
          {{ t('batchRecharge.clearCreds') }}
        </button>
      </div>

      <p v-if="overLimit" class="alert alert-error">
        {{ t('batchRecharge.overLimit', { n: itemCount, max: MAX_ITEMS }) }}
      </p>

      <div v-if="itemCount > 0" class="rounded-xl border bd overflow-hidden">
        <div class="px-3 py-2 bg-soft text-xs font-medium text-muted">{{ t('batchRecharge.previewTitle') }}</div>
        <ul class="max-h-48 overflow-y-auto divide-y" style="border-color: var(--brd)">
          <li
            v-for="(row, idx) in previewRows"
            :key="idx"
            class="px-3 py-1.5 text-xs flex items-center gap-3"
            style="border-color: var(--brd)"
          >
            <span class="text-subtle tabular-nums w-7">{{ idx + 1 }}</span>
            <span class="mono text-ink">{{ row.email || '—' }}</span>
            <span class="ml-auto mono text-subtle">{{ t('batchRecharge.masked') }}</span>
          </li>
        </ul>
      </div>

      <p class="alert alert-info">{{ t('batchRecharge.credNotice') }}</p>

      <label class="flex items-start gap-2 text-sm text-ink cursor-pointer">
        <input v-model="fundingConfirmed" type="checkbox" class="mt-0.5" />
        <span>{{ t('batchRecharge.fundingLabel') }}</span>
      </label>
      <p v-if="itemCount > 0 && !fundingConfirmed" class="text-xs text-warn">
        {{ t('batchRecharge.fundingRequired') }}
      </p>

      <div class="flex flex-wrap items-center justify-between gap-3">
        <span class="text-sm text-muted">
          {{ t('batchRecharge.estimate', { amount: estimatedFee, n: itemCount, fee: usd(currentFeeMinor) }) }}
        </span>
        <button type="button" class="btn-primary" :disabled="!canSubmit" @click="submitBatch">
          {{ creating ? t('batchRecharge.submitting') : t('batchRecharge.submit') }}
        </button>
      </div>

      <p v-if="createError" class="alert alert-error">{{ createError }}</p>
    </div>

    <!-- ── 批次列表 ── -->
    <div class="card !p-0 overflow-hidden">
      <div class="px-4 py-3 border-b bd text-sm font-semibold text-ink">{{ t('batchRecharge.listTitle') }}</div>
      <div v-if="listError" class="p-4"><p class="alert alert-error">{{ listError }}</p></div>
      <div class="overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>{{ t('batchRecharge.colBatch') }}</th>
              <th>{{ t('batchRecharge.colPlan') }}</th>
              <th class="num">{{ t('batchRecharge.colTotal') }}</th>
              <th>{{ t('batchRecharge.colStatus') }}</th>
              <th>{{ t('batchRecharge.colOperator') }}</th>
              <th>{{ t('batchRecharge.colUpdated') }}</th>
              <th>{{ t('batchRecharge.colNote') }}</th>
              <th>{{ t('batchRecharge.colAction') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!batches.length">
              <td colspan="8" class="text-center text-muted">{{ t('batchRecharge.emptyList') }}</td>
            </tr>
            <tr v-for="b in batches" :key="b.batch_id">
              <td class="mono">{{ b.batch_id }}</td>
              <td>{{ b.plan }}</td>
              <td class="num">{{ b.total }}</td>
              <td><span class="pill" :class="batchPillClass(b.status)">{{ batchStatusLabel(b.status) }}</span></td>
              <td>{{ b.operator || '—' }}</td>
              <td class="text-xs text-muted">{{ fmtTime(b.updated_at) }}</td>
              <td class="text-xs text-muted">{{ b.message || '—' }}</td>
              <td>
                <el-button link type="primary" size="small" @click="openDetail(b.batch_id)">
                  {{ t('batchRecharge.viewDetail') }}
                </el-button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- ── 批次明细 ── -->
    <el-drawer v-model="detailOpen" :title="t('batchRecharge.detailTitle')" size="720px" @closed="onDetailClosed">
      <div v-if="detail" class="space-y-4">
        <div class="flex flex-wrap items-center gap-3">
          <span class="mono text-sm text-ink">{{ detail.batch.batch_id }}</span>
          <span class="pill" :class="batchPillClass(detail.batch.status)">
            {{ batchStatusLabel(detail.batch.status) }}
          </span>
          <span class="text-xs text-muted">{{ detail.batch.plan }} · {{ fmtTime(detail.batch.created_at) }}</span>
        </div>
        <p v-if="detail.batch.message" class="text-xs text-muted">{{ detail.batch.message }}</p>

        <div class="grid grid-cols-3 gap-2 sm:grid-cols-6 text-center">
          <div v-for="s in statCells" :key="s.key" class="rounded-lg bg-soft py-2">
            <div class="text-lg font-bold tabular-nums" :style="s.style">{{ s.value }}</div>
            <div class="text-[10px] text-muted">{{ s.label }}</div>
          </div>
        </div>

        <p v-if="unknownCount > 0" class="alert unknown-banner">
          {{ t('batchRecharge.unknownBanner', { n: unknownCount }) }}
        </p>

        <div class="flex flex-wrap items-center gap-3">
          <button type="button" class="btn-secondary !py-1.5" :disabled="reconciling" @click="reconcile">
            {{ reconciling ? t('batchRecharge.reconciling') : t('batchRecharge.reconcile') }}
          </button>
          <span class="text-xs text-muted flex-1 min-w-[220px]">{{ t('batchRecharge.reconcileHint') }}</span>
        </div>
        <p v-if="reconcileError" class="alert alert-error">{{ reconcileError }}</p>

        <div class="overflow-x-auto rounded-xl border bd">
          <table class="data-table">
            <thead>
              <tr>
                <th class="num">{{ t('batchRecharge.colSeq') }}</th>
                <th>{{ t('batchRecharge.colMode') }}</th>
                <th>{{ t('batchRecharge.colEmail') }}</th>
                <th>{{ t('batchRecharge.colStatus') }}</th>
                <th>{{ t('batchRecharge.colOrder') }}</th>
                <th>{{ t('batchRecharge.colMessage') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="!detail.items.length">
                <td colspan="6" class="text-center text-muted">{{ t('batchRecharge.emptyItems') }}</td>
              </tr>
              <tr v-for="it in detail.items" :key="it.client_request_id">
                <td class="num">{{ it.seq }}</td>
                <td class="text-xs">{{ it.cred_mode }}</td>
                <td class="mono text-xs">{{ it.account_email || '—' }}</td>
                <td>
                  <span class="pill" :class="itemPillClass(it.status)">{{ itemStatusLabel(it.status) }}</span>
                  <div v-if="it.status === 'unknown'" class="text-[11px] mt-1 no-retry">
                    {{ t('batchRecharge.unknownTip') }}
                  </div>
                </td>
                <td class="mono text-xs">{{ it.upstream_order_id || '—' }}</td>
                <td class="text-xs text-muted">{{ it.message || '—' }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-if="resubmitRows.length" class="rounded-xl border bd p-3 space-y-2">
          <div class="text-sm font-semibold text-ink">{{ t('batchRecharge.resubmitTitle') }}</div>
          <p class="text-xs text-muted">{{ t('batchRecharge.resubmitHint') }}</p>
          <ul class="text-xs space-y-1">
            <li v-for="r in resubmitRows" :key="r.seq" class="flex flex-wrap items-center gap-2">
              <span class="text-subtle tabular-nums w-7">#{{ r.seq }}</span>
              <span class="mono text-ink">{{ r.account_email || '—' }}</span>
              <span class="text-subtle">{{ r.cred_mode }}</span>
              <span class="text-muted">{{ r.message }}</span>
            </li>
          </ul>
        </div>
      </div>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { authFetch } from '../../lib/api'
import { dialog } from '../../lib/dialog'
import {
  parseMailboxLines,
  parseMailboxesFromSheet,
  parseSessionsFromSheet,
  readWorkbookRows,
  type ImportedMailbox,
  type ImportedSession,
} from '../../lib/batch-session'

const { t } = useI18n({ useScope: 'global' })

const API = '/api/v1/admin/cardplatform/batch-recharge'
const MAX_ITEMS = 100
const POLL_INTERVAL_MS = 3000
const PLANS = [
  { value: 'plus', feeMinor: 100 },
  { value: 'pro_5x', feeMinor: 500 },
  { value: 'pro_20x', feeMinor: 1000 },
]
const ITEM_TERMINAL = new Set(['success', 'failed', 'skipped', 'unknown'])

interface BatchRow {
  batch_id: string
  operator: string
  plan: string
  total: number
  status: string
  message: string
  created_at: string
  updated_at: string
}
interface ItemRow {
  batch_id: string
  seq: number
  client_request_id: string
  plan: string
  cred_mode: string
  account_email: string
  upstream_order_id: string
  status: string
  message: string
  updated_at: string
}
interface Stats {
  total: number
  success: number
  failed: number
  running: number
  pending: number
  unknown: number
}
interface ResubmitRow {
  seq: number
  account_email: string
  cred_mode: string
  status: string
  message: string
}

const plan = ref('plus')
const credMode = ref<'session' | 'mailbox'>('session')
const mailboxText = ref('')
const sessionPool = ref<ImportedSession[]>([])
const importMsg = ref('')
const importing = ref(false)
const fundingConfirmed = ref(false)
const creating = ref(false)
const createError = ref('')
const sessionFileRef = ref<HTMLInputElement | null>(null)
const mailboxFileRef = ref<HTMLInputElement | null>(null)

const batches = ref<BatchRow[]>([])
const listLoading = ref(false)
const listError = ref('')

const detailOpen = ref(false)
const detailId = ref('')
const detail = ref<{ batch: BatchRow; items: ItemRow[]; stats: Stats } | null>(null)
const reconciling = ref(false)
const reconcileError = ref('')
const resubmitRows = ref<ResubmitRow[]>([])
let pollTimer: ReturnType<typeof setInterval> | null = null

const mailboxPool = computed<ImportedMailbox[]>(() => parseMailboxLines(mailboxText.value))
const itemCount = computed(() =>
  credMode.value === 'session' ? sessionPool.value.length : mailboxPool.value.length,
)
const previewRows = computed(() =>
  credMode.value === 'session'
    ? sessionPool.value.map((s) => ({ email: s.email }))
    : mailboxPool.value.map((m) => ({ email: m.email })),
)
const overLimit = computed(() => itemCount.value > MAX_ITEMS)
const currentFeeMinor = computed(() => PLANS.find((p) => p.value === plan.value)?.feeMinor ?? 0)
const estimatedFee = computed(() => ((currentFeeMinor.value * itemCount.value) / 100).toFixed(2))
const canSubmit = computed(
  () => !creating.value && itemCount.value > 0 && !overLimit.value && fundingConfirmed.value,
)
const unknownCount = computed(() => detail.value?.stats?.unknown ?? 0)

const statCells = computed(() => {
  const s = detail.value?.stats
  if (!s) return []
  return [
    { key: 'total', label: t('batchRecharge.statTotal'), value: s.total, style: '' },
    { key: 'success', label: t('batchRecharge.statSuccess'), value: s.success, style: 'color: var(--good)' },
    { key: 'failed', label: t('batchRecharge.statFailed'), value: s.failed, style: 'color: var(--err)' },
    { key: 'running', label: t('batchRecharge.statRunning'), value: s.running, style: 'color: var(--primary)' },
    { key: 'pending', label: t('batchRecharge.statPending'), value: s.pending, style: '' },
    { key: 'unknown', label: t('batchRecharge.statUnknown'), value: s.unknown, style: 'color: var(--warn)' },
  ]
})

function usd(minor: number) {
  return `$${(minor / 100).toFixed(2)}`
}

function fmtTime(v: string) {
  if (!v) return '—'
  const d = new Date(v.includes('T') ? v : v.replace(' ', 'T') + 'Z')
  return Number.isNaN(d.getTime()) ? v : d.toLocaleString()
}

function batchStatusLabel(st: string) {
  const map: Record<string, string> = {
    running: t('batchRecharge.batchStatus.running'),
    done: t('batchRecharge.batchStatus.done'),
    paused: t('batchRecharge.batchStatus.paused'),
  }
  return map[st] || st
}

function batchPillClass(st: string) {
  if (st === 'done') return 'pill-good'
  if (st === 'paused') return 'pill-warn'
  return 'pill-info'
}

function itemStatusLabel(st: string) {
  const map: Record<string, string> = {
    pending: t('batchRecharge.status.pending'),
    issuing: t('batchRecharge.status.issuing'),
    preparing: t('batchRecharge.status.preparing'),
    submitted: t('batchRecharge.status.submitted'),
    processing: t('batchRecharge.status.processing'),
    success: t('batchRecharge.status.success'),
    failed: t('batchRecharge.status.failed'),
    skipped: t('batchRecharge.status.skipped'),
    unknown: t('batchRecharge.status.unknown'),
  }
  return map[st] || st
}

/** unknown 走 .pill-unknown：可能已扣款，红色会诱导操作员点重试 */
function itemPillClass(st: string) {
  if (st === 'unknown') return 'pill-unknown'
  if (st === 'success') return 'pill-good'
  if (st === 'failed') return 'pill-err'
  if (st === 'skipped') return 'pill-warn'
  if (st === 'pending') return ''
  return 'pill-info'
}

function switchMode(m: 'session' | 'mailbox') {
  if (credMode.value === m) return
  credMode.value = m
  clearCreds()
}

function clearCreds() {
  sessionPool.value = []
  mailboxText.value = ''
  importMsg.value = ''
  createError.value = ''
}

async function onPickSessionFile(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0] ?? null
  input.value = ''
  if (!file) return
  importing.value = true
  importMsg.value = ''
  try {
    const rows = await readWorkbookRows(file)
    const { sessions, mode, sessionCol, skippedDup } = parseSessionsFromSheet(rows)
    if (!sessions.length) {
      sessionPool.value = []
      importMsg.value =
        sessionCol < 0
          ? '未识别到 Session 列。请确保有 session 列或单元格为完整 Session JSON'
          : '识别到列但无有效 Session（需完整 JSON 含 sessionToken，或五段 JWE）'
      return
    }
    sessionPool.value = sessions
    importMsg.value =
      `已从「${file.name}」识别 ${sessions.length} 条 Session（${mode}）` +
      (skippedDup > 0 ? `，已去重跳过 ${skippedDup} 条` : '')
  } catch (err) {
    sessionPool.value = []
    importMsg.value = '读取 Excel 失败：' + (err instanceof Error ? err.message : '未知错误')
  } finally {
    importing.value = false
  }
}

async function onPickMailboxFile(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0] ?? null
  input.value = ''
  if (!file) return
  importing.value = true
  importMsg.value = ''
  try {
    if ((file.name || '').toLowerCase().endsWith('.txt')) {
      const rows = parseMailboxLines(await file.text())
      if (!rows.length) {
        importMsg.value = '未识别到邮箱密码行（支持 email----password）'
        return
      }
      mailboxText.value = rows.map((r) => `${r.email}----${r.password}`).join('\n')
      importMsg.value = `已从「${file.name}」识别 ${rows.length} 条邮箱密码`
      return
    }
    const { mailboxes, mode, skippedDup } = parseMailboxesFromSheet(await readWorkbookRows(file))
    if (!mailboxes.length) {
      importMsg.value = '未识别到邮箱/密码列。请使用「邮箱,邮箱密码」或 email----password'
      return
    }
    mailboxText.value = mailboxes.map((r) => `${r.email}----${r.password}`).join('\n')
    importMsg.value =
      `已从「${file.name}」识别 ${mailboxes.length} 条邮箱密码（${mode}）` +
      (skippedDup > 0 ? `，已去重跳过 ${skippedDup} 条` : '')
  } catch (err) {
    importMsg.value = '读取失败：' + (err instanceof Error ? err.message : '未知错误')
  } finally {
    importing.value = false
  }
}

function buildItems() {
  if (credMode.value === 'session') {
    return sessionPool.value.map((s) => ({ mode: 'session', session: s.session }))
  }
  return mailboxPool.value.map((m) => ({ mode: 'mailbox', email: m.email, password: m.password }))
}

async function submitBatch() {
  if (!canSubmit.value) return
  const count = itemCount.value
  const ok = await dialog.confirm(
    `确认为 ${count} 个账号充值 ${plan.value}？预计服务费 $${estimatedFee.value}，另需承担上游订阅实付。`,
    { title: t('batchRecharge.submit'), okText: t('batchRecharge.submit'), cancelText: '取消' },
  )
  if (!ok) return
  creating.value = true
  createError.value = ''
  try {
    const r = await authFetch(API, {
      method: 'POST',
      body: JSON.stringify({
        plan: plan.value,
        funding_confirmed: fundingConfirmed.value,
        items: buildItems(),
      }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      createError.value = d.error || d.msg || t('batchRecharge.errCreate')
      return
    }
    clearCreds()
    fundingConfirmed.value = false
    if (d.deduped) {
      dialog.toast(t('batchRecharge.createdDeduped', { id: d.batch_id }), 'warn', 6000)
    } else {
      dialog.toast(t('batchRecharge.createdNew', { id: d.batch_id, n: d.total ?? count }), 'ok')
    }
    await loadBatches()
    openDetail(d.batch_id)
  } catch (err: any) {
    createError.value = err?.message || t('batchRecharge.errNetwork')
  } finally {
    creating.value = false
  }
}

async function loadBatches() {
  listLoading.value = true
  listError.value = ''
  try {
    const r = await authFetch(`${API}?limit=50`)
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      listError.value = d.error || d.msg || t('batchRecharge.errLoad')
      return
    }
    batches.value = Array.isArray(d.list) ? d.list : []
  } catch (err: any) {
    listError.value = err?.message || t('batchRecharge.errNetwork')
  } finally {
    listLoading.value = false
  }
}

async function loadDetail(id: string) {
  const r = await authFetch(`${API}/${encodeURIComponent(id)}`)
  const d = await r.json().catch(() => ({}))
  if (!r.ok) {
    reconcileError.value = d.error || d.msg || t('batchRecharge.errLoad')
    return
  }
  detail.value = { batch: d.batch, items: Array.isArray(d.items) ? d.items : [], stats: d.stats }
}

function openDetail(id: string) {
  detailId.value = id
  detail.value = null
  resubmitRows.value = []
  reconcileError.value = ''
  detailOpen.value = true
  void loadDetail(id)
}

function onDetailClosed() {
  stopPoll()
  detailId.value = ''
  detail.value = null
  resubmitRows.value = []
}

function stopPoll() {
  if (pollTimer) clearInterval(pollTimer)
  pollTimer = null
}

/** 批次或任一明细未落终态时轮询刷新，落定后自动停 */
watch(
  () => [detailOpen.value, detail.value?.batch?.status, detail.value?.items?.map((i) => i.status).join(',')],
  () => {
    const inFlight =
      detailOpen.value &&
      !!detail.value &&
      (detail.value.batch.status === 'running' ||
        detail.value.items.some((i) => !ITEM_TERMINAL.has(i.status)))
    if (!inFlight) {
      stopPoll()
      return
    }
    if (!pollTimer) {
      pollTimer = setInterval(() => {
        if (detailId.value) void loadDetail(detailId.value)
      }, POLL_INTERVAL_MS)
    }
  },
)

async function reconcile() {
  if (!detailId.value || reconciling.value) return
  reconciling.value = true
  reconcileError.value = ''
  try {
    const r = await authFetch(`${API}/${encodeURIComponent(detailId.value)}/retry`, { method: 'POST' })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      reconcileError.value = d.error || d.msg || t('batchRecharge.errReconcile')
      return
    }
    resubmitRows.value = Array.isArray(d.resubmit) ? d.resubmit : []
    dialog.toast(
      t('batchRecharge.reconcileDone', { n: d.reconciled ?? 0, r: resubmitRows.value.length }),
      'ok',
    )
    await loadDetail(detailId.value)
    await loadBatches()
  } catch (err: any) {
    reconcileError.value = err?.message || t('batchRecharge.errNetwork')
  } finally {
    reconciling.value = false
  }
}

onUnmounted(stopPoll)
void loadBatches()
</script>

<style scoped>
.mono {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}
.text-warn { color: var(--warn); }

.plan-chip {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  padding: 8px 14px;
  border-radius: var(--radius-md);
  border: 1px solid var(--brd-control);
  background: var(--surface);
  color: var(--ink-2);
  cursor: pointer;
  transition: border-color var(--dur-2) var(--ease-standard),
    background-color var(--dur-2) var(--ease-standard), color var(--dur-2) var(--ease-standard);
}
.plan-chip:hover { border-color: var(--primary); }
.plan-chip:focus-visible { outline: none; box-shadow: var(--ring); }
.plan-chip.active {
  border-color: var(--primary);
  background: var(--primary-soft);
  color: var(--primary);
}
.plan-name { font-size: 13px; font-weight: 600; font-family: var(--font-mono); }
.plan-fee { font-size: 11px; color: var(--ink-3); }
.plan-chip.active .plan-fee { color: var(--primary); }

.mode-on {
  border-color: var(--primary);
  background: var(--primary-soft);
  color: var(--primary);
}

/* unknown 的横幅与说明走 warn，绝不用 err —— 红色会诱导操作员去点重试 */
.unknown-banner {
  background: var(--warn-soft);
  color: var(--warn);
  border-color: var(--warn);
  font-weight: 600;
}
.no-retry { color: var(--warn); }
</style>
