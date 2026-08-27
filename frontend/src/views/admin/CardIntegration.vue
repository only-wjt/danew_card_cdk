<template>
  <div class="space-y-4">
    <!-- 出口 IP -->
    <div class="card egress-hero">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div class="text-xs uppercase tracking-wide text-muted">本机出口 IP · 卡台白名单</div>
          <div class="mt-1 flex flex-wrap items-baseline gap-3">
            <span class="text-3xl font-bold mono text-ink">{{ egressIp || '…' }}</span>
            <el-tag v-if="egressIp" size="small" effect="dark" type="warning">填到卡台 API Key 白名单</el-tag>
          </div>
          <p class="text-xs text-subtle mt-2">发码/拉价格/余额从此 IP 出网（不是浏览器 IP）</p>
        </div>
        <div class="flex gap-2">
          <el-button type="primary" :disabled="!egressIp" @click="copyText(egressIp)">复制 IP</el-button>
          <el-button :loading="loadingNet" @click="loadNetwork">重探测</el-button>
        </div>
      </div>
    </div>

    <!-- 兼容工具：卡台凭证只在下方账户维护 -->
    <div class="card space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 class="text-xl font-bold text-ink">代理换码与主台检测</h2>
          <p class="text-sm text-muted mt-1">卡台 Base、Key 和优先级统一在下方「多卡台账户」维护，避免两套配置冲突。</p>
        </div>
        <div class="flex gap-2">
          <el-tag v-if="hints.card_api_key_configured" type="success" effect="plain">主台兼容配置已同步</el-tag>
          <el-tag v-else type="info" effect="plain">尚未配置主台</el-tag>
        </div>
      </div>

      <el-alert
        type="info"
        :closable="false"
        title="下方标记为“主台”的账户会自动镜像给旧模块；不要再维护第二份 Base/Key。"
      />

      <el-form label-position="top" class="max-w-xl" @submit.prevent>
        <el-form-item label="代理换码密码">
          <el-input
            v-model="secrets.agent_swap_password"
            type="password"
            show-password
            clearable
            size="large"
            :placeholder="swapPwHint"
            autocomplete="off"
          />
          <p class="text-xs text-subtle mt-1">
            代理凭此密码进入隐藏页，将<strong>失败且未扣款</strong>的 CDK 换一张新码。留空保存=不修改；
            <el-tag v-if="hints.agent_swap_password_configured" size="small" type="success" effect="plain">已设置</el-tag>
            <el-tag v-else size="small" type="info" effect="plain">未设置</el-tag>
          </p>
        </el-form-item>
        <el-form-item label="发给代理的换码链接（可复制）">
          <div class="flex flex-wrap items-center gap-2 w-full">
            <el-input :model-value="agentSwapUrl" readonly size="large" class="!flex-1 mono" />
            <el-button type="primary" size="large" @click="copyText(agentSwapUrl)">复制链接</el-button>
            <el-button size="large" @click="openAgentSwap()">打开</el-button>
          </div>
          <p class="text-xs text-subtle mt-1">
            路径固定 <code class="mono">/partner/swap</code>（短链 <code class="mono">/a/swap</code>）。导航栏不展示，把完整链接 + 密码发给代理即可。
          </p>
        </el-form-item>
        <div class="flex flex-wrap gap-2">
          <el-button type="primary" size="large" :loading="saving" @click="save">保存换码配置</el-button>
          <el-button type="success" size="large" plain :loading="busy" @click="runAllChecks">
            一键检测
          </el-button>
          <el-button size="large" :loading="pinging" @click="ping">连通</el-button>
          <el-button size="large" :loading="loadingPlans" @click="loadPlans">价格</el-button>
          <el-button size="large" :loading="loadingBal" @click="loadBalance">余额</el-button>
        </div>
      </el-form>
    </div>

    <!-- 多卡台 · 本站统一发码（DN- 双绑） -->
    <div class="card space-y-4">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 class="text-xl font-bold text-ink">多卡台账户</h2>
          <p class="text-sm text-muted mt-1">
            开启后新发码为 <code class="mono">DN-</code> 本站码，生成时向各台各买一张；兑换时 A 不可达自动切 B。老码不受影响。
          </p>
        </div>
        <div class="flex flex-wrap items-center gap-3">
          <el-switch
            v-model="dualBind.enabled"
            active-text="本站双绑发码"
            :loading="savingDualBind"
            @change="saveDualBind"
          />
          <el-tag v-if="dualBind.enabled" type="success" effect="plain">已开启</el-tag>
          <el-tag v-else type="info" effect="plain">关（仍走上方单台）</el-tag>
        </div>
      </div>

      <el-alert
        v-if="platformUnusable.length"
        type="warning"
        :closable="false"
        show-icon
        :title="'部分账户不可用：' + platformUnusable.join('；')"
      />

      <div class="flex flex-wrap items-center gap-4 text-sm">
        <el-checkbox
          v-model="dualBind.allowSingle"
          :disabled="!dualBind.enabled"
          @change="saveDualBind"
        >
          允许单台降级出货（缺 B 仍出货，默认关）
        </el-checkbox>
        <span class="text-muted">活跃账户 {{ activePlatformCount }} / {{ platformAccounts.length }}</span>
      </div>

      <p class="text-sm text-muted">
        每台回调 URL 可改，保存后把同一条贴到该台开发者页。路径须在 <code>/api/v1/webhooks/</code> 下。
        最近事件在
        <router-link class="app-link" to="/ops/webhooks">Webhook 事件</router-link>。
      </p>

      <el-alert
        v-if="missingWebhookNames"
        type="warning"
        :closable="false"
        show-icon
        :title="`启用中尚未配置 Webhook Secret：${missingWebhookNames}。未配时该台回调会被 503/401 拒绝。`"
      />
      <el-alert
        v-else-if="legacySecretSet && !platformAccounts.some((a) => a.has_webhook_secret)"
        type="info"
        :closable="false"
        show-icon
        :title="`仍在用旧的全局 Secret（${legacySecretHint || '已配置'}）。建议按卡台各填一份，避免 A/B 串台。`"
      />

      <el-empty v-if="!platformAccounts.length && !loadingPlatforms" description="尚未添加卡台账户">
        <el-button type="primary" @click="openAccountForm()">添加第一台</el-button>
      </el-empty>

      <div v-else class="space-y-3">
        <div
          v-for="acc in platformAccounts"
          :key="acc.id"
          class="platform-card"
          :class="{ 'platform-card--open': acc.circuit_state === 'open' }"
        >
          <div class="flex flex-wrap items-start justify-between gap-2">
            <div>
              <div class="flex flex-wrap items-center gap-2">
                <span class="font-semibold text-ink">{{ acc.name }}</span>
                <el-tag size="small" :type="acc.is_primary_default ? 'warning' : 'info'" effect="plain">
                  {{ acc.is_primary_default ? '主台' : '备台' }} · P{{ acc.priority }}
                </el-tag>
                <el-tag size="small" :type="acc.status === 'active' ? 'success' : 'info'" effect="plain">
                  {{ acc.status === 'active' ? '启用' : '停用' }}
                </el-tag>
                <el-tag v-if="acc.circuit_state === 'open'" size="small" type="danger" effect="plain">熔断</el-tag>
                <el-tag size="small" :type="acc.has_webhook_secret ? 'success' : 'danger'" effect="plain">
                  {{ acc.has_webhook_secret ? `Webhook ${acc.webhook_secret_hint || '已配'}` : 'Webhook 未配' }}
                </el-tag>
              </div>
              <div class="text-xs text-muted mt-1 mono">{{ acc.site_base }}</div>
              <div class="text-xs text-subtle mt-1">
                协议 {{ protocolLabel(acc.protocol) }}
                · Key {{ acc.has_credential ? '已存' : '未配' }}
                <span v-if="accPing[acc.id]?.spendable_usd"> · 余额 ${{ accPing[acc.id].spendable_usd }}</span>
              </div>
              <div v-if="acc.last_error" class="text-xs mt-1" style="color: var(--err)">
                最近错误：{{ acc.last_error }}
              </div>
            </div>
            <div class="flex flex-wrap gap-1">
              <el-button size="small" :loading="accPingLoading === acc.id" @click="pingAccount(acc.id)">测连通</el-button>
              <el-button size="small" @click="openAccountForm(acc)">编辑</el-button>
              <el-button
                v-if="acc.circuit_state === 'open'"
                size="small"
                type="warning"
                @click="resetCircuit(acc.id)"
              >
                复位熔断
              </el-button>
              <el-button
                size="small"
                :type="acc.status === 'active' ? 'danger' : 'success'"
                plain
                @click="toggleAccountStatus(acc)"
              >
                {{ acc.status === 'active' ? '停用' : '启用' }}
              </el-button>
            </div>
          </div>
          <div class="mt-3 space-y-2">
            <div class="flex flex-wrap items-center gap-2">
              <el-input v-model="webhookUrlInputs[acc.id]" class="!max-w-2xl mono" placeholder="该台回调 URL" />
              <el-button @click="copyText(webhookUrlInputs[acc.id] || accountWebhookUrl(acc))">复制</el-button>
              <el-button type="primary" plain :loading="savingWebhookUrlId === acc.id" @click="saveAccountWebhookUrl(acc)">
                保存 URL
              </el-button>
            </div>
            <div class="flex flex-wrap items-center gap-2">
              <el-input
                v-model="webhookInputs[acc.id]"
                type="password"
                show-password
                class="!max-w-md"
                :placeholder="acc.has_webhook_secret ? `已配置 ${acc.webhook_secret_hint || ''}，留空不改` : '粘贴该台开发者页的 whsec_…'"
              />
              <el-button
                type="primary"
                plain
                :loading="savingWebhookId === acc.id"
                @click="saveAccountWebhook(acc)"
              >
                保存该台 Secret
              </el-button>
            </div>
          </div>
          <div class="mt-3 text-xs">
            <div class="text-muted mb-1">该台最近事件</div>
            <div v-if="!eventsFor(acc.id).length" class="text-subtle">暂无该台回调</div>
            <div v-for="e in eventsFor(acc.id)" :key="e.id" class="text-subtle mono leading-5">
              {{ e.created_at }} · {{ e.event_type || '—' }} · {{ summarizeWebhook(e.payload) }}
            </div>
          </div>
        </div>
      </div>

      <div class="flex flex-wrap gap-2">
        <el-button type="primary" plain @click="openAccountForm()">添加账户</el-button>
        <el-button :loading="loadingPlatforms" @click="loadPlatforms">刷新</el-button>
      </div>
    </div>

    <p class="text-sm text-muted">
      代理购卡的易支付配置已移至
      <router-link class="app-link" to="/ops/agents">代理管理</router-link>。
    </p>

    <!-- 状态摘要卡片（精简，详情进弹窗） -->
    <div class="grid gap-3 sm:grid-cols-3">
      <button type="button" class="status-card" @click="openStatusDialog">
        <div class="sc-label">连通状态</div>
        <div class="sc-value" :class="pingOk === true ? 'ok' : pingOk === false ? 'bad' : ''">
          {{ pingOk === true ? '正常' : pingOk === false ? '异常' : '未检测' }}
        </div>
        <div class="sc-hint">点击查看详情</div>
      </button>
      <button type="button" class="status-card" @click="openBalanceDialog">
        <div class="sc-label">可消费余额</div>
        <div class="sc-value mono">{{ spendableDisplay }}</div>
        <div class="sc-hint">含保证金信息</div>
      </button>
      <button type="button" class="status-card" @click="openPlansDialog">
        <div class="sc-label">服务费（实时）</div>
        <div class="sc-value mono">{{ feeSummary }}</div>
        <div class="sc-hint">v{{ plansVersion ?? '—' }} · 点击展开</div>
      </button>
    </div>

    <div class="flex flex-wrap gap-2">
      <router-link class="btn-primary" to="/ops/cdkeys">去发码</router-link>
      <router-link class="btn-secondary" to="/ops/webhooks">Webhook 事件</router-link>
      <router-link class="btn-secondary" to="/ops/appearance">整站主题</router-link>
    </div>

    <!-- 连通详情弹窗 -->
    <el-dialog v-model="dlgStatus" title="连通检测" width="480px" align-center destroy-on-close>
      <el-result
        :icon="pingOk ? 'success' : 'error'"
        :title="pingOk ? '卡台可达' : '探测失败'"
        :sub-title="pingMsg || ''"
      />
      <div class="result-grid mt-2">
        <div v-for="(v, k) in pingTiles" :key="k" class="result-tile">
          <div class="k">{{ k }}</div>
          <div class="v mono">{{ v }}</div>
        </div>
      </div>
      <template #footer>
        <el-button @click="dlgStatus = false">关闭</el-button>
        <el-button type="primary" :loading="pinging" @click="ping">重新探测</el-button>
      </template>
    </el-dialog>

    <!-- 余额弹窗 -->
    <el-dialog v-model="dlgBal" title="账户余额" width="420px" align-center destroy-on-close>
      <div class="result-grid">
        <div class="result-tile">
          <div class="k">可消费 spendable</div>
          <div class="v mono text-lg">{{ bal.spendable ?? '—' }}</div>
        </div>
        <div class="result-tile">
          <div class="k">总余额 balance</div>
          <div class="v mono text-lg">{{ bal.balance ?? '—' }}</div>
        </div>
        <div class="result-tile">
          <div class="k">风险保证金 reserve</div>
          <div class="v mono text-lg">{{ bal.reserve ?? '—' }}</div>
        </div>
      </div>
      <p class="text-xs text-muted mt-3">主动消费只能用 spendable（总余额含 20U 保证金）</p>
      <template #footer>
        <el-button @click="dlgBal = false">关闭</el-button>
        <el-button type="primary" :loading="loadingBal" @click="loadBalance">刷新</el-button>
      </template>
    </el-dialog>

    <!-- 价格弹窗 -->
    <el-dialog v-model="dlgPlans" title="实时服务费" width="520px" align-center destroy-on-close>
      <p class="text-sm text-muted mb-3">
        GET /openapi/v1/gpt-direct/plans · version {{ plansVersion ?? '—' }}
      </p>
      <div class="grid gap-3 sm:grid-cols-3">
        <div v-for="p in planCards" :key="p.key" class="result-tile text-center">
          <div class="k">{{ p.label }}</div>
          <div class="v mono text-2xl" style="color: var(--primary)">${{ p.fee }}</div>
          <div class="text-xs text-subtle mt-1">/ 张 · 可购</div>
          <!-- 点数按比索计价：服务费之外还要垫这笔上游付款 -->
          <div v-if="p.checkout" class="text-xs text-subtle">兑换垫付 {{ p.checkout }}</div>
          <div v-if="p.minor != null" class="text-xs text-subtle">minor {{ p.minor }}</div>
        </div>
      </div>
      <el-empty v-if="!planCards.length" description="卡台未开放任何可发码档位" :image-size="60" />
      <el-alert
        v-if="feeAllZero"
        class="mt-3"
        type="info"
        :closable="false"
        show-icon
        title="当前账户配置服务费为 $0（卡台 payment config）。若应为 1/5/10U，请在卡台管理端检查 GPT 直充价格版本。"
      />
      <template #footer>
        <el-button @click="dlgPlans = false">关闭</el-button>
        <el-button type="primary" :loading="loadingPlans" @click="loadPlans">刷新价格</el-button>
      </template>
    </el-dialog>

    <!-- 卡台账户编辑 -->
    <el-dialog
      v-model="dlgAccount"
      :title="accountForm.id ? '编辑卡台账户' : '添加卡台账户'"
      width="520px"
      align-center
      destroy-on-close
    >
      <el-form label-position="top" class="space-y-1">
        <el-form-item label="显示名称">
          <el-input v-model="accountForm.name" placeholder="主台 A / 备台 B" />
        </el-form-item>
        <el-form-item label="协议">
          <el-select v-model="accountForm.protocol" class="w-full">
            <el-option label="SpaceX 旧 OpenAPI（/openapi/v1 + sk_）" value="spacexcard-legacy" />
            <el-option label="Avanfinity（暂用旧 OpenAPI 兼容）" value="avanfinity-2026-08" />
          </el-select>
        </el-form-item>
        <el-form-item label="卡台 Base URL">
          <el-input v-model="accountForm.site_base" placeholder="https://www.avanfinity.com" @blur="normalizeAccountBase" />
        </el-form-item>
        <el-form-item label="Open API Key (sk_…)">
          <el-input
            v-model="accountForm.cred_secret"
            type="password"
            show-password
            :placeholder="accountForm.id ? '留空不修改' : '必填'"
            autocomplete="off"
          />
        </el-form-item>
        <el-form-item label="Webhook Secret">
          <el-input
            v-model="accountForm.webhook_secret"
            type="password"
            show-password
            :placeholder="accountForm.id ? '留空不修改' : '按卡台开发者页填写'"
            autocomplete="off"
          />
          <div class="text-xs text-subtle mt-1">A/B 卡台分别验签，用于各自订单状态与卡健康归因。</div>
        </el-form-item>
        <div class="grid gap-3 sm:grid-cols-2">
          <el-form-item label="优先级（越小越优先）">
            <el-input-number v-model="accountForm.priority" :min="1" :max="999" class="!w-full" />
          </el-form-item>
          <el-form-item label="状态">
            <el-select v-model="accountForm.status" class="w-full">
              <el-option label="启用" value="active" />
              <el-option label="停用" value="disabled" />
            </el-select>
          </el-form-item>
        </div>
        <el-checkbox v-model="accountForm.is_primary_default">设为主台（老码默认走这台）</el-checkbox>
      </el-form>
      <template #footer>
        <el-button @click="dlgAccount = false">取消</el-button>
        <el-button type="primary" :loading="savingAccount" @click="saveAccount">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { authFetch } from '../../lib/api'
import { dialog } from '../../lib/dialog'

const PRESETS: Record<string, string> = {
  prod: 'https://spacexcard.com',
  sandbox: 'https://sandbox.spacexcard.com',
}

const form = reactive({ card_api_base: PRESETS.prod })
const secrets = reactive({ agent_swap_password: '' })
const hints = reactive<Record<string, any>>({})
const saving = ref(false)
const busy = ref(false)
const pinging = ref(false)
const loadingPlans = ref(false)
const loadingBal = ref(false)
const loadingNet = ref(false)

const egressIp = ref('')
const pingOk = ref<boolean | null>(null)
const pingMsg = ref('')
const pingTiles = ref<Record<string, string>>({})
const bal = reactive<{ spendable?: string; balance?: string; reserve?: string }>({})
const plansRaw = ref<Record<string, any>>({})
const plansVersion = ref<number | null>(null)

const dlgStatus = ref(false)
const dlgBal = ref(false)
const dlgPlans = ref(false)
const dlgAccount = ref(false)

interface PlatformAccountRow {
  id: number
  name: string
  protocol: string
  site_base: string
  status: string
  priority: number
  is_primary_default: boolean
  has_credential: boolean
  has_webhook_secret?: boolean
  webhook_secret_hint?: string
  webhook_url?: string
  circuit_state: string
  circuit_fail_count: number
  last_error?: string
}

const platformAccounts = ref<PlatformAccountRow[]>([])
const platformUnusable = ref<string[]>([])
const loadingPlatforms = ref(false)
const savingDualBind = ref(false)
const savingAccount = ref(false)
const accPingLoading = ref<number | null>(null)
const accPing = ref<Record<number, { spendable_usd?: string; message?: string }>>({})
const dualBind = reactive({ enabled: false, allowSingle: false })
const webhookUrl = ref('')
const webhookInputs = reactive<Record<number, string>>({})
const webhookUrlInputs = reactive<Record<number, string>>({})
const savingWebhookId = ref<number | null>(null)
const savingWebhookUrlId = ref<number | null>(null)
const legacySecretSet = ref(false)
const legacySecretHint = ref('')
const webhookEvents = ref<Array<{ id: number; account_id?: number; event_type?: string; created_at?: string; payload?: any }>>([])
const accountForm = reactive({
  id: 0,
  name: '',
  protocol: 'spacexcard-legacy',
  site_base: '',
  cred_secret: '',
  webhook_secret: '',
  status: 'active',
  priority: 10,
  is_primary_default: false,
})

const activePlatformCount = computed(
  () => platformAccounts.value.filter((a) => a.status === 'active' && a.circuit_state !== 'open').length,
)
const missingWebhookNames = computed(() =>
  platformAccounts.value
    .filter((a) => a.status === 'active' && !a.has_webhook_secret)
    .map((a) => a.name)
    .join('、'),
)

const swapPwHint = computed(() =>
  hints.agent_swap_password_configured ? '已设置（留空保存不修改）' : '设置代理换码密码（至少 6 位）',
)
const agentSwapUrl = computed(() => {
  if (typeof window === 'undefined') return '/partner/swap'
  return `${window.location.origin}/partner/swap`
})
const siteRoot = computed(() => {
  let b = (form.card_api_base || '').trim().replace(/\/+$/, '')
  b = b.replace(/\/openapi\/v1$/i, '').replace(/\/openapi$/i, '')
  return b || PRESETS.prod
})
const resolvedOpenapi = computed(() => siteRoot.value + '/openapi/v1')
const resolvedCdk = computed(() => siteRoot.value + '/api/v1/cdk')

// 档位清单来自服务端下发的 registry（已按「卡台注册表 ∩ ACC 定价开关」过滤）。
// 这里原来写死 go/plus/pro_5x/pro_20x，两头都不对：卡台新开的点数档看不到，
// 卡台/ACC 关掉的档还照样列。
const planRegistry = ref<any[]>([])
const planCards = computed(() =>
  planRegistry.value.map((p: any) => {
    const minor = p.serviceFeeUsdMinor ?? p.service_fee_usd_minor
    const fee =
      minor != null && minor !== ''
        ? (Number(minor) / 100).toFixed(2)
        : p.service_fee_usd != null
          ? Number(p.service_fee_usd).toFixed(2)
          : '—'
    // 点数按比索计价，服务费之外还要垫这笔付款
    const checkout = p.checkout_amount_minor
      ? `${p.checkout_currency || 'PHP'} ${(Number(p.checkout_amount_minor) / 100).toFixed(2)}`
      : ''
    return { key: p.key, label: p.label || p.key, fee, minor, checkout, enabled: true }
  }),
)
const feeSummary = computed(() =>
  planCards.value.map((p) => `$${p.fee}`).join(' / ') || '—',
)
const feeAllZero = computed(() => planCards.value.every((p) => Number(p.fee) === 0))
const spendableDisplay = computed(() => bal.spendable ?? '—')

function normalizeBase() {
  form.card_api_base = siteRoot.value
}
async function copyText(t: string) {
  try {
    await navigator.clipboard.writeText(t)
    dialog.toast('已复制', 'ok')
  } catch {
    dialog.toast('复制失败', 'err')
  }
}

function openAgentSwap() {
  window.open(agentSwapUrl.value, '_blank')
}

async function loadNetwork() {
  loadingNet.value = true
  try {
    const r = await authFetch('/api/v1/admin/network/egress')
    const d = await r.json().catch(() => ({}))
    egressIp.value = d.egress_ip || ''
  } finally {
    loadingNet.value = false
  }
}

async function loadSettings() {
  const r = await authFetch('/api/v1/admin/settings')
  if (!r.ok) return
  const d = await r.json()
  form.card_api_base = d.card_api_base || PRESETS.prod
  Object.assign(hints, d)
  normalizeBase()
}

async function save() {
  saving.value = true
  try {
    const body: Record<string, string> = {}
    if (secrets.agent_swap_password.trim()) body.agent_swap_password = secrets.agent_swap_password.trim()
    const r = await authFetch('/api/v1/admin/settings', { method: 'PUT', body: JSON.stringify(body) })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '保存失败', 'err')
      return false
    }
    Object.assign(hints, d)
    secrets.agent_swap_password = ''
    dialog.toast('已保存', 'ok')
    return true
  } finally {
    saving.value = false
  }
}

async function ping() {
  pinging.value = true
  try {
    const r = await authFetch('/api/v1/admin/cardplatform/ping')
    const d = await r.json().catch(() => ({}))
    pingOk.value = !!r.ok && !!d.ok
    pingMsg.value = d.message || d.error || ''
    if (d.egress_ip) egressIp.value = d.egress_ip
    pingTiles.value = {
      site: d.site_base || siteRoot.value,
      openapi: d.openapi_base || resolvedOpenapi.value,
      cdk: d.public_cdk_base || resolvedCdk.value,
      probed: d.probed || '—',
      http: String(d.status ?? '—'),
      egress: d.egress_ip || egressIp.value || '—',
    }
    if (d.status === 403) dialog.toast('403：请将出口 IP 加入白名单', 'warn')
    else if (pingOk.value) dialog.toast('连通正常', 'ok')
    else dialog.toast(pingMsg.value || '探测失败', 'err')
  } finally {
    pinging.value = false
  }
}

async function loadPlans() {
  loadingPlans.value = true
  try {
    const r = await authFetch('/api/v1/admin/cardplatform/plans')
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || d.msg || '价格拉取失败', 'err')
      return
    }
    plansRaw.value = d.plans || {}
    planRegistry.value = d.registry || []
    plansVersion.value = d.version ?? null
    dialog.toast('价格已更新', 'ok')
    dlgPlans.value = true
  } finally {
    loadingPlans.value = false
  }
}

async function loadBalance() {
  loadingBal.value = true
  try {
    const r = await authFetch('/api/v1/admin/cardplatform/balance')
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || d.msg || '余额失败', 'err')
      return
    }
    bal.spendable = String(d.spendable_balance ?? '—')
    bal.balance = String(d.balance ?? '—')
    bal.reserve = String(d.account_reserve_amount ?? '—')
    dialog.toast('余额已刷新', 'ok')
    dlgBal.value = true
  } finally {
    loadingBal.value = false
  }
}

async function runAllChecks() {
  busy.value = true
  try {
    await save()
    await ping()
    await loadBalance()
    await loadPlans()
    dlgStatus.value = true
  } finally {
    busy.value = false
  }
}

function openStatusDialog() {
  dlgStatus.value = true
  if (pingOk.value === null) ping()
}
function openBalanceDialog() {
  dlgBal.value = true
  if (bal.spendable == null) loadBalance()
}
function openPlansDialog() {
  dlgPlans.value = true
  if (!Object.keys(plansRaw.value).length) loadPlans()
}

function protocolLabel(p: string) {
  if (p === 'avanfinity-2026-08') return 'Avanfinity'
  return 'SpaceX Legacy'
}

function normalizeAccountBase() {
  let b = (accountForm.site_base || '').trim().replace(/\/+$/, '')
  b = b.replace(/\/openapi\/v1$/i, '').replace(/\/openapi$/i, '')
  accountForm.site_base = b
}

function accountWebhookUrl(acc: PlatformAccountRow) {
  if (acc.webhook_url) return acc.webhook_url
  const base = (webhookUrl.value || '').replace(/\/+$/, '')
  if (!base) return ''
  return `${base}/${acc.id}`
}

function eventsFor(accountId: number) {
  return webhookEvents.value.filter((e) => e.account_id === accountId).slice(0, 5)
}

function summarizeWebhook(p: any) {
  if (!p || typeof p !== 'object') return '—'
  if (p.type === 'gpt_direct.completed' || p.order_id) {
    return `order=${p.order_id || ''} plan=${p.plan || ''} status=${p.status || ''}`
  }
  if (p.event === 'card_transaction') {
    return `${p.type || ''} ${p.status || ''} ${p.merchant_name || p.merchant || ''}`
  }
  return Object.keys(p).slice(0, 4).join(',')
}

async function loadWebhookEvents() {
  const r = await authFetch('/api/v1/admin/webhooks/events')
  const d = await r.json().catch(() => ({}))
  if (r.ok) webhookEvents.value = d.events || []
}

function applyPlatforms(d: any) {
  platformAccounts.value = Array.isArray(d.accounts) ? d.accounts : platformAccounts.value
  platformUnusable.value = Array.isArray(d.unusable) ? d.unusable : platformUnusable.value
  if (d.dual_bind !== undefined) dualBind.enabled = !!d.dual_bind
  if (d.allow_single !== undefined) dualBind.allowSingle = !!d.allow_single
  if (d.webhook_url) webhookUrl.value = d.webhook_url
  else if (!webhookUrl.value && typeof window !== 'undefined') {
    webhookUrl.value = `${window.location.origin}/api/v1/webhooks/cardplatform`
  }
  if (d.legacy_secret_set !== undefined) legacySecretSet.value = !!d.legacy_secret_set
  if (d.legacy_secret_hint !== undefined) legacySecretHint.value = d.legacy_secret_hint || ''
  for (const acc of platformAccounts.value) {
    if (webhookInputs[acc.id] === undefined) webhookInputs[acc.id] = ''
    webhookUrlInputs[acc.id] = acc.webhook_url || accountWebhookUrl(acc)
  }
}

async function loadPlatforms() {
  loadingPlatforms.value = true
  try {
    const r = await authFetch('/api/v1/admin/card-platforms')
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '加载卡台账户失败', 'err')
      return
    }
    applyPlatforms(d)
  } finally {
    loadingPlatforms.value = false
  }
}

async function saveAccountWebhookUrl(acc: PlatformAccountRow) {
  const url = (webhookUrlInputs[acc.id] || '').trim()
  if (!url) {
    dialog.toast('请填写该台回调 URL', 'err')
    return
  }
  savingWebhookUrlId.value = acc.id
  try {
    const r = await authFetch('/api/v1/admin/card-platforms/webhook-url', {
      method: 'POST',
      body: JSON.stringify({ id: acc.id, webhook_url: url }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '保存失败', 'err')
      return
    }
    applyPlatforms(d)
    dialog.toast(`已保存 ${acc.name} 的回调 URL`, 'ok')
  } finally {
    savingWebhookUrlId.value = null
  }
}

async function saveAccountWebhook(acc: PlatformAccountRow) {
  const secret = (webhookInputs[acc.id] || '').trim()
  if (!secret) {
    dialog.toast('请填写该台 webhook secret', 'err')
    return
  }
  savingWebhookId.value = acc.id
  try {
    const r = await authFetch('/api/v1/admin/card-platforms/webhook-secret', {
      method: 'POST',
      body: JSON.stringify({ id: acc.id, webhook_secret: secret }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '保存失败', 'err')
      return
    }
    webhookInputs[acc.id] = ''
    applyPlatforms(d)
    dialog.toast(`已保存 ${acc.name} 的 webhook secret`, 'ok')
  } finally {
    savingWebhookId.value = null
  }
}

async function saveDualBind() {
  savingDualBind.value = true
  try {
    const r = await authFetch('/api/v1/admin/card-platforms/dual-bind', {
      method: 'PUT',
      body: JSON.stringify({ enabled: dualBind.enabled, allow_single: dualBind.allowSingle }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '保存失败', 'err')
      await loadPlatforms()
      return
    }
    applyPlatforms(d)
    dialog.toast(dualBind.enabled ? '已开启本站双绑发码' : '已关闭（恢复单台发码）', 'ok')
  } finally {
    savingDualBind.value = false
  }
}

function openAccountForm(acc?: PlatformAccountRow) {
  if (acc) {
    accountForm.id = acc.id
    accountForm.name = acc.name
    accountForm.protocol = acc.protocol || 'spacexcard-legacy'
    accountForm.site_base = acc.site_base
    accountForm.cred_secret = ''
    accountForm.webhook_secret = ''
    accountForm.status = acc.status || 'active'
    accountForm.priority = acc.priority || 10
    accountForm.is_primary_default = !!acc.is_primary_default
  } else {
    accountForm.id = 0
    accountForm.name = platformAccounts.value.length ? `备台 ${platformAccounts.value.length + 1}` : '主台 A'
    accountForm.protocol = 'spacexcard-legacy'
    accountForm.site_base = siteRoot.value
    accountForm.cred_secret = ''
    accountForm.webhook_secret = ''
    accountForm.status = 'active'
    accountForm.priority = platformAccounts.value.length ? (platformAccounts.value.length + 1) * 10 : 10
    accountForm.is_primary_default = platformAccounts.value.length === 0
  }
  dlgAccount.value = true
}

async function saveAccount() {
  normalizeAccountBase()
  if (!accountForm.name.trim() || !accountForm.site_base.trim()) {
    dialog.toast('请填写名称和 Base URL', 'warn')
    return
  }
  if (!accountForm.id && !accountForm.cred_secret.trim()) {
    dialog.toast('新账户请填写 API Key', 'warn')
    return
  }
  savingAccount.value = true
  try {
    const body: Record<string, unknown> = {
      id: accountForm.id || undefined,
      name: accountForm.name.trim(),
      protocol: accountForm.protocol,
      site_base: accountForm.site_base.trim(),
      status: accountForm.status,
      priority: accountForm.priority,
      is_primary_default: accountForm.is_primary_default,
    }
    if (accountForm.cred_secret.trim()) body.cred_secret = accountForm.cred_secret.trim()
    if (accountForm.webhook_secret.trim()) body.webhook_secret = accountForm.webhook_secret.trim()
    const r = await authFetch('/api/v1/admin/card-platforms/upsert', {
      method: 'POST',
      body: JSON.stringify(body),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '保存失败', 'err')
      return
    }
    applyPlatforms(d)
    dlgAccount.value = false
    dialog.toast('账户已保存', 'ok')
  } finally {
    savingAccount.value = false
  }
}

async function pingAccount(id: number) {
  accPingLoading.value = id
  try {
    const r = await authFetch('/api/v1/admin/card-platforms/ping', {
      method: 'POST',
      body: JSON.stringify({ id }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok || !d.ok) {
      dialog.toast(d.error || d.detail || d.message || '探测失败', 'err')
      return
    }
    accPing.value = { ...accPing.value, [id]: { spendable_usd: d.spendable_usd, message: d.message } }
    dialog.toast(d.message || '连通正常', d.status === 403 ? 'warn' : 'ok')
    await loadPlatforms()
  } finally {
    accPingLoading.value = null
  }
}

async function resetCircuit(id: number) {
  const r = await authFetch('/api/v1/admin/card-platforms/reset-circuit', {
    method: 'POST',
    body: JSON.stringify({ id }),
  })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) {
    dialog.toast(d.error || '复位失败', 'err')
    return
  }
  applyPlatforms(d)
  dialog.toast('熔断已复位', 'ok')
}

async function toggleAccountStatus(acc: PlatformAccountRow) {
  const next = acc.status === 'active' ? 'disabled' : 'active'
  const r = await authFetch('/api/v1/admin/card-platforms/status', {
    method: 'POST',
    body: JSON.stringify({ id: acc.id, status: next }),
  })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) {
    dialog.toast(d.error || '操作失败', 'err')
    return
  }
  applyPlatforms(d)
  dialog.toast(next === 'active' ? '已启用' : '已停用', 'ok')
}

onMounted(async () => {
  await loadSettings()
  await loadNetwork()
  await Promise.all([loadPlatforms(), loadWebhookEvents()])
})
</script>

<style scoped>
.egress-hero {
  background: linear-gradient(120deg, var(--primary-soft), transparent 55%);
  border-color: var(--brd-2);
}
.path-chips { display: flex; flex-direction: column; gap: 8px; }
.path-chip {
  display: flex; flex-wrap: wrap; align-items: center; gap: 10px;
  padding: 10px 12px; border-radius: var(--radius-md);
  background: var(--surface-2); border: 1px solid var(--brd);
}
.path-chip .k {
  font-size: 11px; font-weight: 600; color: var(--ink-3);
  text-transform: uppercase; letter-spacing: .04em; min-width: 72px;
}
.path-chip .v { font-size: 12px; color: var(--ink); word-break: break-all; }

.status-card {
  text-align: left;
  padding: 16px;
  border-radius: var(--radius-lg);
  border: 1px solid var(--brd);
  background: var(--surface);
  cursor: pointer;
  transition: .18s ease;
}
.status-card:hover {
  border-color: var(--primary);
  box-shadow: var(--shadow-sm);
  transform: translateY(-2px);
}
.sc-label { font-size: 12px; color: var(--ink-3); }
.sc-value { margin-top: 6px; font-size: 20px; font-weight: 700; color: var(--ink); }
.sc-value.ok { color: var(--good); }
.sc-value.bad { color: var(--err); }
.sc-hint { margin-top: 6px; font-size: 11px; color: var(--ink-3); }
.mono { font-family: var(--font-mono); font-variant-numeric: tabular-nums; }

.platform-card {
  padding: 14px 16px;
  border-radius: var(--radius-lg);
  border: 1px solid var(--brd);
  background: var(--surface-2);
}
.platform-card--open {
  border-color: color-mix(in srgb, var(--err) 45%, var(--brd));
  background: color-mix(in srgb, var(--err) 6%, var(--surface-2));
}
</style>
