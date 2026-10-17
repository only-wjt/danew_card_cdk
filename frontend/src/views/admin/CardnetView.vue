<template>
  <div class="space-y-5 pb-2">
    <div class="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
      <div>
        <h1 class="text-3xl font-bold text-ink">卡网对接</h1>
        <p class="text-sm text-muted mt-2">卡网付款后用 cn_live_ Key 调本站取码，码由主台发出并落本站库；本页管理客户端、发码请求与服务费记账。</p>
      </div>
      <div class="flex gap-2">
        <button v-for="t in tabs" :key="t.key" :class="tab === t.key ? 'btn-primary' : 'btn-secondary'" @click="switchTab(t.key)">
          {{ t.label }}
        </button>
      </div>
    </div>

    <!-- 客户端 -->
    <template v-if="tab === 'clients'">
      <div class="flex justify-end">
        <button class="btn-primary" @click="openClient()">新建客户端</button>
      </div>
      <div class="card overflow-hidden !p-0">
        <div class="overflow-x-auto">
          <table class="data-table">
            <thead>
              <tr>
                <th>名称</th>
                <th>Key</th>
                <th>允许套餐</th>
                <th>允许地区</th>
                <th>今日 / 额度</th>
                <th>IP 白名单</th>
                <th>状态</th>
                <th>最近调用</th>
                <th class="text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="!clients.length">
                <td colspan="9" class="py-10 text-center text-muted">{{ loading ? '加载中…' : '暂无客户端' }}</td>
              </tr>
              <tr v-for="row in clients" :key="row.client.id">
                <td class="font-medium">{{ row.client.name }}</td>
                <td class="mono text-xs">{{ row.client.key_prefix }}…</td>
                <td class="text-sm">{{ row.client.allowed_plans.join(', ') || '—' }}</td>
                <td class="text-sm">{{ row.client.allowed_countries.join(', ') || 'PH（默认）' }}</td>
                <td class="mono">{{ row.issued_today }} / {{ row.client.daily_code_limit }}</td>
                <td class="text-sm">{{ row.client.ip_allowlist.join(', ') || '不限' }}</td>
                <td>
                  <span :class="row.client.status === 'active' ? 'pill pill-good' : 'pill'">
                    {{ row.client.status === 'active' ? '启用' : '停用' }}
                  </span>
                </td>
                <td class="text-sm text-muted whitespace-nowrap">{{ formatDate(row.client.last_used_at) }}</td>
                <td class="text-right whitespace-nowrap">
                  <button class="btn-ghost !px-2.5 !py-1 text-sm" @click="openClient(row.client)">编辑</button>
                  <button class="btn-ghost !px-2.5 !py-1 text-sm" @click="toggleClient(row.client)">
                    {{ row.client.status === 'active' ? '停用' : '启用' }}
                  </button>
                  <button class="btn-ghost !px-2.5 !py-1 text-sm text-err" @click="rotateKey(row.client)">轮换 Key</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>

    <!-- 发码请求 -->
    <template v-if="tab === 'requests'">
      <div class="card filter-card">
        <div class="filter-grid">
          <div class="form-group !mb-0">
            <label>请求号 / 卡网订单号 / 幂等键</label>
            <input v-model="reqFilters.q" class="input mono" placeholder="CNR… / DJ…" @keyup.enter="searchRequests" />
          </div>
          <div class="form-group !mb-0">
            <label>客户端</label>
            <select v-model="reqFilters.client_id" class="field-select">
              <option value="">全部</option>
              <option v-for="row in clients" :key="row.client.id" :value="String(row.client.id)">{{ row.client.name }}</option>
            </select>
          </div>
          <div class="form-group !mb-0">
            <label>状态</label>
            <select v-model="reqFilters.status" class="field-select">
              <option value="">全部</option>
              <option v-for="(label, key) in requestStatusLabels" :key="key" :value="key">{{ label }}</option>
            </select>
          </div>
          <div class="filter-actions">
            <button class="btn-primary" :disabled="loading" @click="searchRequests">查询</button>
          </div>
        </div>
      </div>
      <div class="card overflow-hidden !p-0">
        <div class="overflow-x-auto">
          <table class="data-table">
            <thead>
              <tr>
                <th>请求号</th>
                <th>客户端</th>
                <th>卡网订单</th>
                <th>套餐 / 地区</th>
                <th>张数</th>
                <th>服务费</th>
                <th>状态</th>
                <th>时间</th>
                <th class="text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="!requests.length">
                <td colspan="9" class="py-10 text-center text-muted">{{ loading ? '加载中…' : '暂无请求' }}</td>
              </tr>
              <tr v-for="r in requests" :key="r.id">
                <td class="mono text-xs">{{ r.request_id }}</td>
                <td>{{ r.client_name || r.client_id }}</td>
                <td class="mono text-xs">
                  {{ r.external_order_no || '—' }}
                  <span v-if="r.external_ref" class="block text-muted">{{ r.external_ref }}</span>
                </td>
                <td>{{ r.plan }} <span class="text-muted">{{ r.payment_country || 'PH(默认)' }}</span></td>
                <td class="mono">{{ r.count }}</td>
                <td class="mono">{{ usd(r.fee_total_minor) }}</td>
                <td>
                  <span :class="requestStatusClass(r.status)">{{ requestStatusLabels[r.status] || r.status }}</span>
                  <span v-if="r.error_message" class="block text-xs text-err mt-1 max-w-[220px] truncate" :title="r.error_message">
                    {{ r.error_code }} {{ r.error_message }}
                  </span>
                </td>
                <td class="text-sm text-muted whitespace-nowrap">{{ formatDate(r.created_at) }}</td>
                <td class="text-right whitespace-nowrap">
                  <button class="btn-ghost !px-2.5 !py-1 text-sm" @click="showRequest(r)">详情</button>
                  <template v-if="r.status === 'review' || r.status === 'pending'">
                    <button class="btn-ghost !px-2.5 !py-1 text-sm text-primary" :disabled="acting === r.id" @click="refetch(r)">重新拉取</button>
                    <button class="btn-ghost !px-2.5 !py-1 text-sm text-err" :disabled="acting === r.id" @click="markFailed(r)">标记失败</button>
                  </template>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="table-foot">
          <Pagination :page="reqPage" :page-size="reqPageSize" :total="reqTotal" @update:page="onReqPage" />
        </div>
      </div>
    </template>

    <!-- 统计 -->
    <template v-if="tab === 'stats'">
      <div class="card filter-card">
        <div class="filter-grid">
          <div class="form-group !mb-0">
            <label>客户端</label>
            <select v-model="statFilters.client_id" class="field-select">
              <option value="">全部</option>
              <option v-for="row in clients" :key="row.client.id" :value="String(row.client.id)">{{ row.client.name }}</option>
            </select>
          </div>
          <div class="form-group !mb-0">
            <label>开始（北京时间）</label>
            <input v-model="statFilters.from" type="date" class="input" />
          </div>
          <div class="form-group !mb-0">
            <label>结束</label>
            <input v-model="statFilters.to" type="date" class="input" />
          </div>
          <div class="filter-actions">
            <button class="btn-primary" :disabled="loading" @click="loadStats">查询</button>
          </div>
        </div>
      </div>
      <div class="grid gap-4 md:grid-cols-4">
        <div class="card"><div class="text-sm text-muted">发码张数</div><div class="text-2xl font-bold mono">{{ statTotal.codes }}</div></div>
        <div class="card"><div class="text-sm text-muted">上游服务费</div><div class="text-2xl font-bold mono">{{ usd(statTotal.fee_minor) }}</div></div>
        <div class="card"><div class="text-sm text-muted">已兑换</div><div class="text-2xl font-bold mono">{{ statTotal.consumed }}</div></div>
        <div class="card"><div class="text-sm text-muted">已作废</div><div class="text-2xl font-bold mono">{{ statTotal.disabled }}</div></div>
      </div>
      <div class="card overflow-hidden !p-0">
        <div class="overflow-x-auto">
          <table class="data-table">
            <thead>
              <tr>
                <th>日期</th>
                <th>客户端</th>
                <th>套餐</th>
                <th>张数</th>
                <th>服务费</th>
                <th>已兑换</th>
                <th>已作废</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="!statRows.length">
                <td colspan="7" class="py-10 text-center text-muted">{{ loading ? '加载中…' : '暂无数据' }}</td>
              </tr>
              <tr v-for="s in statRows" :key="`${s.date}-${s.client_id}-${s.plan}`">
                <td class="mono">{{ s.date }}</td>
                <td>{{ clientName(s.client_id) }}</td>
                <td>{{ s.plan }}</td>
                <td class="mono">{{ s.codes }}</td>
                <td class="mono">{{ usd(s.fee_minor) }}</td>
                <td class="mono">{{ s.consumed }}</td>
                <td class="mono">{{ s.disabled }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>

    <Modal :open="clientOpen" :title="editing.id ? '编辑客户端' : '新建客户端'" @close="clientOpen = false">
      <div class="space-y-3">
        <div class="form-group">
          <label>名称</label>
          <input v-model="editing.name" class="input" placeholder="例如 card.danew.cc" />
        </div>
        <div class="form-group">
          <label>允许套餐（不勾选则不能发任何码）</label>
          <div v-if="optionsLoading" class="text-xs text-muted">正在读取卡台套餐…</div>
          <template v-else>
            <div v-if="planChoices.length" class="check-list">
              <label
                v-for="p in planChoices"
                :key="p.key"
                class="check-tile"
                :class="{ 'is-checked': editing.plans.includes(p.key) }"
              >
                <input type="checkbox" :value="p.key" v-model="editing.plans" />
                <span class="flex-1">
                  <span class="font-medium text-ink">{{ p.label || p.key }}</span>
                  <span class="block text-xs text-muted mono">
                    {{ p.key }}<template v-if="p.stale"> · 卡台已停售</template><template v-else> · {{ usd(p.service_fee_usd_minor) }}/张</template>
                  </span>
                </span>
              </label>
            </div>
            <p v-if="optionsMessage" class="text-xs text-err mt-1">{{ optionsMessage }}</p>
          </template>
        </div>
        <div class="form-group">
          <label>允许地区（不勾选则只允许默认地区 PH）</label>
          <div v-if="!optionsLoading" class="check-list">
            <label
              v-for="r in regionChoices"
              :key="r.country"
              class="check-tile"
              :class="{ 'is-checked': editing.countries.includes(r.country) }"
            >
              <input type="checkbox" :value="r.country" v-model="editing.countries" />
              <span class="flex-1">
                <span class="font-medium text-ink">{{ regionLabel(r.country) }}</span>
                <span class="block text-xs text-muted mono">
                  {{ r.country }}<template v-if="r.currency"> · {{ r.currency }}</template><template v-if="r.stale"> · 卡台已不支持</template>
                </span>
              </span>
            </label>
          </div>
        </div>
        <div class="form-group">
          <label>每日额度（北京时间自然日，0 = 禁止发码）</label>
          <input v-model.number="editing.limit" type="number" min="0" class="input mono" />
        </div>
        <div class="form-group">
          <label>IP 白名单（逗号分隔，留空不限制）</label>
          <input v-model="editing.ips" class="input mono" placeholder="卡网服务器出口 IP" />
        </div>
      </div>
      <template #footer>
        <button class="btn-secondary" @click="clientOpen = false">取消</button>
        <button class="btn-primary" :disabled="saving" @click="saveClient">保存</button>
      </template>
    </Modal>

    <Modal :open="!!newKey" title="API Key（只显示这一次）" @close="newKey = ''">
      <p class="text-sm text-muted mb-3">请立即复制并配置到卡网后台。关闭后无法再次查看，丢失只能轮换。</p>
      <textarea class="input mono text-xs" rows="3" readonly :value="newKey" />
      <template #footer>
        <button class="btn-secondary" @click="copyKey">复制</button>
        <button class="btn-primary" @click="newKey = ''">我已保存</button>
      </template>
    </Modal>

    <Modal :open="detailOpen" title="发码请求详情" wide @close="detailOpen = false">
      <div v-if="detail" class="space-y-3 text-sm">
        <div class="grid gap-2 md:grid-cols-2">
          <div><span class="text-muted">请求号：</span><span class="mono">{{ detail.request.request_id }}</span></div>
          <div><span class="text-muted">幂等键：</span><span class="mono">{{ detail.request.idempotency_key }}</span></div>
          <div><span class="text-muted">卡网订单：</span><span class="mono">{{ detail.request.external_order_no || '—' }}</span></div>
          <div><span class="text-muted">发码卡台：</span><span class="mono">#{{ detail.request.account_id }}</span></div>
          <div><span class="text-muted">调用次数：</span>{{ detail.request.attempts }}</div>
          <div><span class="text-muted">状态：</span>{{ requestStatusLabels[detail.request.status] || detail.request.status }}</div>
        </div>
        <p v-if="detail.request.error_message" class="text-err">{{ detail.request.error_code }} {{ detail.request.error_message }}</p>
        <table class="data-table">
          <thead>
            <tr><th>ID</th><th>卡密</th><th>套餐</th><th>地区</th><th>服务费</th><th>状态</th></tr>
          </thead>
          <tbody>
            <tr v-if="!detail.codes.length"><td colspan="6" class="py-6 text-center text-muted">未落库任何码</td></tr>
            <tr v-for="c in detail.codes" :key="c.id">
              <td class="mono">{{ c.id }}</td>
              <td class="mono text-xs">{{ c.code }}</td>
              <td>{{ c.plan }}</td>
              <td>{{ c.payment_country || '—' }}</td>
              <td class="mono">{{ usd(c.fee_amount_minor) }}</td>
              <td>{{ c.status || 'unused' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <template #footer>
        <button class="btn-secondary" @click="detailOpen = false">关闭</button>
      </template>
    </Modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { authFetch } from '../../lib/api'
import { dialog } from '../../lib/dialog'
import { copyToClipboard } from '../../lib/clipboard'
import { regionLabel } from '../../lib/region'
import Modal from '../../components/Modal.vue'
import Pagination from '../../components/Pagination.vue'

interface CardnetClient {
  id: number
  name: string
  key_prefix: string
  status: string
  allowed_plans: string[]
  allowed_countries: string[]
  daily_code_limit: number
  ip_allowlist: string[]
  last_used_at?: string
}

interface ClientRow {
  client: CardnetClient
  issued_today: number
}

interface IssueRequest {
  id: number
  request_id: string
  client_id: number
  client_name?: string
  idempotency_key: string
  plan: string
  payment_country: string
  count: number
  account_id: number
  external_order_no: string
  external_ref: string
  status: string
  fee_total_minor: number
  error_code?: string
  error_message?: string
  attempts: number
  created_at: string
}

interface IssuedCode {
  id: number
  code: string
  plan: string
  payment_country: string
  fee_amount_minor: number
  status: string
}

interface StatRow {
  date: string
  client_id: number
  plan: string
  codes: number
  fee_minor: number
  consumed: number
  disabled: number
}

const tabs = [
  { key: 'clients', label: '客户端' },
  { key: 'requests', label: '发码请求' },
  { key: 'stats', label: '统计' },
] as const
type TabKey = (typeof tabs)[number]['key']

const tab = ref<TabKey>('clients')
const loading = ref(false)
const clients = ref<ClientRow[]>([])

const requestStatusLabels: Record<string, string> = {
  pending: '处理中',
  succeeded: '成功',
  failed: '失败',
  review: '待核对',
}

const requests = ref<IssueRequest[]>([])
const reqFilters = reactive({ q: '', client_id: '', status: '' })
const reqPage = ref(1)
const reqPageSize = 20
const reqTotal = ref(0)
const acting = ref(0)
const detailOpen = ref(false)
const detail = ref<{ request: IssueRequest; codes: IssuedCode[] } | null>(null)

const statFilters = reactive({ client_id: '', from: '', to: '' })
const statRows = ref<StatRow[]>([])
const statTotal = ref({ codes: 0, consumed: 0, disabled: 0, fee_minor: 0 })

const clientOpen = ref(false)
const saving = ref(false)
const editing = reactive({ id: 0, name: '', plans: [] as string[], countries: [] as string[], limit: 200, ips: '' })

// 表单勾选项：与卡网发码同源（主台可售档位与地区）。已授权但卡台已下架的项也列出来，方便取消勾选。
interface PlanOption { key: string; label: string; service_fee_usd_minor: number; stale?: boolean }
interface RegionOption { country: string; currency: string; stale?: boolean }
const planOptions = ref<PlanOption[]>([])
const regionOptions = ref<RegionOption[]>([])
const optionsLoading = ref(false)
const optionsMessage = ref('')

const planChoices = computed<PlanOption[]>(() => {
  const known = new Set(planOptions.value.map((p) => p.key))
  const stale = editing.plans
    .filter((key) => !known.has(key))
    .map((key) => ({ key, label: key, service_fee_usd_minor: 0, stale: true }))
  return [...planOptions.value, ...stale]
})

const regionChoices = computed<RegionOption[]>(() => {
  const list: RegionOption[] = [...regionOptions.value]
  // 卡台未下发地区清单时至少给出默认地区 PH。
  if (!list.some((r) => r.country === 'PH')) list.unshift({ country: 'PH', currency: 'PHP' })
  for (const code of editing.countries) {
    if (!list.some((r) => r.country === code)) list.push({ country: code, currency: '', stale: true })
  }
  return list
})

async function loadOptions() {
  optionsLoading.value = true
  optionsMessage.value = ''
  try {
    const d = await api('/api/v1/admin/cardnet/options')
    planOptions.value = d.plans || []
    regionOptions.value = d.payment_regions || []
    optionsMessage.value = d.message || (planOptions.value.length ? '' : '卡台没有可售套餐')
  } catch (e: any) {
    planOptions.value = []
    regionOptions.value = []
    optionsMessage.value = e.message
  } finally {
    optionsLoading.value = false
  }
}
const newKey = ref('')

const clientNames = computed(() => {
  const m: Record<number, string> = {}
  for (const row of clients.value) m[row.client.id] = row.client.name
  return m
})

function clientName(id: number) {
  return clientNames.value[id] || `#${id}`
}

function formatDate(v?: string) {
  if (!v) return '—'
  const d = new Date(v.includes('T') ? v : `${v.replace(' ', 'T')}Z`)
  return Number.isNaN(d.getTime()) ? v : d.toLocaleString('zh-CN')
}

function usd(minor: number) {
  return `$${((minor || 0) / 100).toFixed(2)}`
}

function splitInput(v: string) {
  return v.split(/[,，\s]+/).map((s) => s.trim()).filter(Boolean)
}

function requestStatusClass(s: string) {
  if (s === 'succeeded') return 'pill pill-good'
  if (s === 'review') return 'pill pill-warn'
  if (s === 'pending') return 'pill pill-info'
  return 'pill'
}

async function api(path: string, init?: RequestInit) {
  const res = await authFetch(path, init)
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error || data.message || `请求失败（${res.status}）`)
  return data
}

async function loadClients() {
  loading.value = true
  try {
    const d = await api('/api/v1/admin/cardnet/clients')
    clients.value = d.list || []
  } catch (e: any) {
    dialog.toast(e.message, 'err')
  } finally {
    loading.value = false
  }
}

function openClient(c?: CardnetClient) {
  editing.id = c?.id || 0
  editing.name = c?.name || ''
  editing.plans = [...(c?.allowed_plans || [])]
  editing.countries = [...(c?.allowed_countries || [])]
  editing.limit = c?.daily_code_limit ?? 200
  editing.ips = (c?.ip_allowlist || []).join(', ')
  clientOpen.value = true
  void loadOptions()
}

async function saveClient() {
  saving.value = true
  try {
    const body = JSON.stringify({
      name: editing.name,
      allowed_plans: editing.plans,
      allowed_countries: editing.countries,
      daily_code_limit: Number(editing.limit) || 0,
      ip_allowlist: splitInput(editing.ips),
    })
    if (editing.id) {
      await api(`/api/v1/admin/cardnet/clients/${editing.id}`, { method: 'PUT', body })
      dialog.toast('已保存', 'ok')
    } else {
      const d = await api('/api/v1/admin/cardnet/clients', { method: 'POST', body })
      newKey.value = d.api_key
    }
    clientOpen.value = false
    await loadClients()
  } catch (e: any) {
    dialog.toast(e.message, 'err')
  } finally {
    saving.value = false
  }
}

async function toggleClient(c: CardnetClient) {
  const next = c.status === 'active' ? 'disabled' : 'active'
  if (next === 'disabled' && !(await dialog.confirm(`停用「${c.name}」后卡网将无法发码，确定？`, { danger: true }))) return
  try {
    await api(`/api/v1/admin/cardnet/clients/${c.id}`, {
      method: 'PUT',
      body: JSON.stringify({
        name: c.name,
        allowed_plans: c.allowed_plans,
        allowed_countries: c.allowed_countries,
        daily_code_limit: c.daily_code_limit,
        ip_allowlist: c.ip_allowlist,
        status: next,
      }),
    })
    await loadClients()
  } catch (e: any) {
    dialog.toast(e.message, 'err')
  }
}

async function rotateKey(c: CardnetClient) {
  if (!(await dialog.confirm(`轮换后「${c.name}」的旧 Key 立即失效，需要同步更新卡网配置。确定？`, { danger: true }))) return
  try {
    const d = await api(`/api/v1/admin/cardnet/clients/${c.id}/rotate-key`, { method: 'POST' })
    newKey.value = d.api_key
    await loadClients()
  } catch (e: any) {
    dialog.toast(e.message, 'err')
  }
}

async function copyKey() {
  dialog.toast((await copyToClipboard(newKey.value)) ? '已复制' : '复制失败，请手动选择', 'ok')
}

async function loadRequests() {
  loading.value = true
  try {
    const params = new URLSearchParams({ page: String(reqPage.value), page_size: String(reqPageSize) })
    if (reqFilters.q.trim()) params.set('q', reqFilters.q.trim())
    if (reqFilters.client_id) params.set('client_id', reqFilters.client_id)
    if (reqFilters.status) params.set('status', reqFilters.status)
    const d = await api(`/api/v1/admin/cardnet/requests?${params}`)
    requests.value = d.list || []
    reqTotal.value = d.total || 0
  } catch (e: any) {
    dialog.toast(e.message, 'err')
  } finally {
    loading.value = false
  }
}

function searchRequests() {
  reqPage.value = 1
  loadRequests()
}

function onReqPage(p: number) {
  reqPage.value = p
  loadRequests()
}

async function showRequest(r: IssueRequest) {
  try {
    detail.value = await api(`/api/v1/admin/cardnet/requests/${r.id}`)
    detailOpen.value = true
  } catch (e: any) {
    dialog.toast(e.message, 'err')
  }
}

async function refetch(r: IssueRequest) {
  if (!(await dialog.confirm('用同一个上游幂等键向卡台重新拉取这批码（卡台只会重放原批次，不会重复扣费）。继续？'))) return
  acting.value = r.id
  try {
    const d = await api(`/api/v1/admin/cardnet/requests/${r.id}/refetch`, { method: 'POST' })
    const ok = d.http_status === 200
    dialog.toast(ok ? '已拉取成功' : `仍未成功：${d.result?.message || d.http_status}`, ok ? 'ok' : 'warn', 5000)
    await loadRequests()
  } catch (e: any) {
    dialog.toast(e.message, 'err')
  } finally {
    acting.value = 0
  }
}

async function markFailed(r: IssueRequest) {
  const note = await dialog.prompt('仅在确认卡台没有扣费、或已在卡台手动作废这批码后操作。卡网随后会换新键重发。请填写核对说明：', {
    title: '标记失败',
    placeholder: '例如：卡台后台核对无此批次',
  })
  if (!note || !String(note).trim()) return
  acting.value = r.id
  try {
    await api(`/api/v1/admin/cardnet/requests/${r.id}/mark-failed`, { method: 'POST', body: JSON.stringify({ note }) })
    dialog.toast('已标记失败', 'ok')
    await loadRequests()
  } catch (e: any) {
    dialog.toast(e.message, 'err')
  } finally {
    acting.value = 0
  }
}

async function loadStats() {
  loading.value = true
  try {
    const params = new URLSearchParams()
    if (statFilters.client_id) params.set('client_id', statFilters.client_id)
    if (statFilters.from) params.set('from', statFilters.from)
    if (statFilters.to) params.set('to', statFilters.to)
    const d = await api(`/api/v1/admin/cardnet/stats?${params}`)
    statRows.value = d.rows || []
    statTotal.value = d.total || { codes: 0, consumed: 0, disabled: 0, fee_minor: 0 }
  } catch (e: any) {
    dialog.toast(e.message, 'err')
  } finally {
    loading.value = false
  }
}

function switchTab(key: TabKey) {
  tab.value = key
  if (key === 'clients') loadClients()
  if (key === 'requests') loadRequests()
  if (key === 'stats') loadStats()
}

onMounted(loadClients)
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
.check-list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
  gap: 8px;
}
</style>
