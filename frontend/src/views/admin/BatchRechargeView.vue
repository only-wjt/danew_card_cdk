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
              <span class="plan-fee">{{ t('batchRecharge.fee') }} {{ usd(DISPLAY_FEE_MINOR) }}</span>
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
          {{ t('batchRecharge.estimate', { amount: estimatedFee, n: itemCount, fee: estimatedFee }) }}
        </span>
        <button type="button" class="btn-primary" :disabled="!canSubmit" @click="openCreateConfirm">
          {{ creating ? t('batchRecharge.submitting') : t('batchRecharge.submit') }}
        </button>
      </div>
      <p v-if="itemCount === 0" class="text-xs text-subtle">{{ t('batchRecharge.needItems') }}</p>

      <p v-if="createError" class="alert alert-error">{{ createError }}</p>
    </div>

    <!-- ── 批次列表 ── -->
    <div class="card !p-0 overflow-hidden">
      <div class="px-4 py-3 border-b bd flex flex-wrap items-center gap-3">
        <span class="text-sm font-semibold text-ink">{{ t('batchRecharge.listTitle') }}</span>
        <div class="flex flex-wrap items-center gap-2 ml-auto">
          <span class="text-xs text-muted">{{ t('batchRecharge.filterSource') }}</span>
          <el-select v-model="filterSource" size="small" style="width: 130px" @change="onFilterChange">
            <el-option :label="t('batchRecharge.sourceAll')" value="" />
            <el-option :label="t('batchRecharge.sourceSelf')" value="self" />
            <el-option :label="t('batchRecharge.sourceAgent')" value="agent" />
          </el-select>
          <span class="text-xs text-muted">{{ t('batchRecharge.filterAgent') }}</span>
          <el-select v-model="filterAgentId" size="small" style="width: 160px" @change="onFilterChange">
            <el-option :label="t('batchRecharge.agentAll')" :value="0" />
            <el-option v-for="a in agentOptions" :key="a.id" :label="a.label" :value="a.id" />
          </el-select>
        </div>
      </div>
      <div v-if="listError" class="p-4"><p class="alert alert-error">{{ listError }}</p></div>
      <div class="overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>{{ t('batchRecharge.colBatch') }}</th>
              <th>{{ t('batchRecharge.colPlan') }}</th>
              <th class="num">{{ t('batchRecharge.colTotal') }}</th>
              <th class="num">{{ t('batchRecharge.colSuccess') }}</th>
              <th class="num">{{ t('batchRecharge.colFailed') }}</th>
              <th>{{ t('batchRecharge.colStatus') }}</th>
              <th>{{ t('batchRecharge.colChannel') }}</th>
              <th>{{ t('batchRecharge.colOperator') }}</th>
              <th>{{ t('batchRecharge.colUpdated') }}</th>
              <th>{{ t('batchRecharge.colNote') }}</th>
              <th>{{ t('batchRecharge.colAction') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!batches.length">
              <td colspan="11" class="text-center text-muted">{{ t('batchRecharge.emptyList') }}</td>
            </tr>
            <tr v-for="b in batches" :key="b.batch_id">
              <td class="mono">{{ b.batch_id }}</td>
              <td>{{ b.plan }}</td>
              <td class="num">{{ b.total }}</td>
              <td class="num stat-success">{{ b.success ?? 0 }}</td>
              <td class="num stat-failed">{{ b.failed ?? 0 }}</td>
              <td><span class="pill" :class="batchPillClass(b.status)">{{ batchStatusLabel(b.status) }}</span></td>
              <td>
                <span v-if="b.agent_user_id" class="pill pill-info">
                  {{ b.agent_name || `#${b.agent_user_id}` }}
                </span>
                <span v-else class="text-muted text-xs">{{ t('batchRecharge.channelSelf') }}</span>
              </td>
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

        <div class="export-bar">
          <span class="text-xs text-muted">{{ t('batchRecharge.exportLabel') }}</span>
          <div class="flex flex-wrap gap-1">
            <button
              v-for="s in EXPORT_SCOPES"
              :key="s"
              type="button"
              class="btn-secondary !py-1 !px-2 text-xs"
              :class="{ 'mode-on': exportScope === s }"
              @click="exportScope = s"
            >
              {{ t(`batchRecharge.exportScope.${s}`) }}
            </button>
          </div>
          <button type="button" class="btn-secondary !py-1.5" :disabled="exporting" @click="exportExcel">
            {{ exporting ? t('batchRecharge.exporting') : t('batchRecharge.exportExcel') }}
          </button>
        </div>
        <p class="text-xs text-subtle">{{ t('batchRecharge.exportHint') }}</p>

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

    <el-dialog
      v-model="confirmOpen"
      :title="t('batchRecharge.confirmTitle')"
      width="540px"
      align-center
      destroy-on-close
      :close-on-click-modal="!creating"
    >
      <div class="confirm-box">
        <div class="confirm-code" :class="{ 'is-high': plan === 'pro_20x' }">{{ plan }}</div>
        <div class="confirm-human">{{ planHumanLabel }}</div>
        <p class="confirm-stop">{{ t('batchRecharge.confirmCannotStop') }}</p>
        <p v-if="plan === 'pro_20x'" class="confirm-extra">{{ t('batchRecharge.confirmHighPrice') }}</p>
        <dl class="confirm-facts">
          <div>
            <dt>{{ t('batchRecharge.confirmCount') }}</dt>
            <dd>{{ itemCount }}</dd>
          </div>
          <div>
            <dt>{{ t('batchRecharge.confirmUnitFee') }}</dt>
            <dd>{{ usd(DISPLAY_FEE_MINOR) }}</dd>
          </div>
          <div>
            <dt>{{ t('batchRecharge.confirmTotalFee') }}</dt>
            <dd>{{ usd(DISPLAY_FEE_MINOR) }}</dd>
          </div>
        </dl>
        <p v-if="createError" class="alert alert-error" style="margin-top: 12px; text-align: left">{{ createError }}</p>
      </div>
      <template #footer>
        <button type="button" class="btn-secondary" :disabled="creating" @click="confirmOpen = false">
          {{ t('batchRecharge.confirmCancel') }}
        </button>
        <button type="button" class="btn-primary" :disabled="creating || itemCount === 0" @click="submitBatch">
          {{ creating ? t('batchRecharge.submitting') : t('batchRecharge.confirmOk', { plan }) }}
        </button>
      </template>
    </el-dialog>
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
const EXPORT_SCOPES = ['all', 'success', 'failed'] as const
/** 管理端仅展示 $0.00；真实 $1/$5/$10 扣费仍在 admin_batch_recharge.go。 */
const DISPLAY_FEE_MINOR = 0
const PLANS = [{ value: 'plus' }, { value: 'pro_5x' }, { value: 'pro_20x' }]
const ITEM_TERMINAL = new Set(['success', 'failed', 'skipped', 'unknown'])

interface BatchRow {
  batch_id: string
  operator: string
  agent_user_id?: number
  agent_name?: string
  plan: string
  total: number
  success?: number
  failed?: number
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
const filterSource = ref('')
const filterAgentId = ref(0)
const agentOptions = ref<{ id: number; label: string }[]>([])

const detailOpen = ref(false)
const detailId = ref('')
const detail = ref<{ batch: BatchRow; items: ItemRow[]; stats: Stats } | null>(null)
const reconciling = ref(false)
const reconcileError = ref('')
const resubmitRows = ref<ResubmitRow[]>([])
const confirmOpen = ref(false)
const exportScope = ref<(typeof EXPORT_SCOPES)[number]>('all')
const exporting = ref(false)
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
const estimatedFee = computed(() => (DISPLAY_FEE_MINOR / 100).toFixed(2))
const canSubmit = computed(
  () => !creating.value && itemCount.value > 0 && !overLimit.value && fundingConfirmed.value,
)
const unknownCount = computed(() => detail.value?.stats?.unknown ?? 0)
const planHumanLabel = computed(() => t(`batchRecharge.planHuman.${plan.value}`, { fee: usd(DISPLAY_FEE_MINOR) }))

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
    return sessionPool.value.map((s) => ({
      mode: 'session',
      session: s.session,
      email: s.email,
      gpt_password: s.gptPassword,
      email_password: s.emailPassword,
    }))
  }
  return mailboxPool.value.map((m) => ({
    mode: 'mailbox',
    email: m.email,
    password: m.password,
    email_password: m.password,
  }))
}

function openCreateConfirm() {
  if (creating.value || overLimit.value || !fundingConfirmed.value) return
  if (itemCount.value <= 0) {
    dialog.toast(t('batchRecharge.needItems'), 'warn')
    return
  }
  confirmOpen.value = true
}

async function submitBatch() {
  if (itemCount.value <= 0) {
    dialog.toast(t('batchRecharge.needItems'), 'warn')
    return
  }
  if (!fundingConfirmed.value || overLimit.value || creating.value) return
  const count = itemCount.value
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
    confirmOpen.value = false
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

async function loadAgentOptions() {
  try {
    const r = await authFetch('/api/v1/admin/agents?limit=200')
    if (!r.ok) return
    const d = await r.json()
    agentOptions.value = (d.list || []).map((a: any) => ({
      id: a.id,
      label: a.display_name || a.username,
    }))
  } catch {
    agentOptions.value = []
  }
}

function onFilterChange() {
  void loadBatches()
}

async function loadBatches() {
  listLoading.value = true
  listError.value = ''
  try {
    const params = new URLSearchParams({ limit: '50' })
    if (filterAgentId.value) params.set('agent_user_id', String(filterAgentId.value))
    else if (filterSource.value) params.set('source', filterSource.value)
    const r = await authFetch(`${API}?${params}`)
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

async function exportExcel() {
  if (!detailId.value || exporting.value) return
  exporting.value = true
  try {
    const r = await authFetch(
      `${API}/${encodeURIComponent(detailId.value)}/export?scope=${exportScope.value}`,
    )
    if (!r.ok) {
      const d = await r.json().catch(() => ({}))
      dialog.toast(d.error || d.msg || t('batchRecharge.errExport'), 'err')
      return
    }
    const blob = await r.blob()
    const cd = r.headers.get('Content-Disposition') || ''
    const m = cd.match(/filename="?([^"]+)"?/)
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = m?.[1] || `batch-recharge-${detailId.value}-${exportScope.value}.xlsx`
    a.click()
    URL.revokeObjectURL(a.href)
  } catch (err: any) {
    dialog.toast(err?.message || t('batchRecharge.errNetwork'), 'err')
  } finally {
    exporting.value = false
  }
}

onUnmounted(stopPoll)
void loadBatches()
void loadAgentOptions()
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

.stat-success { color: var(--good); font-weight: 600; }
.stat-failed { color: var(--err); font-weight: 600; }

.export-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.confirm-box { text-align: center; }
.confirm-code {
  font-family: var(--font-mono);
  font-size: 42px;
  font-weight: 800;
  line-height: 1.1;
  letter-spacing: 0.02em;
  color: var(--ink);
}
.confirm-code.is-high { color: var(--warn); }
.confirm-human {
  margin-top: 8px;
  font-size: 20px;
  font-weight: 700;
  color: var(--ink);
}
.confirm-stop {
  margin-top: 12px;
  font-size: 14px;
  font-weight: 600;
  color: var(--ink-2);
}
.confirm-extra {
  margin-top: 6px;
  font-size: 13px;
  font-weight: 700;
  color: var(--warn);
}
.confirm-facts {
  margin: 18px 0 0;
  display: grid;
  gap: 10px;
  text-align: left;
}
.confirm-facts > div {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  padding: 10px 14px;
  border-radius: var(--radius-md);
  background: var(--surface-2);
  border: 1px solid var(--brd);
}
.confirm-facts dt {
  font-size: 13px;
  color: var(--ink-3);
}
.confirm-facts dd {
  font-family: var(--font-mono);
  font-size: 22px;
  font-weight: 800;
  color: var(--ink);
}
</style>
