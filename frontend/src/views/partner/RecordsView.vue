<template>
  <div class="space-y-5">
    <section class="card filter-card">
      <div class="filter-grid">
        <div class="form-group !mb-0">
          <label>邮箱</label>
          <input v-model="filters.email" class="input" placeholder="模糊匹配" @keyup.enter="search" />
        </div>
        <div class="form-group !mb-0">
          <label>卡密</label>
          <input v-model="filters.cdk" class="input" placeholder="前缀或完整码" @keyup.enter="search" />
        </div>
        <div class="form-group !mb-0">
          <label>状态</label>
          <select v-model="filters.status" class="field-select">
            <option value="">全部状态</option>
            <option v-for="s in statusOptions" :key="s.value" :value="s.value">{{ s.label }}</option>
          </select>
        </div>
        <div class="filter-actions">
          <button class="btn-primary" :disabled="loading" @click="search">{{ loading ? '查询中…' : '查询' }}</button>
          <button class="btn-secondary" @click="resetFilters">重置</button>
        </div>
      </div>
      <div class="form-group !mb-0 mt-4">
        <label>Session 检索 <span class="text-subtle font-normal">（粘贴完整 JSON，走加密检索接口，不进 URL）</span></label>
        <textarea
          v-model="filters.session"
          class="input mono text-xs"
          placeholder='{"user":{"email":"..."}, ...}'
        />
      </div>
    </section>

    <section class="card overflow-hidden !p-0">
      <div class="table-head">
        <span>共 <b class="text-ink">{{ total }}</b> 条记录</span>
        <span v-if="sessionMode" class="pill pill-info">Session 模式</span>
      </div>
      <div class="overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>时间</th>
              <th>套餐</th>
              <th>邮箱</th>
              <th>CDK</th>
              <th>状态</th>
              <th>客户单号</th>
              <th>说明</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td colspan="7" class="py-10 text-center text-muted">加载中…</td>
            </tr>
            <tr v-else-if="!rows.length">
              <td colspan="7" class="py-10 text-center text-muted">暂无记录，调整筛选条件后重试</td>
            </tr>
            <tr v-for="row in rows" :key="row.request_id">
              <td class="text-muted whitespace-nowrap text-sm">{{ formatDate(row.created_at) }}</td>
              <td><span class="pill pill-info">{{ row.plan }}</span></td>
              <td class="text-sm">{{ row.account_email || '—' }}</td>
              <td class="mono text-sm">{{ row.cdk_prefix || '—' }}</td>
              <td><span :class="statusClass(row.status)">{{ statusLabel(row.status) }}</span></td>
              <td class="mono text-xs text-muted">{{ row.client_reference || '—' }}</td>
              <td class="text-sm text-muted max-w-[200px] truncate" :title="row.message">{{ row.message || '—' }}</td>
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
import Pagination from '../../components/Pagination.vue'
import { formatPartnerDate, statusClass, statusLabel } from './partnerUi'

const statusOptions = [
  { value: 'success', label: '成功' },
  { value: 'failed', label: '失败' },
  { value: 'processing', label: '处理中' },
  { value: 'pending', label: '排队中' },
  { value: 'unknown', label: '待确认' },
  { value: 'running', label: '进行中' },
]

const filters = reactive({ email: '', cdk: '', session: '', status: '' })
const rows = ref<any[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)
const sessionMode = computed(() => !!filters.session.trim())

const formatDate = formatPartnerDate

async function load() {
  loading.value = true
  try {
    if (filters.session.trim()) {
      const res = await agentFetch('/api/v1/agent/records/search-session', {
        method: 'POST',
        body: JSON.stringify({ session: filters.session.trim(), page: page.value, page_size: pageSize }),
      })
      const d = await res.json()
      if (res.ok) {
        rows.value = d.list || []
        total.value = d.total || 0
      }
      return
    }
    const q = new URLSearchParams({ page: String(page.value), page_size: String(pageSize) })
    if (filters.email) q.set('email', filters.email.trim())
    if (filters.cdk) q.set('cdk', filters.cdk.trim())
    if (filters.status) q.set('status', filters.status)
    const res = await agentFetch(`/api/v1/agent/records?${q}`)
    const d = await res.json()
    if (res.ok) {
      rows.value = d.list || []
      total.value = d.total || 0
    }
  } finally {
    loading.value = false
  }
}

function search() {
  page.value = 1
  load()
}

function onPage(p: number) {
  page.value = p
  load()
}

function resetFilters() {
  filters.email = ''
  filters.cdk = ''
  filters.session = ''
  filters.status = ''
  search()
}

onMounted(load)
</script>

<style scoped>
.filter-card { padding: 20px !important; }
.filter-actions {
  display: flex;
  gap: 10px;
  align-items: flex-end;
}
.filter-actions .btn-primary,
.filter-actions .btn-secondary { flex: 1; }
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
</style>
