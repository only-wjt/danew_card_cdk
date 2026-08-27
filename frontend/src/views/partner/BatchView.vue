<template>
  <div class="space-y-5">
    <!-- ── 新建批次 ── -->
    <section class="card">
      <h3 class="section-title">新建批量任务</h3>

      <div class="step-bar">
        <span class="step-pill" :class="{ 'step-on': step === 1 }">1. 校验卡密</span>
        <span class="step-pill" :class="{ 'step-on': step === 2, 'step-done': step === 2 }">2. 填写凭据</span>
      </div>

      <div class="form-grid">
        <div class="form-group !mb-0">
          <label>套餐</label>
          <select v-model="plan" class="field-select" @change="onPlanChange">
            <option value="" disabled>请选择套餐</option>
            <option v-for="p in rechargePlans" :key="p.key" :value="p.key">
              {{ p.label || p.key }}
            </option>
          </select>
        </div>
        <div v-if="step === 2" class="form-group !mb-0">
          <label>凭据类型</label>
          <div class="mode-switch">
            <button
              type="button"
              class="btn-secondary"
              :class="{ 'mode-on': credMode === 'session' }"
              @click="credMode = 'session'"
            >
              Session
            </button>
            <button
              type="button"
              class="btn-secondary"
              :class="{ 'mode-on': credMode === 'mailbox' }"
              @click="credMode = 'mailbox'"
            >
              邮箱密码
            </button>
          </div>
        </div>
      </div>

      <!-- Step 1: CDK -->
      <div class="form-group mt-4 !mb-0">
        <label>
          卡密列表
          <span class="text-subtle font-normal">（每行一张完整卡密）</span>
        </label>
        <textarea
          v-model="cdkText"
          rows="6"
          class="input mono text-xs"
          placeholder="CDK-AAAA-BBBB-CCCC&#10;CDK-DDDD-EEEE-FFFF"
          @input="onCdkInput"
        />
      </div>

      <div class="parse-bar">
        <span>已粘贴 <b class="text-ink">{{ cdkLineCount }}</b> 行</span>
        <span v-if="quota.unused_cdk_count != null" class="text-subtle">
          账户可用卡密 {{ quota.unused_cdk_count }} 张
        </span>
      </div>

      <div v-if="step === 1" class="mt-4 flex flex-wrap gap-3">
        <button class="btn-primary" :disabled="!canValidate" @click="validateCdks">
          {{ validating ? '校验中…' : '校验卡密' }}
        </button>
        <button class="btn-secondary" :disabled="validating" @click="clearAll">清空</button>
      </div>
      <p v-if="validateMsg && step === 1" class="mt-3 text-sm" :class="validateOk ? 'text-err' : 'text-err'">
        {{ validateMsg }}
      </p>

      <!-- 校验结果 -->
      <div v-if="validateSummary" class="validate-panel mt-4">
        <div class="stat-row">
          <div class="stat-cell">
            <span class="stat-num">{{ validateSummary.total_lines }}</span><span>非空行</span>
          </div>
          <div class="stat-cell">
            <span class="stat-num">{{ validateSummary.unique_count }}</span><span>去重后</span>
          </div>
          <div class="stat-cell">
            <span class="stat-num text-warn">{{ validateSummary.duplicate_lines }}</span><span>重复</span>
          </div>
          <div class="stat-cell">
            <span class="stat-num text-good">{{ validateSummary.valid_count }}</span><span>有效</span>
          </div>
          <div class="stat-cell">
            <span class="stat-num text-err">{{ validateSummary.invalid_count }}</span><span>无效</span>
          </div>
        </div>

        <p v-if="validateSummary.valid_count === 0" class="alert alert-warn mt-3">
          没有可用的卡密，请检查套餐是否匹配、卡密是否已分配给你。
        </p>
        <p v-else-if="overBatchLimit" class="alert alert-warn mt-3">
          有效卡密 {{ validateSummary.valid_count }} 张，超出单批上限 {{ quota.max_batch_items }}，请减少后重新校验。
        </p>
        <p v-else class="alert alert-info mt-3">
          共 <b>{{ validateSummary.valid_count }}</b> 张卡密可用，请在下方按相同行序填写凭据（第 1 行凭据对应第 1 张有效卡密）。
        </p>

        <details v-if="validateSummary.duplicates?.length" class="issue-details mt-3">
          <summary>重复行（{{ validateSummary.duplicate_lines }}）</summary>
          <ul class="issue-list">
            <li v-for="row in validateSummary.duplicates" :key="'d' + row.line">
              第 {{ row.line }} 行 <span class="mono">{{ row.code }}</span> — {{ row.message }}
            </li>
          </ul>
        </details>
        <details v-if="validateSummary.invalid?.length" class="issue-details mt-2">
          <summary>无效卡密（{{ validateSummary.invalid_count }}）</summary>
          <ul class="issue-list">
            <li v-for="row in validateSummary.invalid" :key="'i' + row.line">
              第 {{ row.line }} 行 <span class="mono">{{ row.code }}</span> — {{ row.message }}
            </li>
          </ul>
        </details>

        <div v-if="step === 1 && validateSummary.valid_count > 0 && !overBatchLimit" class="mt-4">
          <button class="btn-primary" @click="goStep2">下一步：填写 {{ validateSummary.valid_count }} 条凭据</button>
          <button class="btn-secondary ml-3" @click="() => resetValidation()">重新校验</button>
        </div>
      </div>

      <!-- Step 2: credentials -->
      <template v-if="step === 2 && validatedCodes.length">
        <div class="form-group mt-4 !mb-0">
          <label v-if="credMode === 'session'">
            Session 列表
            <span class="text-subtle font-normal">（每行一条，须与有效卡密数量一致：{{ validatedCodes.length }} 行）</span>
          </label>
          <label v-else>
            邮箱密码列表
            <span class="text-subtle font-normal">（每行一条，须 {{ validatedCodes.length }} 行）</span>
          </label>
          <textarea
            v-model="credText"
            rows="6"
            class="input mono text-xs"
            :placeholder="credPlaceholder"
          />
        </div>

        <div class="parse-bar">
          <span>
            凭据 <b class="text-ink">{{ credLineCount }}</b> 行
            <span class="text-subtle">/ 需要 {{ validatedCodes.length }} 行</span>
          </span>
        </div>

        <p v-if="overConcurrency" class="alert alert-warn mt-3">
          当前在途 {{ quota.in_flight }} 条，本批 {{ validatedCodes.length }} 条，超出在途上限
          {{ quota.max_concurrent_recharge }} 条。
        </p>
        <p v-else-if="credMismatch" class="alert alert-warn mt-3">
          凭据行数与有效卡密不一致（{{ credLineCount }} ≠ {{ validatedCodes.length }}）。
        </p>

        <div class="mt-4 flex flex-wrap gap-3">
          <button class="btn-secondary" @click="step = 1">上一步</button>
          <button class="btn-primary" :disabled="!canSubmit" @click="submit">
            {{ submitting ? '提交中…' : `提交 ${validatedCodes.length} 条` }}
          </button>
          <button class="btn-secondary" :disabled="submitting" @click="clearAll">全部清空</button>
        </div>
      </template>

      <p v-if="submitMsg" class="mt-3 text-sm" :class="submitOk ? 'text-good' : 'text-err'">{{ submitMsg }}</p>
    </section>

    <!-- ── 批次列表 ── -->
    <section class="card overflow-hidden !p-0">
      <div class="table-head">
        <span>共 <b class="text-ink">{{ total }}</b> 个批次</span>
        <button class="btn-secondary !min-h-0 !py-1.5 text-sm" :disabled="loading" @click="loadBatches">
          {{ loading ? '刷新中…' : '刷新' }}
        </button>
      </div>
      <div class="overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>批次号</th>
              <th>套餐</th>
              <th>进度</th>
              <th>状态</th>
              <th>创建时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading && !batches.length">
              <td colspan="6" class="py-10 text-center text-muted">加载中…</td>
            </tr>
            <tr v-else-if="!batches.length">
              <td colspan="6" class="py-10 text-center text-muted">还没有批次，先在上方提交一批</td>
            </tr>
            <tr v-for="b in batches" :key="b.batch_id">
              <td class="mono text-xs">{{ b.batch_id }}</td>
              <td><span class="pill pill-info">{{ b.plan }}</span></td>
              <td class="text-sm">
                <span class="text-good">{{ b.success }} 成功</span>
                <span v-if="b.failed" class="text-err"> · {{ b.failed }} 失败</span>
                <span v-if="b.unknown" class="text-warn"> · {{ b.unknown }} 待确认</span>
                <span class="text-subtle"> / 共 {{ b.total }}</span>
              </td>
              <td><span :class="statusClass(b.status)">{{ statusLabel(b.status) }}</span></td>
              <td class="text-muted whitespace-nowrap text-sm">{{ formatDate(b.created_at) }}</td>
              <td>
                <button class="btn-secondary !min-h-0 !py-1 !px-3 text-xs" @click="openDetail(b.batch_id)">
                  详情
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="table-foot">
        <Pagination :page="page" :page-size="pageSize" :total="total" @update:page="onPage" />
      </div>
    </section>

    <!-- ── 批次详情 ── -->
    <Modal :open="detailOpen" wide :title="`批次 ${detail?.batch?.batch_id || ''}`" @close="closeDetail">
      <div v-if="detail" class="space-y-4">
        <div class="stat-row">
          <div class="stat-cell"><span class="stat-num">{{ detail.batch.total }}</span><span>总数</span></div>
          <div class="stat-cell"><span class="stat-num text-good">{{ detail.batch.success }}</span><span>成功</span></div>
          <div class="stat-cell"><span class="stat-num text-err">{{ detail.batch.failed }}</span><span>失败</span></div>
          <div class="stat-cell"><span class="stat-num text-warn">{{ detail.batch.unknown }}</span><span>待确认</span></div>
        </div>

        <p v-if="detail.batch.message" class="alert alert-info">{{ detail.batch.message }}</p>
        <p v-if="detail.batch.unknown > 0" class="alert alert-warn">
          有 {{ detail.batch.unknown }} 条结果待确认，上游可能已扣款。<b>请勿对这些账号重新提交</b>，
          请联系管理员核对。
        </p>

        <div class="flex flex-wrap items-center gap-2">
          <span class="text-sm text-subtle">导出：</span>
          <button
            v-for="s in EXPORT_SCOPES"
            :key="s.value"
            class="btn-secondary !min-h-0 !py-1 !px-3 text-xs"
            @click="exportBatch(s.value)"
          >
            {{ s.label }}
          </button>
        </div>

        <div class="overflow-x-auto detail-table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>#</th>
                <th>request_id</th>
                <th>业务单号</th>
                <th>邮箱</th>
                <th>状态</th>
                <th>说明</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="it in detail.items" :key="it.request_id">
                <td class="text-subtle">{{ it.seq }}</td>
                <td class="mono text-xs">{{ it.request_id }}</td>
                <td class="mono text-xs text-muted">{{ it.client_reference || '—' }}</td>
                <td class="text-sm">{{ it.account_email || '—' }}</td>
                <td><span :class="statusClass(it.status)">{{ statusLabel(it.status) }}</span></td>
                <td class="text-xs text-muted max-w-[220px] truncate" :title="it.message">{{ it.message || '—' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
      <template #footer>
        <button class="btn-secondary" @click="closeDetail">关闭</button>
      </template>
    </Modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import Modal from '../../components/Modal.vue'
import Pagination from '../../components/Pagination.vue'
import { agentFetch } from '../../lib/agentApi'
import { extractCdkSession, parseMailboxLines } from '../../lib/batch-session'
import { formatPartnerDate, statusClass, statusLabel } from './partnerUi'

interface PlanRow { key: string; label: string; fee_usd: number; fulfillment?: string }
interface BatchRow {
  batch_id: string
  plan: string
  total: number
  success: number
  failed: number
  skipped: number
  unknown: number
  status: string
  message: string
  created_at: string
}
interface ItemRow {
  seq: number
  request_id: string
  client_reference: string
  account_email: string
  status: string
  message: string
}

const EXPORT_SCOPES = [
  { value: 'all', label: '全部' },
  { value: 'success', label: '仅成功' },
  { value: 'failed', label: '仅失败' },
]

const plans = ref<PlanRow[]>([])
const rechargePlans = computed(() =>
  plans.value.filter((p) => p.fulfillment !== 'local_stock' && p.key !== 'gpt_white'),
)
const plan = ref('')
const credMode = ref<'session' | 'mailbox'>('session')
const cdkText = ref('')
const credText = ref('')
const step = ref(1)
const validating = ref(false)
const validateMsg = ref('')
const validateOk = ref(false)
const validateSummary = ref<ValidateSummary | null>(null)
const validatedCodes = ref<string[]>([])
const submitting = ref(false)
const submitMsg = ref('')
const submitOk = ref(false)

interface ValidateIssue {
  line: number
  code: string
  error_code: string
  message: string
}
interface ValidateSummary {
  total_lines: number
  empty_skipped: number
  duplicate_lines: number
  unique_count: number
  valid_count: number
  invalid_count: number
  valid_codes: string[]
  duplicates: ValidateIssue[]
  invalid: ValidateIssue[]
}

const batches = ref<BatchRow[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)

const detailOpen = ref(false)
const detail = ref<{ batch: BatchRow; items: ItemRow[] } | null>(null)
let detailTimer: ReturnType<typeof setInterval> | null = null
let listTimer: ReturnType<typeof setInterval> | null = null

const quota = ref({ max_batch_items: 0, max_concurrent_recharge: 0, in_flight: 0, unused_cdk_count: 0 as number | null })

const formatDate = formatPartnerDate

const cdkLines = computed(() => cdkText.value.split(/\r?\n/))
const cdkLineCount = computed(() => cdkLines.value.filter((l) => l.trim()).length)

const credPlaceholder = computed(() =>
  credMode.value === 'session'
    ? '每行一条 session JSON 或 sessionToken'
    : 'user@example.com----password',
)

const parsedCreds = computed(() => {
  if (credMode.value === 'mailbox') {
    const out: Array<{ email: string; password: string }> = []
    for (const line of credText.value.split(/\r?\n/)) {
      const m = parseMailboxLines(line)
      if (m.length) out.push(m[0])
    }
    return out
  }
  const out: Array<{ session: string }> = []
  for (const line of credText.value.split(/\r?\n/)) {
    const session = extractCdkSession(line)
    if (session) out.push({ session })
  }
  return out
})

const credLineCount = computed(() => parsedCreds.value.length)

const overBatchLimit = computed(
  () =>
    validateSummary.value != null &&
    quota.value.max_batch_items > 0 &&
    validateSummary.value.valid_count > quota.value.max_batch_items,
)

const overConcurrency = computed(
  () =>
    step.value === 2 &&
    quota.value.max_concurrent_recharge > 0 &&
    validatedCodes.value.length > 0 &&
    quota.value.in_flight + validatedCodes.value.length > quota.value.max_concurrent_recharge,
)

const credMismatch = computed(
  () => step.value === 2 && credLineCount.value !== validatedCodes.value.length,
)

const canValidate = computed(
  () => !validating.value && !!plan.value && cdkLineCount.value > 0,
)

const parsedItems = computed(() => {
  if (validatedCodes.value.length === 0 || credMismatch.value) return []
  const creds = parsedCreds.value
  return validatedCodes.value.map((cdk, i) => {
    if (credMode.value === 'mailbox') {
      const m = creds[i] as { email: string; password: string }
      return {
        cdk_code: cdk,
        mode: 'mailbox',
        email: m.email,
        password: m.password,
        email_password: m.password,
      }
    }
    const s = creds[i] as { session: string }
    return { cdk_code: cdk, mode: 'session', session: s.session }
  })
})

const canSubmit = computed(
  () =>
    !submitting.value &&
    step.value === 2 &&
    validatedCodes.value.length > 0 &&
    !credMismatch.value &&
    !overBatchLimit.value &&
    !overConcurrency.value &&
    parsedItems.value.length === validatedCodes.value.length,
)

function onPlanChange() {
  resetValidation()
}

function onCdkInput() {
  if (validateSummary.value) resetValidation(false)
}

function resetValidation(clearMsg = true) {
  validateSummary.value = null
  validatedCodes.value = []
  credText.value = ''
  step.value = 1
  if (clearMsg) {
    validateMsg.value = ''
    submitMsg.value = ''
  }
}

function clearAll() {
  cdkText.value = ''
  credText.value = ''
  resetValidation()
}

function goStep2() {
  if (!validateSummary.value?.valid_count) return
  validatedCodes.value = [...(validateSummary.value.valid_codes || [])]
  credText.value = ''
  step.value = 2
}

async function validateCdks() {
  validating.value = true
  validateMsg.value = ''
  validateSummary.value = null
  validatedCodes.value = []
  step.value = 1
  try {
    const res = await agentFetch('/api/v1/agent/cdk/validate', {
      method: 'POST',
      body: JSON.stringify({ plan: plan.value, codes: cdkLines.value }),
    })
    const d = await res.json()
    if (!res.ok) {
      validateOk.value = false
      validateMsg.value = d.error || '校验失败'
      return
    }
    validateSummary.value = d.summary
    validateOk.value = true
  } catch {
    validateOk.value = false
    validateMsg.value = '网络异常，请稍后重试'
  } finally {
    validating.value = false
  }
}

async function loadPlans() {
  const res = await agentFetch('/api/v1/agent/plans')
  if (!res.ok) return
  const d = await res.json()
  plans.value = d.plans || []
  if (!plan.value && rechargePlans.value.length) plan.value = rechargePlans.value[0].key
}

async function loadQuota() {
  const res = await agentFetch('/api/v1/agent/settings')
  if (!res.ok) return
  const d = await res.json()
  quota.value = {
    max_batch_items: d.max_batch_items || 0,
    max_concurrent_recharge: d.max_concurrent_recharge || 0,
    in_flight: quota.value.in_flight,
    unused_cdk_count: d.unused_cdk_count ?? null,
  }
}

async function loadBatches() {
  loading.value = true
  try {
    const res = await agentFetch(`/api/v1/agent/batch-recharge?page=${page.value}&page_size=${pageSize}`)
    if (!res.ok) return
    const d = await res.json()
    batches.value = d.list || []
    total.value = d.total || 0
    // 在途条数用于提交前的额度提示，服务端仍会独立判定。
    quota.value.in_flight = batches.value.reduce(
      (n, b) => n + (b.total - b.success - b.failed - b.skipped - b.unknown),
      0,
    )
  } finally {
    loading.value = false
  }
}

async function submit() {
  submitting.value = true
  submitMsg.value = ''
  try {
    const res = await agentFetch('/api/v1/agent/batch-recharge', {
      method: 'POST',
      // 幂等键让「提交后网络超时又点一次」不会变成两批扣款
      headers: { 'Idempotency-Key': newIdempotencyKey() },
      body: JSON.stringify({
        plan: plan.value,
        items: parsedItems.value,
      }),
    })
    const d = await res.json()
    if (!res.ok) {
      submitOk.value = false
      submitMsg.value = d.error || '提交失败'
      return
    }
    submitOk.value = true
    submitMsg.value = d.deduped
      ? `检测到重复提交，已返回原批次 ${d.batch_id}`
      : `已受理批次 ${d.batch_id}，共 ${d.total} 条`
    clearAll()
    page.value = 1
    await loadBatches()
    await loadQuota()
  } catch {
    submitOk.value = false
    submitMsg.value = '网络异常，请确认是否已提交后再重试'
  } finally {
    submitting.value = false
  }
}

function newIdempotencyKey() {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) return crypto.randomUUID()
  return `idem-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

async function openDetail(batchID: string) {
  detailOpen.value = true
  await loadDetail(batchID)
  detailTimer = setInterval(() => {
    if (detail.value && detail.value.batch.status === 'running') loadDetail(batchID)
  }, 5000)
}

async function loadDetail(batchID: string) {
  const res = await agentFetch(`/api/v1/agent/batch-recharge/${encodeURIComponent(batchID)}`)
  if (!res.ok) return
  detail.value = await res.json()
}

function closeDetail() {
  detailOpen.value = false
  detail.value = null
  if (detailTimer) {
    clearInterval(detailTimer)
    detailTimer = null
  }
}

async function exportBatch(scope: string) {
  const id = detail.value?.batch.batch_id
  if (!id) return
  const res = await agentFetch(
    `/api/v1/agent/batch-recharge/${encodeURIComponent(id)}/export?scope=${scope}`,
  )
  if (!res.ok) return
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `batch-${id}-${scope}.xlsx`
  a.click()
  URL.revokeObjectURL(url)
}

function onPage(p: number) {
  page.value = p
  loadBatches()
}

onMounted(async () => {
  await Promise.all([loadPlans(), loadQuota(), loadBatches()])
  listTimer = setInterval(() => {
    if (batches.value.some((b) => b.status === 'running')) loadBatches()
  }, 8000)
})

onUnmounted(() => {
  if (listTimer) clearInterval(listTimer)
  if (detailTimer) clearInterval(detailTimer)
})
</script>

<style scoped>
.section-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--ink);
  margin-bottom: 16px;
}
.step-bar {
  display: flex;
  gap: 8px;
  margin-bottom: 16px;
}
.step-pill {
  font-size: 13px;
  padding: 6px 12px;
  border-radius: 999px;
  border: 1px solid var(--brd);
  color: var(--ink-3);
}
.step-pill.step-on {
  border-color: var(--primary);
  background: var(--primary-soft);
  color: var(--primary);
  font-weight: 600;
}
.step-pill.step-done {
  border-color: var(--good);
  color: var(--good);
}
.validate-panel {
  border: 1px solid var(--brd);
  border-radius: var(--radius-sm);
  padding: 14px;
  background: var(--soft);
}
.issue-details summary {
  cursor: pointer;
  font-size: 13px;
  color: var(--ink-2);
}
.issue-list {
  margin: 8px 0 0;
  padding-left: 18px;
  font-size: 12px;
  color: var(--ink-3);
  max-height: 160px;
  overflow-y: auto;
}
.issue-list li { margin-bottom: 4px; }
.mode-switch { display: flex; gap: 8px; }
.mode-switch .btn-secondary { flex: 1; }
.mode-on {
  border-color: var(--primary);
  background: var(--primary-soft);
  color: var(--primary);
}
.parse-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 10px;
  font-size: 13px;
  color: var(--ink-2);
}
.funding-check {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 14px;
  font-size: 14px;
  color: var(--ink);
  cursor: pointer;
}
.table-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 18px;
  border-bottom: 1px solid var(--brd);
  font-size: 13px;
  color: var(--ink-2);
}
.table-foot {
  padding: 14px 18px;
  border-top: 1px solid var(--brd);
}
.stat-row { display: flex; gap: 10px; flex-wrap: wrap; }
.stat-cell {
  flex: 1;
  min-width: 88px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 10px 12px;
  border: 1px solid var(--brd);
  border-radius: var(--radius-sm);
  font-size: 12px;
  color: var(--ink-3);
}
.stat-num { font-size: 20px; font-weight: 700; color: var(--ink); }
.detail-table-wrap { max-height: 340px; overflow-y: auto; }
</style>
