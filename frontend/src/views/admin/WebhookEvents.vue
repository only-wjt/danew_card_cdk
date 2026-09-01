<template>
  <div class="space-y-4">
    <el-card shadow="never">
      <template #header>
        <div class="flex flex-wrap items-center justify-between gap-2">
          <span class="font-semibold">最近回调事件</span>
          <el-button :loading="loading" @click="load">刷新</el-button>
        </div>
      </template>
      <p class="text-sm text-muted mb-3">
        按验签归到对应卡台。Secret 在
        <router-link class="app-link" to="/ops/integration">卡台接入</router-link>
        各账户下配置。
      </p>
      <el-radio-group v-model="filterAccountId" size="small" class="mb-3">
        <el-radio-button :value="0">全部 {{ events.length }}</el-radio-button>
        <el-radio-button v-for="acc in accounts" :key="acc.id" :value="acc.id">
          {{ acc.name }} {{ countFor(acc.id) }}
        </el-radio-button>
        <el-radio-button v-if="orphanCount" :value="-1">未归属 {{ orphanCount }}</el-radio-button>
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

const accounts = ref<WebhookAccount[]>([])
const events = ref<WebhookEventRow[]>([])
const filterAccountId = ref(0)
const loading = ref(false)
const error = ref('')

const orphanCount = computed(() => events.value.filter((e) => !e.account_id).length)
const visibleEvents = computed(() => {
  if (filterAccountId.value === 0) return events.value
  if (filterAccountId.value === -1) return events.value.filter((e) => !e.account_id)
  return events.value.filter((e) => e.account_id === filterAccountId.value)
})

function countFor(id: number) {
  return events.value.filter((e) => e.account_id === id).length
}

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

async function load() {
  loading.value = true
  error.value = ''
  try {
    const r = await authFetch('/api/v1/admin/webhooks/events')
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      error.value = d.error || '加载失败'
      return
    }
    accounts.value = d.accounts || []
    events.value = d.events || []
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>
