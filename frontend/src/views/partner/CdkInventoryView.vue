<template>
  <div class="space-y-5">
    <section class="grid gap-3 sm:grid-cols-3">
      <article v-for="s in summaryCards" :key="s.key" class="summary-card">
        <div class="text-sm text-muted">{{ s.label }}</div>
        <div class="summary-value" :class="s.class">{{ s.value }}</div>
        <div class="text-xs text-subtle mt-1">{{ s.hint }}</div>
      </article>
    </section>

    <section class="card filter-card">
      <div class="filter-grid">
        <div class="form-group !mb-0">
          <label>卡密</label>
          <input v-model="filters.code" class="input mono" placeholder="前缀或完整码" @keyup.enter="search" />
        </div>
        <div class="form-group !mb-0">
          <label>套餐</label>
          <select v-model="filters.plan" class="field-select">
            <option value="">全部套餐</option>
            <option v-for="p in plans" :key="p.key" :value="p.key">{{ planLabel(p.key) }}</option>
          </select>
        </div>
        <div class="form-group !mb-0">
          <label>状态</label>
          <select v-model="filters.status" class="field-select">
            <option value="">全部</option>
            <option value="unused">未使用</option>
            <option value="reserved">充值中</option>
            <option value="consumed">已消耗</option>
          </select>
        </div>
        <div class="filter-actions">
          <button class="btn-primary" :disabled="loading" @click="search">{{ loading ? '查询中…' : '查询' }}</button>
          <button class="btn-secondary" @click="resetFilters">重置</button>
        </div>
      </div>
      <div class="flex flex-wrap gap-2 mt-4 pt-4 border-t" style="border-color: var(--brd)">
        <button class="btn-secondary !min-h-0 !py-1.5 text-sm" :disabled="!unusedOnPage.length" @click="copyCodes(unusedOnPage)">
          复制本页可用
        </button>
        <button class="btn-secondary !min-h-0 !py-1.5 text-sm" :disabled="copyingAll || summary.unused === 0" @click="copyAllUnused">
          {{ copyingAll ? '拉取中…' : `复制全部可用（${summary.unused}）` }}
        </button>
        <router-link to="/partner/batch" class="btn-primary !min-h-0 !py-1.5 text-sm">去批量充值 →</router-link>
      </div>
    </section>

    <section class="card overflow-hidden !p-0">
      <div class="table-head">
        <span>共 <b class="text-ink">{{ total }}</b> 张卡密</span>
        <span class="text-xs text-muted">站长分配或购卡入库后可复制发给下级；GPT白号不能用于本站代充</span>
      </div>
      <div class="overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>分配时间</th>
              <th>套餐</th>
              <th>卡密</th>
              <th>状态</th>
              <th class="text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td colspan="5" class="py-10 text-center text-muted">加载中…</td>
            </tr>
            <tr v-else-if="!rows.length">
              <td colspan="5" class="py-10 text-center text-muted">
                暂无卡密。请联系站长在后台「代理管理 → 发卡密」为你分配。
              </td>
            </tr>
            <tr v-for="row in rows" :key="row.code">
              <td class="text-muted whitespace-nowrap text-sm">{{ formatDate(row.created_at) }}</td>
              <td><span class="pill pill-info">{{ planLabel(row.plan) }}</span></td>
              <td class="mono text-sm select-all">{{ row.code }}</td>
              <td><span :class="cdkStatusClass(row.status)">{{ cdkStatusLabel(row.status) }}</span></td>
              <td class="text-right whitespace-nowrap">
                <button
                  v-if="isCopyable(row.status)"
                  class="btn-ghost !px-2.5 !py-1 text-sm"
                  @click="copyCodes([row.code])"
                >
                  复制
                </button>
                <span v-else class="text-xs text-muted">—</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="table-foot">
        <Pagination :page="page" :page-size="pageSize" :total="total" @update:page="onPage" />
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { agentFetch } from '../../lib/agentApi'
import { dialog } from '../../lib/dialog'
import Pagination from '../../components/Pagination.vue'
import { formatPartnerDate } from './partnerUi'

interface CdkRow {
  code: string
  code_prefix?: string
  plan: string
  status: string
  created_at: string
}

interface Summary {
  total: number
  unused: number
  reserved: number
  consumed: number
}

const rows = ref<CdkRow[]>([])
const plans = ref<{ key: string; label?: string }[]>([])
const summary = ref<Summary>({ total: 0, unused: 0, reserved: 0, consumed: 0 })
const loading = ref(false)
const copyingAll = ref(false)
const page = ref(1)
const pageSize = 50
const total = ref(0)
const filters = reactive({ code: '', plan: '', status: 'unused' })

const summaryCards = computed(() => [
  { key: 'unused', label: '可用', value: String(summary.value.unused), hint: '可直接用于充值', class: 'text-good' },
  { key: 'reserved', label: '充值中', value: String(summary.value.reserved), hint: '已提交、等待结果', class: 'text-warn' },
  { key: 'consumed', label: '已消耗', value: String(summary.value.consumed), hint: '充值成功或已核销', class: 'text-muted' },
])

const unusedOnPage = computed(() =>
  rows.value.filter((r) => isCopyable(r.status)).map((r) => r.code),
)

function formatDate(v: string) {
  return formatPartnerDate(v)
}

function planLabel(key: string) {
  const hit = plans.value.find((p) => p.key === key)
  if (hit?.label) return hit.label
  if (key === 'gpt_white') return 'GPT白号'
  if (key === 'pro' || key === 'pro_20x') return 'Pro 20x'
  return key || '—'
}

function isCopyable(status: string) {
  const s = (status || '').toLowerCase()
  return s === '' || s === 'unused'
}

function cdkStatusLabel(s: string) {
  switch ((s || '').toLowerCase()) {
    case 'unused':
    case '':
      return '未使用'
    case 'reserved':
      return '充值中'
    case 'consumed':
      return '已消耗'
    default:
      return s || '—'
  }
}

function cdkStatusClass(s: string) {
  switch ((s || '').toLowerCase()) {
    case 'unused':
    case '':
      return 'pill pill-good'
    case 'reserved':
      return 'pill pill-warn'
    case 'consumed':
      return 'pill'
    default:
      return 'pill pill-info'
  }
}

async function loadPlans() {
  const res = await agentFetch('/api/v1/agent/plans')
  if (res.ok) {
    const d = await res.json()
    plans.value = d.plans || []
  }
}

async function load() {
  loading.value = true
  try {
    const params = new URLSearchParams({
      page: String(page.value),
      page_size: String(pageSize),
    })
    if (filters.code.trim()) params.set('code', filters.code.trim())
    if (filters.plan) params.set('plan', filters.plan)
    if (filters.status) params.set('status', filters.status)
    const res = await agentFetch(`/api/v1/agent/cdks?${params}`)
    const d = await res.json()
    if (!res.ok) {
      dialog.toast(d.error || '加载失败', 'err')
      return
    }
    rows.value = d.list || []
    total.value = d.total || 0
    if (d.summary) summary.value = d.summary
  } finally {
    loading.value = false
  }
}

function search() {
  page.value = 1
  void load()
}

function resetFilters() {
  filters.code = ''
  filters.plan = ''
  filters.status = 'unused'
  search()
}

function onPage(p: number) {
  page.value = p
  void load()
}

async function copyCodes(codes: string[]) {
  if (!codes.length) return
  try {
    await navigator.clipboard.writeText(codes.join('\n'))
    dialog.toast(`已复制 ${codes.length} 张卡密`, 'ok')
  } catch {
    dialog.toast('复制失败，请手动选中复制', 'err')
  }
}

async function copyAllUnused() {
  copyingAll.value = true
  try {
    const codes: string[] = []
    let p = 1
    let fetched = 0
    const maxPages = 20
    while (p <= maxPages) {
      const params = new URLSearchParams({
        page: String(p),
        page_size: '200',
        status: 'unused',
      })
      if (filters.plan) params.set('plan', filters.plan)
      const res = await agentFetch(`/api/v1/agent/cdks?${params}`)
      const d = await res.json()
      if (!res.ok) {
        dialog.toast(d.error || '拉取失败', 'err')
        return
      }
      const batch = (d.list || []) as CdkRow[]
      for (const row of batch) {
        if (isCopyable(row.status)) codes.push(row.code)
      }
      fetched += batch.length
      if (fetched >= (d.total || 0) || !batch.length) break
      p++
    }
    if (!codes.length) {
      dialog.toast('没有可用卡密', 'warn')
      return
    }
    await copyCodes(codes)
  } finally {
    copyingAll.value = false
  }
}

onMounted(async () => {
  await Promise.all([loadPlans(), load()])
})
</script>

<style scoped>
.summary-card {
  padding: 18px 20px;
  border-radius: var(--radius-md, 12px);
  background: var(--surface);
  border: 1px solid var(--brd);
}
.summary-value {
  margin-top: 6px;
  font-size: 1.75rem;
  font-weight: 800;
  font-variant-numeric: tabular-nums;
  color: var(--ink);
}
.text-good { color: var(--good); }
.text-warn { color: var(--warn); }
.filter-card .filter-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
  align-items: end;
}
.filter-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.table-head,
.table-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  padding: 14px 18px;
}
.table-head { border-bottom: 1px solid var(--brd); font-size: 13px; color: var(--ink-2); }
.table-foot { border-top: 1px solid var(--brd); }
</style>
