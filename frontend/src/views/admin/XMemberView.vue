<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <div>
        <h2 class="text-xl font-bold text-ink">X 会员</h2>
        <p class="text-sm text-muted mt-1">每个套餐只从一家卡台出码，在「供货设置」里二选一。卡台凭证在「卡台」页改。</p>
      </div>
      <el-button @click="load">刷新</el-button>
    </div>

    <div class="grid gap-3 md:grid-cols-2">
      <div class="card space-y-2">
        <div class="flex items-center gap-2">
          <span class="dot" :class="spacexAcc && spacexCount ? 'ok' : 'off'" />
          <span class="font-semibold">SpaceX</span>
          <el-tag size="small" :type="spacexCount ? 'success' : 'info'" effect="plain">{{ spacexCount ? `供 ${spacexCount} 个套餐` : '没有套餐走这里' }}</el-tag>
        </div>
        <p class="text-sm">{{ spacexAcc ? spacexAcc.name + '（GPT 主台，同一套凭证）' : '还没有 SpaceX 主台' }}</p>
        <p class="text-sm text-muted">发码时锁定付款地区，客户兑换时填 X Cookie。</p>
        <p v-if="spacexAcc?.last_error" class="text-sm" style="color: var(--warn, #b45309)">{{ spacexAcc.last_error }}</p>
        <el-button size="small" @click="goPlatform(spacexAcc?.id || 0)">在卡台查看</el-button>
      </div>
      <div v-if="!avanXAcc" class="card space-y-2">
        <div class="flex items-center gap-2">
          <span class="dot idle" />
          <span class="font-semibold">Avanfinity X</span>
          <el-tag size="small" type="info" effect="plain">未接入 · 可选</el-tag>
        </div>
        <template v-if="avanGptAcc">
          <p class="text-sm">{{ avanGptAcc.name }}（GPT 备台）已配好凭证，X 可以直接用同一套。</p>
          <p class="text-sm text-muted">点下面开通后，再去卡台选一下付款方式（自动开卡或固定卡）并启用，Avanfinity 套餐就能发码。</p>
          <el-button size="small" type="primary" :loading="connecting" @click="connectAvan">用 {{ avanGptAcc.name }} 的凭证开通 X</el-button>
        </template>
        <template v-else>
          <p class="text-sm text-muted">现在所有 X 套餐都能走 SpaceX，不接也能发码。要用 Avanfinity 出码，先在卡台页添加一台「Avanfinity · X CDK」。</p>
          <el-button size="small" @click="router.push('/ops/platforms')">去卡台添加</el-button>
        </template>
      </div>
      <div v-for="ch in avanStrips" :key="ch.channel" class="card space-y-2">
        <div class="flex items-center gap-2">
          <span class="dot" :class="ch.alert || !ch.enabled ? 'off' : 'ok'" />
          <span class="font-semibold">{{ ch.channel === 'x_cdk' ? 'Avanfinity X' : channelName(ch.channel) }}</span>
          <el-tag size="small" :type="ch.enabled ? 'success' : 'info'" effect="plain">{{ ch.enabled ? '已启用' : '未启用' }}</el-tag>
          <el-tag v-if="ch.channel === 'x_cdk'" size="small" effect="plain">{{ avanCount ? `供 ${avanCount} 个套餐` : '没有套餐走这里' }}</el-tag>
        </div>
        <p class="text-sm">{{ ch.account_name || '未绑定卡台' }}</p>
        <p class="text-sm text-muted">
          钱包 {{ ch.wallet_usd || '—' }}
          <template v-if="ch.channel === 'x_cdk'"> · 未兑负债 ${{ ch.liability_usd || '0' }} · {{ ch.unused }} 张未兑</template>
          <template v-else> · 付款卡 {{ ch.card_label || '未选' }} 余额 {{ ch.card_balance || '—' }}</template>
        </p>
        <p v-if="ch.payments_enabled === false" class="text-sm" style="color: var(--err)">上游已关闭付款</p>
        <p v-if="ch.alert" class="text-sm" style="color: var(--warn, #b45309)">{{ ch.alert }}</p>
        <p v-if="ch.channel === 'x_cdk' && !ch.enabled" class="text-sm" style="color: var(--warn, #b45309)">通道还没启用：点下面按钮去卡台「付款卡」，选好后点「保存并启用通道」，再回来填「上限与告警」。</p>
        <el-button v-if="ch.channel === 'x_cdk' && !ch.enabled" size="small" type="primary" @click="goPlatform(ch.account_id || avanXAcc?.id || 0, 'cards')">去选付款方式</el-button>
        <el-button v-else size="small" @click="goPlatform(ch.account_id)">在卡台查看</el-button>
      </div>
    </div>

    <div v-if="overview" class="text-sm text-muted">
      今日开通 {{ overview.done_today || 0 }} · 进行中 {{ overview.running || 0 }} · 待处理 {{ overview.todo || 0 }} · 未兑 {{ overview.unused || 0 }}
    </div>

    <el-radio-group v-model="tab">
      <el-radio-button value="issue">发码</el-radio-button>
      <el-radio-button value="records">兑换记录</el-radio-button>
      <el-radio-button value="supply">供货设置</el-radio-button>
      <el-radio-button value="settings">上限与告警</el-radio-button>
    </el-radio-group>

    <div v-if="tab === 'issue'" class="card space-y-4">
      <p v-if="!sellable.length" class="text-sm text-muted">还没有在售的套餐，先去「供货设置」选卡台。</p>
      <div class="grid gap-2 sm:grid-cols-3">
        <div v-for="p in sellable" :key="p.key" role="button" tabindex="0" class="rounded-lg border px-3 py-2 text-left cursor-pointer" :class="[issue.plan === p.key ? 'border-current' : '', planBlocked(p) ? 'opacity-60' : '']" @click="issue.plan = p.key" @keydown.enter="issue.plan = p.key">
          <div class="flex items-center justify-between gap-2">
            <span class="font-medium">{{ p.label }}</span>
            <span class="src-tag" :class="p.source">{{ sourceName(p.source) }}</span>
          </div>
          <div class="text-xs text-muted mt-1">
            <template v-if="p.source !== 'avan'">兑换时客户填 X Cookie</template>
            <span v-else-if="!avanXAcc" style="color: var(--warn, #b45309)">Avanfinity X 未接入</span>
            <span v-else-if="!avanEnabled" style="color: var(--warn, #b45309)">X CDK 通道未启用</span>
            <button v-else-if="!limitFilled(p.avan_plan)" type="button" class="app-link" @click.stop="tab = 'settings'">还没填上限，去填</button>
            <template v-else>{{ planCardLine(p.avan_plan) }}</template>
          </div>
        </div>
      </div>
      <p class="text-sm text-muted">{{ currentSupply?.source === 'spacex' ? 'SpaceX：发码时锁定付款地区，客户兑换时要填 X 的 Cookie（auth_token / ct0）。' : 'Avanfinity：发码不扣钱，兑换时从钱包出，客户只填 X 用户名。上限在发码时锁死。' }}</p>
      <div v-if="currentSupply?.source === 'spacex'" class="flex flex-wrap items-center gap-2">
        <span class="text-sm">付款地区</span>
        <el-radio-group v-model="issue.payment_country" size="small">
          <el-radio-button v-for="r in regions" :key="r" :value="r">{{ r }}</el-radio-button>
        </el-radio-group>
      </div>
      <div class="flex flex-wrap gap-2">
        <el-button v-for="n in [1, 10, 50, 100]" :key="n" size="small" @click="issue.quantity = n">{{ n }}</el-button>
        <el-input-number v-model="issue.quantity" :min="1" :max="200" />
        <el-input v-model="issue.note" class="!max-w-xs" placeholder="备注，客服可搜" />
        <el-button type="primary" :loading="issuing" :disabled="!currentSupply || planBlocked(currentSupply)" @click="doIssue">生成</el-button>
      </div>
      <p v-if="currentSupply && planBlocked(currentSupply)" class="text-sm" style="color: var(--warn, #b45309)">
        {{ !avanXAcc ? '这个套餐走 Avanfinity，但 Avanfinity X 还没接入，先在上方开通；或在「供货设置」里改成 SpaceX。' : 'X CDK 通道还没启用：先去卡台「付款卡」点「保存并启用通道」。' }}
      </p>
      <p v-if="currentSupply" class="text-sm">{{ issue.quantity }} 张 {{ currentSupply.label }} · 来自 {{ sourceName(currentSupply.source) }}<template v-if="currentSupply.source === 'spacex'"> · {{ issue.payment_country }} 付款</template></p>
      <div v-if="links.length" class="space-y-1">
        <div class="flex gap-2">
          <el-button @click="copy(links.join('\n'))">复制兑换链接</el-button>
          <el-button @click="copy(issuedCodes.join('\n'))">复制卡密</el-button>
        </div>
        <div v-for="link in links" :key="link" class="mono text-xs">{{ link }}</div>
      </div>
      <div class="text-sm font-medium">最近批次</div>
      <div v-for="b in batches" :key="b.id" class="flex flex-wrap items-center justify-between gap-2 text-sm">
        <span>{{ b.created_at }} · {{ planName(b.plan) }} · {{ channelName(b.channel) }} · {{ b.used }}/{{ b.quantity }} · {{ batchStatus(b.status) }} · {{ b.note }}</span>
        <span class="flex gap-2">
          <button v-if="b.status === 'pending'" class="app-link" type="button" @click="retryBatch(b.id)">重试这一批</button>
          <button class="app-link" type="button" @click="exportBatch(b.id)">导出</button>
        </span>
      </div>
    </div>

    <div v-else-if="tab === 'records'" class="grid gap-3 lg:grid-cols-[minmax(0,1fr)_22rem]">
      <div class="space-y-3">
        <div class="flex flex-wrap gap-2">
          <el-button v-for="g in groups" :key="g.key" size="small" :type="recGroup === g.key ? 'primary' : 'default'" @click="recGroup = g.key; loadRecords()">{{ g.label }}</el-button>
          <el-input v-model="recQ" class="!max-w-xs" placeholder="搜卡密 / 用户名 / 备注" @change="loadRecords" />
        </div>
        <button v-for="r in records" :key="r.code_id" type="button" class="card w-full space-y-1 text-left text-sm" :class="selected?.code_id === r.code_id ? 'ring-1 ring-current' : ''" @click="selected = r">
          <div class="flex flex-wrap items-center gap-2">
            <span class="mono">{{ r.code }}</span>
            <span>{{ planName(r.plan) }}</span>
            <span>{{ r.recipient ? '@' + r.recipient : '—' }}</span>
            <el-tag size="small" effect="plain">{{ statusName(r.status) }}</el-tag>
          </div>
          <p class="text-muted">{{ r.message || groupName(r.group) }} · {{ usd(r.estimated_usd_e4) }}</p>
        </button>
        <p v-if="!records.length" class="text-sm text-muted">这一组是空的。</p>
      </div>
      <div v-if="selected" class="card space-y-3 text-sm">
        <div class="font-semibold">{{ statusName(selected.status) }}</div>
        <p v-if="selected.message" class="rounded-lg p-3" style="background: var(--warn-soft, #fff7ed)">{{ selected.message }}</p>
        <div>套餐 {{ planName(selected.plan) }} · {{ channelName(selected.channel) }}</div>
        <div>开通给 {{ selected.recipient ? '@' + selected.recipient : '—' }}</div>
        <div v-if="selected.note">备注 {{ selected.note }}</div>
        <div>官方金额 {{ selected.amount_minor || '—' }} {{ selected.currency }} · 参考 {{ usd(selected.estimated_usd_e4) }} · 服务费 {{ usd(selected.service_fee_e4) }}</div>
        <div class="font-medium">处理过程</div>
        <div v-for="(ev, i) in eventsOf(selected)" :key="i" class="text-muted">{{ ev.t }} · {{ ev.s }}</div>
        <p v-if="!eventsOf(selected).length" class="text-muted">还没有过程记录。</p>
        <div class="flex flex-wrap gap-2">
          <el-button v-if="selected.redemption_id" size="small" @click="requery(selected.redemption_id)">重新查询</el-button>
          <el-button v-if="selected.group === 'todo' && selected.redemption_id" size="small" @click="resolve(selected)">人工处理</el-button>
          <el-button v-if="selected.status === 'unused' || selected.status === 'uncertain'" size="small" @click="disableCode(selected)">作废</el-button>
          <el-button size="small" @click="copy(locationOrigin() + '/x?code=' + selected.code)">复制兑换链接</el-button>
        </div>
        <details class="text-xs text-muted">
          <summary>排障信息</summary>
          <div class="mt-2 space-y-1">
            <div>账户 {{ selected.account_id || '—' }}</div>
            <div>上游订单 {{ selected.upstream_order_id || '—' }}</div>
            <div>请求 {{ selected.client_request_id || '—' }}</div>
            <div>上游状态 {{ selected.upstream_status || '—' }} · 已查 {{ selected.poll_count || 0 }} 次</div>
            <div>注资 {{ selected.funding_dispatched ? '已发出' : '未发出' }} · 付款 {{ selected.payment_dispatched ? '已发出' : '未发出' }}</div>
          </div>
        </details>
      </div>
    </div>

    <XSupplyTable v-else-if="tab === 'supply'" @change="onSupplyChange" />

    <div v-else class="space-y-4">
      <div class="grid gap-3 md:grid-cols-2">
        <div v-for="ch in visibleChannels" :key="'sw-' + ch.channel" class="card space-y-2">
          <div class="flex items-center justify-between">
            <span class="font-semibold">{{ channelName(ch.channel) }}</span>
            <el-switch :model-value="ch.enabled" @change="onToggle(ch, $event)" />
          </div>
          <p class="text-sm">{{ channelLine(ch) }}</p>
          <p class="text-xs text-muted">启用前要在「卡台」里选好账户和付款卡。一个通道同时只用一个卡台。</p>
          <el-button size="small" @click="testQuote(ch.channel)">试报价</el-button>
        </div>
      </div>

      <div class="card space-y-3">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div class="font-semibold">每个套餐的花费上限</div>
          <el-button size="small" @click="fillBuffer">按实测 +15% 填入</el-button>
        </div>
        <p class="text-xs text-muted">金额用美元十进制。已经发出去的码按发码当时的快照执行，改这里不影响它们。</p>
        <div class="overflow-x-auto">
          <table class="data-table">
            <thead>
              <tr>
                <th>套餐</th>
                <th>通道</th>
                <th>启用</th>
                <th>最近实测</th>
                <th>币种</th>
                <th>官方金额上限</th>
                <th>CDK 钱包上限</th>
                <th>CDK 注资</th>
                <th v-if="X_DIRECT_UI">直充服务费上限</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in visibleLimits" :key="row.channel + row.plan">
                <td>{{ planName(row.plan) }}</td>
                <td>{{ row.channel === 'x_cdk' ? 'CDK' : '直充' }}</td>
                <td><el-switch v-model="row.enabled" /></td>
                <td class="text-xs">{{ sampleLine(row.channel, row.plan) }}</td>
                <td><el-input v-model="row.currency" class="!w-20" /></td>
                <td><el-input v-model.number="row.max_official_amount_minor" class="!w-28" /></td>
                <td><el-input v-model="row.max_wallet_debit_usd" class="!w-24" :disabled="row.channel !== 'x_cdk'" /></td>
                <td><el-input v-model="row.funding_amount_usd" class="!w-24" :disabled="row.channel !== 'x_cdk'" /></td>
                <td v-if="X_DIRECT_UI"><el-input v-model="row.max_service_fee_usd" class="!w-24" :disabled="row.channel !== 'x_direct'" /></td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="grid gap-3 sm:grid-cols-3">
          <el-form-item label="钱包低于（美元）告警" class="!mb-0">
            <el-input v-model="alerts.wallet_usd" />
          </el-form-item>
          <el-form-item v-if="X_DIRECT_UI" label="付款卡低于（美元）告警" class="!mb-0">
            <el-input v-model="alerts.card_usd" />
          </el-form-item>
          <el-form-item label="单子卡住超过（分钟）" class="!mb-0">
            <el-input v-model="alerts.stuck_minutes" />
          </el-form-item>
        </div>
        <el-button type="primary" :loading="saving" @click="saveLimits">保存上限和告警</el-button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { dialog } from '../../lib/dialog'
import { authFetch } from '../../lib/api'
import { X_DIRECT_UI } from '../../lib/features'
import XSupplyTable from '../../components/XSupplyTable.vue'

interface SupplyRow {
  key: string
  label: string
  source: string
  options: string[]
  enabled: boolean
  spacex_plan: string
  avan_plan: string
}

interface Sample {
  channel: string
  plan: string
  currency: string
  amount_minor: number
  estimated_usd_e4: number
  service_fee_e4: number
  created_at: string
}
interface RecordRow {
  code_id: number
  redemption_id: number
  code: string
  plan: string
  channel: string
  status: string
  group: string
  note: string
  recipient: string
  message: string
  events: string
  amount_minor: number
  currency: string
  estimated_usd_e4: number
  service_fee_e4: number
  poll_count: number
  funding_dispatched: boolean
  payment_dispatched: boolean
  upstream_order_id: string
  upstream_status: string
  client_request_id: string
  account_id: number
}

const router = useRouter()
const route = useRoute()
const tab = ref(String(route.query.tab || 'issue'))
watch(() => route.query.tab, (v) => { if (v) tab.value = String(v) })
const supply = ref<SupplyRow[]>([])
const regions = ['JP', 'US', 'PH', 'NG', 'TR', 'EG']
const sellable = computed(() => supply.value.filter((p) => p.source !== 'off'))
const currentSupply = computed(() => sellable.value.find((p) => p.key === issue.plan))
const visibleStrips = computed(() => strips.value.filter((s) => X_DIRECT_UI || s.channel !== 'x_direct'))
// SpaceX 的 X 套餐和 GPT 共用主台凭证；Avanfinity X 是单独一台 api-v1 账户。
const spacexAcc = computed(() => {
  const sx = accounts.value.filter((a) => !a.protocol || a.protocol === 'spacexcard-legacy')
  return sx.find((a) => a.is_primary_default) || sx.find((a) => a.status === 'active') || sx[0]
})
const avanXAcc = computed(() => accounts.value.find((a) => a.protocol === 'avanfinity-api-v1'))
// GPT 备台和 X 账户用同一套 Avanfinity 凭证；有备台时可以一键开通 X。
const avanGptAcc = computed(() => accounts.value.find((a) => a.protocol === 'avanfinity-2026-08' && a.app_id && a.has_credential))
const avanEnabled = computed(() => !!channels.value.find((c) => c.channel === 'x_cdk')?.enabled)
const connecting = ref(false)
const spacexCount = computed(() => supply.value.filter((p) => p.source === 'spacex').length)
const avanCount = computed(() => supply.value.filter((p) => p.source === 'avan').length)
const avanStrips = computed(() => (avanXAcc.value ? visibleStrips.value : visibleStrips.value.filter((s) => s.channel !== 'x_cdk')))
const visibleChannels = computed(() => channels.value.filter((s) => X_DIRECT_UI || s.channel !== 'x_direct'))
const visibleLimits = computed(() => limits.value.filter((s) => X_DIRECT_UI || s.channel !== 'x_direct'))
const channels = ref<any[]>([])
const limits = ref<any[]>([])
const samples = ref<Sample[]>([])
const strips = ref<any[]>([])
const accounts = ref<any[]>([])
const saving = ref(false)
const issuing = ref(false)
const alerts = reactive({ wallet_usd: '', card_usd: '', stuck_minutes: '' })
const overview = ref<any>(null)
const issue = reactive({ plan: 'premium_3m', channel: 'x_cdk', quantity: 1, note: '', payment_country: 'JP' })
const links = ref<string[]>([])
const issuedCodes = ref<string[]>([])
const batches = ref<any[]>([])
const records = ref<RecordRow[]>([])
const selected = ref<RecordRow | null>(null)
const recGroup = ref('todo')
const recQ = ref('')
const groups = [
  { key: 'todo', label: '待处理' },
  { key: 'running', label: '进行中' },
  { key: 'done', label: '已完成' },
  { key: 'unused', label: '未使用' },
  { key: 'failed', label: '失败已退回' },
  { key: 'all', label: '全部' },
]
const names: Record<string, string> = {
  premium_3m: 'Premium · 3 个月',
  premium_6m: 'Premium · 6 个月',
  premium_12m: 'Premium · 12 个月',
  premium_plus_3m: 'Premium+ · 3 个月',
  premium_plus_6m: 'Premium+ · 6 个月',
  premium_plus_12m: 'Premium+ · 12 个月',
  x_basic_monthly: 'Basic · 月付',
  x_basic_yearly: 'Basic · 年付',
  x_premium_monthly: 'Premium · 月付',
  x_premium_yearly: 'Premium · 12 个月',
  x_premium_plus_monthly: 'Premium+ · 月付',
  x_premium_plus_yearly: 'Premium+ · 12 个月',
}
const statusNames: Record<string, string> = {
  unused: '未使用',
  quoted: '待确认',
  funding: '注资中',
  funded: '已注资',
  paying: '付款中',
  paid_pending_delivery: '已付款，待到账',
  completed: '已开通',
  review_required: '待人工',
  requires_action: '需要处理',
  uncertain: '结果不确定',
  disabled: '已作废',
}
let timer: ReturnType<typeof setInterval> | null = null

function planName(k: string) { return names[k] || k }
function statusName(k: string) { return statusNames[k] || k }
function channelName(k: string) { return k === 'x_cdk' ? 'X CDK' : k === 'x_direct' ? 'X 直充' : k }
function groupName(k: string) { return groups.find((g) => g.key === k)?.label || k }
function batchStatus(k: string) {
  if (k === 'pending') return '待确认'
  if (k === 'failed') return '失败'
  return '已生成'
}
function usd(e4: number) {
  if (!e4) return '—'
  return '$' + (e4 / 10000).toFixed(2)
}
function accountName(id: number) {
  return accounts.value.find((a) => a.id === id)?.name || (id ? `账户 ${id}` : '未绑定卡台')
}
function channelLine(ch: any) {
  const who = accountName(ch.account_id)
  if (ch.channel === 'x_direct') return `${who} · 付款卡 ${ch.card_id || '未选'}`
  return `${who} · ${ch.auto_card ? '自动开卡' : ch.card_id ? '固定卡 ' + ch.card_id : '未选付款方式'}`
}
function sampleOf(channel: string, plan: string) {
  return samples.value.find((s) => s.channel === channel && s.plan === plan)
}
function sampleLine(channel: string, plan: string) {
  const s = sampleOf(channel, plan)
  if (!s) return '还没有'
  const money = s.estimated_usd_e4 ? usd(s.estimated_usd_e4) : `${s.amount_minor || '—'} ${s.currency || ''}`
  return `${money} · ${s.created_at || ''}`
}
function planCardLine(plan: string) {
  const row = limits.value.find((r) => r.channel === issue.channel && r.plan === plan)
  const sample = sampleOf(issue.channel, plan)
  if (issue.channel === 'x_direct' && sample?.estimated_usd_e4) return `实测 ≈ ${usd(sample.estimated_usd_e4)}`
  if (row?.max_wallet_debit_usd) return `每张最多 $${row.max_wallet_debit_usd}`
  if (row?.max_official_amount_minor) return `上限 ${row.max_official_amount_minor} ${row.currency || ''}`
  return '还没填上限'
}
// 和后端 limitsReady 对齐：钱包上限、币种、官方金额上限都要有。
function limitFilled(plan: string) {
  const row = limits.value.find((r) => r.channel === 'x_cdk' && r.plan === plan)
  return !!(row?.max_wallet_debit_usd && row?.currency && row?.max_official_amount_minor)
}
function planBlocked(p: SupplyRow) {
  return p.source === 'avan' && (!avanXAcc.value || !avanEnabled.value)
}
async function connectAvan() {
  connecting.value = true
  try {
    const r = await authFetch('/api/v1/admin/x/connect-avan', { method: 'POST' })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) { dialog.toast(d.error || '开通失败', 'err'); return }
    dialog.toast('已开通，去选付款方式并启用通道', 'ok')
    await load()
    goPlatform(d.id, 'cards')
  } finally { connecting.value = false }
}
function eventsOf(row: RecordRow) {
  try {
    const parsed = JSON.parse(row.events || '[]')
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}
function goPlatform(id: number, tab = 'overview') {
  router.push({ path: '/ops/platforms', query: { account: String(id || ''), tab } })
}
function sourceName(s: string) { return s === 'spacex' ? 'SpaceX' : s === 'avan' ? 'Avanfinity' : '停售' }
function onSupplyChange(plans: unknown[]) {
  supply.value = plans as SupplyRow[]
  if (!sellable.value.some((p) => p.key === issue.plan) && sellable.value.length) issue.plan = sellable.value[0].key
}
async function loadSupply() {
  const r = await authFetch('/api/v1/admin/x/supply')
  const d = await r.json().catch(() => ({}))
  if (!r.ok) return
  onSupplyChange(d.plans || [])
}
function locationOrigin() {
  return typeof window === 'undefined' ? '' : window.location.origin
}
async function copy(text: string) {
  try { await navigator.clipboard.writeText(text); dialog.toast('已复制', 'ok') } catch { dialog.toast('复制失败', 'err') }
}
async function doIssue() {
  issuing.value = true
  try {
    const body = { plan: issue.plan, quantity: issue.quantity, note: issue.note, payment_country: issue.payment_country }
    const r = await authFetch('/api/v1/admin/x/supply/issue', { method: 'POST', body: JSON.stringify(body) })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) { dialog.toast(d.error || '发码失败', 'err'); return }
    if (d.partial_error) dialog.toast('部分失败：' + d.partial_error, 'warn')
    links.value = d.links || []
    issuedCodes.value = d.codes || []
    dialog.toast(`已生成 ${issuedCodes.value.length} 张`, 'ok')
    await loadBatches()
    await loadOverview()
  } finally { issuing.value = false }
}
async function retryBatch(id: number) {
  const r = await authFetch(`/api/v1/admin/x/batches/${id}/retry`, { method: 'POST' })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) { dialog.toast(d.error || '重试失败', 'err'); return }
  issuedCodes.value = d.codes || []
  links.value = issuedCodes.value.map((code: string) => locationOrigin() + '/x?code=' + code)
  dialog.toast(`这一批有 ${issuedCodes.value.length} 张`, 'ok')
  await loadBatches()
}
async function exportBatch(id: number) {
  const r = await authFetch(`/api/v1/admin/x/batches/${id}/export`)
  if (!r.ok) { dialog.toast('导出失败', 'err'); return }
  const blob = await r.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `x-batch-${id}.xlsx`
  a.click()
  URL.revokeObjectURL(url)
}
async function loadBatches() {
  const r = await authFetch('/api/v1/admin/x/batches')
  const d = await r.json().catch(() => ({}))
  batches.value = d.batches || []
}
async function loadRecords() {
  const r = await authFetch(`/api/v1/admin/x/records?group=${recGroup.value}&q=${encodeURIComponent(recQ.value)}`)
  const d = await r.json().catch(() => ({}))
  records.value = d.records || []
  if (selected.value) {
    selected.value = records.value.find((row) => row.code_id === selected.value?.code_id) || selected.value
  }
}
async function requery(id: number) {
  const r = await authFetch(`/api/v1/admin/x/records/${id}/requery`, { method: 'POST' })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) { dialog.toast(d.error || '查询失败', 'err'); return }
  dialog.toast('已重新查询', 'ok')
  await loadRecords()
}
async function resolve(row: RecordRow) {
  const outcome = await dialog.select('处理完成后，客户页会按这个结果变化。', [
    { label: '标记为已开通', value: 'completed', desc: '客户看到开通成功' },
    { label: '放回未使用', value: 'release', desc: '只有确认没扣款时才能放回' },
    { label: '作废卡密', value: 'disable', desc: '客户不能再使用这张码' },
  ], { title: '人工处理' })
  if (!outcome) return
  const note = await dialog.prompt('处理备注（必填，会写入审计）', { title: '处理备注' })
  if (!note || !String(note).trim()) return
  const r = await authFetch(`/api/v1/admin/x/records/${row.redemption_id}/resolve`, {
    method: 'POST',
    body: JSON.stringify({ outcome, note: String(note).trim() }),
  })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) { dialog.toast(d.error || '保存失败', 'err'); return }
  dialog.toast('已处理', 'ok')
  await loadRecords()
  await loadOverview()
}
async function disableCode(row: RecordRow) {
  const ok = await dialog.confirm(`作废 ${row.code}？CDK 通道会同时撤销还没动钱的上游码。`, { title: '作废卡密', danger: true, okText: '作废' })
  if (!ok) return
  const r = await authFetch(`/api/v1/admin/x/codes/${row.code_id}/disable`, { method: 'POST' })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) { dialog.toast(d.error || '作废失败', 'err'); return }
  dialog.toast('已作废', 'ok')
  await loadRecords()
}
async function testQuote(channel: string) {
  const recipient = await dialog.prompt('用一个测试 X 用户名报价。直充会马上取消，CDK 会发一张测试码再撤销，不会留给客户。', {
    title: '试报价',
    placeholder: 'username',
  })
  if (!recipient || !String(recipient).trim()) return
  const r = await authFetch('/api/v1/admin/x/test-quote', {
    method: 'POST',
    body: JSON.stringify({ channel, plan: issue.plan, recipient: String(recipient).trim() }),
  })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) { dialog.toast(d.error || '试报价失败', 'err'); return }
  const s = d.sample || {}
  dialog.toast(`已记下 ${s.amount_minor || ''} ${s.currency || ''} ${usd(s.estimated_usd_e4)}`, 'ok')
  await load()
}
function fillBuffer() {
  let n = 0
  for (const row of limits.value) {
    const s = sampleOf(row.channel, row.plan)
    if (!s) continue
    if (s.amount_minor > 0) row.max_official_amount_minor = Math.ceil(s.amount_minor * 1.15)
    if (s.currency) row.currency = s.currency
    if (s.estimated_usd_e4 > 0 && row.channel === 'x_cdk') {
      row.max_wallet_debit_usd = (s.estimated_usd_e4 * 1.15 / 10000).toFixed(4)
    }
    if (s.service_fee_e4 > 0 && row.channel === 'x_direct') {
      row.max_service_fee_usd = (s.service_fee_e4 * 1.15 / 10000).toFixed(4)
    }
    n++
  }
  dialog.toast(n ? `已填入 ${n} 行，保存后才生效` : '还没有实测报价', n ? 'ok' : 'warn')
}
async function loadOverview() {
  const ovRes = await authFetch('/api/v1/admin/x/overview')
  overview.value = await ovRes.json().catch(() => null)
  strips.value = overview.value?.strips || []
}
async function load() {
  const [cfgRes, accRes] = await Promise.all([
    authFetch('/api/v1/admin/x/config'),
    authFetch('/api/v1/admin/card-platforms'),
  ])
  const cfg = await cfgRes.json().catch(() => ({}))
  const acc = await accRes.json().catch(() => ({}))
  if (cfgRes.ok) {
    channels.value = cfg.channels || []
    limits.value = cfg.limits || []
    samples.value = cfg.samples || []
    alerts.wallet_usd = cfg.alerts?.wallet_usd || ''
    alerts.card_usd = cfg.alerts?.card_usd || ''
    alerts.stuck_minutes = cfg.alerts?.stuck_minutes || ''
  }
  if (accRes.ok) accounts.value = acc.accounts || []
  await loadOverview()
}
function onToggle(ch: any, value: string | number | boolean) {
  toggleChannel(ch, value === true || value === 'true' || value === 1)
}
async function toggleChannel(ch: any, enabled: boolean) {
  const r = await authFetch('/api/v1/admin/x/channels', {
    method: 'PUT',
    body: JSON.stringify({ ...ch, enabled }),
  })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) {
    dialog.toast(d.error || '不能启用', 'err')
    await load()
    return
  }
  channels.value = d.channels || channels.value
  dialog.toast(enabled ? '通道已启用' : '通道已停用', 'ok')
}
async function saveLimits() {
  saving.value = true
  try {
    const r = await authFetch('/api/v1/admin/x/plan-limits', {
      method: 'PUT',
      body: JSON.stringify({ limits: limits.value, alerts }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '保存失败', 'err')
      return
    }
    limits.value = d.limits || limits.value
    dialog.toast('已保存', 'ok')
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  void load()
  void loadSupply()
  void loadBatches()
  void loadRecords()
  timer = setInterval(() => void loadOverview(), 30000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<style scoped>
.dot { display: inline-block; width: 8px; height: 8px; border-radius: 999px; background: #16a34a; }
.dot.off { background: #d97706; }
.dot.ok { background: #16a34a; }
.dot.idle { background: #9ca3af; }
.src-tag { font-size: 11px; padding: 1px 6px; border-radius: 4px; border: 1px solid currentColor; opacity: .8; }
.src-tag.spacex { color: #2563eb; }
.src-tag.avan { color: #7c3aed; }
</style>
