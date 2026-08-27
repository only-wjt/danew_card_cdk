<template>
  <div class="orders-page space-y-6">
    <!-- 购卡区 -->
    <section class="buy-hero">
      <div class="buy-hero-inner">
        <div class="buy-hero-copy">
          <div class="buy-badge">在线购卡</div>
          <h2 class="buy-title">选择套餐，支付后自动入库</h2>
          <p class="buy-sub">
            支付成功后卡密会立即写入「我的卡密」，无需站长手工分配。单笔最多 {{ maxCount }} 张。
          </p>
        </div>
        <div class="buy-panel card">
          <div class="form-grid form-grid-2">
            <div class="form-group !mb-0">
              <label>套餐</label>
              <select v-model="buy.plan" class="field-select">
                <option value="" disabled>请选择</option>
                <option v-for="p in pricedPlans" :key="p.key" :value="p.key">
                  {{ p.label || p.key }} · ¥{{ p.price_yuan }}{{ planStockText(p) }}
                </option>
              </select>
            </div>
            <div class="form-group !mb-0">
              <label>数量</label>
              <div class="qty-row">
                <button type="button" class="qty-btn" :disabled="buy.count <= 1" @click="buy.count--">−</button>
                <input v-model.number="buy.count" type="number" class="input mono text-center qty-input" min="1" :max="maxCount" />
                <button type="button" class="qty-btn" :disabled="buy.count >= maxCount" @click="buy.count++">+</button>
              </div>
            </div>
          </div>

          <div v-if="payTypes.length" class="pay-methods">
            <span class="text-xs text-muted">支付方式</span>
            <div v-if="payTypes.length > 1" class="pay-switch">
              <button
                v-for="pt in payTypes"
                :key="pt"
                type="button"
                class="pay-opt"
                :class="{ active: buy.payType === pt }"
                @click="buy.payType = pt"
              >
                <span class="pay-icon" :class="{ wx: pt === 'wxpay' }">{{ payTypeIcon(pt) }}</span>
                {{ payTypeLabel(pt) }}
              </button>
            </div>
            <div v-else class="pay-single">
              <span class="pay-icon" :class="{ wx: payTypes[0] === 'wxpay' }">{{ payTypeIcon(payTypes[0]) }}</span>
              <span>{{ payTypeLabel(payTypes[0]) }}</span>
            </div>
          </div>

          <div class="buy-summary">
            <div class="summary-line">
              <span class="text-muted">单价</span>
              <span class="mono font-semibold">¥{{ unitYuan }}</span>
            </div>
            <div class="summary-line total">
              <span>应付合计</span>
              <span class="total-amount mono">¥{{ totalYuan }}</span>
            </div>
          </div>

          <div class="buy-submit-row">
            <button
              class="btn-primary buy-submit"
              :disabled="!canBuy || buying || !!purchaseHint"
              @click="submitBuy"
            >
              {{ buying ? '创建订单中…' : '去支付' }}
            </button>
            <el-tooltip
              v-if="purchaseHint"
              :content="purchaseHint"
              placement="top"
              effect="dark"
              :show-after="120"
            >
              <button type="button" class="buy-hint-trigger" aria-label="购卡提示">
                <span aria-hidden="true">!</span>
              </button>
            </el-tooltip>
          </div>
        </div>
      </div>
    </section>

    <!-- 订单列表 -->
    <section class="card overflow-hidden !p-0">
      <div class="table-head flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 class="section-title">我的购卡订单</h3>
          <p class="text-xs text-muted mt-0.5">支付完成后可在下方复制卡密，或前往「我的卡密」</p>
        </div>
        <div class="flex gap-2">
          <select v-model="filterStatus" class="field-select !min-h-0 !py-1.5 text-sm" @change="reloadOrders">
            <option value="">全部状态</option>
            <option v-for="s in statusOptions" :key="s.value" :value="s.value">{{ s.label }}</option>
          </select>
          <button class="btn-secondary !min-h-0 !py-1.5 text-sm" :disabled="loading" @click="reloadOrders">刷新</button>
        </div>
      </div>

      <div class="overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>订单号</th>
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
              <td colspan="7" class="py-10 text-center text-muted">加载中…</td>
            </tr>
            <tr v-else-if="!orders.length">
              <td colspan="7" class="py-10 text-center text-muted">暂无订单，在上方选择套餐并支付</td>
            </tr>
            <tr v-for="o in orders" :key="o.order_no">
              <td class="mono text-xs">{{ o.order_no }}</td>
              <td>
                <span class="font-medium">{{ o.plan_label || o.plan }}</span>
                <span class="block text-xs text-muted mono">{{ o.plan }}</span>
              </td>
              <td class="mono">{{ o.count }}</td>
              <td class="mono font-semibold">¥{{ o.total_amount_yuan }}</td>
              <td>
                <span :class="orderStatusClass(o.status)">{{ orderStatusLabel(o.status) }}</span>
                <span
                  v-if="o.fail_reason && o.status === 'paid_undelivered'"
                  class="block text-xs text-err mt-0.5 max-w-[180px] truncate"
                  :title="o.fail_reason"
                >{{ o.fail_reason }}</span>
              </td>
              <td class="text-sm text-muted whitespace-nowrap">{{ formatDate(o.created_at) }}</td>
              <td class="text-right whitespace-nowrap">
                <button v-if="o.status === 'pending_pay'" class="btn-ghost !px-2.5 !py-1 text-sm" @click="repay(o)">继续支付</button>
                <button v-if="o.status === 'delivered' && o.issued_codes?.length" class="btn-ghost !px-2.5 !py-1 text-sm" @click="showCodes(o)">查看卡密</button>
                <button class="btn-ghost !px-2.5 !py-1 text-sm" @click="refreshOne(o)">刷新</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="table-foot">
        <Pagination :page="page" :page-size="pageSize" :total="total" @update:page="onPage" />
      </div>
    </section>

    <Modal :open="codesOpen" title="订单卡密" wide @close="codesOpen = false">
      <p class="text-sm text-muted mb-3">共 {{ codeModalList.length }} 张，已同步到「我的卡密」</p>
      <textarea class="input mono text-xs" rows="10" readonly :value="codeModalList.join('\n')" />
      <template #footer>
        <button class="btn-secondary" @click="codesOpen = false">关闭</button>
        <button class="btn-primary" @click="copyCodes">复制全部</button>
      </template>
    </Modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { agentFetch } from '../../lib/agentApi'
import { dialog } from '../../lib/dialog'
import Modal from '../../components/Modal.vue'
import Pagination from '../../components/Pagination.vue'
import { formatPartnerDate } from './partnerUi'

interface PlanRow {
  key: string
  label?: string
  price_cny_cents: number
  price_yuan: string
  fulfillment?: string
  stock?: number
}

interface OrderRow {
  order_no: string
  plan: string
  plan_label?: string
  count: number
  unit_price_cents: number
  total_amount_cents: number
  total_amount_yuan: string
  unit_price_yuan: string
  status: string
  pay_type?: string
  issued_codes?: string[]
  issued_count?: number
  fail_reason?: string
  created_at: string
  pay_url?: string
}

const maxCount = 50
const route = useRoute()
const router = useRouter()

const plans = ref<PlanRow[]>([])
const orders = ref<OrderRow[]>([])
const loading = ref(false)
const buying = ref(false)
const purchaseReady = ref(true)
const purchaseReason = ref('')
const cardPlatformReady = ref(true)
const payTypes = ref<Array<'alipay' | 'wxpay'>>(['alipay'])
const page = ref(1)
const pageSize = 20
const total = ref(0)
const filterStatus = ref('')

const buy = reactive({ plan: '', count: 1, payType: 'alipay' as 'alipay' | 'wxpay' })

const codesOpen = ref(false)
const codeModalList = ref<string[]>([])

const pricedPlans = computed(() => plans.value.filter((p) => (p.price_cny_cents || 0) > 0))

const selectedPlan = computed(() => plans.value.find((p) => p.key === buy.plan))

const unitYuan = computed(() => selectedPlan.value?.price_yuan || '0.00')

const totalYuan = computed(() => {
  const cents = (selectedPlan.value?.price_cny_cents || 0) * Math.max(1, buy.count || 1)
  return (cents / 100).toFixed(2)
})

function planFulfillment(p?: PlanRow) {
  if (!p) return 'card_platform'
  if (p.fulfillment) return p.fulfillment
  return p.key === 'gpt_white' ? 'local_stock' : 'card_platform'
}

function planStockText(p: PlanRow) {
  if (planFulfillment(p) !== 'local_stock' || typeof p.stock !== 'number') return ''
  return p.stock > 0 ? ` · 剩 ${p.stock}` : ' · 缺货'
}

/** 本站库存档位的可售数量；卡台档位返回 null（不限）。 */
const selectedStock = computed(() => {
  const p = selectedPlan.value
  if (!p || planFulfillment(p) !== 'local_stock') return null
  return typeof p.stock === 'number' ? p.stock : null
})

const purchaseHint = computed(() => {
  if (!purchaseReady.value) return purchaseReason.value || '暂无法下单'
  if (planFulfillment(selectedPlan.value) !== 'local_stock' && !cardPlatformReady.value) {
    return '卡台未配置，暂无法自动发码'
  }
  const stock = selectedStock.value
  if (stock != null && stock <= 0) return '该档位暂时缺货，请联系站长补货'
  if (stock != null && buy.count > stock) return `库存仅剩 ${stock} 个，请减少数量`
  return ''
})

const canBuy = computed(() => {
  if (!buy.plan || buy.count < 1 || buy.count > maxCount) return false
  if ((selectedPlan.value?.price_cny_cents || 0) <= 0) return false
  const stock = selectedStock.value
  if (stock != null && buy.count > stock) return false
  return true
})

const statusOptions = [
  { value: 'pending_pay', label: '待支付' },
  { value: 'delivered', label: '已发货' },
  { value: 'paid_undelivered', label: '待处理' },
  { value: 'expired', label: '已过期' },
]

function formatDate(v: string) {
  return formatPartnerDate(v)
}

function orderStatusLabel(s: string) {
  switch (s) {
    case 'pending_pay': return '待支付'
    case 'paid': return '已支付'
    case 'issuing': return '发货中'
    case 'delivered': return '已发货'
    case 'paid_undelivered': return '待处理'
    case 'expired': return '已过期'
    case 'cancelled': return '已取消'
    default: return s || '—'
  }
}

function orderStatusClass(s: string) {
  switch (s) {
    case 'delivered': return 'pill pill-good'
    case 'pending_pay': return 'pill pill-warn'
    case 'paid_undelivered': return 'pill pill-err'
    case 'issuing': return 'pill pill-info'
    default: return 'pill'
  }
}


function payTypeLabel(pt: string) {
  return pt === 'wxpay' ? '微信' : '支付宝'
}

function payTypeIcon(pt: string) {
  return pt === 'wxpay' ? '微' : '支'
}

function applyPayTypes(raw: unknown) {
  const allowed = new Set(['alipay', 'wxpay'])
  const next = Array.isArray(raw)
    ? raw.map((v) => String(v).trim().toLowerCase()).filter((v) => allowed.has(v))
    : []
  payTypes.value = (next.length ? next : ['alipay']) as Array<'alipay' | 'wxpay'>
  if (!payTypes.value.includes(buy.payType)) {
    buy.payType = payTypes.value[0]
  }
}

async function loadPlans() {
  const res = await agentFetch('/api/v1/agent/plans')
  if (!res.ok) {
    dialog.toast('加载套餐失败', 'err')
    return
  }
  const d = await res.json()
    plans.value = (d.plans || []).map((p: any) => ({
      key: p.key,
      label: p.label,
      price_cny_cents: p.price_cny_cents || 0,
      price_yuan: p.price_yuan || (p.price_cny_cents != null ? (p.price_cny_cents / 100).toFixed(2) : '0.00'),
      fulfillment: p.fulfillment || (p.key === 'gpt_white' ? 'local_stock' : 'card_platform'),
      stock: typeof p.stock === 'number' ? p.stock : undefined,
    }))
    purchaseReady.value = d.purchase?.ready !== false
    purchaseReason.value = String(d.purchase?.reason || '')
    cardPlatformReady.value = d.purchase?.card_platform_ready !== false
    applyPayTypes(d.purchase?.pay_types)
    if (!buy.plan && pricedPlans.value.length) {
      buy.plan = pricedPlans.value[0].key
    }
}

async function loadOrders() {
  loading.value = true
  try {
    const params = new URLSearchParams({ page: String(page.value), page_size: String(pageSize) })
    if (filterStatus.value) params.set('status', filterStatus.value)
    const res = await agentFetch(`/api/v1/agent/orders?${params}`)
    const d = await res.json()
    if (!res.ok) {
      dialog.toast(d.error || '加载失败', 'err')
      return
    }
    orders.value = d.list || []
    total.value = d.total || 0
  } finally {
    loading.value = false
  }
}

function reloadOrders() {
  page.value = 1
  // 顺手刷套餐：本站库存会被别的代理买走，缺货要及时反映到下单区。
  void loadPlans()
  loadOrders()
}

function onPage(p: number) {
  page.value = p
  loadOrders()
}

async function submitBuy() {
  if (!canBuy.value || purchaseHint.value) return
  buying.value = true
  try {
    const res = await agentFetch('/api/v1/agent/orders', {
      method: 'POST',
      body: JSON.stringify({ plan: buy.plan, count: buy.count, pay_type: buy.payType }),
    })
    const d = await res.json()
    if (!res.ok) {
      dialog.toast(d.error || '下单失败', 'err')
      return
    }
    const payURL = d.pay_url as string
    if (payURL) {
      window.location.href = payURL
      return
    }
    dialog.toast('订单已创建', 'ok')
    await loadOrders()
  } finally {
    buying.value = false
  }
}

async function refreshOne(o: OrderRow) {
  const res = await agentFetch(`/api/v1/agent/orders/${encodeURIComponent(o.order_no)}`)
  const d = await res.json()
  if (res.ok) {
    const idx = orders.value.findIndex((x) => x.order_no === o.order_no)
    if (idx >= 0) orders.value[idx] = { ...orders.value[idx], ...d }
  }
}

function showCodes(o: OrderRow) {
  codeModalList.value = o.issued_codes || []
  codesOpen.value = true
}

async function copyCodes() {
  try {
    await navigator.clipboard.writeText(codeModalList.value.join('\n'))
    dialog.toast('已复制', 'ok')
  } catch {
    dialog.toast('复制失败', 'err')
  }
}

async function repay(o: OrderRow) {
  buying.value = true
  try {
    const res = await agentFetch(`/api/v1/agent/orders/${encodeURIComponent(o.order_no)}/repay`, {
      method: 'POST',
    })
    const d = await res.json()
    if (!res.ok) {
      dialog.toast(d.error || '无法继续支付', 'err')
      await loadOrders()
      return
    }
    if (d.pay_url) {
      window.location.href = d.pay_url
      return
    }
    dialog.toast('无法获取支付链接', 'err')
  } finally {
    buying.value = false
  }
}

async function pollOrderAfterPay(orderNo: string, attempts = 15) {
  for (let i = 0; i < attempts; i++) {
    await new Promise((r) => setTimeout(r, 2000))
    const res = await agentFetch(`/api/v1/agent/orders/${encodeURIComponent(orderNo)}`)
    if (!res.ok) continue
    const d = await res.json()
    const idx = orders.value.findIndex((x) => x.order_no === orderNo)
    if (idx >= 0) orders.value[idx] = { ...orders.value[idx], ...d }
    if (d.status === 'delivered') {
      if (d.issued_codes?.length) showCodes(d)
      dialog.toast('支付成功，卡密已入库', 'ok')
      return
    }
    if (d.status === 'paid_undelivered') {
      dialog.toast('支付成功，发货处理中，请联系站长或稍后刷新', 'warn')
      return
    }
    if (d.status === 'expired') {
      dialog.toast('订单已过期', 'warn')
      return
    }
    if (!['pending_pay', 'paid', 'issuing'].includes(String(d.status))) return
  }
  dialog.toast('支付处理中，请稍后刷新订单', 'info')
}

watch(
  () => route.query.paid,
  async (paid) => {
    if (paid === '1') {
      const orderNo = String(route.query.order_no || '')
      await loadOrders()
      router.replace({ path: '/partner/orders' })
      if (orderNo) await pollOrderAfterPay(orderNo)
    }
  },
  { immediate: true },
)

onMounted(async () => {
  await loadPlans()
  await loadOrders()
  const no = String(route.query.order_no || '')
  if (no && route.query.paid !== '1') {
    const res = await agentFetch(`/api/v1/agent/orders/${encodeURIComponent(no)}`)
    if (res.ok) {
      const d = await res.json()
      if (d.status === 'delivered' && d.issued_codes?.length) showCodes(d)
    }
  }
})
</script>

<style scoped>
.buy-hero {
  border-radius: var(--radius-lg);
  border: 1px solid color-mix(in srgb, var(--primary) 22%, var(--brd));
  background: linear-gradient(135deg, color-mix(in srgb, var(--primary) 10%, var(--surface)), var(--surface));
  padding: 24px;
}
.buy-hero-inner {
  display: grid;
  gap: 24px;
}
@media (min-width: 1024px) {
  .buy-hero-inner { grid-template-columns: 1fr 1.1fr; align-items: start; }
}
.buy-badge {
  display: inline-block;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--primary);
  background: var(--primary-soft);
  padding: 4px 10px;
  border-radius: 999px;
}
.buy-title {
  margin-top: 12px;
  font-size: 1.65rem;
  font-weight: 800;
  color: var(--ink);
  font-family: var(--font-display, var(--font-serif));
}
.buy-sub { margin-top: 8px; font-size: 14px; color: var(--ink-2); max-width: 42ch; line-height: 1.55; }
.buy-panel { box-shadow: var(--shadow-md); }
.qty-row { display: flex; align-items: center; gap: 8px; }
.qty-btn {
  width: 36px; height: 36px; border-radius: 10px;
  border: 1px solid var(--brd); background: var(--surface-2);
  font-size: 18px; font-weight: 600; color: var(--ink);
  display: grid; place-items: center;
}
.qty-btn:disabled { opacity: 0.4; cursor: not-allowed; }
.qty-input { flex: 1; max-width: 80px; }
.pay-single {
  margin-top: 8px;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  border-radius: 12px;
  border: 1px solid var(--brd);
  background: var(--surface-2);
  font-size: 14px;
  font-weight: 600;
  color: var(--ink);
}
.pay-methods { margin-top: 16px; }
.pay-switch { display: flex; gap: 10px; margin-top: 8px; }
.pay-opt {
  flex: 1; display: flex; align-items: center; justify-content: center; gap: 8px;
  padding: 12px; border-radius: 12px; border: 2px solid var(--brd);
  background: var(--surface-2); font-size: 14px; font-weight: 600; color: var(--ink-2);
  transition: border-color .15s, background .15s;
}
.pay-opt.active {
  border-color: var(--primary);
  background: color-mix(in srgb, var(--primary) 8%, var(--surface));
  color: var(--ink);
}
.pay-icon {
  width: 28px; height: 28px; border-radius: 8px;
  background: #1677ff; color: #fff; font-size: 13px; font-weight: 800;
  display: grid; place-items: center;
}
.pay-icon.wx { background: #07c160; }
.buy-summary {
  margin-top: 18px; padding-top: 16px;
  border-top: 1px dashed var(--brd);
}
.summary-line { display: flex; justify-content: space-between; align-items: center; font-size: 14px; margin-bottom: 8px; }
.summary-line.total { margin-top: 4px; font-size: 15px; font-weight: 600; color: var(--ink); }
.total-amount { font-size: 1.5rem; font-weight: 800; color: var(--primary); }
.buy-submit-row {
  margin-top: 16px;
  display: flex;
  align-items: stretch;
  gap: 10px;
}
.buy-submit { flex: 1; min-height: 48px; font-size: 15px; margin-top: 0; }
.buy-hint-trigger {
  flex-shrink: 0;
  width: 48px;
  min-height: 48px;
  border-radius: 12px;
  border: 1px solid color-mix(in srgb, var(--warn, #d97706) 35%, var(--brd));
  background: color-mix(in srgb, var(--warn, #d97706) 10%, var(--surface));
  color: var(--warn, #d97706);
  font-size: 18px;
  font-weight: 800;
  display: grid;
  place-items: center;
  cursor: help;
  transition: background 0.15s, border-color 0.15s;
}
.buy-hint-trigger:hover {
  background: color-mix(in srgb, var(--warn, #d97706) 16%, var(--surface));
  border-color: color-mix(in srgb, var(--warn, #d97706) 55%, var(--brd));
}
.section-title { font-size: 1.05rem; font-weight: 700; color: var(--ink); }
.table-head { padding: 16px 20px; border-bottom: 1px solid var(--brd); }
.table-foot { padding: 12px 16px; border-top: 1px solid var(--brd); }
.pill-err { background: color-mix(in srgb, var(--err, #dc2626) 12%, transparent); color: var(--err, #dc2626); }
</style>
