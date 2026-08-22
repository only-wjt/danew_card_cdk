<template>
  <div class="space-y-5 max-w-3xl">
    <section class="card space-y-5">
      <div>
        <h3 class="section-title">Webhook 回调</h3>
        <p class="text-sm text-muted mt-1">充值结果变更时，系统会向此 URL 发送 POST 通知。</p>
      </div>
      <div class="form-group">
        <label>Webhook URL</label>
        <input v-model="form.webhook_url" class="input" placeholder="https://your-server.com/webhook/agent" />
      </div>
      <div class="flex flex-wrap gap-3 items-center">
        <button class="btn-secondary" :disabled="saving" @click="rotateSecret">轮换 Webhook Secret</button>
        <span v-if="hasSecret" class="pill pill-good">已配置 Secret</span>
        <span v-else class="pill">未配置 Secret</span>
      </div>
      <div v-if="webhookSecret" class="secret-box">
        <div class="text-xs text-muted mb-1">新 Secret（仅展示一次）</div>
        <div class="mono text-sm break-all select-all">{{ webhookSecret }}</div>
        <button class="btn-ghost !px-3 mt-2" @click="copySecret">复制</button>
      </div>
    </section>

    <section class="card space-y-4">
      <div>
        <h3 class="section-title">客户单号前缀</h3>
        <p class="text-sm text-muted mt-1">发起充值时若未传 client_reference，将自动加上此前缀。</p>
      </div>
      <div class="form-group">
        <label>前缀</label>
        <input v-model="form.ref_prefix" class="input mono" placeholder="shop-a-" />
      </div>
      <div>
        <button class="btn-primary" :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存设置' }}</button>
      </div>
    </section>

    <section class="card space-y-4">
      <div>
        <h3 class="section-title">账号配额</h3>
        <p class="text-sm text-muted mt-1">由管理员配置，此处只读展示。</p>
      </div>
      <div class="quota-grid">
        <div class="quota-item">
          <div class="quota-label">每分钟请求数</div>
          <div class="quota-value mono">{{ limits.rpm }}</div>
        </div>
        <div class="quota-item">
          <div class="quota-label">在途充值上限</div>
          <div class="quota-value mono">{{ limits.concurrent }} 条</div>
          <div class="quota-hint">按明细条数计，单条与批量共用</div>
        </div>
        <div class="quota-item">
          <div class="quota-label">单批最多条数</div>
          <div class="quota-value mono">{{ limits.batch }} 条</div>
        </div>
      </div>
    </section>

    <section class="card !p-0 overflow-hidden">
      <div class="log-head">
        <div>
          <h3 class="section-title">回调投递日志</h3>
          <p class="text-sm text-muted mt-1">回调没收到时先看这里的状态码与错误。</p>
        </div>
        <button class="btn-secondary !min-h-0 !py-1.5 text-sm" :disabled="logLoading" @click="loadDeliveries">
          {{ logLoading ? '刷新中…' : '刷新' }}
        </button>
      </div>
      <div class="overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>时间</th>
              <th>事件</th>
              <th>关联</th>
              <th>状态</th>
              <th>尝试</th>
              <th>最近错误</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!deliveries.length">
              <td colspan="7" class="py-8 text-center text-muted">
                暂无投递记录。配置回调地址并生成 Secret 后，充值完成时会自动推送。
              </td>
            </tr>
            <tr v-for="d in deliveries" :key="d.id">
              <td class="text-muted whitespace-nowrap text-sm">{{ formatDate(d.created_at) }}</td>
              <td class="mono text-xs">{{ d.event_type }}</td>
              <td class="mono text-xs text-muted">{{ d.request_id || d.batch_id || '—' }}</td>
              <td><span class="pill" :class="deliveryPill(d.status)">{{ deliveryLabel(d.status) }}</span></td>
              <td class="text-xs text-muted">
                {{ d.attempts }}<span v-if="d.last_status_code"> · HTTP {{ d.last_status_code }}</span>
              </td>
              <td class="text-xs text-muted max-w-[220px] truncate" :title="d.last_error">
                {{ d.last_error || '—' }}
              </td>
              <td>
                <button
                  v-if="d.status === 'failed'"
                  class="btn-secondary !min-h-0 !py-1 !px-3 text-xs"
                  @click="retryDelivery(d.id)"
                >
                  重投
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="logTotal > logPageSize" class="log-foot">
        <Pagination :page="logPage" :page-size="logPageSize" :total="logTotal" @update:page="onLogPage" />
      </div>
    </section>

    <section class="card space-y-3">
      <h3 class="section-title">对接示例</h3>
      <pre class="code-block">curl -X POST {{ baseUrl }}/api/v1/agent/recharge \
  -H "Authorization: Bearer ak_live_xxx" \
  -H "Content-Type: application/json" \
  -d '{"plan":"plus","account":{"mode":"session","session":"..."}}'</pre>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import Pagination from '../../components/Pagination.vue'
import { agentFetch } from '../../lib/agentApi'
import { dialog } from '../../lib/dialog'
import { formatPartnerDate } from './partnerUi'

interface DeliveryRow {
  id: number
  event_type: string
  batch_id: string
  request_id: string
  status: string
  attempts: number
  last_status_code: number
  last_error: string
  created_at: string
}

const form = reactive({ webhook_url: '', ref_prefix: '' })
const saving = ref(false)
const hasSecret = ref(false)
const webhookSecret = ref('')
const limits = ref({ rpm: 60, concurrent: 10, batch: 20 })

const deliveries = ref<DeliveryRow[]>([])
const logTotal = ref(0)
const logPage = ref(1)
const logPageSize = 10
const logLoading = ref(false)

const formatDate = formatPartnerDate

const DELIVERY_STATUS: Record<string, { label: string; pill: string }> = {
  pending: { label: '待投递', pill: 'pill-info' },
  delivered: { label: '已送达', pill: 'pill-good' },
  failed: { label: '失败', pill: 'pill-err' },
}
function deliveryLabel(s: string) {
  return DELIVERY_STATUS[s]?.label || s
}
function deliveryPill(s: string) {
  return DELIVERY_STATUS[s]?.pill || ''
}

const baseUrl = computed(() => {
  if (typeof window === 'undefined') return 'https://your-domain.com'
  return window.location.origin
})

onMounted(async () => {
  const [sRes, mRes] = await Promise.all([
    agentFetch('/api/v1/agent/settings'),
    agentFetch('/api/v1/auth/agent/me'),
  ])
  if (sRes.ok) {
    const d = await sRes.json()
    form.webhook_url = d.webhook_url || ''
    form.ref_prefix = d.ref_prefix || ''
    hasSecret.value = !!d.has_webhook_secret
  }
  if (mRes.ok) {
    const m = await mRes.json()
    limits.value = {
      rpm: m.rate_limit_rpm || 60,
      concurrent: m.max_concurrent_recharge || 10,
      batch: m.max_batch_items || 20,
    }
  }
  await loadDeliveries()
})

async function loadDeliveries() {
  logLoading.value = true
  try {
    const res = await agentFetch(
      `/api/v1/agent/webhooks/deliveries?page=${logPage.value}&page_size=${logPageSize}`,
    )
    if (!res.ok) return
    const d = await res.json()
    deliveries.value = d.list || []
    logTotal.value = d.total || 0
  } finally {
    logLoading.value = false
  }
}

function onLogPage(p: number) {
  logPage.value = p
  loadDeliveries()
}

async function retryDelivery(id: number) {
  const res = await agentFetch(`/api/v1/agent/webhooks/deliveries/${id}/retry`, { method: 'POST' })
  const d = await res.json()
  dialog.toast(res.ok ? '已重新入队' : d.error || '重投失败', res.ok ? 'ok' : 'err')
  if (res.ok) loadDeliveries()
}

async function save() {
  saving.value = true
  try {
    const res = await agentFetch('/api/v1/agent/settings', {
      method: 'PUT',
      body: JSON.stringify(form),
    })
    dialog.toast(res.ok ? '设置已保存' : '保存失败', res.ok ? 'ok' : 'err')
  } finally {
    saving.value = false
  }
}

async function rotateSecret() {
  const ok = await dialog.confirm('轮换后旧 Secret 立即失效，需同步更新你的验签逻辑。', {
    title: '轮换 Webhook Secret',
    danger: true,
  })
  if (!ok) return
  const res = await agentFetch('/api/v1/agent/settings/webhook-secret/rotate', { method: 'POST' })
  const d = await res.json()
  if (res.ok) {
    webhookSecret.value = d.webhook_secret
    hasSecret.value = true
    dialog.toast('Secret 已轮换', 'ok')
  } else {
    dialog.toast(d.error || '轮换失败', 'err')
  }
}

async function copySecret() {
  try {
    await navigator.clipboard.writeText(webhookSecret.value)
    dialog.toast('已复制', 'ok')
  } catch {
    dialog.toast('复制失败', 'err')
  }
}
</script>

<style scoped>
.section-title {
  font-size: 1.05rem;
  font-weight: 700;
  color: var(--ink);
}
.secret-box {
  padding: 14px;
  border-radius: var(--radius-md, 12px);
  background: var(--good-soft, #ecfdf5);
  border: 1px solid color-mix(in srgb, var(--good) 30%, var(--brd));
}
.quota-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 12px;
}
.quota-hint { margin-top: 4px; font-size: 11px; color: var(--ink-3); }
.log-head {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 20px;
  border-bottom: 1px solid var(--brd);
}
.log-foot { padding: 14px 18px; border-top: 1px solid var(--brd); }
.quota-item {
  padding: 16px;
  border-radius: var(--radius-md, 12px);
  background: var(--surface-2);
  border: 1px solid var(--brd);
}
.quota-label { font-size: 13px; color: var(--ink-2); }
.quota-value { margin-top: 8px; font-size: 1.5rem; font-weight: 800; color: var(--ink); }
.code-block {
  margin: 0;
  padding: 16px;
  border-radius: var(--radius-md, 12px);
  background: var(--surface-2);
  border: 1px solid var(--brd);
  font-size: 12px;
  line-height: 1.6;
  overflow-x: auto;
  font-family: var(--font-mono);
  color: var(--ink-2);
}
</style>
