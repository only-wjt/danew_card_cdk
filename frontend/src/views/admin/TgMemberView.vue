<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <div>
        <h2 class="text-xl font-bold text-ink">TG 会员</h2>
        <p class="text-sm text-muted mt-1">只走 Avanfinity CDK，和 X 共用同一套凭证、同一个钱包。客户填 Telegram 用户名。</p>
      </div>
      <el-button @click="load">刷新</el-button>
    </div>

    <div v-if="overview" class="text-sm text-muted">
      今日开通 {{ overview.done_today || 0 }} · 进行中 {{ overview.running || 0 }} · 待处理 {{ overview.todo || 0 }} · 未兑 {{ overview.unused || 0 }}
    </div>
    <div v-if="overview?.blocked" class="rounded-lg border px-3 py-2 text-sm" style="border-color: var(--warn, #f1d39a); background: var(--warn-soft, #fff7ed)">
      {{ overview.blocked }}
    </div>

    <el-radio-group v-model="tab">
      <el-radio-button value="issue">发码</el-radio-button>
      <el-radio-button value="records">兑换记录</el-radio-button>
      <el-radio-button value="limits">上限与告警</el-radio-button>
    </el-radio-group>

    <template v-if="tab === 'issue'">
      <div class="card space-y-4">
        <div class="grid gap-2 sm:grid-cols-3">
          <button
            v-for="p in plans"
            :key="p.key"
            type="button"
            class="rounded-lg border px-3 py-2 text-left"
            :class="issue.plan === p.key ? 'border-current' : ''"
            @click="issue.plan = p.key"
          >
            <div class="flex items-center justify-between gap-2">
              <span class="font-medium">{{ p.label }}</span>
              <span class="text-xs rounded border px-1.5 text-violet-700 border-violet-200 bg-violet-50">Avanfinity</span>
            </div>
            <div class="text-xs text-muted mt-1">
              <button v-if="!limitFilled(p.key)" type="button" class="app-link" @click.stop="tab = 'limits'">还没填上限，去填</button>
              <template v-else>每张最多 ${{ limitOf(p.key)?.max_wallet_debit_usd }}</template>
            </div>
          </button>
        </div>
        <p class="text-sm text-muted">发码不扣钱，兑换时从和 X 共用的钱包出。客户只填 Telegram 用户名。上限在发码时锁死。</p>
        <div class="flex flex-wrap items-center gap-3">
          <span class="text-sm text-muted">数量</span>
          <el-button size="small" :disabled="issue.quantity <= 1" @click="issue.quantity--">−</el-button>
          <input v-model.number="issue.quantity" type="number" min="1" max="200" class="input !w-16 text-center mono" />
          <el-button size="small" :disabled="issue.quantity >= 200" @click="issue.quantity++">+</el-button>
          <el-button v-for="n in [1, 10, 50, 100, 200]" :key="n" size="small" @click="issue.quantity = n">{{ n }}</el-button>
          <el-input v-model="issue.note" size="small" class="!w-48" placeholder="备注，客服可搜" />
          <el-button type="primary" :loading="issuing" :disabled="!!overview?.blocked" @click="doIssue">生成 {{ issue.quantity }} 张 {{ planLabel(issue.plan) }}</el-button>
        </div>
        <div v-if="issued.length" class="rounded-xl bg-soft p-3 space-y-2 border">
          <div class="flex justify-between gap-2">
            <span class="text-sm font-medium">本批 {{ issued.length }} 张</span>
            <el-button size="small" @click="copy(issued.join('\n'))">复制</el-button>
          </div>
          <textarea class="input mono text-sm w-full" readonly :value="issued.join('\n')" />
        </div>
      </div>

      <section class="card space-y-3">
        <div>
          <h2 class="text-lg font-semibold">CDK 列表</h2>
          <p class="text-xs text-muted">共 {{ listTotal }} 条 · 只列 Telegram 的 DNT- 码，不进 GPT 会员</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <el-input v-model="listQ" clearable class="!w-[240px]" placeholder="搜索卡密 / 用户名 / 备注" @keyup.enter="loadList" />
          <el-select v-model="listGroup" class="!w-[130px]" @change="loadList">
            <el-option v-for="g in groups" :key="g.key" :label="g.label" :value="g.key" />
          </el-select>
          <el-button type="primary" :loading="loadingList" @click="loadList">查询</el-button>
        </div>
        <el-table :data="listRows" v-loading="loadingList" size="small" empty-text="暂无数据">
          <el-table-column label="ID" width="72" prop="code_id" />
          <el-table-column label="卡密" min-width="220">
            <template #default="{ row }"><span class="mono text-xs break-all">{{ row.code }}</span></template>
          </el-table-column>
          <el-table-column label="套餐" min-width="140">
            <template #default="{ row }">{{ row.plan_label || planLabel(row.plan) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="110">
            <template #default="{ row }"><el-tag size="small">{{ statusName(row.status) }}</el-tag></template>
          </el-table-column>
          <el-table-column label="开通给" min-width="120">
            <template #default="{ row }">{{ row.recipient ? '@' + row.recipient : '—' }}</template>
          </el-table-column>
          <el-table-column label="备注" min-width="100" prop="note" />
          <el-table-column label="时间" min-width="150" prop="created_at" />
          <el-table-column label="" width="120">
            <template #default="{ row }">
              <el-button link type="primary" size="small" @click="openRecord(row)">详情</el-button>
              <el-button v-if="row.status === 'unused'" link type="danger" size="small" @click="disable(row)">作废</el-button>
            </template>
          </el-table-column>
        </el-table>
      </section>
    </template>

    <div v-else-if="tab === 'records'" class="space-y-3">
      <div class="card !py-3 flex flex-wrap items-center gap-2 text-sm">
        <span class="text-muted">共 <b>{{ recTotal }}</b> 笔</span>
        <el-select v-model="recGroup" style="width: 140px" @change="loadRecords">
          <el-option v-for="g in groups" :key="g.key" :label="g.label" :value="g.key" />
        </el-select>
        <el-input v-model="recQ" clearable class="!w-[240px]" placeholder="卡密 / 用户名 / 备注" @keyup.enter="loadRecords" />
        <el-button type="primary" :loading="loadingRecords" @click="loadRecords">查询</el-button>
      </div>
      <div class="card overflow-hidden !p-0">
        <el-table :data="records" v-loading="loadingRecords" size="small" empty-text="暂无兑换记录">
          <el-table-column label="记录" width="72">
            <template #default="{ row }">#{{ row.code_id }}</template>
          </el-table-column>
          <el-table-column label="卡密" min-width="180">
            <template #default="{ row }">
              <div class="mono text-xs">{{ row.code }}</div>
              <div class="text-xs text-muted">TG CDK</div>
            </template>
          </el-table-column>
          <el-table-column label="套餐" min-width="140">
            <template #default="{ row }">{{ row.plan_label }}</template>
          </el-table-column>
          <el-table-column label="开通给" min-width="120">
            <template #default="{ row }">{{ row.recipient ? '@' + row.recipient : '—' }}</template>
          </el-table-column>
          <el-table-column label="金额" width="140">
            <template #default="{ row }">{{ money(row) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="110">
            <template #default="{ row }"><el-tag size="small">{{ row.status === 'completed' ? '完成' : statusName(row.status) }}</el-tag></template>
          </el-table-column>
          <el-table-column label="时间" min-width="150" prop="created_at" />
          <el-table-column label="操作" width="80">
            <template #default="{ row }"><el-button link type="primary" size="small" @click="openRecord(row)">详情</el-button></template>
          </el-table-column>
        </el-table>
      </div>
    </div>

    <div v-else class="card space-y-3">
      <div class="font-semibold">每个套餐的花费上限</div>
      <p class="text-xs text-muted">发码时锁进这张 DNT- 码。改这里不影响已经发出去的码。币种用小写，官方金额用最小单位（例如 30000 表示 300.00）。</p>
      <table class="data-table">
        <thead>
          <tr><th>套餐</th><th>启用</th><th>币种</th><th>官方金额上限</th><th>钱包上限（美元）</th><th>注资（美元）</th></tr>
        </thead>
        <tbody>
          <tr v-for="row in limits" :key="row.plan">
            <td>{{ planLabel(row.plan) }}</td>
            <td><el-switch v-model="row.enabled" /></td>
            <td><el-input v-model="row.currency" class="!w-20" /></td>
            <td><el-input v-model.number="row.max_official_amount_minor" class="!w-28" /></td>
            <td><el-input v-model="row.max_wallet_debit_usd" class="!w-24" /></td>
            <td><el-input v-model="row.funding_amount_usd" class="!w-24" /></td>
          </tr>
        </tbody>
      </table>
      <el-button type="primary" :loading="saving" @click="saveLimits">保存上限</el-button>
    </div>

    <el-drawer v-model="detailOpen" title="兑换详情" size="420px">
      <div v-if="selected" class="space-y-4 text-sm">
        <div class="flex items-center gap-2">
          <el-tag>{{ selected.status === 'completed' ? '已开通' : statusName(selected.status) }}</el-tag>
          <span class="text-muted">TG CDK</span>
        </div>
        <p v-if="selected.message" class="rounded-lg p-3" style="background: var(--warn-soft, #fff7ed)">{{ selected.message }}</p>
        <el-descriptions :column="1" border size="small">
          <el-descriptions-item label="卡密"><span class="mono text-xs break-all">{{ selected.code }}</span></el-descriptions-item>
          <el-descriptions-item label="套餐">{{ selected.plan_label || planLabel(selected.plan) }}</el-descriptions-item>
          <el-descriptions-item label="开通给">{{ selected.recipient ? '@' + selected.recipient : '—' }}</el-descriptions-item>
          <el-descriptions-item label="官方金额">{{ money(selected) }}</el-descriptions-item>
          <el-descriptions-item label="时间">{{ selected.created_at || '—' }}</el-descriptions-item>
        </el-descriptions>
        <div>
          <div class="font-medium mb-2">处理过程</div>
          <div v-for="(ev, i) in eventsOf(selected)" :key="i" class="text-muted">{{ ev.t }} · {{ ev.s }}</div>
          <p v-if="!eventsOf(selected).length" class="text-muted">还没有过程记录。</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <el-button v-if="selected.redemption_id" size="small" @click="requery(selected)">重新查询</el-button>
          <el-button v-if="selected.group === 'todo' && selected.redemption_id" size="small" @click="resolve(selected, 'completed')">确认已开通</el-button>
          <el-button v-if="selected.group === 'todo' && selected.redemption_id" size="small" @click="resolve(selected, 'release')">退回重提</el-button>
          <el-button v-if="selected.status === 'unused'" size="small" @click="disable(selected)">作废</el-button>
        </div>
      </div>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { dialog } from '../../lib/dialog'
import { authFetch } from '../../lib/api'

const plans = [
  { key: 'premium_3m', label: 'Premium · 3 个月' },
  { key: 'premium_6m', label: 'Premium · 6 个月' },
  { key: 'premium_12m', label: 'Premium · 12 个月' },
]
const groups = [
  { key: 'all', label: '全部' },
  { key: 'todo', label: '待处理' },
  { key: 'running', label: '进行中' },
  { key: 'done', label: '已完成' },
  { key: 'unused', label: '未使用' },
  { key: 'failed', label: '失败已退回' },
]
const names: Record<string, string> = {
  unused: '未使用', quoted: '待确认', funding: '注资中', funded: '已注资', paying: '付款中',
  paid_pending_delivery: '等待到账', completed: '已开通', uncertain: '待处理', disabled: '已作废',
  review_required: '待处理', requires_action: '待处理',
}

const tab = ref('issue')
const overview = ref<any>(null)
const limits = ref<any[]>([])
const issue = reactive({ plan: 'premium_3m', quantity: 1, note: '' })
const issuing = ref(false)
const issued = ref<string[]>([])
const listRows = ref<any[]>([])
const listTotal = ref(0)
const listQ = ref('')
const listGroup = ref('all')
const loadingList = ref(false)
const records = ref<any[]>([])
const recTotal = ref(0)
const recQ = ref('')
const recGroup = ref('all')
const loadingRecords = ref(false)
const saving = ref(false)
const detailOpen = ref(false)
const selected = ref<any>(null)

function planLabel(k: string) { return plans.find((p) => p.key === k)?.label || k }
function statusName(s: string) { return names[s] || s }
function limitOf(plan: string) { return limits.value.find((r) => r.plan === plan) }
function limitFilled(plan: string) {
  const row = limitOf(plan)
  return !!row && row.enabled && Number(row.max_official_amount_minor) > 0 && row.max_wallet_debit_usd && row.funding_amount_usd && row.currency
}
function money(row: any) {
  if (!row?.amount_minor) return '—'
  const cur = String(row.currency || '').toUpperCase()
  return `${Number(row.amount_minor).toLocaleString('en-US')} ${cur}`
}
function eventsOf(row: any) {
  try {
    const raw = JSON.parse(row.events_json || '[]')
    return Array.isArray(raw) ? raw : []
  } catch {
    return []
  }
}
async function copy(text: string) {
  try { await navigator.clipboard.writeText(text); dialog.toast('已复制', 'ok') } catch { dialog.toast('复制失败', 'err') }
}

async function loadLimits() {
  const r = await authFetch('/api/v1/admin/tg/plan-limits')
  const d = await r.json().catch(() => ({}))
  limits.value = d.limits || []
}
async function loadOverview() {
  const r = await authFetch('/api/v1/admin/tg/overview')
  overview.value = await r.json().catch(() => null)
}
async function loadList() {
  loadingList.value = true
  try {
    const q = new URLSearchParams({ group: listGroup.value, q: listQ.value })
    const r = await authFetch('/api/v1/admin/tg/records?' + q)
    const d = await r.json().catch(() => ({}))
    listRows.value = d.records || []
    listTotal.value = d.total || 0
  } finally {
    loadingList.value = false
  }
}
async function loadRecords() {
  loadingRecords.value = true
  try {
    const q = new URLSearchParams({ group: recGroup.value, q: recQ.value })
    const r = await authFetch('/api/v1/admin/tg/records?' + q)
    const d = await r.json().catch(() => ({}))
    records.value = d.records || []
    recTotal.value = d.total || 0
  } finally {
    loadingRecords.value = false
  }
}
async function load() {
  await Promise.all([loadOverview(), loadLimits(), loadList(), loadRecords()])
}
async function doIssue() {
  issuing.value = true
  try {
    const r = await authFetch('/api/v1/admin/tg/issue', {
      method: 'POST',
      body: JSON.stringify({ plan: issue.plan, quantity: issue.quantity, note: issue.note }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '发码失败', 'err')
      return
    }
    issued.value = d.codes || []
    dialog.toast(`已生成 ${issued.value.length} 张`, 'ok')
    await load()
  } finally {
    issuing.value = false
  }
}
async function saveLimits() {
  saving.value = true
  try {
    const r = await authFetch('/api/v1/admin/tg/plan-limits', { method: 'PUT', body: JSON.stringify({ limits: limits.value }) })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) { dialog.toast(d.error || '保存失败', 'err'); return }
    dialog.toast('已保存', 'ok')
  } finally {
    saving.value = false
  }
}
function openRecord(row: any) {
  selected.value = row
  detailOpen.value = true
}
async function requery(row: any) {
  const r = await authFetch('/api/v1/admin/tg/records/' + row.redemption_id + '/requery', { method: 'POST' })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) { dialog.toast(d.error || '查询失败', 'err'); return }
  dialog.toast('已重新查询', 'ok')
  await load()
  detailOpen.value = false
}
async function resolve(row: any, outcome: string) {
  const note = outcome === 'completed' ? '人工确认已开通' : '人工退回重提'
  const r = await authFetch('/api/v1/admin/tg/records/' + row.redemption_id + '/resolve', {
    method: 'POST',
    body: JSON.stringify({ outcome, note }),
  })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) { dialog.toast(d.error || '处理失败', 'err'); return }
  dialog.toast('已处理', 'ok')
  detailOpen.value = false
  await load()
}
async function disable(row: any) {
  const ok = await dialog.confirm('作废后客户不能再使用这张码。只有未使用的码可以作废。', { title: '作废卡密', okText: '作废' })
  if (!ok) return
  const r = await authFetch('/api/v1/admin/tg/codes/' + row.code_id + '/disable', { method: 'POST' })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) { dialog.toast(d.error || '作废失败', 'err'); return }
  dialog.toast('已作废', 'ok')
  detailOpen.value = false
  await load()
}

onMounted(load)
</script>
