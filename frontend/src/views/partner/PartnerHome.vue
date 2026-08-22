<template>
  <div class="space-y-6">
    <section class="welcome-card">
      <div>
        <div class="text-sm text-muted">欢迎回来</div>
        <h2 class="welcome-name">{{ auth.name || auth.username }}</h2>
        <p class="welcome-hint">在这里查看可用套餐、最近兑换记录，以及 API 对接说明。</p>
      </div>
      <div class="welcome-actions">
        <router-link to="/partner/records" class="btn-primary">查看记录</router-link>
        <router-link to="/partner/cdks" class="btn-secondary">我的卡密</router-link>
        <router-link to="/partner/api-keys" class="btn-secondary">管理 API Key</router-link>
      </div>
    </section>

    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <article v-for="s in statCards" :key="s.label" class="stat-card">
        <div class="stat-icon" :style="{ background: s.bg }">{{ s.icon }}</div>
        <div>
          <div class="text-sm text-muted">{{ s.label }}</div>
          <div class="stat-value">{{ s.value }}</div>
          <div class="text-xs text-subtle mt-1">{{ s.hint }}</div>
        </div>
      </article>
    </div>

    <div class="grid gap-5 xl:grid-cols-5">
      <section class="card xl:col-span-3 space-y-4">
        <div class="flex items-center justify-between gap-3">
          <h3 class="section-title">可用套餐</h3>
          <span class="text-xs text-muted">{{ plans.length }} 个档位</span>
        </div>
        <div v-if="loading" class="py-8 text-center text-muted text-sm">加载中…</div>
        <div v-else-if="!plans.length" class="empty-block">暂无可用套餐，请联系管理员配置权限。</div>
        <div v-else class="plan-grid">
          <div v-for="p in plans" :key="p.key" class="plan-tile">
            <div class="plan-tile-head">
              <span class="plan-key mono">{{ p.key }}</span>
              <span v-if="p.is_credit" class="pill pill-info">点数</span>
            </div>
            <div class="plan-label">{{ p.label || p.key }}</div>
            <div class="plan-fee mono">${{ formatFee(p.fee_usd) }}</div>
            <div class="text-xs text-subtle">服务费 / 次</div>
          </div>
        </div>
      </section>

      <div class="xl:col-span-2 space-y-5">
        <section class="card space-y-3">
          <div class="flex items-center justify-between">
            <h3 class="section-title">最近记录</h3>
            <router-link to="/partner/records" class="text-sm app-link">全部 →</router-link>
          </div>
          <div v-if="!recentRows.length" class="empty-block !py-6">暂无兑换记录</div>
          <div v-else class="recent-list">
            <div v-for="r in recentRows" :key="r.request_id" class="recent-item">
              <div class="flex items-center justify-between gap-2">
                <span class="font-medium text-ink text-sm">{{ r.plan }}</span>
                <span :class="statusClass(r.status)">{{ statusLabel(r.status) }}</span>
              </div>
              <div class="text-xs text-muted mt-1 truncate">{{ r.account_email || r.cdk_prefix || '—' }}</div>
              <div class="text-xs text-subtle mt-0.5">{{ formatDate(r.created_at) }}</div>
            </div>
          </div>
        </section>

        <section class="card space-y-3">
          <h3 class="section-title">API 速查</h3>
          <div class="api-snippet">
            <div class="api-line"><span class="api-method post">POST</span><code>/api/v1/agent/recharge</code></div>
            <div class="api-line"><span class="api-method get">GET</span><code>/api/v1/agent/recharge/:id</code></div>
            <div class="api-line"><span class="api-method get">GET</span><code>/api/v1/agent/records</code></div>
          </div>
          <p class="text-xs text-muted leading-relaxed">
            请求头携带 <code class="mono">Authorization: Bearer &lt;api_key&gt;</code>。
            并发上限 <b>{{ limits.concurrent }}</b> 笔，每分钟 <b>{{ limits.rpm }}</b> 次请求。
          </p>
          <router-link to="/partner/settings" class="text-sm app-link">查看对接设置 →</router-link>
        </section>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { agentFetch } from '../../lib/agentApi'
import { useAgentAuthStore } from '../../stores/agentAuth'
import { statusClass, statusLabel } from './partnerUi'

const auth = useAgentAuthStore()
const plans = ref<any[]>([])
const recentRows = ref<any[]>([])
const recentTotal = ref(0)
const loading = ref(true)
const limits = ref({ rpm: 60, concurrent: 2 })
const unusedCdks = ref<number | null>(null)

const statCards = computed(() => [
  { label: '可用卡密', value: unusedCdks.value != null ? String(unusedCdks.value) : '—', hint: '站长已分配、未使用', icon: '🎫', bg: 'var(--good-soft, #ecfdf5)' },
  { label: '可用套餐', value: String(plans.value.length), hint: '当前账号可发码档位', icon: '📦', bg: 'var(--primary-soft)' },
  { label: '累计记录', value: String(recentTotal.value), hint: '历史兑换总条数', icon: '📋', bg: 'var(--warn-soft, #fffbeb)' },
  { label: '请求配额', value: `${limits.value.rpm}/min`, hint: 'API 每分钟上限', icon: '⚡', bg: 'var(--surface-2)' },
])

function formatFee(v: number) {
  return v != null ? Number(v).toFixed(2) : '—'
}

function formatDate(v: string) {
  return v ? new Date(v).toLocaleString('zh-CN') : '—'
}

onMounted(async () => {
  loading.value = true
  try {
    const [pRes, rRes, mRes] = await Promise.all([
      agentFetch('/api/v1/agent/plans'),
      agentFetch('/api/v1/agent/records?page_size=5'),
      agentFetch('/api/v1/auth/agent/me'),
    ])
    if (pRes.ok) plans.value = (await pRes.json()).plans || []
    if (rRes.ok) {
      const d = await rRes.json()
      recentRows.value = d.list || []
      recentTotal.value = d.total || 0
    }
    if (mRes.ok) {
      const m = await mRes.json()
      limits.value = {
        rpm: m.rate_limit_rpm || 60,
        concurrent: m.max_concurrent_recharge || 2,
      }
      unusedCdks.value = m.unused_cdk_count ?? null
    }
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.welcome-card {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 24px 28px;
  border-radius: var(--radius-lg);
  border: 1px solid color-mix(in srgb, var(--primary) 25%, var(--brd));
  background: linear-gradient(135deg, color-mix(in srgb, var(--primary) 8%, var(--surface)), var(--surface));
}
.welcome-name {
  margin-top: 4px;
  font-size: 1.75rem;
  font-weight: 800;
  color: var(--ink);
  font-family: var(--font-display, var(--font-serif));
}
.welcome-hint {
  margin-top: 8px;
  font-size: 14px;
  color: var(--ink-2);
  max-width: 52ch;
}
.welcome-actions { display: flex; flex-wrap: wrap; gap: 10px; }
.stat-card {
  display: flex;
  gap: 14px;
  align-items: flex-start;
  padding: 18px;
  border-radius: var(--radius-lg);
  border: 1px solid var(--brd);
  background: var(--surface);
  box-shadow: var(--shadow-sm);
}
.stat-icon {
  width: 42px;
  height: 42px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  flex-shrink: 0;
}
.stat-value {
  margin-top: 6px;
  font-size: 1.5rem;
  font-weight: 800;
  color: var(--ink);
  font-family: var(--font-mono);
}
.section-title {
  font-size: 1.05rem;
  font-weight: 700;
  color: var(--ink);
}
.plan-grid {
  display: grid;
  gap: 12px;
  grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
}
.plan-tile {
  padding: 14px;
  border-radius: var(--radius-md, 12px);
  border: 1px solid var(--brd);
  background: var(--surface-2);
  transition: border-color .15s, transform .15s;
}
.plan-tile:hover {
  border-color: color-mix(in srgb, var(--primary) 40%, var(--brd));
  transform: translateY(-1px);
}
.plan-tile-head { display: flex; align-items: center; justify-content: space-between; gap: 6px; }
.plan-key { font-size: 11px; color: var(--ink-3); }
.plan-label { margin-top: 8px; font-weight: 600; color: var(--ink); }
.plan-fee { margin-top: 6px; font-size: 1.25rem; font-weight: 700; color: var(--primary); }
.empty-block {
  padding: 32px 16px;
  text-align: center;
  font-size: 14px;
  color: var(--ink-3);
  border: 1px dashed var(--brd);
  border-radius: var(--radius-md, 12px);
}
.recent-list { display: flex; flex-direction: column; gap: 10px; }
.recent-item {
  padding: 12px;
  border-radius: var(--radius-md, 12px);
  background: var(--surface-2);
  border: 1px solid var(--brd);
}
.api-snippet { display: flex; flex-direction: column; gap: 8px; }
.api-line {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--ink-2);
}
.api-method {
  font-size: 10px;
  font-weight: 700;
  padding: 2px 6px;
  border-radius: 4px;
  flex-shrink: 0;
}
.api-method.post { background: var(--primary-soft); color: var(--primary); }
.api-method.get { background: var(--surface-3, var(--surface-2)); color: var(--ink-2); }
</style>
