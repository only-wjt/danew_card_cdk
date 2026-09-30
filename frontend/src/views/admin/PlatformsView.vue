<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 class="text-xl font-bold text-ink">卡台</h2>
        <p class="text-sm text-muted mt-1">OpenAI 发码和 X 会员分开管。先选左边的卡台，再看它的凭证、卡和回调。</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <div class="rounded-lg border border-line px-3 py-2">
          <div class="text-xs text-muted">出口 IP · 填到白名单</div>
          <div class="mono font-semibold text-ink">{{ egressIp || '…' }}</div>
        </div>
        <el-button :disabled="!egressIp" @click="copyText(egressIp)">复制</el-button>
        <el-button type="primary" @click="openCreate">添加卡台</el-button>
      </div>
    </div>

    <el-alert v-if="problems.length" type="warning" :closable="false" show-icon :title="`${problems.length} 个卡台需要处理`">
      <div v-for="p in problems" :key="p.id + p.tab" class="flex flex-wrap items-center gap-2 py-1">
        <span class="text-sm">{{ p.text }}</span>
        <el-button link type="primary" @click="openAccount(p.id, p.tab)">{{ p.action }}</el-button>
      </div>
    </el-alert>

    <div class="grid gap-4 lg:grid-cols-[240px_minmax(0,1fr)]">
      <aside class="space-y-1">
        <div class="px-2 pt-1 text-xs tracking-wide text-muted">OPENAI 发码</div>
        <button
          v-for="a in openaiAccounts"
          :key="a.id"
          type="button"
          class="side-item"
          :class="{ on: sel === String(a.id) }"
          @click="openAccount(a.id, 'overview')"
        >
          <span class="dot" :class="healthOf(a)" />
          <span class="min-w-0 flex-1 text-left">
            <span class="block truncate text-sm">{{ a.name }}</span>
            <span class="block text-xs text-muted">{{ a.is_primary_default ? '主台' : '备台' }} · {{ brief(a) }}</span>
          </span>
        </button>
        <button type="button" class="side-item" :class="{ on: sel === 'policy' }" @click="sel = 'policy'">
          <span class="text-sm">发码策略</span>
          <span class="ml-auto text-xs text-muted">双绑 · 顺序</span>
        </button>

        <div class="px-2 pt-3 text-xs tracking-wide text-muted">X 会员</div>
        <button
          v-for="a in xAccounts"
          :key="a.id"
          type="button"
          class="side-item"
          :class="{ on: sel === String(a.id) }"
          @click="openAccount(a.id, 'overview')"
        >
          <span class="dot" :class="healthOf(a)" />
          <span class="min-w-0 flex-1 text-left">
            <span class="block truncate text-sm">{{ a.name }}</span>
            <span class="block text-xs text-muted">{{ capLabel(a) }} · {{ brief(a) }}</span>
          </span>
        </button>
        <p v-if="!xAccounts.length" class="px-2 text-xs text-muted">还没有 X 会员卡台</p>

        <div class="px-2 pt-3 text-xs tracking-wide text-muted">其他</div>
        <button type="button" class="side-item" :class="{ on: sel === 'orphan' }" @click="sel = 'orphan'">
          <span class="text-sm">未归属回调</span>
        </button>
        <button type="button" class="side-item" :class="{ on: sel === 'swap' }" @click="sel = 'swap'">
          <span class="text-sm">代理换码</span>
        </button>
      </aside>

      <section class="min-w-0">
        <div v-if="current" class="space-y-4">
          <div class="flex flex-wrap items-center gap-2">
            <h3 class="text-lg font-semibold text-ink">{{ current.name }}</h3>
            <el-tag :type="healthOf(current) === 'ok' ? 'success' : healthOf(current) === 'bad' ? 'danger' : 'warning'" effect="plain">
              {{ healthText(current) }}
            </el-tag>
            <el-tag v-if="current.serves_openai" effect="plain">{{ current.is_primary_default ? '主台' : '备台' }}</el-tag>
            <el-tag v-else type="warning" effect="plain">{{ capLabel(current) }}</el-tag>
            <span class="text-xs text-muted">{{ protocolLabel(current.protocol) }}</span>
            <div class="ml-auto flex gap-2">
              <el-button v-if="current.serves_openai" :loading="pinging" @click="pingOpenAI">测连通</el-button>
              <el-button v-else type="primary" :loading="probing" @click="probeX">测试连接</el-button>
              <el-button :type="current.status === 'active' ? 'danger' : 'success'" plain @click="toggleStatus">
                {{ current.status === 'active' ? '停用' : '启用' }}
              </el-button>
            </div>
          </div>
          <el-alert v-if="current.last_error" :type="current.serves_openai ? 'warning' : 'error'" :closable="false" :title="current.last_error" />

          <el-radio-group v-model="tab" size="small">
            <el-radio-button value="overview">概览</el-radio-button>
            <el-radio-button value="cards">{{ current.serves_openai ? '选卡' : '付款卡' }}</el-radio-button>
            <el-radio-button v-if="current.serves_openai" value="webhook">回调</el-radio-button>
            <el-radio-button v-if="!current.serves_openai" value="calls">调用记录</el-radio-button>
            <el-radio-button value="credentials">凭证</el-radio-button>
          </el-radio-group>

          <div v-if="tab === 'overview' && current.serves_openai" class="card space-y-3">
            <p class="text-sm text-muted">{{ current.site_base }}</p>
            <p class="text-sm">熔断 {{ current.circuit_state === 'open' ? '已打开' : '关闭' }} · 最近成功 {{ current.last_ok_at || '—' }}</p>
            <p class="text-sm">可消费余额 <strong>{{ openaiSpendable ? '$' + openaiSpendable : '读取中…' }}</strong>
              <span v-if="openaiReserve" class="text-muted"> · 保证金 ${{ openaiReserve }}，可消费余额不含这笔</span>
            </p>
            <div v-if="planFees.length" class="text-sm">
              <div class="text-muted mb-1">套餐服务费</div>
              <div v-for="p in planFees" :key="p.key" class="flex justify-between gap-4">
                <span>{{ p.label || p.key }}</span>
                <span>${{ Number(p.fee_usd || 0).toFixed(2) }}</span>
              </div>
            </div>
            <p v-if="pingMsg" class="text-sm">{{ pingMsg }}</p>
            <el-button v-if="current.circuit_state === 'open'" type="warning" plain @click="resetCircuit">复位熔断</el-button>
          </div>

          <div v-else-if="tab === 'overview'" class="space-y-3">
            <div class="card space-y-1 text-sm">
              <div>地址 {{ current.site_base }}</div>
              <div>App ID <span class="mono">{{ current.app_id || '—' }}</span></div>
              <div class="text-muted">X 没有回调。测试连接按顺序检查凭证、权限、卡片和写接口白名单，挂在哪一步就停在哪一步。</div>
            </div>
            <div v-if="probeSteps.length" class="card space-y-2">
              <div v-for="s in probeSteps" :key="s.key" class="text-sm">
                <span :class="s.state === 'ok' ? 'text-emerald-600' : s.state === 'fail' ? 'text-red-600' : 'text-muted'">
                  {{ s.state === 'ok' ? '通过' : s.state === 'fail' ? '失败' : '跳过' }}
                </span>
                · {{ s.title }} · {{ s.detail }}
              </div>
            </div>
          </div>

          <CardSelectionConfig v-else-if="tab === 'cards' && current.serves_openai" embedded :fixed-account-id="current.id" />

          <div v-else-if="tab === 'cards'" class="card space-y-3">
            <p class="text-sm text-muted">{{ capLabel(current) }} 兑换时用哪张卡。通道开关和花费上限在「X 会员」。</p>
            <el-radio-group v-model="payMode" class="flex flex-col items-start gap-2">
              <el-radio v-if="hasCap(current, 'x_cdk')" value="auto">每张码自动开一张新卡</el-radio>
              <el-radio v-for="card in xCards" :key="card.id" :value="String(card.id)" :disabled="!cardOk(card)">
                {{ card.cardNumberMasked || ('卡 ' + card.id) }} · {{ card.status || '未知' }} · 余额 {{ card.balance || '—' }}
              </el-radio>
            </el-radio-group>
            <p v-if="!xCards.length" class="text-sm text-muted">还没有拉到卡。点「测试连接」，或确认这个 App 能读到卡。</p>
            <div v-if="payMode === 'auto'" class="grid gap-2 sm:grid-cols-3">
              <el-input v-model="autoCard.product" placeholder="开卡产品编码" />
              <el-input v-model="autoCard.first" placeholder="名" />
              <el-input v-model="autoCard.last" placeholder="姓" />
            </div>
            <el-button type="primary" :loading="savingChannel" @click="savePayCard">保存付款卡</el-button>
          </div>

          <div v-else-if="tab === 'webhook'" class="card space-y-3">
            <el-alert v-if="!current.has_webhook_secret" type="warning" :closable="false" title="还没填 Webhook Secret。把地址贴到这台的开发者页，再把 whsec_… 填回来。" />
            <el-input :model-value="webhookUrl" readonly />
            <div class="flex gap-2">
              <el-button @click="copyText(webhookUrl)">复制地址</el-button>
            </div>
            <div class="flex flex-wrap gap-2">
              <el-input v-model="webhookUrlDraft" class="!max-w-xl" placeholder="要改回调路径时再改这里" />
              <el-button :loading="savingHook" @click="saveWebhookUrl">保存地址</el-button>
            </div>
            <div class="flex flex-wrap gap-2">
              <el-input v-model="webhookSecret" class="!max-w-md" type="password" show-password :placeholder="current.has_webhook_secret ? '已配置，留空不改' : 'whsec_…'" />
              <el-button type="primary" :loading="savingHook" @click="saveWebhookSecret">保存 Secret</el-button>
            </div>
            <WebhookEvents embedded :fixed-account-id="current.id" />
          </div>

          <div v-else-if="tab === 'calls'" class="card space-y-2 text-sm">
            <p class="text-muted">X 没有回调。这里是本站调用上游的记录，用来看白名单、限流和权限。</p>
            <div v-for="(row, i) in calls" :key="i" class="flex gap-3">
              <span class="text-muted">{{ row.At }}</span>
              <span>{{ row.Method }} {{ row.Path }}</span>
              <span>{{ row.Status }}</span>
              <span class="text-muted">{{ row.Detail }}</span>
            </div>
            <p v-if="!calls.length" class="text-muted">还没有调用记录。</p>
          </div>

          <div v-else class="card space-y-3">
            <el-form label-position="top">
              <el-form-item label="名称"><el-input v-model="edit.name" /></el-form-item>
              <el-form-item label="卡台地址"><el-input v-model="edit.site_base" /></el-form-item>
              <template v-if="!current.serves_openai">
                <el-form-item label="App ID"><el-input v-model="edit.cred_public" /></el-form-item>
                <el-form-item label="App Secret"><el-input v-model="edit.cred_secret" type="password" show-password placeholder="留空不修改" /></el-form-item>
                <el-checkbox v-model="edit.xCdk">X CDK</el-checkbox>
                <el-checkbox v-model="edit.xDirect" class="ml-4">X 直充</el-checkbox>
              </template>
              <el-form-item v-else label="Open API Key">
                <el-input v-model="edit.cred_secret" type="password" show-password placeholder="留空不修改" />
              </el-form-item>
            </el-form>
            <el-button type="primary" :loading="saving" @click="saveEdit">保存</el-button>
          </div>
        </div>

        <div v-else-if="sel === 'policy'" class="card space-y-4">
          <h3 class="text-lg font-semibold text-ink">OpenAI 发码策略</h3>
          <div class="flex items-center gap-3">
            <el-switch v-model="dual.enabled" active-text="本站双绑发码" @change="saveDual" />
            <span class="text-sm text-muted">新码为 DN-，每台各买一张；兑换时 A 不可达自动切 B</span>
          </div>
          <el-checkbox v-model="dual.allowSingle" :disabled="!dual.enabled" @change="saveDual">允许单台降级出货</el-checkbox>
          <div class="space-y-2">
            <div class="text-sm font-semibold">顺序和主台</div>
            <p class="text-xs text-muted">排第一的是主台。老码仍走原来的台，不会因为换主台被转发走。</p>
            <div v-for="(a, i) in openaiAccounts" :key="a.id" class="flex items-center gap-2 rounded border border-line px-3 py-2">
              <span class="text-sm font-medium">{{ i + 1 }}. {{ a.name }}</span>
              <el-tag v-if="i === 0" size="small" effect="plain">主台</el-tag>
              <div class="ml-auto">
                <el-button link :disabled="i === 0" @click="move(i, -1)">上移</el-button>
                <el-button link :disabled="i === openaiAccounts.length - 1" @click="move(i, 1)">下移</el-button>
              </div>
            </div>
          </div>
        </div>

        <div v-else-if="sel === 'orphan'" class="space-y-2">
          <h3 class="text-lg font-semibold text-ink">未归属回调</h3>
          <p class="text-sm text-muted">路径没对上任何卡台。通常是开发者页填错了地址。</p>
          <WebhookEvents embedded orphans-only />
        </div>

        <div v-else class="card max-w-xl space-y-3">
          <h3 class="text-lg font-semibold text-ink">代理换码</h3>
          <p class="text-sm text-muted">代理凭密码进入隐藏页，把失败且未扣款的 CDK 换成新码。导航里不展示这个入口。</p>
          <el-input v-model="swapPassword" type="password" show-password :placeholder="swapSet ? '已设置，留空不修改' : '至少 6 位'" />
          <div class="flex gap-2">
            <el-input :model-value="swapUrl" readonly />
            <el-button @click="copyText(swapUrl)">复制</el-button>
          </div>
          <el-button type="primary" :loading="saving" @click="saveSwap">保存</el-button>
        </div>
      </section>
    </div>

    <el-dialog v-model="dlg" title="添加卡台" width="520px" align-center destroy-on-close>
      <el-form label-position="top">
        <el-form-item label="这个卡台用来做什么">
          <el-select v-model="create.kind" class="w-full">
            <el-option label="OpenAI · SpaceX 旧 OpenAPI" value="legacy" />
            <el-option label="OpenAI · Avanfinity" value="avan_openai" />
            <el-option label="X 会员 · Avanfinity" value="x" />
          </el-select>
        </el-form-item>
        <el-form-item label="名称"><el-input v-model="create.name" /></el-form-item>
        <el-form-item label="卡台地址"><el-input v-model="create.site_base" placeholder="https://" /></el-form-item>
        <template v-if="create.kind === 'x'">
          <el-form-item label="App ID"><el-input v-model="create.app_id" /></el-form-item>
          <el-form-item label="App Secret"><el-input v-model="create.secret" type="password" show-password /></el-form-item>
          <el-checkbox v-model="create.xCdk">X CDK</el-checkbox>
          <el-checkbox v-model="create.xDirect" class="ml-4">X 直充</el-checkbox>
        </template>
        <template v-else>
          <el-form-item label="Open API Key"><el-input v-model="create.secret" type="password" show-password /></el-form-item>
          <p class="text-xs text-muted">新卡台默认排在最后，不设为主台。顺序到「发码策略」里调。回调 Secret 可以稍后填。</p>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="dlg = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="createAccount">保存并测连通</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { authFetch } from '../../lib/api'
import { dialog } from '../../lib/dialog'
import CardSelectionConfig from './CardSelectionConfig.vue'
import WebhookEvents from './WebhookEvents.vue'

interface Acc {
  id: number
  name: string
  protocol: string
  capabilities: string
  app_id: string
  site_base: string
  serves_openai: boolean
  status: string
  priority: number
  is_primary_default: boolean
  has_credential: boolean
  has_webhook_secret: boolean
  webhook_url?: string
  circuit_state: string
  last_error?: string
  last_ok_at?: string
}

const route = useRoute()
const router = useRouter()
const accounts = ref<Acc[]>([])
const sel = ref('policy')
const tab = ref('overview')
const egressIp = ref('')
const dlg = ref(false)
const saving = ref(false)
const pinging = ref(false)
const probing = ref(false)
const savingHook = ref(false)
const savingChannel = ref(false)
const pingMsg = ref('')
const pingSpendable = ref('')
const openaiSpendable = ref('')
const openaiReserve = ref('')
const planFees = ref<{ key: string; label: string; fee_usd: number }[]>([])
const probeSteps = ref<{ key: string; title: string; state: string; detail: string }[]>([])
const xCards = ref<any[]>([])
const calls = ref<any[]>([])
const payMode = ref('auto')
const autoCard = reactive({ product: '', first: '', last: '' })
const channelRow = ref<any>(null)
const webhookUrlDraft = ref('')
const webhookSecret = ref('')
const swapPassword = ref('')
const swapSet = ref(false)
const dual = reactive({ enabled: false, allowSingle: false })
const edit = reactive({ name: '', site_base: '', cred_public: '', cred_secret: '', xCdk: false, xDirect: false })
const create = reactive({
  kind: 'x',
  name: '',
  site_base: '',
  app_id: '',
  secret: '',
  xCdk: true,
  xDirect: false,
})

const openaiAccounts = computed(() => accounts.value.filter((a) => a.serves_openai).sort((a, b) => a.priority - b.priority || a.id - b.id))
const xAccounts = computed(() => accounts.value.filter((a) => !a.serves_openai))
const current = computed(() => accounts.value.find((a) => String(a.id) === sel.value) || null)
const webhookUrl = computed(() => current.value?.webhook_url || '')
const swapUrl = computed(() => (typeof window === 'undefined' ? '/partner/swap' : `${window.location.origin}/partner/swap`))

const problems = computed(() => {
  const out: { id: number; tab: string; text: string; action: string }[] = []
  for (const a of accounts.value) {
    if (a.status !== 'active') continue
    if (a.serves_openai && !a.has_webhook_secret) {
      out.push({ id: a.id, tab: 'webhook', text: `${a.name}：还没配 Webhook Secret，回调会被拒绝。`, action: '去配置回调' })
    }
    if (a.serves_openai && a.circuit_state === 'open') {
      out.push({ id: a.id, tab: 'overview', text: `${a.name}：熔断已打开，不再参与发码。`, action: '查看' })
    }
    if (!a.serves_openai && a.last_error) {
      out.push({ id: a.id, tab: 'overview', text: `${a.name}：${a.last_error}`, action: '测连通' })
    }
  }
  return out
})

function hasCap(a: Acc, cap: string) {
  return (a.capabilities || '').split(',').includes(cap)
}
function capLabel(a: Acc) {
  const bits = []
  if (hasCap(a, 'x_cdk')) bits.push('X CDK')
  if (hasCap(a, 'x_direct')) bits.push('X 直充')
  return bits.join(' · ') || 'X 会员'
}
function protocolLabel(p: string) {
  if (p === 'avanfinity-api-v1') return 'Avanfinity · X 会员'
  if (p === 'avanfinity-2026-08') return 'Avanfinity · OpenAI'
  return 'SpaceX 旧 OpenAPI'
}
function healthOf(a: Acc) {
  if (a.status !== 'active') return 'off'
  if (!a.serves_openai && a.last_error) return 'bad'
  if (a.circuit_state === 'open') return 'bad'
  if (a.serves_openai && !a.has_webhook_secret) return 'warn'
  return 'ok'
}
function healthText(a: Acc) {
  const h = healthOf(a)
  if (h === 'bad') return a.serves_openai ? '不可用' : '需要处理'
  if (h === 'warn') return '需要处理'
  if (h === 'off') return '已停用'
  return '正常'
}
function brief(a: Acc) {
  if (a.status !== 'active') return '已停用'
  if (!a.serves_openai && a.last_error) return '不可用'
  if (a.circuit_state === 'open') return '熔断'
  if (a.serves_openai && !a.has_webhook_secret) return '缺回调'
  return '正常'
}

async function copyText(t: string) {
  try {
    await navigator.clipboard.writeText(t)
    dialog.toast('已复制', 'ok')
  } catch {
    dialog.toast('复制失败', 'err')
  }
}

async function load() {
  const r = await authFetch('/api/v1/admin/card-platforms')
  const d = await r.json().catch(() => ({}))
  if (!r.ok) return
  accounts.value = (d.accounts || []).map((a: any) => ({
    ...a,
    serves_openai: a.serves_openai !== false && a.protocol !== 'avanfinity-api-v1',
    capabilities: a.capabilities || '',
    app_id: a.app_id || '',
  }))
  dual.enabled = !!d.dual_bind
  dual.allowSingle = !!d.allow_single
}

async function loadEgress() {
  const r = await authFetch('/api/v1/admin/network/egress')
  const d = await r.json().catch(() => ({}))
  egressIp.value = d.egress_ip || ''
}

async function loadSwap() {
  const r = await authFetch('/api/v1/admin/settings')
  if (!r.ok) return
  const d = await r.json()
  swapSet.value = !!d.agent_swap_password_configured
}

function openAccount(id: number, nextTab: string) {
  sel.value = String(id)
  tab.value = nextTab
  probeSteps.value = []
  pingMsg.value = ''
  openaiSpendable.value = ''
  openaiReserve.value = ''
  planFees.value = []
}

async function loadOpenAIOverview() {
  const a = current.value
  if (!a?.serves_openai) return
  const r = await authFetch('/api/v1/admin/card-platforms/ping', { method: 'POST', body: JSON.stringify({ id: a.id }) })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) {
    pingMsg.value = d.error || '读不到余额'
    return
  }
  openaiSpendable.value = d.spendable_usd || ''
  openaiReserve.value = d.reserve_usd || ''
  planFees.value = d.plan_fees || []
  pingSpendable.value = d.spendable_usd || ''
  if (d.egress_ip) egressIp.value = d.egress_ip
}

function fillEdit() {
  const a = current.value
  if (!a) return
  edit.name = a.name
  edit.site_base = a.site_base
  edit.cred_public = a.app_id
  edit.cred_secret = ''
  edit.xCdk = hasCap(a, 'x_cdk')
  edit.xDirect = hasCap(a, 'x_direct')
  webhookUrlDraft.value = a.webhook_url || ''
  webhookSecret.value = ''
}

function openCreate() {
  create.kind = 'x'
  create.name = ''
  create.site_base = ''
  create.app_id = ''
  create.secret = ''
  create.xCdk = true
  create.xDirect = false
  dlg.value = true
}

function capsOf(xCdk: boolean, xDirect: boolean) {
  return [xCdk ? 'x_cdk' : '', xDirect ? 'x_direct' : ''].filter(Boolean).join(',')
}

async function createAccount() {
  const x = create.kind === 'x'
  if (x && !create.xCdk && !create.xDirect) {
    dialog.toast('至少勾选 X CDK 或 X 直充', 'err')
    return
  }
  saving.value = true
  try {
    const body: any = {
      name: create.name.trim(),
      site_base: create.site_base.trim(),
      cred_secret: create.secret.trim(),
      protocol: x ? 'avanfinity-api-v1' : create.kind === 'avan_openai' ? 'avanfinity-2026-08' : 'spacexcard-legacy',
      status: 'active',
      priority: 100,
      is_primary_default: false,
    }
    if (x) {
      body.cred_public = create.app_id.trim()
      body.capabilities = capsOf(create.xCdk, create.xDirect)
    }
    const r = await authFetch('/api/v1/admin/card-platforms/upsert', { method: 'POST', body: JSON.stringify(body) })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '保存失败', 'err')
      return
    }
    accounts.value = (d.accounts || accounts.value).map((a: any) => ({
      ...a,
      serves_openai: a.serves_openai !== false && a.protocol !== 'avanfinity-api-v1',
    }))
    const created = [...accounts.value].reverse().find((a) => a.name === body.name)
    dlg.value = false
    if (created) openAccount(created.id, 'overview')
    if (x && created) await probeX()
    else if (created) await pingOpenAI()
    dialog.toast('已添加', 'ok')
  } finally {
    saving.value = false
  }
}

async function saveEdit() {
  const a = current.value
  if (!a) return
  saving.value = true
  try {
    const body: any = {
      id: a.id,
      name: edit.name.trim(),
      site_base: edit.site_base.trim(),
      protocol: a.protocol,
      cred_secret: edit.cred_secret.trim(),
      status: a.status,
      priority: a.priority,
      is_primary_default: a.is_primary_default,
    }
    if (!a.serves_openai) {
      body.cred_public = edit.cred_public.trim()
      body.capabilities = capsOf(edit.xCdk, edit.xDirect)
    }
    const r = await authFetch('/api/v1/admin/card-platforms/upsert', { method: 'POST', body: JSON.stringify(body) })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '保存失败', 'err')
      return
    }
    await load()
    dialog.toast('已保存', 'ok')
  } finally {
    saving.value = false
  }
}

async function toggleStatus() {
  const a = current.value
  if (!a) return
  const status = a.status === 'active' ? 'disabled' : 'active'
  const r = await authFetch('/api/v1/admin/card-platforms/status', { method: 'POST', body: JSON.stringify({ id: a.id, status }) })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) {
    dialog.toast(d.error || '操作失败', 'err')
    return
  }
  await load()
}

async function pingOpenAI() {
  const a = current.value
  if (!a) return
  pinging.value = true
  try {
    const r = await authFetch('/api/v1/admin/card-platforms/ping', { method: 'POST', body: JSON.stringify({ id: a.id }) })
    const d = await r.json().catch(() => ({}))
    pingMsg.value = d.message || d.error || ''
    pingSpendable.value = d.spendable_usd || ''
    if (d.egress_ip) egressIp.value = d.egress_ip
    dialog.toast(r.ok ? pingMsg.value || '已探测' : d.error || '探测失败', r.ok ? 'ok' : 'err')
    await load()
  } finally {
    pinging.value = false
  }
}

async function probeX() {
  const a = current.value
  if (!a) return
  probing.value = true
  try {
    const r = await authFetch('/api/v1/admin/card-platforms/probe-x', { method: 'POST', body: JSON.stringify({ id: a.id }) })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '测试失败', 'err')
      return
    }
    probeSteps.value = d.steps || []
    xCards.value = d.cards || xCards.value
    if (d.egress_ip) egressIp.value = d.egress_ip
    dialog.toast(d.ok ? '测试通过' : '有一步没通过', d.ok ? 'ok' : 'warn')
    await load()
  } finally {
    probing.value = false
  }
}

async function resetCircuit() {
  const a = current.value
  if (!a) return
  await authFetch('/api/v1/admin/card-platforms/reset-circuit', { method: 'POST', body: JSON.stringify({ id: a.id }) })
  await load()
}

async function saveWebhookUrl() {
  const a = current.value
  if (!a) return
  savingHook.value = true
  try {
    const r = await authFetch('/api/v1/admin/card-platforms/webhook-url', {
      method: 'POST',
      body: JSON.stringify({ id: a.id, webhook_url: webhookUrlDraft.value }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '保存失败', 'err')
      return
    }
    await load()
    dialog.toast('地址已保存', 'ok')
  } finally {
    savingHook.value = false
  }
}

async function saveWebhookSecret() {
  const a = current.value
  if (!a || !webhookSecret.value.trim()) return
  savingHook.value = true
  try {
    const r = await authFetch('/api/v1/admin/card-platforms/webhook-secret', {
      method: 'POST',
      body: JSON.stringify({ id: a.id, webhook_secret: webhookSecret.value.trim() }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '保存失败', 'err')
      return
    }
    webhookSecret.value = ''
    await load()
    dialog.toast('Secret 已保存', 'ok')
  } finally {
    savingHook.value = false
  }
}

function cardOk(card: any) {
  const st = String(card.status || '').toLowerCase()
  return !st.includes('frozen') && !st.includes('closed') && !st.includes('disabled')
}

async function loadXPay() {
  const a = current.value
  if (!a || a.serves_openai) return
  const [cardsRes, cfgRes] = await Promise.all([
    authFetch(`/api/v1/admin/card-platforms/x-cards?id=${a.id}`),
    authFetch('/api/v1/admin/x/config'),
  ])
  const cards = await cardsRes.json().catch(() => ({}))
  const cfg = await cfgRes.json().catch(() => ({}))
  if (cardsRes.ok) xCards.value = cards.cards || []
  const cap = hasCap(a, 'x_direct') && !hasCap(a, 'x_cdk') ? 'x_direct' : 'x_cdk'
  const row = (cfg.channels || []).find((c: any) => c.channel === cap && c.account_id === a.id)
    || (cfg.channels || []).find((c: any) => c.channel === cap)
  channelRow.value = row || null
  if (row?.auto_card) payMode.value = 'auto'
  else if (row?.card_id) payMode.value = String(row.card_id)
  else payMode.value = hasCap(a, 'x_cdk') ? 'auto' : (xCards.value[0] ? String(xCards.value[0].id) : 'auto')
  autoCard.product = row?.auto_card_product || ''
  autoCard.first = row?.auto_card_first_name || ''
  autoCard.last = row?.auto_card_last_name || ''
}

async function savePayCard() {
  const a = current.value
  if (!a) return
  const channel = hasCap(a, 'x_direct') && !hasCap(a, 'x_cdk') ? 'x_direct' : hasCap(a, 'x_cdk') ? 'x_cdk' : 'x_direct'
  const bound = channelRow.value
  if (bound && bound.account_id && bound.account_id !== a.id) {
    const ok = await dialog.confirm(`这个通道现在绑在其他卡台上。要把它切到「${a.name}」吗？`)
    if (!ok) return
  }
  const auto = payMode.value === 'auto'
  savingChannel.value = true
  try {
    const r = await authFetch('/api/v1/admin/x/channels', {
      method: 'PUT',
      body: JSON.stringify({
        channel,
        account_id: a.id,
        enabled: !!(channelRow.value && channelRow.value.enabled && channelRow.value.account_id === a.id),
        card_id: auto ? 0 : Number(payMode.value),
        auto_card: auto,
        auto_card_product: autoCard.product,
        auto_card_first_name: autoCard.first,
        auto_card_last_name: autoCard.last,
      }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '保存失败', 'err')
      return
    }
    dialog.toast('付款卡已保存', 'ok')
    await loadXPay()
  } finally {
    savingChannel.value = false
  }
}

async function saveDual() {
  const r = await authFetch('/api/v1/admin/card-platforms/dual-bind', {
    method: 'PUT',
    body: JSON.stringify({ enabled: dual.enabled, allow_single: dual.allowSingle }),
  })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) {
    dialog.toast(d.error || '保存失败', 'err')
    await load()
    return
  }
  dialog.toast('已保存', 'ok')
}

async function move(index: number, dir: number) {
  const ids = openaiAccounts.value.map((a) => a.id)
  const j = index + dir
  ;[ids[index], ids[j]] = [ids[j], ids[index]]
  const r = await authFetch('/api/v1/admin/card-platforms/reorder', { method: 'POST', body: JSON.stringify({ ids }) })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) {
    dialog.toast(d.error || '调整失败', 'err')
    return
  }
  await load()
}

async function saveSwap() {
  if (swapPassword.value && swapPassword.value.trim().length < 6) {
    dialog.toast('密码至少 6 位', 'err')
    return
  }
  saving.value = true
  try {
    const body: any = {}
    if (swapPassword.value.trim()) body.agent_swap_password = swapPassword.value.trim()
    const r = await authFetch('/api/v1/admin/settings', { method: 'PUT', body: JSON.stringify(body) })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '保存失败', 'err')
      return
    }
    swapPassword.value = ''
    swapSet.value = true
    dialog.toast('已保存', 'ok')
  } finally {
    saving.value = false
  }
}

watch(current, () => fillEdit())
async function loadCalls() {
  const a = current.value
  if (!a) return
  const r = await authFetch('/api/v1/admin/card-platforms/x-calls?id=' + a.id)
  const d = await r.json().catch(() => ({}))
  calls.value = d.calls || []
}
watch(tab, (v) => {
  if (v === 'cards' && current.value && !current.value.serves_openai) loadXPay()
  if (v === 'calls') loadCalls()
})

onMounted(async () => {
  await Promise.all([load(), loadEgress(), loadSwap()])
  const qAccount = String(route.query.account || route.query.account_id || '')
  const qTab = String(route.query.tab || '')
  const panel = String(route.query.panel || '')
  if (panel === 'orphan') sel.value = 'orphan'
  else if (panel === 'swap') sel.value = 'swap'
  else if (panel === 'policy') sel.value = 'policy'
  else if (qAccount && accounts.value.some((a) => String(a.id) === qAccount)) {
    sel.value = qAccount
    tab.value = qTab || 'overview'
  } else if (panel === 'webhook') {
    const p = openaiAccounts.value.find((a) => a.is_primary_default) || openaiAccounts.value[0]
    if (p) openAccount(p.id, 'webhook')
  } else if (problems.value[0]) {
    openAccount(problems.value[0].id, problems.value[0].tab)
  } else if (openaiAccounts.value[0]) {
    openAccount(openaiAccounts.value[0].id, 'overview')
  } else if (xAccounts.value[0]) {
    openAccount(xAccounts.value[0].id, 'overview')
  }
})

watch([sel, tab], () => {
  if (tab.value === 'overview' && current.value?.serves_openai) void loadOpenAIOverview()
  if (sel.value === 'policy' || sel.value === 'orphan' || sel.value === 'swap') {
    router.replace({ query: { panel: sel.value } })
    return
  }
  router.replace({ query: { account: sel.value, tab: tab.value } })
})
</script>

<style scoped>
.side-item {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 8px;
  border-radius: 8px;
  padding: 8px 10px;
}
.side-item.on {
  background: var(--fill, rgba(0, 0, 0, 0.04));
}
.dot {
  width: 8px;
  height: 8px;
  border-radius: 999px;
  background: #16a34a;
  flex-shrink: 0;
}
.dot.warn { background: #ca8a04; }
.dot.bad { background: #dc2626; }
.dot.off { background: #a3a3a3; }
</style>
