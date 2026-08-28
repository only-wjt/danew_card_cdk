<template>
  <div class="pb-2 space-y-6">
    <div class="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
      <div>
        <h1 class="text-3xl font-bold text-ink">代理管理</h1>
        <p class="text-sm text-muted mt-2">
          创建代理账号、配置可用套餐与并发限制。代理登录入口：
          <a :href="partnerLoginUrl" target="_blank" class="app-link mono">{{ partnerLoginUrl }}</a>
        </p>
      </div>
      <div class="flex flex-wrap gap-2">
        <button class="btn-secondary" @click="load">刷新</button>
        <button class="btn-primary" @click="openCreate">新建代理</button>
      </div>
    </div>

    <div class="card">
      <label class="flex items-start gap-3 cursor-pointer">
        <input type="checkbox" class="mt-1" :checked="policy.block_public_redeem" @change="togglePolicy" />
        <span>
          <span class="font-medium text-ink">已分配给代理的卡密禁止客户在公开兑换页自助兑换</span>
          <span class="block text-xs text-muted mt-1">
            建议开启。关闭后，同一张已发给代理的卡密可能被客户抢先兑换，导致代理这单失败。
            客户仍可正常查询卡密状态与充值进度。
          </span>
        </span>
      </label>
    </div>

    <div class="card space-y-4">
      <div>
        <h2 class="text-lg font-semibold text-ink">易支付 · 代理购卡</h2>
        <p class="text-sm text-muted mt-1">
          与卡网同一商户即可；本站订单号以 <code class="mono">AG</code> 开头，与卡网订单区分。
          异步通知：<code class="mono">{{ epayNotifyUrl }}</code>
          · 支付跳转：<code class="mono">{{ epayReturnUrl }}</code>
        </p>
      </div>
      <div class="form-grid form-grid-2 max-w-4xl">
        <div class="form-group">
          <label>站点对外地址</label>
          <input
            v-model="epayForm.public_base_url"
            class="input"
            placeholder="留空则使用当前访问地址（IP/域名+端口）"
          />
          <p class="text-xs text-subtle mt-1">
            易支付 notify / return 使用该根地址。留空时默认
            <code class="mono">{{ publicBaseDefault }}</code>。
            若 Cloudflare 拦截回调，可填直连 IP 或灰云子域（如 <code class="mono">https://pay.example.com</code>）。
          </p>
        </div>
        <div class="form-group">
          <label>易支付网关地址</label>
          <input v-model="epayForm.epay_api_base" class="input" placeholder="https://api.payqixiang.cn" />
          <p class="text-xs text-subtle mt-1">
            七相聚合填 <code class="mono">https://api.payqixiang.cn</code>（不要带 submit.php）。下单走文档推荐的
            <code class="mono">mapi.php</code> 统一下单。
          </p>
        </div>
        <div class="form-group">
          <label>商户 PID</label>
          <input v-model="epayForm.epay_pid" class="input mono" />
        </div>
        <div class="form-group">
          <label>商户密钥 Key</label>
          <input
            v-model="epaySecrets.epay_key"
            class="input mono"
            type="password"
            :placeholder="epayKeyHint"
            autocomplete="off"
          />
        </div>
      </div>
      <div>
        <div class="text-sm font-medium text-ink mb-2">已开通支付方式</div>
        <div class="check-list pay-type-list">
          <label class="check-tile" :class="{ 'is-checked': epayPayTypes.includes('alipay') }">
            <input type="checkbox" value="alipay" v-model="epayPayTypes" />
            <span class="font-medium text-ink">支付宝</span>
          </label>
          <label class="check-tile" :class="{ 'is-checked': epayPayTypes.includes('wxpay') }">
            <input type="checkbox" value="wxpay" v-model="epayPayTypes" />
            <span class="font-medium text-ink">微信</span>
          </label>
        </div>
        <p class="text-xs text-subtle mt-2">代理购卡页只展示已勾选的通道；未开通微信时可只保留支付宝。</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <button class="btn-primary" :disabled="savingEpay" @click="saveEpay">
          {{ savingEpay ? '保存中…' : '保存易支付配置' }}
        </button>
        <button class="btn-secondary" :disabled="testingEpay" @click="testEpay">
          {{ testingEpay ? '测试中…' : '测试签名/下单' }}
        </button>
      </div>
      <p class="text-xs text-subtle">
        测试会向七相发起 0.01 元 mapi 下单；若报「签名校验失败」，请到七相商户后台复制 PID 与 Key 重新粘贴保存。
      </p>
    </div>

    <div class="card space-y-4">
      <div>
        <h2 class="text-lg font-semibold text-ink">{{ t('adminAgents.globalFeesTitle') }}</h2>
        <p class="text-xs text-muted mt-1">{{ t('adminAgents.globalFeesHint') }}</p>
      </div>
      <p v-if="!catalogFromLive" class="text-xs text-muted">{{ t('adminAgents.catalogFallbackHint') }}</p>
      <div v-if="!feePlanRows.length" class="text-sm text-muted">{{ t('adminAgents.noPlans') }}</div>
      <div v-else class="fee-grid">
        <div v-for="p in feePlanRows" :key="p.key" class="fee-row">
          <div>
            <div class="font-medium text-ink">{{ p.label }}</div>
            <div class="text-xs text-muted mono">{{ p.key }}</div>
          </div>
          <div class="form-group !mb-0">
            <label class="!text-xs">{{ t('adminAgents.feeCny') }}</label>
            <input
              v-model="globalFeeForm[p.key]"
              class="input mono"
              type="number"
              min="0"
              max="100000"
              step="0.01"
              :placeholder="t('adminAgents.globalFeesPlaceholder')"
            />
          </div>
        </div>
      </div>
      <div class="flex justify-end">
        <button class="btn-primary" :disabled="globalFeesSaving" @click="submitGlobalFees">
          {{ globalFeesSaving ? t('adminAgents.savingFees') : t('adminAgents.saveFees') }}
        </button>
      </div>
    </div>

    <div class="card space-y-4">
      <div>
        <h2 class="text-lg font-semibold text-ink">{{ t('adminAgents.localStockTitle') }}</h2>
        <p class="text-xs text-muted mt-1">{{ t('adminAgents.localStockHint') }}</p>
      </div>
      <div class="flex flex-wrap gap-2 text-sm">
        <span class="pill">{{ t('adminAgents.localStockTotal', { n: localStock.total }) }}</span>
        <span class="pill pill-good">{{ t('adminAgents.localStockUnassigned', { n: localStock.unassigned }) }}</span>
        <span class="pill pill-info">{{ t('adminAgents.localStockAssigned', { n: localStock.assigned }) }}</span>
      </div>
      <div class="form-group !mb-0">
        <label>{{ t('adminAgents.localStockImportLabel') }}</label>
        <textarea
          v-model="localStockText"
          class="input mono text-sm"
          rows="8"
          :placeholder="t('adminAgents.localStockPlaceholder')"
        />
      </div>
      <div class="flex flex-wrap items-center justify-between gap-2">
        <p v-if="localStockResult" class="text-xs text-muted">{{ localStockResult }}</p>
        <button class="btn-primary" :disabled="localStockImporting" @click="importLocalStock">
          {{ localStockImporting ? t('adminAgents.localStockImporting') : t('adminAgents.localStockImport') }}
        </button>
      </div>
    </div>

    <div class="card overflow-hidden !p-0">
      <div class="overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>用户名</th>
              <th>显示名</th>
              <th>状态</th>
              <th>允许套餐</th>
              <th>卡密库存</th>
              <th>限流 / 并发</th>
              <th>创建时间</th>
              <th class="text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td colspan="8" class="py-10 text-center text-muted">加载中…</td>
            </tr>
            <tr v-else-if="!list.length">
              <td colspan="8" class="py-10 text-center text-muted">暂无代理，点击右上角新建</td>
            </tr>
            <tr v-for="row in list" :key="row.id">
              <td class="font-medium text-ink mono">{{ row.username }}</td>
              <td class="text-muted">{{ row.display_name || '—' }}</td>
              <td>
                <span :class="statusClass(row.status)">{{ statusLabel(row.status) }}</span>
              </td>
              <td class="text-sm text-muted max-w-[220px]">
                <template v-if="row.allowed_plans?.length">
                  <span v-for="k in row.allowed_plans" :key="k" class="pill pill-info mr-1 mb-1 inline-block">{{ planLabel(k) }}</span>
                </template>
                <span v-else class="pill">全部可售档位</span>
              </td>
              <td class="text-sm whitespace-nowrap">
                <span class="pill pill-good">可用 {{ row.cdk_stock?.unused || 0 }}</span>
                <span v-if="row.cdk_stock?.reserved" class="pill pill-warn ml-1">在途 {{ row.cdk_stock.reserved }}</span>
                <span class="text-xs text-muted ml-1">已用 {{ row.cdk_stock?.consumed || 0 }}</span>
              </td>
              <td class="mono text-sm text-muted whitespace-nowrap">
                {{ row.rate_limit_rpm || 60 }}/min · 在途 {{ row.max_concurrent_recharge || 10 }} 条 · 单批 {{ row.max_batch_items || 20 }} 条
              </td>
              <td class="text-muted whitespace-nowrap text-sm">{{ formatDate(row.created_at) }}</td>
              <td class="text-right whitespace-nowrap">
                <button class="btn-ghost !px-2.5 !py-1.5 text-sm" @click="openRecords(row)">查订单</button>
                <button class="btn-ghost !px-2.5 !py-1.5 text-sm" @click="openAssignCdks(row)">发卡密</button>
                <button class="btn-ghost !px-2.5 !py-1.5 text-sm" @click="openPlans(row)">套餐</button>
                <button class="btn-ghost !px-2.5 !py-1.5 text-sm" @click="openAgentFees(row)">{{ t('adminAgents.perAgentBtn') }}</button>
                <button class="btn-ghost !px-2.5 !py-1.5 text-sm" @click="openLimits(row)">限流</button>
                <button class="btn-ghost !px-2.5 !py-1.5 text-sm" @click="toggleStatus(row)">{{ row.status === 'active' ? '停用' : '启用' }}</button>
                <button class="btn-ghost !px-2.5 !py-1.5 text-sm" @click="resetPassword(row)">重置密码</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 新建代理 -->
    <Modal :open="createOpen" title="新建代理" wide @close="createOpen = false">
      <div class="space-y-4">
        <div class="form-grid form-grid-2">
          <div class="form-group">
            <label>用户名 <span class="text-subtle">*</span></label>
            <input v-model="createForm.username" class="input" placeholder="字母数字，用于登录" autocomplete="off" />
          </div>
          <div class="form-group">
            <label>显示名</label>
            <input v-model="createForm.display_name" class="input" placeholder="可选，默认同用户名" />
          </div>
        </div>
        <div class="form-group">
          <label>初始密码</label>
          <input v-model="createForm.password" class="input mono" type="password" placeholder="留空则自动生成强密码" autocomplete="new-password" />
        </div>
        <div>
          <div class="text-sm font-medium text-ink mb-1">允许套餐</div>
          <p class="text-xs text-muted mb-3">不勾选表示跟随卡台全部可售档位。GPT白号始终可购。</p>
          <div v-if="!allowedPlanOptions.length" class="text-xs text-muted">卡台档位未加载，可先保存账号稍后在「套餐」里配置。</div>
          <div v-else class="check-list">
            <label v-for="p in allowedPlanOptions" :key="p.key" class="check-tile" :class="{ 'is-checked': createForm.allowedPlans.includes(p.key) }">
              <input type="checkbox" :value="p.key" v-model="createForm.allowedPlans" />
              <span class="flex-1">
                <span class="font-medium text-ink">{{ p.label }}</span>
                <span class="text-xs text-muted ml-2 mono">${{ formatFee(p.service_fee_usd) }}</span>
              </span>
            </label>
          </div>
        </div>
      </div>
      <template #footer>
        <button class="btn-secondary" @click="createOpen = false">取消</button>
        <button class="btn-primary" :disabled="creating" @click="submitCreate">{{ creating ? '创建中…' : '创建' }}</button>
      </template>
    </Modal>

    <!-- 配置套餐 -->
    <Modal :open="plansOpen" :title="`配置套餐 · ${editingAgent?.username || ''}`" wide @close="plansOpen = false">
      <p class="text-sm text-muted mb-4">不勾选任何项 = 允许卡台当前全部可售档位。GPT白号始终可购。</p>
      <div v-if="!allowedPlanOptions.length" class="alert alert-error">无法加载档位清单。</div>
      <div v-else class="check-list">
        <label v-for="p in allowedPlanOptions" :key="p.key" class="check-tile" :class="{ 'is-checked': plansForm.allowedPlans.includes(p.key) }">
          <input type="checkbox" :value="p.key" v-model="plansForm.allowedPlans" />
          <span class="flex-1">
            <span class="font-medium text-ink">{{ p.label }}</span>
            <span class="text-xs text-muted ml-2 mono">${{ formatFee(p.service_fee_usd) }}</span>
          </span>
        </label>
      </div>
      <template #footer>
        <button class="btn-secondary" @click="plansOpen = false">取消</button>
        <button class="btn-primary" :disabled="plansSaving" @click="submitPlans">{{ plansSaving ? '保存中…' : '保存' }}</button>
      </template>
    </Modal>

    <!-- 单代理套餐定价 -->
    <Modal :open="agentFeesOpen" :title="t('adminAgents.perAgentTitle', { name: editingAgent?.username || '' })" wide @close="agentFeesOpen = false">
      <p class="text-sm text-muted mb-4">{{ t('adminAgents.perAgentHint') }}</p>
      <div v-if="!feePlanRows.length" class="alert alert-error">{{ t('adminAgents.noPlans') }}</div>
      <div v-else class="fee-grid">
        <div v-for="p in feePlanRows" :key="p.key" class="fee-row">
          <div>
            <div class="font-medium text-ink">
              {{ p.label }}
              <span v-if="agentFeeForm[p.key] !== ''" class="pill pill-info ml-2">{{ t('adminAgents.overrideTag') }}</span>
              <span v-else class="pill ml-2">{{ t('adminAgents.usingDefault') }}</span>
            </div>
            <div class="text-xs text-muted mono">{{ p.key }} · {{ t('adminAgents.defaultHint', { fee: formatFee(globalFeeNumber(p.key)) }) }}</div>
          </div>
          <div class="form-group !mb-0">
            <label class="!text-xs">{{ t('adminAgents.feeCny') }}</label>
            <input
              v-model="agentFeeForm[p.key]"
              class="input mono"
              type="number"
              min="0"
              max="100000"
              step="0.01"
              :placeholder="t('adminAgents.defaultHint', { fee: formatFee(globalFeeNumber(p.key)) })"
            />
          </div>
        </div>
      </div>
      <template #footer>
        <button class="btn-secondary" @click="agentFeesOpen = false">取消</button>
        <button class="btn-primary" :disabled="agentFeesSaving" @click="submitAgentFees">
          {{ agentFeesSaving ? t('adminAgents.savingFees') : t('adminAgents.saveFees') }}
        </button>
      </template>
    </Modal>

    <!-- 限流 / 并发 -->
    <Modal :open="limitsOpen" :title="`限流设置 · ${editingAgent?.username || ''}`" @close="limitsOpen = false">
      <div class="space-y-4">
        <div class="form-group">
          <label>每分钟请求数（RPM）</label>
          <input v-model.number="limitsForm.rate_limit_rpm" class="input mono" type="number" min="1" max="600" />
          <p class="text-xs text-muted">覆盖代理 API 全部接口，默认 60。</p>
        </div>
        <div class="form-group">
          <label>在途充值条数上限</label>
          <input v-model.number="limitsForm.max_concurrent_recharge" class="input mono" type="number" min="1" max="200" />
          <p class="text-xs text-muted">
            按<b>明细条数</b>计（不是批次数），单条与批量共用这一个额度，默认 10，防止代理刷爆卡台。
          </p>
        </div>
        <div class="form-group">
          <label>单批最多条数</label>
          <input v-model.number="limitsForm.max_batch_items" class="input mono" type="number" min="1" max="100" />
          <p class="text-xs text-muted">批量接口单次提交上限，默认 20。不能大于上面的在途上限，否则整批必被拒。</p>
        </div>
        <p v-if="limitsForm.max_batch_items > limitsForm.max_concurrent_recharge" class="alert alert-warn">
          单批条数大于在途上限，该代理提交任何整批都会被并发闸门拒绝。
        </p>
      </div>
      <template #footer>
        <button class="btn-secondary" @click="limitsOpen = false">取消</button>
        <button class="btn-primary" :disabled="limitsSaving" @click="submitLimits">{{ limitsSaving ? '保存中…' : '保存' }}</button>
      </template>
    </Modal>

    <!-- 分配卡密 -->
    <Modal :open="assignOpen" :title="`分配卡密 · ${editingAgent?.username || ''}`" wide @close="assignOpen = false">
      <p class="text-sm text-muted mb-3">
        在「CDK 卡密」页发码或入库后，把完整卡密粘贴到下方（每行一张），划给该代理。
        分配后代理可在门户「我的卡密」页查看并复制，无需再通过微信交接。
      </p>
      <div class="form-group">
        <label>卡密列表</label>
        <textarea v-model="assignForm.codesText" rows="8" class="input mono text-xs" placeholder="每行一张完整卡密" />
      </div>
      <p v-if="assignResult" class="text-sm" :class="assignResult.ok ? 'text-good' : 'text-err'">{{ assignResult.msg }}</p>
      <p class="text-xs text-muted mt-3">
        发错了可以收回：填了卡密就只收回这些，留空则收回该代理名下全部未使用卡密。已消耗或正在充值中的不会动。
      </p>
      <template #footer>
        <button class="btn-secondary" @click="assignOpen = false">关闭</button>
        <button class="btn-ghost" :disabled="assignSaving" @click="submitUnassign">收回卡密</button>
        <button class="btn-primary" :disabled="assignSaving" @click="submitAssign">{{ assignSaving ? '处理中…' : '确认分配' }}</button>
      </template>
    </Modal>

    <!-- 查订单 -->
    <Modal :open="recordsOpen" :title="`订单查询 · ${editingAgent?.username || ''}`" wide @close="recordsOpen = false">
      <div class="form-grid form-grid-2">
        <div class="form-group">
          <label>邮箱</label>
          <input v-model="recordsFilters.email" class="input" placeholder="模糊匹配" @keyup.enter="searchRecords" />
        </div>
        <div class="form-group">
          <label>卡密</label>
          <input v-model="recordsFilters.cdk" class="input mono" placeholder="前缀或完整码" @keyup.enter="searchRecords" />
        </div>
      </div>
      <div class="flex items-end gap-2 mb-4">
        <div class="form-group !mb-0 flex-1">
          <label>状态</label>
          <select v-model="recordsFilters.status" class="input">
            <option value="">全部状态</option>
            <option value="success">成功</option>
            <option value="failed">失败</option>
            <option value="skipped">已跳过</option>
            <option value="unknown">结果未知</option>
            <option value="submitted">已提交</option>
            <option value="pending">排队中</option>
          </select>
        </div>
        <button class="btn-primary" :disabled="recordsLoading" @click="searchRecords">
          {{ recordsLoading ? '查询中…' : '查询' }}
        </button>
      </div>

      <div class="overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>时间</th>
              <th>套餐</th>
              <th>邮箱</th>
              <th>卡密</th>
              <th>状态</th>
              <th>说明</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="recordsLoading">
              <td colspan="6" class="py-8 text-center text-muted">加载中…</td>
            </tr>
            <tr v-else-if="!records.length">
              <td colspan="6" class="py-8 text-center text-muted">暂无记录</td>
            </tr>
            <tr v-for="r in records" :key="r.request_id">
              <td class="text-muted whitespace-nowrap text-xs">{{ formatDate(r.created_at) }}</td>
              <td class="text-sm">{{ planLabel(r.plan) }}</td>
              <td class="text-sm">{{ r.account_email || '—' }}</td>
              <td class="mono text-xs">{{ r.cdk_prefix || '—' }}</td>
              <td><span :class="itemStatusClass(r.status)">{{ itemStatusLabel(r.status) }}</span></td>
              <td class="text-xs text-muted max-w-[180px] truncate" :title="r.message">{{ r.message || '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="mt-4">
        <Pagination :page="recordsPage" :page-size="20" :total="recordsTotal" @update:page="onRecordsPage" />
      </div>
      <template #footer>
        <button class="btn-secondary" @click="recordsOpen = false">关闭</button>
      </template>
    </Modal>

    <!-- 密码展示（仅一次） -->
    <Modal :open="passwordOpen" title="请妥善保存密码" @close="passwordOpen = false">
      <p class="text-sm text-muted mb-3">
        账号 <b class="text-ink">{{ passwordInfo.username }}</b> 的密码只显示这一次，关闭后无法再次查看。
      </p>
      <div class="rounded-xl border p-4 bg-soft" style="border-color: var(--brd)">
        <div class="text-xs text-muted mb-1">登录密码</div>
        <div class="mono text-lg text-ink break-all select-all">{{ passwordInfo.password }}</div>
      </div>
      <template #footer>
        <button class="btn-secondary" @click="copyPassword">复制密码</button>
        <button class="btn-primary" @click="passwordOpen = false">我已保存</button>
      </template>
    </Modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { authFetch } from '../../lib/api'
import { dialog } from '../../lib/dialog'
import Modal from '../../components/Modal.vue'
import Pagination from '../../components/Pagination.vue'

const { t } = useI18n({ useScope: 'global' })

interface CDKStock {
  total: number
  unused: number
  reserved: number
  consumed: number
}

interface AgentRow {
  id: number
  username: string
  display_name: string
  status: string
  allowed_plans: string[]
  rate_limit_rpm: number
  max_concurrent_recharge: number
  max_batch_items: number
  created_at: string
  cdk_stock?: CDKStock
}

interface RecordRow {
  request_id: string
  plan: string
  account_email: string
  cdk_prefix: string
  status: string
  message: string
  created_at: string
}

interface PlanOption {
  key: string
  label: string
  service_fee_usd: number | null
}

/** 卡台未配置时也能填价的稳定清单，与后端 corePricedPlans 对齐。 */
const CORE_PRICE_PLANS: PlanOption[] = [
  { key: 'plus', label: 'Plus', service_fee_usd: null },
  { key: 'pro_5x', label: 'Pro 5x', service_fee_usd: null },
  { key: 'pro_20x', label: 'Pro 20x', service_fee_usd: null },
  { key: 'go', label: 'Go', service_fee_usd: null },
  { key: 'gpt_white', label: 'GPT白号', service_fee_usd: null },
  { key: 'credit250', label: 'Codex 点数 250', service_fee_usd: null },
  { key: 'credit500', label: 'Codex 点数 500', service_fee_usd: null },
  { key: 'credit1000', label: 'Codex 点数 1000', service_fee_usd: null },
]

interface PriceCatalogRow {
  key: string
  label?: string
}

const list = ref<AgentRow[]>([])
const loading = ref(false)
const planOptions = ref<PlanOption[]>([])
const planLabelMap = computed(() => Object.fromEntries(planOptions.value.map((p) => [p.key, p.label])))

const partnerLoginUrl = computed(() => {
  if (typeof window === 'undefined') return '/partner/login'
  return `${window.location.origin}/partner/login`
})

const createOpen = ref(false)
const creating = ref(false)
const createForm = reactive({
  username: '',
  password: '',
  display_name: '',
  allowedPlans: [] as string[],
})

const editingAgent = ref<AgentRow | null>(null)
const plansOpen = ref(false)
const plansSaving = ref(false)
const plansForm = reactive({ allowedPlans: [] as string[] })

const limitsOpen = ref(false)
const limitsSaving = ref(false)
const limitsForm = reactive({ rate_limit_rpm: 60, max_concurrent_recharge: 10, max_batch_items: 20 })

const assignOpen = ref(false)
const assignSaving = ref(false)
const assignForm = reactive({ codesText: '' })
const assignResult = ref<{ ok: boolean; msg: string } | null>(null)

const recordsOpen = ref(false)
const recordsLoading = ref(false)
const records = ref<RecordRow[]>([])
const recordsTotal = ref(0)
const recordsPage = ref(1)
const recordsFilters = reactive({ email: '', cdk: '', status: '' })

const policy = reactive({ block_public_redeem: true })

const epayForm = reactive({ epay_api_base: '', epay_pid: '', public_base_url: '' })
const epaySecrets = reactive({ epay_key: '' })
const epayPayTypes = ref<Array<'alipay' | 'wxpay'>>(['alipay'])
const epayHints = reactive<Record<string, any>>({})
const savingEpay = ref(false)
const testingEpay = ref(false)

const publicBaseDefault = computed(() =>
  typeof window !== 'undefined' ? window.location.origin : 'http://127.0.0.1:8080',
)
const epayPublicBase = computed(() => {
  const effective = String(epayHints.public_base_url_effective || '').trim().replace(/\/+$/, '')
  if (effective) return effective
  return publicBaseDefault.value
})
const epayNotifyUrl = computed(() => `${epayPublicBase.value}/api/v1/webhooks/epay`)
const epayReturnUrl = computed(
  () => `${epayPublicBase.value}/partner/orders?paid=1&order_no=…`,
)
const epayKeyHint = computed(() =>
  epayHints.epay_key_configured ? `已配置 ${epayHints.epay_key_hint || ''}`.trim() : '粘贴商户密钥',
)

const globalFeeForm = reactive<Record<string, string>>({})
const globalFeesSaving = ref(false)
const agentFeesOpen = ref(false)
const agentFeesSaving = ref(false)
const agentFeeForm = reactive<Record<string, string>>({})
const priceCatalog = ref<PriceCatalogRow[]>([])
const catalogFromLive = ref(false)
const localStock = reactive({ total: 0, unassigned: 0, assigned: 0 })
const localStockText = ref('')
const localStockImporting = ref(false)
const localStockResult = ref('')

const allowedPlanOptions = computed(() => {
  const seen = new Set<string>()
  const rows: PlanOption[] = []
  const push = (p: PlanOption) => {
    if (!p.key || seen.has(p.key)) return
    seen.add(p.key)
    rows.push(p)
  }
  for (const p of CORE_PRICE_PLANS) push(p)
  for (const p of planOptions.value) push(p)
  return rows
})

const feePlanRows = computed(() => {
  const seen = new Set<string>()
  const rows: PlanOption[] = []
  const push = (key: string, label?: string) => {
    if (!key || seen.has(key)) return
    seen.add(key)
    rows.push({
      key,
      label: label || planLabelMap.value[key] || key,
      service_fee_usd: null,
    })
  }
  for (const p of CORE_PRICE_PLANS) push(p.key, p.label)
  for (const p of priceCatalog.value) push(p.key, p.label)
  for (const p of planOptions.value) push(p.key, p.label)
  for (const key of Object.keys(globalFeeForm)) push(key)
  for (const key of Object.keys(agentFeeForm)) push(key)
  return rows
})

function feeFieldValue(raw: unknown) {
  if (raw == null || raw === '') return ''
  const n = Number(raw)
  return Number.isFinite(n) ? String(n) : ''
}

function ensureFeeFields(target: Record<string, string>, incoming?: Record<string, unknown>, replace = false) {
  for (const p of CORE_PRICE_PLANS) {
    if (target[p.key] === undefined) target[p.key] = ''
  }
  for (const p of priceCatalog.value) {
    if (p.key && target[p.key] === undefined) target[p.key] = ''
  }
  for (const p of planOptions.value) {
    if (target[p.key] === undefined) target[p.key] = ''
  }
  if (!incoming) return
  if (replace) {
    const keys = new Set([
      ...Object.keys(target),
      ...Object.keys(incoming),
      ...CORE_PRICE_PLANS.map((p) => p.key),
      ...priceCatalog.value.map((p) => p.key),
      ...planOptions.value.map((p) => p.key),
    ])
    for (const key of keys) {
      if (!key) continue
      target[key] = feeFieldValue(incoming[key])
    }
    return
  }
  for (const [key, raw] of Object.entries(incoming)) {
    if (target[key] === undefined) target[key] = ''
    const next = feeFieldValue(raw)
    if (next !== '') target[key] = next
  }
}

function collectFeePayload(form: Record<string, string>) {
  const fees: Record<string, number> = {}
  for (const [key, raw] of Object.entries(form)) {
    const s = String(raw ?? '').trim()
    if (s === '') continue
    const n = Number(s)
    if (!Number.isFinite(n)) continue
    fees[key] = n
  }
  return fees
}

function globalFeeNumber(key: string) {
  const n = Number(globalFeeForm[key])
  return Number.isFinite(n) && String(globalFeeForm[key] ?? '').trim() !== '' ? n : 0
}

const passwordOpen = ref(false)
const passwordInfo = reactive({ username: '', password: '' })

function formatDate(v: string) {
  return v ? new Date(v).toLocaleString('zh-CN') : '—'
}

function formatFee(v: number | null | undefined) {
  if (v == null) return '—'
  return Number(v).toFixed(2)
}

function planLabel(key: string) {
  return planLabelMap.value[key] || CORE_PRICE_PLANS.find((p) => p.key === key)?.label || key
}

function statusLabel(s: string) {
  return s === 'active' ? '正常' : s === 'suspended' ? '已停用' : s
}

function statusClass(s: string) {
  if (s === 'active') return 'pill pill-good'
  if (s === 'suspended') return 'pill pill-warn'
  return 'pill'
}

const itemStatusText: Record<string, string> = {
  success: '成功',
  failed: '失败',
  skipped: '已跳过',
  unknown: '结果未知',
  submitted: '已提交',
  pending: '排队中',
  preparing: '准备中',
  issuing: '发码中',
}

function itemStatusLabel(s: string) {
  return itemStatusText[s] || s || '—'
}

function itemStatusClass(s: string) {
  if (s === 'success') return 'pill pill-good'
  if (s === 'failed') return 'pill pill-err'
  if (s === 'unknown' || s === 'skipped') return 'pill pill-warn'
  return 'pill pill-info'
}

async function loadPlans() {
  try {
    const res = await authFetch('/api/v1/admin/cardplatform/plans')
    if (!res.ok) {
      planOptions.value = []
      return
    }
    const d = await res.json()
    const registry = d.registry || []
    const plans = d.plans || {}
    planOptions.value = registry
      .filter((r: any) => r.key && plans[r.key]?.enabled !== false)
      .map((r: any) => ({
        key: r.key,
        label: r.label || plans[r.key]?.label || r.key,
        service_fee_usd: plans[r.key]?.service_fee_usd ?? (plans[r.key]?.serviceFeeUsdMinor != null ? plans[r.key].serviceFeeUsdMinor / 100 : null),
      }))
    ensureFeeFields(globalFeeForm)
    ensureFeeFields(agentFeeForm)
  } catch {
    planOptions.value = []
  }
}

async function load() {
  loading.value = true
  try {
    const res = await authFetch('/api/v1/admin/agents')
    if (res.ok) {
      const d = await res.json()
      list.value = d.list || []
    }
  } finally {
    loading.value = false
  }
}

function openCreate() {
  createForm.username = ''
  createForm.password = ''
  createForm.display_name = ''
  createForm.allowedPlans = []
  createOpen.value = true
}

async function submitCreate() {
  const username = createForm.username.trim()
  if (!username) {
    dialog.toast('请填写用户名', 'warn')
    return
  }
  creating.value = true
  try {
    const res = await authFetch('/api/v1/admin/agents', {
      method: 'POST',
      body: JSON.stringify({
        username,
        password: createForm.password,
        display_name: createForm.display_name.trim(),
        allowed_plans: createForm.allowedPlans,
      }),
    })
    const d = await res.json()
    if (!res.ok) {
      dialog.toast(d.error || '创建失败', 'err')
      return
    }
    createOpen.value = false
    passwordInfo.username = username
    passwordInfo.password = d.password
    passwordOpen.value = true
    dialog.toast('代理创建成功', 'ok')
    await load()
  } finally {
    creating.value = false
  }
}

function openPlans(row: AgentRow) {
  editingAgent.value = row
  plansForm.allowedPlans = [...(row.allowed_plans || [])]
  plansOpen.value = true
}

async function submitPlans() {
  if (!editingAgent.value) return
  plansSaving.value = true
  try {
    const res = await authFetch(`/api/v1/admin/agents/${editingAgent.value.id}/plans`, {
      method: 'PUT',
      body: JSON.stringify({ allowed_plans: plansForm.allowedPlans }),
    })
    const d = await res.json()
    if (!res.ok) {
      dialog.toast(d.error || '保存失败', 'err')
      return
    }
    plansOpen.value = false
    dialog.toast('套餐已更新', 'ok')
    await load()
  } finally {
    plansSaving.value = false
  }
}

function openLimits(row: AgentRow) {
  editingAgent.value = row
  limitsForm.rate_limit_rpm = row.rate_limit_rpm || 60
  limitsForm.max_concurrent_recharge = row.max_concurrent_recharge || 10
  limitsForm.max_batch_items = row.max_batch_items || 20
  limitsOpen.value = true
}

async function submitLimits() {
  if (!editingAgent.value) return
  limitsSaving.value = true
  try {
    const res = await authFetch(`/api/v1/admin/agents/${editingAgent.value.id}/limits`, {
      method: 'PUT',
      body: JSON.stringify({
        rate_limit_rpm: limitsForm.rate_limit_rpm,
        max_concurrent_recharge: limitsForm.max_concurrent_recharge,
        max_batch_items: limitsForm.max_batch_items,
      }),
    })
    const d = await res.json()
    if (!res.ok) {
      dialog.toast(d.error || '保存失败', 'err')
      return
    }
    limitsOpen.value = false
    dialog.toast('限流设置已更新', 'ok')
    await load()
  } finally {
    limitsSaving.value = false
  }
}

function openAssignCdks(row: AgentRow) {
  editingAgent.value = row
  assignForm.codesText = ''
  assignResult.value = null
  assignOpen.value = true
}

async function submitAssign() {
  if (!editingAgent.value) return
  const codes = assignForm.codesText
    .split(/\r?\n/)
    .map((s) => s.trim())
    .filter(Boolean)
  if (!codes.length) {
    dialog.toast('请粘贴至少一张卡密', 'warn')
    return
  }
  assignSaving.value = true
  assignResult.value = null
  try {
    const res = await authFetch(`/api/v1/admin/agents/${editingAgent.value.id}/assign-cdks`, {
      method: 'POST',
      body: JSON.stringify({ codes }),
    })
    const d = await res.json()
    if (!res.ok) {
      assignResult.value = { ok: false, msg: d.error || '分配失败' }
      return
    }
    const skipped = (d.skipped || []) as string[]
    assignResult.value = {
      ok: true,
      msg: `成功 ${d.assigned || 0} 张` + (skipped.length ? `，跳过 ${skipped.length} 张` : ''),
    }
    assignForm.codesText = ''
    dialog.toast(assignResult.value.msg, 'ok')
    await load()
  } finally {
    assignSaving.value = false
  }
}

async function submitUnassign() {
  if (!editingAgent.value) return
  const codes = assignForm.codesText
    .split(/\r?\n/)
    .map((s) => s.trim())
    .filter(Boolean)
  const scope = codes.length ? `这 ${codes.length} 张卡密` : '该代理名下全部未使用卡密'
  const ok = await dialog.confirm(`将收回「${editingAgent.value.username}」的${scope}，收回后代理无法再用它们充值。`, {
    title: '收回卡密',
    danger: true,
  })
  if (!ok) return
  assignSaving.value = true
  assignResult.value = null
  try {
    const res = await authFetch(`/api/v1/admin/agents/${editingAgent.value.id}/unassign-cdks`, {
      method: 'POST',
      body: JSON.stringify({ codes }),
    })
    const d = await res.json()
    if (!res.ok) {
      assignResult.value = { ok: false, msg: d.error || '收回失败' }
      return
    }
    const skipped = (d.skipped || []) as string[]
    assignResult.value = {
      ok: true,
      msg: `已收回 ${d.released || 0} 张` + (skipped.length ? `，跳过 ${skipped.length} 张` : ''),
    }
    assignForm.codesText = ''
    dialog.toast(assignResult.value.msg, 'ok')
    await load()
  } finally {
    assignSaving.value = false
  }
}

function openRecords(row: AgentRow) {
  editingAgent.value = row
  recordsFilters.email = ''
  recordsFilters.cdk = ''
  recordsFilters.status = ''
  recordsPage.value = 1
  records.value = []
  recordsTotal.value = 0
  recordsOpen.value = true
  void loadRecords()
}

async function loadRecords() {
  if (!editingAgent.value) return
  recordsLoading.value = true
  try {
    const params = new URLSearchParams({ page: String(recordsPage.value), page_size: '20' })
    if (recordsFilters.email.trim()) params.set('email', recordsFilters.email.trim())
    if (recordsFilters.cdk.trim()) params.set('cdk', recordsFilters.cdk.trim())
    if (recordsFilters.status) params.set('status', recordsFilters.status)
    const res = await authFetch(`/api/v1/admin/agents/${editingAgent.value.id}/records?${params}`)
    const d = await res.json()
    if (!res.ok) {
      dialog.toast(d.error || '查询失败', 'err')
      return
    }
    records.value = d.list || []
    recordsTotal.value = d.total || 0
  } finally {
    recordsLoading.value = false
  }
}

function searchRecords() {
  recordsPage.value = 1
  void loadRecords()
}

function onRecordsPage(p: number) {
  recordsPage.value = p
  void loadRecords()
}

function applyPriceCatalog(d: { plans?: PriceCatalogRow[]; catalog_source?: string }) {
  priceCatalog.value = Array.isArray(d.plans) ? d.plans.filter((p) => p?.key) : []
  catalogFromLive.value = d.catalog_source === 'live'
}

async function loadDefaultFees() {
  try {
    const res = await authFetch('/api/v1/admin/agent-plan-fees')
    if (!res.ok) {
      ensureFeeFields(globalFeeForm)
      return
    }
    const d = await res.json()
    applyPriceCatalog(d)
    ensureFeeFields(globalFeeForm, d.fees || {}, true)
  } catch {
    ensureFeeFields(globalFeeForm)
  }
}

async function submitGlobalFees() {
  globalFeesSaving.value = true
  try {
    const res = await authFetch('/api/v1/admin/agent-plan-fees', {
      method: 'PUT',
      body: JSON.stringify({ fees: collectFeePayload(globalFeeForm) }),
    })
    const d = await res.json().catch(() => ({}))
    if (!res.ok) {
      dialog.toast(d.error || t('adminAgents.feesSaveFail'), 'err')
      return
    }
    applyPriceCatalog(d)
    ensureFeeFields(globalFeeForm, d.fees || {}, true)
    dialog.toast(t('adminAgents.feesSaved'), 'ok')
  } finally {
    globalFeesSaving.value = false
  }
}

async function openAgentFees(row: AgentRow) {
  editingAgent.value = row
  for (const key of Object.keys(agentFeeForm)) delete agentFeeForm[key]
  ensureFeeFields(agentFeeForm)
  agentFeesOpen.value = true
  try {
    const res = await authFetch(`/api/v1/admin/agents/${row.id}/plan-fees`)
    if (!res.ok) return
    const d = await res.json()
    applyPriceCatalog(d)
    ensureFeeFields(agentFeeForm, d.overrides || {}, true)
  } catch {
    /* 对话框仍可编辑，保存时再报错 */
  }
}

async function submitAgentFees() {
  if (!editingAgent.value) return
  agentFeesSaving.value = true
  try {
    const res = await authFetch(`/api/v1/admin/agents/${editingAgent.value.id}/plan-fees`, {
      method: 'PUT',
      body: JSON.stringify({ fees: collectFeePayload(agentFeeForm) }),
    })
    const d = await res.json().catch(() => ({}))
    if (!res.ok) {
      dialog.toast(d.error || t('adminAgents.feesSaveFail'), 'err')
      return
    }
    applyPriceCatalog(d)
    ensureFeeFields(agentFeeForm, d.overrides || {}, true)
    agentFeesOpen.value = false
    dialog.toast(t('adminAgents.feesSaved'), 'ok')
  } finally {
    agentFeesSaving.value = false
  }
}

async function loadEpaySettings() {
  try {
    const r = await authFetch('/api/v1/admin/settings')
    if (!r.ok) return
    const d = await r.json()
    epayForm.epay_api_base = d.epay_api_base || ''
    epayForm.epay_pid = d.epay_pid || ''
    epayForm.public_base_url = d.public_base_url || ''
    const types = String(d.epay_pay_types || 'alipay')
      .split(',')
      .map((v: string) => v.trim().toLowerCase())
      .filter((v: string) => v === 'alipay' || v === 'wxpay')
    epayPayTypes.value = (types.length ? types : ['alipay']) as Array<'alipay' | 'wxpay'>
    Object.assign(epayHints, d)
  } catch {
    /* 读不到就保持空表单 */
  }
}

async function testEpay() {
  testingEpay.value = true
  try {
    const r = await authFetch('/api/v1/admin/epay/test', { method: 'POST' })
    const d = await r.json().catch(() => ({}))
    if (!r.ok || !d.ok) {
      dialog.toast(d.error || '易支付测试失败', 'err')
      return
    }
    dialog.toast(d.message || '易支付配置正确', 'ok')
  } finally {
    testingEpay.value = false
  }
}

async function saveEpay() {
  if (!epayPayTypes.value.length) {
    dialog.toast('请至少勾选一种支付方式', 'warn')
    return
  }
  savingEpay.value = true
  try {
    const body: Record<string, string> = {
      epay_api_base: epayForm.epay_api_base.trim(),
      epay_pid: epayForm.epay_pid.trim(),
      epay_pay_types: epayPayTypes.value.join(','),
      public_base_url: epayForm.public_base_url.trim(),
    }
    if (epaySecrets.epay_key.trim()) body.epay_key = epaySecrets.epay_key.trim()
    const r = await authFetch('/api/v1/admin/settings', { method: 'PUT', body: JSON.stringify(body) })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || '保存失败', 'err')
      return
    }
    Object.assign(epayHints, d)
    epaySecrets.epay_key = ''
    dialog.toast('易支付配置已保存', 'ok')
  } finally {
    savingEpay.value = false
  }
}

async function loadPolicy() {
  try {
    const res = await authFetch('/api/v1/admin/agent-policy')
    if (!res.ok) return
    const d = await res.json()
    policy.block_public_redeem = d.block_public_redeem !== false
  } catch {
    /* 读不到就保持默认开 */
  }
}

async function togglePolicy(e: Event) {
  const next = (e.target as HTMLInputElement).checked
  const res = await authFetch('/api/v1/admin/agent-policy', {
    method: 'PUT',
    body: JSON.stringify({ block_public_redeem: next }),
  })
  if (!res.ok) {
    const d = await res.json().catch(() => ({}))
    dialog.toast(d.error || '保存失败', 'err')
    await loadPolicy()
    return
  }
  policy.block_public_redeem = next
  dialog.toast(next ? '已开启拦截' : '已关闭拦截', 'ok')
}

async function toggleStatus(row: AgentRow) {
  const next = row.status === 'active' ? 'suspended' : 'active'
  const label = next === 'suspended' ? '停用' : '启用'
  const ok = await dialog.confirm(`确定要${label}代理「${row.username}」吗？`, {
    title: `${label}代理`,
    danger: next === 'suspended',
  })
  if (!ok) return
  const res = await authFetch(`/api/v1/admin/agents/${row.id}/status`, {
    method: 'PUT',
    body: JSON.stringify({ status: next }),
  })
  if (!res.ok) {
    const d = await res.json().catch(() => ({}))
    dialog.toast(d.error || '操作失败', 'err')
    return
  }
  dialog.toast(`已${label}`, 'ok')
  await load()
}

async function resetPassword(row: AgentRow) {
  const ok = await dialog.confirm(`将为「${row.username}」生成新密码，旧密码立即失效。`, {
    title: '重置密码',
    danger: true,
  })
  if (!ok) return
  const res = await authFetch(`/api/v1/admin/agents/${row.id}/reset-password`, { method: 'POST' })
  const d = await res.json()
  if (!res.ok) {
    dialog.toast(d.error || '重置失败', 'err')
    return
  }
  passwordInfo.username = row.username
  passwordInfo.password = d.password
  passwordOpen.value = true
}

async function copyPassword() {
  try {
    await navigator.clipboard.writeText(passwordInfo.password)
    dialog.toast('密码已复制', 'ok')
  } catch {
    dialog.toast('复制失败，请手动选中复制', 'err')
  }
}

async function loadLocalStock() {
  try {
    const res = await authFetch('/api/v1/admin/local-stock/summary')
    if (!res.ok) return
    const d = await res.json()
    const row = (d.list || []).find((x: any) => x.key === 'gpt_white') || (d.list || [])[0]
    if (row) {
      localStock.total = row.total || 0
      localStock.unassigned = row.unassigned || 0
      localStock.assigned = row.assigned || 0
    }
  } catch {
    /* keep zeros */
  }
}

async function importLocalStock() {
  const text = localStockText.value
  if (!text.trim()) {
    dialog.toast(t('adminAgents.localStockEmpty'), 'warn')
    return
  }
  localStockImporting.value = true
  try {
    const res = await authFetch('/api/v1/admin/local-stock/import', {
      method: 'POST',
      body: JSON.stringify({ plan: 'gpt_white', text }),
    })
    const d = await res.json()
    if (!res.ok) {
      dialog.toast(d.error || t('adminAgents.localStockFail'), 'err')
      return
    }
    localStockText.value = ''
    const skipped = Array.isArray(d.skipped) ? d.skipped.length : 0
    localStockResult.value = t('adminAgents.localStockResult', { n: d.imported || 0, s: skipped })
    dialog.toast(localStockResult.value, 'ok')
    const row = (d.summary || []).find((x: any) => x.key === 'gpt_white')
    if (row) {
      localStock.total = row.total || 0
      localStock.unassigned = row.unassigned || 0
      localStock.assigned = row.assigned || 0
    } else {
      await loadLocalStock()
    }
  } finally {
    localStockImporting.value = false
  }
}

onMounted(async () => {
  ensureFeeFields(globalFeeForm)
  await Promise.all([load(), loadPlans(), loadPolicy(), loadEpaySettings(), loadLocalStock()])
  await loadDefaultFees()
})
</script>

<style scoped>
.check-list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 10px;
}
.pay-type-list {
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  max-width: 320px;
}
.fee-grid {
  display: grid;
  gap: 12px;
}
.fee-row {
  display: grid;
  grid-template-columns: minmax(140px, 1fr) minmax(180px, 220px);
  gap: 16px;
  align-items: end;
  padding: 12px;
  border: 1px solid var(--brd);
  border-radius: var(--radius-md, 12px);
  background: var(--surface-2);
}
</style>
