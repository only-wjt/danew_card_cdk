<template>
  <div class="space-y-5 pb-2">
    <div class="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
      <div>
        <h1 class="text-3xl font-bold text-ink">代理购卡订单</h1>
        <p class="text-sm text-muted mt-2">易支付到账后自动发码入库；「待处理」可一键重试发货。</p>
      </div>
      <button class="btn-secondary" :disabled="loading" @click="load">刷新</button>
    </div>

    <div class="card filter-card">
      <div class="filter-grid">
        <div class="form-group !mb-0">
          <label>订单号</label>
          <input v-model="filters.order_no" class="input mono" placeholder="AG…" @keyup.enter="search" />
        </div>
        <div class="form-group !mb-0">
          <label>代理</label>
          <select v-model="filters.agent_id" class="field-select">
            <option value="">全部代理</option>
            <option v-for="a in agents" :key="a.id" :value="String(a.id)">{{ a.username }}</option>
          </select>
        </div>
        <div class="form-group !mb-0">
          <label>状态</label>
          <select v-model="filters.status" class="field-select">
            <option value="">全部</option>
            <option v-for="s in statusOptions" :key="s.value" :value="s.value">{{ s.label }}</option>
          </select>
        </div>
        <div class="filter-actions">
          <button class="btn-primary" :disabled="loading" @click="search">查询</button>
          <button class="btn-secondary" @click="resetFilters">重置</button>
        </div>
      </div>
    </div>

    <div class="card overflow-hidden !p-0">
      <div class="overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>订单号</th>
              <th>代理</th>
              <th>套餐</th>
              <th>数量</th>
              <th>金额</th>
              <th>状态</th>
              <th>时间</th>
              <th class="text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td colspan="8" class="py-10 text-center text-muted">加载中…</td>
            </tr>
            <tr v-else-if="!rows.length">
              <td colspan="8" class="py-10 text-center text-muted">暂无订单</td>
            </tr>
            <tr v-for="o in rows" :key="o.order_no">
              <td class="mono text-xs">{{ o.order_no }}</td>
              <td class="font-medium">{{ o.agent_username || '—' }}</td>
              <td>
                <span>{{ o.plan_label || o.plan }}</span>
                <span class="block text-xs text-muted mono">{{ o.plan }}</span>
              </td>
              <td class="mono">{{ o.issued_count || 0 }}/{{ o.count }}</td>
              <td class="mono font-semibold">¥{{ o.total_amount_yuan }}</td>
              <td>
                <span :class="statusClass(o.status)">{{ statusLabel(o.status) }}</span>
                <span v-if="o.fail_reason" class="block text-xs text-err mt-1 max-w-[200px] truncate" :title="o.fail_reason">{{ o.fail_reason }}</span>
              </td>
              <td class="text-sm text-muted whitespace-nowrap">{{ formatDate(o.created_at) }}</td>
              <td class="text-right whitespace-nowrap">
                <button
                  v-if="o.status === 'paid_undelivered' || o.status === 'paid' || o.status === 'issuing'"
                  class="btn-ghost !px-2.5 !py-1 text-sm text-primary"
                  :disabled="retrying === o.order_no"
                  @click="retry(o)"
                >
                  {{ retrying === o.order_no ? '重试中…' : '重试发货' }}
                </button>
                <button v-if="o.issued_codes?.length" class="btn-ghost !px-2.5 !py-1 text-sm" @click="showCodes(o)">卡密</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="table-foot">
        <Pagination :page="page" :page-size="pageSize" :total="total" @update:page="onPage" />
      </div>
    </div>

    <Modal :open="codesOpen" title="已发码" wide @close="codesOpen = false">
      <textarea class="input mono text-xs" rows="12" readonly :value="(codeList || []).join('\n')" />
      <template #footer>
        <button class="btn-secondary" @click="codesOpen = false">关闭</button>
      </template>
    </Modal>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { authFetch } from '../../lib/api'
import { dialog } from '../../lib/dialog'
import Modal from '../../components/Modal.vue'
import Pagination from '../../components/Pagination.vue'

interface AgentOption {
  id: number
  username: string
}

interface OrderRow {
  order_no: string
  agent_username?: string
  plan: string
  plan_label?: string
  count: number
  issued_count?: number
  total_amount_yuan: string
  status: string
  fail_reason?: string
  issued_codes?: string[]
  created_at: string
}

const rows = ref<OrderRow[]>([])
const agents = ref<AgentOption[]>([])
const loading = ref(false)
const retrying = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const filters = reactive({ order_no: '', status: '', agent_id: '' })
const codesOpen = ref(false)
const codeList = ref<string[]>([])

const statusOptions = [
  { value: 'pending_pay', label: '待支付' },
  { value: 'delivered', label: '已发货' },
  { value: 'paid_undelivered', label: '待处理' },
  { value: 'paid', label: '已支付' },
  { value: 'issuing', label: '发货中' },
  { value: 'expired', label: '已过期' },
]

function formatDate(v: string) {
  return v ? new Date(v).toLocaleString('zh-CN') : '—'
}

function statusLabel(s: string) {
  const m: Record<string, string> = {
    pending_pay: '待支付',
    paid: '已支付',
    issuing: '发货中',
    delivered: '已发货',
    paid_undelivered: '待处理',
    expired: '已过期',
  }
  return m[s] || s
}

function statusClass(s: string) {
  if (s === 'delivered') return 'pill pill-good'
  if (s === 'paid_undelivered') return 'pill pill-warn'
  if (s === 'pending_pay') return 'pill pill-info'
  return 'pill'
}

async function load() {
  loading.value = true
  try {
    const params = new URLSearchParams({ page: String(page.value), page_size: String(pageSize) })
    if (filters.status) params.set('status', filters.status)
    if (filters.order_no.trim()) params.set('order_no', filters.order_no.trim())
    if (filters.agent_id) params.set('agent_id', filters.agent_id)
    const res = await authFetch(`/api/v1/admin/agent-orders?${params}`)
    const d = await res.json()
    if (!res.ok) {
      dialog.toast(d.error || '加载失败', 'err')
      return
    }
    rows.value = d.list || []
    total.value = d.total || 0
  } finally {
    loading.value = false
  }
}

function search() {
  page.value = 1
  load()
}

function resetFilters() {
  filters.order_no = ''
  filters.status = ''
  filters.agent_id = ''
  search()
}

async function loadAgents() {
  const res = await authFetch('/api/v1/admin/agents')
  if (!res.ok) return
  const d = await res.json()
  agents.value = (d.list || []).map((a: any) => ({ id: a.id, username: a.username }))
}

function onPage(p: number) {
  page.value = p
  load()
}

async function retry(o: OrderRow) {
  retrying.value = o.order_no
  try {
    const res = await authFetch(`/api/v1/admin/agent-orders/${encodeURIComponent(o.order_no)}/retry`, { method: 'POST' })
    const d = await res.json()
    if (!res.ok) {
      dialog.toast(d.error || '重试失败', 'err')
      return
    }
    dialog.toast('已重试', 'ok')
    await load()
  } finally {
    retrying.value = ''
  }
}

function showCodes(o: OrderRow) {
  codeList.value = o.issued_codes || []
  codesOpen.value = true
}

onMounted(async () => {
  await loadAgents()
  await load()
})
</script>

<style scoped>
.filter-card { padding: 16px 20px; }
.filter-grid {
  display: grid;
  gap: 12px;
  grid-template-columns: 1fr 160px 160px auto;
  align-items: end;
}
@media (max-width: 768px) {
  .filter-grid { grid-template-columns: 1fr; }
}
.filter-actions { display: flex; gap: 8px; }
.table-foot { padding: 12px 16px; border-top: 1px solid var(--brd); }
</style>
