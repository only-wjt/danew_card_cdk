<template>
  <div class="space-y-4">
    <el-card shadow="never">
      <template #header>
        <div class="flex flex-wrap items-center justify-between gap-2">
          <span class="font-semibold">最近回调事件</span>
          <el-button :loading="loading" @click="load">刷新</el-button>
        </div>
      </template>
      <p v-if="!embedded" class="text-sm text-muted mb-3">
        按验签归到对应卡台。Secret 在
        <router-link class="app-link" to="/ops/platforms">卡台</router-link>
        各账户下配置。
      </p>
      <el-radio-group v-if="!embedded" v-model="filterAccountId" size="small" class="mb-3" @change="onFilter">
        <el-radio-button :value="0">全部</el-radio-button>
        <el-radio-button v-for="acc in accounts" :key="acc.id" :value="acc.id">{{ acc.name }}</el-radio-button>
        <el-radio-button :value="-1">未归属</el-radio-button>
      </el-radio-group>
      <div v-if="error" class="alert alert-error mb-3">{{ error }}</div>
      <div class="overflow-x-auto">
      <table class="data-table">
        <thead>
          <tr>
            <th>ID</th>
            <th>卡台</th>
            <th>类型</th>
            <th>幂等键</th>
            <th>时间</th>
            <th>摘要</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading">
            <td colspan="6" class="py-6 text-center text-muted">加载中…</td>
          </tr>
          <tr v-else-if="!visibleEvents.length">
            <td colspan="6" class="py-6 text-center text-muted">该卡台暂无回调（未配 Secret 时继续用轮询）</td>
          </tr>
          <tr v-for="e in visibleEvents" :key="e.id">
            <td class="mono">{{ e.id }}</td>
            <td>{{ e.account_name || '未归属' }}</td>
            <td>{{ e.event_type }}</td>
            <td class="mono text-xs">{{ e.idem_key }}</td>
            <td class="text-sm text-muted">{{ e.created_at }}</td>
            <td class="text-xs text-subtle">{{ summarize(e.payload) }}</td>
          </tr>
        </tbody>
      </table>
      </div>
      <div class="mt-3 flex justify-end">
        <el-pagination
          background
          layout="total, prev, pager, next, sizes"
          :total="total"
          :page-size="pageSize"
          :current-page="page"
          :page-sizes="[20, 50, 100]"
          @current-change="(p: number) => { page = p; load() }"
          @size-change="(s: number) => { pageSize = s; page = 1; load() }"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { authFetch } from '../../lib/api'

interface WebhookAccount {
  id: number
  name: string
}

interface WebhookEventRow {
  id: number
  account_id?: number
  account_name?: string
  event_type: string
  idem_key: string
  created_at: string
  payload: any
}

const props = withDefaults(defineProps<{ fixedAccountId?: number; orphansOnly?: boolean; embedded?: boolean }>(), {
  fixedAccountId: 0,
  orphansOnly: false,
  embedded: false,
})

const accounts = ref<WebhookAccount[]>([])
const events = ref<WebhookEventRow[]>([])
const filterAccountId = ref(0)
const loading = ref(false)
const error = ref('')
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)

const visibleEvents = computed(() => events.value)

function summarize(p: any) {
  if (!p || typeof p !== 'object') return '—'
  if (p.type === 'gpt_direct.completed' || p.order_id) {
    return `order=${p.order_id || ''} plan=${p.plan || ''} status=${p.status || ''}`
  }
  if (p.event === 'card_transaction') {
    return `${p.type || ''} ${p.status || ''} ${p.merchant_name || p.merchant || ''}`
  }
  if (p.event === 'card_operation') {
    return `${p.operation || ''} ${p.status || ''}`
  }
  return Object.keys(p).slice(0, 4).join(',')
}

function onFilter() {
  page.value = 1
  void load()
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const params = new URLSearchParams({
      page: String(page.value),
      page_size: String(pageSize.value),
    })
    if (props.orphansOnly) params.set('account_id', '-1')
    else if (props.fixedAccountId) params.set('account_id', String(props.fixedAccountId))
    else if (filterAccountId.value) params.set('account_id', String(filterAccountId.value))
    const r = await authFetch('/api/v1/admin/webhooks/events?' + params.toString())
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      error.value = d.error || '加载失败'
      return
    }
    accounts.value = d.accounts || []
    events.value = d.events || []
    total.value = Number(d.total || 0)
    if (props.orphansOnly) filterAccountId.value = -1
    else if (props.fixedAccountId) filterAccountId.value = props.fixedAccountId
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>
