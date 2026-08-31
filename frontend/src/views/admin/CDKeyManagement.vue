<template>
  <div class="pb-2 space-y-4">
    <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
      <div>
        <h1 class="text-2xl font-bold text-ink">CDK 卡密</h1>
        <div class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted">
          <el-tag :type="configured ? 'success' : 'danger'" size="small">
            {{ configured ? 'API 已配置' : '未配置 Key' }}
          </el-tag>
          <span v-if="egressIp">
            出口 IP <b class="mono text-ink">{{ egressIp }}</b>
            <el-button link type="primary" @click="copyText(egressIp)">复制</el-button>
          </span>
          <span v-if="balanceText">余额 <b class="mono text-ink">{{ balanceText }}</b></span>
          <span class="text-subtle">{{ priceSource }} · v{{ pricingVersion ?? '—' }}</span>
        </div>
      </div>
      <div class="flex flex-wrap gap-2">
        <el-popover placement="bottom-end" :width="420" trigger="click">
          <template #reference>
            <el-button size="small">代理换码</el-button>
          </template>
          <div class="space-y-2">
            <div class="text-sm font-medium text-ink">代理换码页</div>
            <p class="text-xs text-muted">失败且未扣款的卡密可换新码，需在卡台接入里设置代理密码。</p>
            <div class="mono text-xs break-all">{{ agentSwapUrl }}</div>
            <div class="flex gap-2">
              <el-button type="primary" size="small" @click="copyText(agentSwapUrl)">复制链接</el-button>
              <el-button size="small" @click="openAgentSwap">打开</el-button>
            </div>
          </div>
        </el-popover>
        <router-link to="/ops/integration" class="btn-secondary !py-1.5 !px-3 text-sm">卡台配置</router-link>
        <el-button size="small" :loading="loadingMeta" @click="refreshAll">刷新</el-button>
      </div>
    </div>

    <div v-if="metaError" class="alert alert-error">{{ metaError }}</div>
    <el-button v-if="!configured" type="warning" size="small" @click="$router.push('/ops/integration')">
      去配置 API Key
    </el-button>

    <section class="card !p-0 overflow-hidden">
      <button type="button" class="fold-head" @click="issueOpen = !issueOpen">
        <span class="font-semibold text-ink">购买并生成</span>
        <span class="text-xs text-muted">
          {{ planLabel(form.plan) }} · {{ form.count }} 张 · ${{ estimatedTotal }}
        </span>
        <span class="fold-caret">{{ issueOpen ? '收起' : '展开' }}</span>
      </button>
      <div v-show="issueOpen" class="p-4 space-y-4 border-t" style="border-color: var(--brd)">
        <div class="grid gap-2 sm:grid-cols-3">
          <button
            v-for="p in planCards"
            :key="p.key"
            type="button"
            class="plan-card-sm"
            :class="{ 'plan-card-sm--on': form.plan === p.key }"
            @click="selectPlan(p.key)"
          >
            <span class="font-medium">{{ p.label }}</span>
            <span class="mono text-ink">${{ formatUsd(p.service_fee_usd) }}</span>
            <!-- 点数是比索计价：兑换时代理要垫这笔付款，不写出来会被当成只花 $0.10 -->
            <small v-if="p.checkoutText" class="text-xs text-muted">兑换垫付 {{ p.checkoutText }}</small>
          </button>
        </div>
        <div class="flex flex-wrap items-center gap-3">
          <span class="text-sm text-muted">数量</span>
          <el-button size="small" :disabled="form.count <= 1" @click="form.count = Math.max(1, form.count - 1)">−</el-button>
          <input v-model.number="form.count" type="number" min="1" :max="ISSUE_MAX" class="input !w-16 text-center mono" />
          <el-button size="small" :disabled="form.count >= ISSUE_MAX" @click="form.count = Math.min(ISSUE_MAX, form.count + 1)">+</el-button>
          <el-button-group>
            <el-button size="small" @click="form.count = 1">1</el-button>
            <el-button size="small" @click="form.count = 10">10</el-button>
            <el-button size="small" @click="form.count = 50">50</el-button>
            <el-button size="small" @click="form.count = 100">100</el-button>
            <el-button size="small" @click="form.count = ISSUE_MAX">200</el-button>
          </el-button-group>
          <el-checkbox v-model="form.funding_confirmed">确认承担兑换资金</el-checkbox>
          <el-button type="primary" :loading="issuing" :disabled="!canIssue" @click="issue">
            {{ issuing ? '购买中…' : `购买 ${form.count} 张 ${planLabel(form.plan)} · $${estimatedTotal}` }}
          </el-button>
        </div>
        <p v-if="!configured" class="text-xs" style="color: var(--err)">
          请先在
          <router-link class="app-link" to="/ops/integration">卡台配置</router-link>
          填写凭证{{ dualBindEnabled ? '并添加至少一个可用卡台账户' : '' }}
        </p>
        <p v-else-if="dualIssueHint" class="text-xs text-muted">{{ dualIssueHint }}</p>
        <p v-else-if="!form.funding_confirmed" class="text-xs text-muted">勾选「确认承担兑换资金」后再购买。实付由本账户承担，服务费从卡台余额扣除。</p>
        <div v-if="issueError" class="alert alert-error">{{ issueError }}</div>
        <div v-if="issueOk" class="alert alert-success">{{ issueOk }}</div>
        <div v-if="recentCodes.length" class="rounded-xl bg-soft p-3 space-y-2 border" style="border-color: var(--good)">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div class="text-sm font-medium" style="color: var(--good)">
              本批 {{ recentCodes.length }} 张
              <span v-if="recentMeta" class="text-xs text-muted font-normal"> · {{ recentMeta.plan }} · {{ recentMeta.atLabel }}</span>
            </div>
            <div class="flex gap-1">
              <el-button size="small" type="success" @click="copyAll">复制</el-button>
              <el-button size="small" @click="downloadCodes">导出</el-button>
              <el-button size="small" text type="danger" @click="clearRecent">清除</el-button>
            </div>
          </div>
          <textarea
            class="input mono text-sm !min-h-[88px] w-full"
            readonly
            :value="recentCodes.join('\n')"
            @focus="($event.target as HTMLTextAreaElement).select()"
          />
          <button
            v-if="recentCodes.some((code) => code.toUpperCase().startsWith('DN-'))"
            type="button"
            class="binding-fold-head"
            @click="toggleRecentBindings"
          >
            <span>本次上游绑定（仅排障，不对代理展示）</span>
            <span>{{ recentBindingsOpen ? '收起' : '展开' }}</span>
          </button>
          <div v-show="recentBindingsOpen" class="space-y-2">
            <div v-if="loadingRecentBindings" class="text-xs text-muted">正在加载绑定…</div>
            <div v-for="item in recentBindings" :key="item.code" class="binding-debug-row">
              <div class="mono text-xs text-ink break-all">{{ item.code }}</div>
              <div class="flex flex-wrap gap-2 mt-1">
                <el-tag
                  v-for="binding in item.bindings"
                  :key="binding.id"
                  size="small"
                  :type="bindingStatusType(binding.status)"
                  effect="plain"
                >
                  {{ accountName(binding.account_id) }} · {{ binding.remote_code_prefix || '无前缀' }} · {{ bindingStatusLabel(binding.status) }}
                </el-tag>
                <span v-if="!item.bindings.length" class="text-xs" style="color: var(--err)">未找到绑定</span>
              </div>
              <div v-if="item.error" class="text-xs mt-1" style="color: var(--err)">{{ item.error }}</div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <section class="card space-y-3">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 class="text-lg font-semibold text-ink">CDK 列表</h2>
          <p class="text-xs text-muted mt-0.5">
            共 {{ total }} 条
            <span v-if="storeStats.fullInStore != null"> · 本站已存 {{ storeStats.fullInStore }}</span>
            <span v-if="listMode === 'upstream' && storeStats.fullOnPage != null"> · 本页完整 {{ storeStats.fullOnPage }}</span>
          </p>
        </div>
        <el-radio-group v-model="listMode" size="small" @change="onListModeChange">
          <el-radio-button value="stored">本站完整码库</el-radio-button>
          <el-radio-button value="upstream">卡台状态列表</el-radio-button>
        </el-radio-group>
      </div>

      <div class="flex flex-wrap items-center gap-2">
        <el-input
          v-model="listQ"
          clearable
          class="!w-[260px]"
          :placeholder="listMode === 'stored' ? '搜索备注 / 完整码 / ID / 前缀' : '模糊搜索：ID / 码前缀'"
          @keyup.enter="searchList"
          @clear="searchList"
        />
        <el-select v-model="listStatus" clearable placeholder="状态" class="!w-[120px]" @change="searchList">
          <el-option v-for="s in statusOptions" :key="s" :label="statusLabel(s)" :value="s" />
        </el-select>
        <el-select v-model="listPlan" clearable placeholder="套餐" class="!w-[120px]" @change="searchList">
          <el-option v-for="k in planKeys" :key="k" :label="planLabel(k)" :value="k" />
        </el-select>
        <template v-if="listMode === 'stored'">
          <el-select v-model="listCodeKind" clearable placeholder="种类" class="!w-[120px]" @change="searchList">
            <el-option label="本站码" value="site" />
            <el-option label="旧码" value="legacy" />
            <el-option label="白号" value="local_stock" />
          </el-select>
          <el-select v-model="listBindingState" clearable placeholder="绑定" class="!w-[130px]" @change="searchList">
            <el-option label="双绑完整" value="complete" />
            <el-option label="绑定降级" value="degraded" />
            <el-option label="无绑定" value="none" />
          </el-select>
          <el-select v-model="listFulfilledAccount" clearable placeholder="履约台" class="!w-[140px]" @change="searchList">
            <el-option
              v-for="acc in platformAccounts"
              :key="acc.id"
              :label="acc.name"
              :value="String(acc.id)"
            />
          </el-select>
          <el-select v-model="listFailover" clearable placeholder="Failover" class="!w-[120px]" @change="searchList">
            <el-option label="发生过切台" value="yes" />
            <el-option label="未切台" value="no" />
          </el-select>
        </template>
        <el-button type="primary" :loading="loadingList" @click="searchList">查询</el-button>
        <el-button :loading="loadingList" @click="loadList">刷新</el-button>
        <el-button :loading="syncingUpstream" @click="syncFromCardplatform">从卡台同步完整码</el-button>
        <span class="flex-1"></span>
        <el-dropdown trigger="click" @command="onCopyCommand">
          <el-button size="small">
            复制 / 导出<el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="copySelected" :disabled="!selectedFullCodes.length">
                复制选中 ({{ selectedFullCodes.length }})
              </el-dropdown-item>
              <el-dropdown-item command="exportSelected" :disabled="!selectedFullCodes.length">
                导出选中 .txt
              </el-dropdown-item>
              <el-dropdown-item command="copyPage" :disabled="!pageFullCodes.length">
                复制本页 ({{ pageFullCodes.length }})
              </el-dropdown-item>
              <el-dropdown-item divided command="exportAll" :disabled="!storeStats.fullInStore">
                导出本站全部{{ exportingAll ? '…' : '' }}
              </el-dropdown-item>
              <el-dropdown-item command="copyAllStored" :disabled="!storeStats.fullInStore">
                复制本站全部
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-dropdown trigger="click" @command="onBatchCommand">
          <el-button size="small">
            批量操作<el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="note" :disabled="!selectedNoteIds.length">
                批量备注 ({{ selectedNoteIds.length }})
              </el-dropdown-item>
              <el-dropdown-item command="clearNote" :disabled="!selectedHasNoteIds.length">
                去除备注 ({{ selectedHasNoteIds.length }})
              </el-dropdown-item>
              <el-dropdown-item divided command="disable" :disabled="!selectedDisableableIds.length">
                批量禁用 ({{ selectedDisableableIds.length }})
              </el-dropdown-item>
              <el-dropdown-item command="enable" :disabled="!selectedEnableableIds.length">
                解除禁用 ({{ selectedEnableableIds.length }})
              </el-dropdown-item>
              <el-dropdown-item divided command="syncCache">同步本机缓存</el-dropdown-item>
              <el-dropdown-item command="clearSel" :disabled="!selectedRows.length">清空选择</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>

      <div v-if="selectedRows.length" class="sel-bar">
        <span>已选 <b>{{ selectedRows.length }}</b></span>
        <el-button size="small" type="primary" :disabled="!selectedFullCodes.length" @click="copySelectedFull">
          复制选中
        </el-button>
        <el-button size="small" :disabled="!selectedNoteIds.length" :loading="noting" @click="batchSetNoteSelected">
          备注
        </el-button>
        <el-button size="small" type="danger" plain :disabled="!selectedDisableableIds.length" :loading="disabling" @click="batchDisableSelected">
          禁用
        </el-button>
        <el-button size="small" text @click="clearSelection">取消</el-button>
      </div>

      <div v-if="listError" class="alert alert-error">{{ listError }}</div>
      <div v-if="listMode === 'stored' && !loadingList && !displayRows.length" class="alert alert-info">
        本站尚未存入完整码。展开上方「购买并生成」发码，或在批量操作里同步本机缓存。
      </div>

      <el-table
        ref="tableRef"
        :data="displayRows"
        v-loading="loadingList"
        size="small"
        stripe
        empty-text="暂无数据"
        row-key="rowKey"
        @selection-change="onSelectionChange"
      >
        <el-table-column type="selection" width="44" :selectable="rowSelectable" reserve-selection />
        <el-table-column label="ID" width="72">
          <template #default="{ row }">{{ row.row_id || row.id || '—' }}</template>
        </el-table-column>
        <el-table-column label="卡密" min-width="240">
          <template #default="{ row }">
            <button
              type="button"
              class="code-cell"
              :title="row.fullCode ? '点击复制完整码' : '仅有前缀（请切换到「本站完整码库」或重新发码）'"
              @click="copyRowCode(row)"
            >
              <span class="mono break-all code-cell__text" :class="row.fullCode ? 'is-full' : 'is-prefix'">
                {{ row.displayCode || '—' }}
              </span>
              <span class="code-cell__meta">
                <el-tag v-if="row.fullCode" size="small" type="success" effect="plain">完整</el-tag>
                <el-tag v-else size="small" type="info" effect="plain">仅前缀</el-tag>
                <span class="text-subtle">{{ (row.displayCode || '').length }}字 · 点复制</span>
              </span>
            </button>
          </template>
        </el-table-column>
        <el-table-column v-if="listMode === 'stored'" label="种类" width="92">
          <template #default="{ row }">
            <el-tag size="small" :type="codeKindType(row.code_kind)" effect="plain">
              {{ codeKindLabel(row.code_kind) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="plan" label="套餐" width="100">
          <template #default="{ row }">{{ planLabel(row.plan) }}</template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column v-if="listMode === 'stored'" label="绑定" min-width="180">
          <template #default="{ row }">
            <template v-if="row.code_kind === 'site'">
              <div class="text-xs text-ink">
                {{ row.binding_total >= 2 ? `双绑完整（当前可用 ${row.binding_usable}/${row.binding_total}）` : `绑定降级 ${row.binding_usable}/${row.binding_total}` }}
              </div>
              <div class="text-xs text-subtle binding-summary">{{ row.binding_summary || '—' }}</div>
            </template>
            <span v-else class="text-subtle">—</span>
          </template>
        </el-table-column>
        <el-table-column v-if="listMode === 'stored'" label="履约台" width="120">
          <template #default="{ row }">
            <span v-if="row.fulfilled_account">{{ row.fulfilled_account }}</span>
            <span v-else-if="row.code_kind === 'legacy'" class="text-subtle">原台</span>
            <span v-else class="text-subtle">—</span>
          </template>
        </el-table-column>
        <el-table-column v-if="listMode === 'stored'" label="Failover" width="105">
          <template #default="{ row }">
            <el-tag v-if="row.failover_used" size="small" type="warning" :title="row.failover_reason || ''">
              已切台
            </el-tag>
            <span v-else class="text-subtle">否</span>
          </template>
        </el-table-column>
        <el-table-column label="服务费" width="88">
          <template #default="{ row }">
            <span class="mono">${{ ((row.fee_amount_minor || 0) / 100).toFixed(2) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="备注" min-width="140">
          <template #default="{ row }">
            <button type="button" class="note-cell" :title="row.note ? '点击编辑备注' : '点击添加备注'" @click="editNoteOne(row)">
              <span v-if="row.note" class="note-cell__text">{{ row.note }}</span>
              <span v-else class="note-cell__empty">添加备注…</span>
            </button>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="时间" min-width="148" />
        <el-table-column label="" width="72" fixed="right" align="right">
          <template #default="{ row }">
            <el-dropdown trigger="click" @command="(cmd: string) => onRowCommand(cmd, row)">
              <el-button size="small" link>操作</el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="copy" :disabled="!row.fullCode">复制完整码</el-dropdown-item>
                  <el-dropdown-item command="note" :disabled="!row.id">编辑备注</el-dropdown-item>
                  <el-dropdown-item v-if="canDisableRow(row)" command="disable" divided>禁用</el-dropdown-item>
                  <el-dropdown-item v-if="canEnableRow(row)" command="enable">解除禁用</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </template>
        </el-table-column>
      </el-table>

      <div class="flex flex-wrap items-center justify-between gap-3 text-sm text-muted">
        <span>{{ listMode === 'stored' ? '本站完整码库' : '卡台状态' }} · 第 {{ page }} 页 · 共 {{ total }} 条</span>
        <el-pagination
          background
          layout="total, sizes, prev, pager, next"
          :total="total"
          :page-size="pageSize"
          :current-page="page"
          :page-sizes="[20, 50, 100, 200]"
          :disabled="loadingList"
          @current-change="(p: number) => { page = p; loadList() }"
          @size-change="(s: number) => { pageSize = s; page = 1; loadList() }"
        />
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { authFetch } from '../../lib/api'
import { dialog } from '../../lib/dialog'
import { copyToClipboard } from '../../lib/clipboard'

const RECENT_KEY = 'cdk_recent_issued_v1'
/** 浏览器兜底缓存（历史本机数据）；主存储已改为服务器 SQLite */
const CODE_CACHE_KEY = 'cdk_full_code_cache_v1'
/** 卡台完整码形如 GPTD-xxxxxxxxxxxx-xxxxxxxxxxxx-xxxxxxxxxxxx（约 43 字符） */
const FULL_CODE_MIN_LEN = 20
const ISSUE_MAX = 200
/** 代理换码隐藏页（完整 URL，便于复制发给代理） */
const agentSwapUrl =
  typeof window !== 'undefined' ? `${window.location.origin}/partner/swap` : '/partner/swap'

type CodeCacheEntry = { code: string; plan?: string; prefix?: string; at?: number; id?: number }
/** id -> entry；prefix -> entry（仅作兜底 / 回填服务器） */
const codeCache = ref<Record<string, CodeCacheEntry>>({})
const storeStats = reactive({ fullOnPage: null as number | null, fullInStore: null as number | null })
const syncingCache = ref(false)
const syncingUpstream = ref(false)
const exportingAll = ref(false)
const disabling = ref(false)
const noting = ref(false)
/** stored = 本站完整码库（默认可复制导出）；upstream = 卡台状态列表 */
const listMode = ref<'stored' | 'upstream'>('stored')
const selectedRows = ref<any[]>([])
const tableRef = ref<any>(null)
const issueOpen = ref(false)
const recentBindingsOpen = ref(false)
const loadingRecentBindings = ref(false)
const recentBindings = ref<Array<{ code: string; bindings: any[]; error?: string }>>([])

const plans = ref<Record<string, any>>({})
const pricingVersion = ref<number | null>(null)
const priceSource = ref('—')
const balanceText = ref('')
const egressIp = ref('')
const metaError = ref('')
const loadingMeta = ref(false)
const configured = ref(false)
const dualBindEnabled = ref(false)
const activePlatformN = ref(0)

const form = reactive({
  plan: 'plus',
  count: 1,
  funding_confirmed: false,
})
const issuing = ref(false)
const issueError = ref('')
const issueOk = ref('')
const recentCodes = ref<string[]>([])
const recentMeta = ref<{ plan: string; atLabel: string } | null>(null)

const rows = ref<any[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loadingList = ref(false)
const listError = ref('')
const listQ = ref('')
const listStatus = ref('')
const listPlan = ref('')
const listCodeKind = ref('')
const listBindingState = ref('')
const listFulfilledAccount = ref('')
const listFailover = ref('')
const platformAccounts = ref<Array<{ id: number; name: string }>>([])
const statusOptions = ['unused', 'reserved', 'consumed', 'frozen', 'disabled', 'review']

// 档位清单、展示顺序、能不能卖，全部由服务端下发的 registry 决定。
// ★代理侧不再维护任何档位清单，也不再自己判断可见性★
// 写死清单的代价见过一次：卡台开了 Codex 点数，代理明明能发码却在界面上看不到。
// 自己判可见性的代价也见过一次（2026-08-22 线上）：Claude 档位混进来还能发码——
// 它在 ACC 定价表里 enabled=true，但我们根本没有 Claude 兑换流程。
// 服务端已按「卡台注册表 ∩ ACC 定价开关」过滤，这里直接用，不要再加回落分支。
const planRegistry = ref<any[]>([])
const planKeys = computed(() => planRegistry.value.map((r: any) => r.key))
function planMeta(k: string) {
  return planRegistry.value.find((r: any) => r.key === k) || null
}

// 点数档真正要垫的是比索付款，$0.10 只是我们的服务费。
// 只显示服务费的话，代理会把一张 ₱2260 的码当成一毛钱的东西发出去。
function checkoutText(meta: any): string {
  if (!meta?.checkout_amount_minor) return ''
  const cur = meta.checkout_currency || 'PHP'
  const amount = (Number(meta.checkout_amount_minor) / 100).toFixed(2)
  return `${cur} ${amount}`
}

const planCards = computed(() =>
  planRegistry.value.map((meta: any) => ({
    key: meta.key,
    label: meta.label || meta.key,
    enabled: true, // 服务端只下发可卖档位
    service_fee_usd: meta.service_fee_usd ?? null,
    serviceFeeUsdMinor: meta.serviceFeeUsdMinor,
    isCredit: !!meta.is_credit,
    checkoutText: checkoutText(meta),
    requiresActiveSubscription: !!meta.requires_active_subscription,
  })),
)

const canIssue = computed(() =>
  configured.value && form.funding_confirmed && form.count >= 1 && form.count <= ISSUE_MAX && !issuing.value &&
  // 选中的档位必须在可卖清单里：默认值 plus 也可能被卡台/ACC 关掉，
  // 不判的话按钮是亮的、点下去被后端 400 挡回来。
  planKeys.value.includes(form.plan),
)

const isCreditPlan = computed(() => String(form.plan).toLowerCase().startsWith('credit'))
const dualIssueHint = computed(() => {
  if (!dualBindEnabled.value) return ''
  const n = Math.max(1, Math.min(ISSUE_MAX, form.count || 1))
  if (isCreditPlan.value) return `credit* 仅向主台购 ${n} 张，无自动切台`
  const slow = n >= 20 ? '。数量较大时整批可能超过 3 分钟，超时请用「从卡台同步完整码」，不要重复购买' : ''
  const unit = Number(feeOf(form.plan))
  if (!Number.isFinite(unit)) {
    return `将同时向各可用卡台各购 ${n} 张本站码（DN-），上游约双倍服务费${slow}`
  }
  return `将同时向各可用卡台各购 ${n} 张本站码（DN-），上游约 $${(unit * n * 2).toFixed(2)}${slow}`
})

// 可卖清单变了（首次加载 / 卡台改了配置）就把选中项收回到清单内。
watch(planKeys, (keys) => {
  if (keys.length && !keys.includes(form.plan)) form.plan = keys[0]
})

/** 列表行：合并服务器 full_code + 本机兜底缓存 */
const displayRows = computed(() =>
  rows.value.map((row, idx) => {
    const full = lookupFullCode(row)
    const id = row.id != null ? String(row.id) : `i${idx}`
    return {
      ...row,
      rowKey: `${listMode.value}-${id}-${String(row.code_prefix || row.code || idx)}`,
      fullCode: full,
      displayCode: full || String(row.code_prefix || row.code || '').trim() || '',
    }
  }),
)

const pageFullCodes = computed(() =>
  displayRows.value.map((r) => String(r.fullCode || '').trim()).filter((c) => isFullCode(c)),
)
const selectedFullCodes = computed(() =>
  selectedRows.value.map((r) => String(r.fullCode || lookupFullCode(r) || '').trim()).filter((c) => isFullCode(c)),
)
/** 可禁用：有 id 且状态为 unused */
function canDisableRow(row: any) {
  const id = Number(row?.id)
  if (!Number.isFinite(id) || id <= 0) return false
  const st = String(row?.status || 'unused').toLowerCase()
  return st === 'unused' || st === ''
}
/** 可解除禁用：status=disabled */
function canEnableRow(row: any) {
  const id = Number(row?.id)
  if (!Number.isFinite(id) || id <= 0) return false
  return String(row?.status || '').toLowerCase() === 'disabled'
}
const selectedDisableableIds = computed(() => {
  const ids: number[] = []
  const seen = new Set<number>()
  for (const r of selectedRows.value) {
    if (!canDisableRow(r)) continue
    const id = Number(r.id)
    if (!Number.isFinite(id) || id <= 0 || seen.has(id)) continue
    seen.add(id)
    ids.push(id)
  }
  return ids
})
const selectedEnableableIds = computed(() => {
  const ids: number[] = []
  const seen = new Set<number>()
  for (const r of selectedRows.value) {
    if (!canEnableRow(r)) continue
    const id = Number(r.id)
    if (!Number.isFinite(id) || id <= 0 || seen.has(id)) continue
    seen.add(id)
    ids.push(id)
  }
  return ids
})

function rowSelectable(row: any) {
  const id = Number(row?.id)
  return !!row?.fullCode || canDisableRow(row) || canEnableRow(row) || (Number.isFinite(id) && id > 0)
}
function onSelectionChange(rowsSel: any[]) {
  selectedRows.value = Array.isArray(rowsSel) ? rowsSel : []
}
function clearSelection() {
  selectedRows.value = []
  tableRef.value?.clearSelection?.()
}
function onListModeChange() {
  page.value = 1
  clearSelection()
  loadList()
}

function onCopyCommand(cmd: string) {
  if (cmd === 'copySelected') return copySelectedFull()
  if (cmd === 'exportSelected') return exportSelectedFull()
  if (cmd === 'copyPage') return copyPageFull()
  if (cmd === 'exportAll') return exportAllStored()
  if (cmd === 'copyAllStored') return copyAllStored()
}

function onBatchCommand(cmd: string) {
  if (cmd === 'note') return batchSetNoteSelected()
  if (cmd === 'clearNote') return batchClearNoteSelected()
  if (cmd === 'disable') return batchDisableSelected()
  if (cmd === 'enable') return batchEnableSelected()
  if (cmd === 'syncCache') return syncLocalCacheToServer()
  if (cmd === 'clearSel') return clearSelection()
}

function onRowCommand(cmd: string, row: any) {
  if (cmd === 'copy') return copyRowCode(row)
  if (cmd === 'note') return editNoteOne(row)
  if (cmd === 'disable') return disableOne(row)
  if (cmd === 'enable') return enableOne(row)
  return undefined
}

function downloadText(filename: string, text: string) {
  const blob = new Blob([text.endsWith('\n') ? text : text + '\n'], { type: 'text/plain;charset=utf-8' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = filename
  a.click()
  URL.revokeObjectURL(a.href)
}

async function copyCodes(codes: string[], label: string) {
  if (!codes.length) {
    dialog.toast('没有可复制的完整码', 'warn')
    return
  }
  const ok = await copyToClipboard(codes.join('\n'))
  dialog.toast(ok ? `已复制 ${codes.length} 张${label}` : '复制失败，请导出 .txt 后打开', ok ? 'ok' : 'err')
}

async function copySelectedFull() {
  await copyCodes(selectedFullCodes.value, '完整码（选中）')
}
async function copyPageFull() {
  await copyCodes(pageFullCodes.value, '完整码（本页）')
}
function exportSelectedFull() {
  const codes = selectedFullCodes.value
  if (!codes.length) {
    dialog.toast('请先勾选带「完整」标签的行', 'warn')
    return
  }
  downloadText(`cdk-selected-${codes.length}-${Date.now()}.txt`, codes.join('\n'))
  dialog.toast(`已导出 ${codes.length} 张`, 'ok')
}

function appendStoredFilters(qs: URLSearchParams) {
  if (listPlan.value) qs.set('plan', listPlan.value)
  if (listQ.value.trim()) qs.set('q', listQ.value.trim())
  if (listStatus.value) qs.set('status', listStatus.value)
  if (listCodeKind.value) qs.set('code_kind', listCodeKind.value)
  if (listBindingState.value) qs.set('binding_state', listBindingState.value)
  if (listFulfilledAccount.value) qs.set('fulfilled_account_id', listFulfilledAccount.value)
  if (listFailover.value) qs.set('failover', listFailover.value)
}

async function fetchAllStoredCodes(): Promise<string[]> {
  const qs = new URLSearchParams({ limit: '10000' })
  appendStoredFilters(qs)
  const r = await authFetch(`/api/v1/admin/cardplatform/cdks/stored?${qs.toString()}`)
  const d = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error(d.error || d.msg || '拉取本站完整码失败')
  const list = Array.isArray(d.list) ? d.list : []
  const codes: string[] = []
  const seen = new Set<string>()
  for (const it of list) {
    const c = extractFullCode(it) || String(it?.code || '').trim()
    if (!isFullCode(c) || seen.has(c)) continue
    seen.add(c)
    codes.push(c)
  }
  if (d.full_code_in_store != null) storeStats.fullInStore = Number(d.full_code_in_store)
  return codes
}

async function copyAllStored() {
  exportingAll.value = true
  try {
    const codes = await fetchAllStoredCodes()
    await copyCodes(codes, '完整码（本站全部）')
  } catch (e: any) {
    dialog.toast(e?.message || '复制失败', 'err')
  } finally {
    exportingAll.value = false
  }
}

async function exportAllStored() {
  exportingAll.value = true
  try {
    // 优先走后端 txt 附件（更省内存）
    const qs = new URLSearchParams({ format: 'txt', limit: '10000' })
    appendStoredFilters(qs)
    const r = await authFetch(`/api/v1/admin/cardplatform/cdks/stored?${qs.toString()}`)
    if (!r.ok) {
      const d = await r.json().catch(() => ({}))
      throw new Error(d.error || d.msg || '导出失败')
    }
    const text = await r.text()
    const lines = text.split(/\r?\n/).map((s) => s.trim()).filter(Boolean)
    if (!lines.length) {
      dialog.toast('本站没有可导出的完整码', 'warn')
      return
    }
    downloadText(`cdk-stored-all-${lines.length}-${Date.now()}.txt`, lines.join('\n'))
    dialog.toast(`已导出 ${lines.length} 张完整码`, 'ok')
  } catch (e: any) {
    dialog.toast(e?.message || '导出失败', 'err')
  } finally {
    exportingAll.value = false
  }
}

async function disableOne(row: any) {
  const id = Number(row?.id)
  if (!canDisableRow(row) || !id) {
    dialog.toast('只能禁用未使用(unused)的 CDK', 'warn')
    return
  }
  const ok = await dialog.confirm(`确定禁用 ID ${id}？禁用后不可兑换（不退服务费）。`, {
    title: '禁用 CDK',
    okText: '禁用',
    danger: true,
  })
  if (!ok) return
  disabling.value = true
  try {
    const r = await authFetch(`/api/v1/admin/cardplatform/cdks/${id}/disable`, { method: 'POST', body: '{}' })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || d.msg || '禁用失败', 'err')
      return
    }
    dialog.toast(`已禁用 #${id}`, 'ok')
    await loadList()
  } finally {
    disabling.value = false
  }
}

async function batchDisableSelected() {
  const ids = selectedDisableableIds.value
  if (!ids.length) {
    dialog.toast('请勾选可禁用的 CDK（unused）', 'warn')
    return
  }
  const ok = await dialog.confirm(
    `将禁用 ${ids.length} 张未使用 CDK，禁用后不可兑换（不退服务费，可再「解除禁用」）。确定？`,
    { title: '批量禁用', okText: '批量禁用', danger: true },
  )
  if (!ok) return
  disabling.value = true
  try {
    const r = await authFetch('/api/v1/admin/cardplatform/cdks/batch-disable', {
      method: 'POST',
      body: JSON.stringify({ ids }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || d.msg || '批量禁用失败', 'err')
      return
    }
    const nOk = Number(d.disabled_count) || (Array.isArray(d.disabled) ? d.disabled.length : 0)
    const nFail = Number(d.failed_count) || (Array.isArray(d.failed) ? d.failed.length : 0)
    dialog.toast(`禁用完成：成功 ${nOk}，失败 ${nFail}`, nFail ? 'warn' : 'ok')
    await loadList()
  } finally {
    disabling.value = false
  }
}

async function enableOne(row: any) {
  const id = Number(row?.id)
  if (!canEnableRow(row) || !id) {
    dialog.toast('只能对「已禁用」的 CDK 解除禁用', 'warn')
    return
  }
  const ok = await dialog.confirm(`确定解除禁用 ID ${id}？解除后恢复为未使用，可再次兑换。`, {
    title: '解除禁用',
    okText: '解除禁用',
  })
  if (!ok) return
  disabling.value = true
  try {
    const r = await authFetch(`/api/v1/admin/cardplatform/cdks/${id}/enable`, { method: 'POST', body: '{}' })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || d.msg || '解除失败', 'err')
      return
    }
    dialog.toast(`已解除禁用 #${id}`, 'ok')
    await loadList()
  } finally {
    disabling.value = false
  }
}

async function batchEnableSelected() {
  const ids = selectedEnableableIds.value
  if (!ids.length) {
    dialog.toast('请勾选「已禁用」的 CDK', 'warn')
    return
  }
  const ok = await dialog.confirm(
    `将解除禁用 ${ids.length} 张 CDK，恢复为未使用。确定？`,
    { title: '批量解除禁用', okText: '解除禁用' },
  )
  if (!ok) return
  disabling.value = true
  try {
    const r = await authFetch('/api/v1/admin/cardplatform/cdks/batch-enable', {
      method: 'POST',
      body: JSON.stringify({ ids }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || d.msg || '批量解除失败', 'err')
      return
    }
    const nOk = Number(d.enabled_count) || (Array.isArray(d.enabled) ? d.enabled.length : 0)
    const nFail = Number(d.failed_count) || (Array.isArray(d.failed) ? d.failed.length : 0)
    dialog.toast(`解除完成：成功 ${nOk}，失败 ${nFail}`, nFail ? 'warn' : 'ok')
    await loadList()
  } finally {
    disabling.value = false
  }
}


const selectedNoteIds = computed(() => {
  const ids: number[] = []
  const seen = new Set<number>()
  for (const r of selectedRows.value) {
    const id = Number(r?.id)
    if (!Number.isFinite(id) || id <= 0 || seen.has(id)) continue
    seen.add(id)
    ids.push(id)
  }
  return ids
})
const selectedHasNoteIds = computed(() => {
  const ids: number[] = []
  const seen = new Set<number>()
  for (const r of selectedRows.value) {
    const id = Number(r?.id)
    if (!Number.isFinite(id) || id <= 0 || seen.has(id)) continue
    if (!String(r?.note || '').trim()) continue
    seen.add(id)
    ids.push(id)
  }
  return ids
})

async function editNoteOne(row: any) {
  const id = Number(row?.id)
  if (!Number.isFinite(id) || id <= 0) {
    dialog.toast('缺少 CDK ID，无法备注', 'warn')
    return
  }
  const cur = String(row?.note || '')
  const next = await dialog.prompt('为该 CDK 填写备注（留空并确定可清空）', {
    title: `备注 #${id}`,
    defaultValue: cur,
    placeholder: '最多 200 字，例如批次/客户/用途',
    okText: '保存',
  })
  if (next === null || next === undefined) return
  noting.value = true
  try {
    const r = await authFetch(`/api/v1/admin/cardplatform/cdks/${id}/note`, {
      method: 'PUT',
      body: JSON.stringify({ note: String(next) }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || d.msg || '保存备注失败', 'err')
      return
    }
    const note = String(d.note || '').trim()
    // 本地立刻反映
    row.note = note
    for (const it of rows.value) {
      if (Number(it?.id) === id) it.note = note
    }
    dialog.toast(note ? '备注已保存' : '备注已清空', 'ok')
  } finally {
    noting.value = false
  }
}

async function batchSetNoteSelected() {
  const ids = selectedNoteIds.value
  if (!ids.length) {
    dialog.toast('请先勾选要备注的 CDK', 'warn')
    return
  }
  const next = await dialog.prompt(`为选中的 ${ids.length} 张 CDK 设置同一备注`, {
    title: '批量备注',
    defaultValue: '',
    placeholder: '最多 200 字；若要清空请用「批量去除备注」',
    okText: '写入备注',
  })
  if (next === null || next === undefined) return
  const note = String(next).trim()
  if (!note) {
    dialog.toast('备注为空：请用「批量去除备注」清空，或填写内容后再提交', 'warn')
    return
  }
  noting.value = true
  try {
    const r = await authFetch('/api/v1/admin/cardplatform/cdks/batch-note', {
      method: 'POST',
      body: JSON.stringify({ ids, note }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || d.msg || '批量备注失败', 'err')
      return
    }
    const nOk = Number(d.updated_count) || 0
    const nFail = Number(d.failed_count) || 0
    dialog.toast(`批量备注完成：成功 ${nOk}，失败 ${nFail}`, nFail ? 'warn' : 'ok')
    await loadList()
  } finally {
    noting.value = false
  }
}

async function batchClearNoteSelected() {
  const ids = selectedHasNoteIds.value.length
    ? selectedHasNoteIds.value
    : selectedNoteIds.value
  if (!ids.length) {
    dialog.toast('请先勾选要去除备注的 CDK', 'warn')
    return
  }
  const ok = await dialog.confirm(
    `将清除选中 ${ids.length} 张 CDK 的备注，确定？`,
    { title: '批量去除备注', okText: '清除备注', danger: true },
  )
  if (!ok) return
  noting.value = true
  try {
    const r = await authFetch('/api/v1/admin/cardplatform/cdks/batch-clear-note', {
      method: 'POST',
      body: JSON.stringify({ ids }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || d.msg || '批量清除失败', 'err')
      return
    }
    dialog.toast(`已清除 ${d.cleared_count ?? ids.length} 条备注`, 'ok')
    await loadList()
  } finally {
    noting.value = false
  }
}

// ★服务费只认卡台下发的值，猜不到就显示「—」★
// 曾对未知档位一律返回 10：Codex 点数实际是 $0.10，
// 会被显示成 $10（差 100 倍），代理按这个数字定价就直接亏钱。
function feeOf(k: string) {
  const p = plans.value[k]
  if (p?.service_fee_usd != null) return Number(p.service_fee_usd).toFixed(2)
  if (p?.serviceFeeUsdMinor != null) return (p.serviceFeeUsdMinor / 100).toFixed(2)
  return '—'
}
function formatUsd(v: any) {
  const n = Number(v)
  return Number.isFinite(n) ? n.toFixed(2) : '—'
}
function planLabel(k: string) {
  // 文案以卡台注册表为准，其次是定价配置里的 label，最后退回裸键。
  // ★不要在代理侧维护档位文案★——卡台改名或加档，这里会立刻不一致。
  return planMeta(k)?.label || plans.value[k]?.label || k
}
const estimatedTotal = computed(() => {
  // 服务费拿不到时 feeOf 返回「—」，Number('—') 是 NaN，
  // 按钮上会显示「购买 3 张 · $NaN」。宁可显示「—」也不要给代理看一个假数字。
  const unit = Number(feeOf(form.plan))
  if (!Number.isFinite(unit)) return '—'
  const c = Math.max(1, Math.min(ISSUE_MAX, form.count || 1))
  return (unit * c).toFixed(2)
})

function selectPlan(k: string) {
  form.plan = k
}
function statusType(s: string) {
  const st = String(s || '').toLowerCase()
  if (st === 'unused' || st === '') return 'success'
  if (st === 'consumed') return 'info'
  if (st === 'reserved' || st === 'review') return 'warning'
  if (st === 'frozen') return 'warning'
  if (st === 'disabled') return 'danger'
  return 'info'
}
function statusLabel(s: string) {
  const st = String(s || '').toLowerCase()
  const map: Record<string, string> = {
    unused: '未使用',
    reserved: '预留中',
    consumed: '已消耗',
    frozen: '已冻结',
    disabled: '已禁用',
    review: '待审核',
  }
  if (!st) return '未使用'
  return map[st] || st
}

function codeKindLabel(kind: string) {
  const labels: Record<string, string> = {
    site: '本站码',
    legacy: '旧码',
    local_stock: '白号',
  }
  return labels[String(kind || 'legacy').toLowerCase()] || kind || '旧码'
}

function codeKindType(kind: string) {
  const k = String(kind || 'legacy').toLowerCase()
  if (k === 'site') return 'success'
  if (k === 'local_stock') return 'warning'
  return 'info'
}

function bindingStatusLabel(status: string) {
  const labels: Record<string, string> = {
    unused: '可用',
    redeeming: '兑换中',
    reserved: '已预留',
    consumed: '已核销',
    refunded: '已退款',
    retired: '已退役',
    disabled: '已禁用',
    failed: '失败',
    unknown: '待确认',
  }
  return labels[String(status || '').toLowerCase()] || status || '未知'
}

function bindingStatusType(status: string) {
  const s = String(status || '').toLowerCase()
  if (s === 'unused') return 'success'
  if (s === 'refunded' || s === 'retired' || s === 'consumed') return 'info'
  if (s === 'redeeming' || s === 'reserved') return 'warning'
  return 'danger'
}

function accountName(accountId: number) {
  return platformAccounts.value.find((a) => Number(a.id) === Number(accountId))?.name || `账户 ${accountId}`
}

async function loadRecentBindings() {
  const codes = recentCodes.value.filter((code) => code.toUpperCase().startsWith('DN-'))
  recentBindings.value = []
  if (!codes.length) return
  loadingRecentBindings.value = true
  try {
    recentBindings.value = await Promise.all(
      codes.map(async (code) => {
        const qs = new URLSearchParams({ code })
        const r = await authFetch(`/api/v1/admin/card-platforms/bindings?${qs.toString()}`)
        const d = await r.json().catch(() => ({}))
        if (!r.ok) return { code, bindings: [], error: d.error || '加载绑定失败' }
        return { code, bindings: Array.isArray(d.bindings) ? d.bindings : [] }
      }),
    )
  } finally {
    loadingRecentBindings.value = false
  }
}

async function toggleRecentBindings() {
  recentBindingsOpen.value = !recentBindingsOpen.value
  if (recentBindingsOpen.value && !recentBindings.value.length) {
    await loadRecentBindings()
  }
}

function isFullCode(code: string) {
  const c = String(code || '').trim()
  // 完整码至少 20 字符；前缀一般 ≤14
  return c.length >= FULL_CODE_MIN_LEN && c.includes('-')
}

function extractFullCode(item: any): string {
  if (!item || typeof item !== 'object') return ''
  const candidates = [item.full_code, item.fullCode, item.code, item.cdk_code, item.value]
  for (const raw of candidates) {
    const s = String(raw || '').trim()
    if (isFullCode(s)) return s
  }
  return ''
}

function loadCodeCache() {
  try {
    const raw = localStorage.getItem(CODE_CACHE_KEY)
    if (!raw) return
    const o = JSON.parse(raw)
    if (o && typeof o === 'object') codeCache.value = o
  } catch {
    /* ignore */
  }
}

function saveCodeCache() {
  try {
    localStorage.setItem(CODE_CACHE_KEY, JSON.stringify(codeCache.value))
  } catch {
    /* ignore quota */
  }
}

function rememberIssued(items: any[], plan: string) {
  const next = { ...codeCache.value }
  for (const it of items) {
    const code = extractFullCode(it)
    if (!code) continue
    const id = it?.id != null ? String(it.id) : ''
    const prefix = String(it?.code_prefix || code.slice(0, 14) || '').trim()
    const entry: CodeCacheEntry = {
      code,
      plan: String(it?.plan || plan || ''),
      prefix,
      at: Date.now(),
      id: it?.id != null ? Number(it.id) : undefined,
    }
    if (id) next[`id:${id}`] = entry
    if (prefix) next[`pfx:${prefix}`] = entry
    next[`code:${code}`] = entry
  }
  codeCache.value = next
  saveCodeCache()
}

function lookupFullCode(row: any): string {
  if (!row) return ''
  // 优先服务器列表补全的 full_code / code
  const direct = extractFullCode(row)
  if (direct) return direct
  // 兜底：浏览器旧缓存（历史未落库的码）
  const id = row.id != null ? String(row.id) : ''
  const prefix = String(row.code_prefix || '').trim()
  const cache = codeCache.value
  if (id && cache[`id:${id}`]?.code) return cache[`id:${id}`].code
  if (prefix && cache[`pfx:${prefix}`]?.code) return cache[`pfx:${prefix}`].code
  if (prefix) {
    for (const v of Object.values(cache)) {
      if (v?.code && v.code.startsWith(prefix)) return v.code
    }
  }
  return ''
}

/** 把本机完整码缓存上传到服务器 SQLite，解决换浏览器/清缓存丢码 */
async function syncLocalCacheToServer(opts?: { quiet?: boolean }) {
  const quiet = !!opts?.quiet
  const items: { id: number; code: string; code_prefix?: string; plan?: string }[] = []
  const seen = new Set<string>()
  for (const v of Object.values(codeCache.value)) {
    const code = String(v?.code || '').trim()
    if (!isFullCode(code) || seen.has(code)) continue
    seen.add(code)
    items.push({
      id: Number(v?.id) || 0,
      code,
      code_prefix: v?.prefix || code.slice(0, 14),
      plan: v?.plan || '',
    })
  }
  if (!items.length) {
    if (!quiet) dialog.toast('本机没有可同步的完整码缓存', 'info')
    return
  }
  syncingCache.value = true
  try {
    const r = await authFetch('/api/v1/admin/cardplatform/cdks/store', {
      method: 'POST',
      body: JSON.stringify({ items }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      if (!quiet) dialog.toast(d.error || '同步失败', 'err')
      return
    }
    const saved = Number(d.saved) || 0
    if (!quiet || saved > 0) {
      dialog.toast(`已同步到服务器：成功 ${saved}，跳过 ${d.skipped ?? 0}，失败 ${d.failed ?? 0}`, 'ok')
    }
    await loadList()
  } finally {
    syncingCache.value = false
  }
}

function persistRecent(codes: string[], plan: string) {
  const payload = { codes, plan, at: Date.now() }
  try {
    sessionStorage.setItem(RECENT_KEY, JSON.stringify(payload))
  } catch {
    /* ignore quota */
  }
  recentMeta.value = {
    plan,
    atLabel: new Date(payload.at).toLocaleString(),
  }
}

function loadPersistedRecent() {
  try {
    const raw = sessionStorage.getItem(RECENT_KEY)
    if (!raw) return
    const o = JSON.parse(raw)
    const codes = Array.isArray(o?.codes) ? o.codes.map((x: any) => String(x || '').trim()).filter(Boolean) : []
    if (!codes.length) return
    recentCodes.value = codes
    recentMeta.value = {
      plan: String(o.plan || '—'),
      atLabel: o.at ? new Date(o.at).toLocaleString() : '—',
    }
  } catch {
    /* ignore */
  }
}

function clearRecent() {
  recentCodes.value = []
  recentMeta.value = null
	recentBindings.value = []
	recentBindingsOpen.value = false
  try {
    sessionStorage.removeItem(RECENT_KEY)
  } catch {
    /* ignore */
  }
  dialog.toast('已清除本批完整码缓存', 'info')
}

function openAgentSwap() {
  window.open(agentSwapUrl, '_blank')
}

async function copyText(t: string) {
  const ok = await copyToClipboard(t)
  dialog.toast(ok ? '已复制' : '复制失败，请在文本框中全选手动复制', ok ? 'ok' : 'err')
}

async function copyRowCode(row: any) {
  // 优先服务器 full_code，其次本机兜底缓存
  const code = String(row?.fullCode || extractFullCode(row) || row?.displayCode || '').trim()
  if (!code) {
    dialog.toast('无可复制内容', 'warn')
    return
  }
  const isFull = isFullCode(code)
  if (!isFull) {
    dialog.toast('仅有前缀：本站未存该码完整串。请在本页重新发码，或点「同步本机缓存到服务器」。', 'warn')
  }
  const ok = await copyToClipboard(code)
  if (!ok) {
    dialog.toast('复制失败，请长按文本手动复制', 'err')
    return
  }
  dialog.toast(isFull ? '已复制完整卡密' : '已复制前缀（非完整码）', isFull ? 'ok' : 'warn')
}

async function loadMeta() {
  loadingMeta.value = true
  metaError.value = ''
  try {
    const [pr, br, er, sr, cr] = await Promise.all([
      authFetch('/api/v1/admin/cardplatform/plans'),
      authFetch('/api/v1/admin/cardplatform/balance'),
      authFetch('/api/v1/admin/network/egress'),
      authFetch('/api/v1/admin/settings'),
      authFetch('/api/v1/admin/card-platforms'),
    ])
    if (sr.ok) {
      const s = await sr.json()
      configured.value = !!s.card_api_key_configured
    }
    if (cr.ok) {
      const c = await cr.json()
      dualBindEnabled.value = !!c.dual_bind
      const accounts = Array.isArray(c.accounts) ? c.accounts : []
      platformAccounts.value = accounts.map((a: any) => ({ id: Number(a.id), name: String(a.name || `账户 ${a.id}`) }))
      activePlatformN.value = accounts.filter(
        (a: any) => a.status === 'active' && a.circuit_state !== 'open' && a.has_credential,
      ).length
      if (dualBindEnabled.value) {
        configured.value = activePlatformN.value > 0
      }
    }
    if (er.ok) {
      const e = await er.json()
      egressIp.value = e.egress_ip || ''
    }
    if (pr.ok) {
      const d = await pr.json()
      plans.value = d.plans || {}
      // 服务端已按「卡台注册表 ∩ ACC 定价开关」过滤，这里拿到什么就显示什么
      planRegistry.value = d.registry || []
      pricingVersion.value = d.version ?? null
      priceSource.value = 'live'
    } else {
      const d = await pr.json().catch(() => ({}))
      metaError.value = d.error || d.msg || '无法获取实时价格（检查 Key / 出口 IP 白名单）'
      // ★取不到实时档位时不要编一份出来★：这里编的清单既不知道卡台开了哪些档，
      // 也不知道 ACC 的开关状态，照着它发码就是在赌。清空 + 上面的报错更诚实。
      plans.value = {}
      planRegistry.value = []
      priceSource.value = 'unavailable'
    }
    if (br.ok) {
      const b = await br.json()
      balanceText.value = String(b.spendable_balance ?? b.balance ?? '')
    }
  } catch (e: any) {
    metaError.value = e?.message || '网络错误'
  } finally {
    loadingMeta.value = false
  }
}

async function issue() {
  issueError.value = ''
  issueOk.value = ''
  if (!canIssue.value) return
  issuing.value = true
  try {
    const r = await authFetch('/api/v1/admin/cardplatform/cdks', {
      method: 'POST',
      body: JSON.stringify({
        plan: form.plan,
        count: form.count,
        funding_confirmed: true,
      }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      const msg = d.error || d.msg || '发码失败'
      issueError.value = msg
      if (String(msg).includes('403') || d.code === 403) {
        issueError.value += ' — 可能是 IP 未进白名单，请复制上方出口 IP 到卡台'
      }
      const low = String(msg).toLowerCase()
      if (low.includes('timeout') || msg.includes('超时') || low.includes('deadline')) {
        issueError.value += '。不要重复购买，请点「从卡台同步完整码」找回。'
      }
      return
    }
    const issued = Array.isArray(d.issued) ? d.issued : (Array.isArray(d.data?.issued) ? d.data.issued : [])
    const codes = issued.map(extractFullCode).filter(Boolean)
    if (!codes.length) {
      issueError.value = '卡台未返回完整码。请点「从卡台同步完整码」找回，不要重复购买。'
      recentCodes.value = []
      return
    }
    // 浏览器兜底 + 列表以服务器为准
    rememberIssued(issued, form.plan)
    recentCodes.value = codes
    persistRecent(codes, form.plan)
    if (d.site_dual_bind) {
      recentBindings.value = []
      recentBindingsOpen.value = false
    } else {
      recentBindings.value = []
      recentBindingsOpen.value = false
    }
    issueOpen.value = true
    const shortOnes = codes.filter((c: string) => !isFullCode(c))
    const storedN = Number(d.stored_count ?? d.stored)
    const storeFail = Number(d.store_failed) || 0
    let okMsg = d.recovered
      ? (d.msg || `发码请求未完成，已从卡台找回 ${codes.length} 张完整码（请勿再买一次）`)
      : shortOnes.length
        ? `成功 ${codes.length} 张，但有 ${shortOnes.length} 张长度异常，请核对`
        : d.site_dual_bind
          ? `成功 ${codes.length} 张本站码 DN-（双绑已落库）`
          : `成功 ${codes.length} 张完整码（每条约 ${codes[0]?.length || '—'} 字符）`
    if (Number.isFinite(storedN)) {
      okMsg += ` · 服务器已存 ${storedN}`
      if (storeFail > 0) okMsg += `（${storeFail} 条落库失败）`
    }
    if (d.partial_error) okMsg += ` · 部分失败：${d.partial_error}`
    issueOk.value = okMsg
    dialog.toast(issueOk.value, shortOnes.length || storeFail ? 'warn' : 'ok')
    // 发码成功后自动尝试复制全部，减少漏拷
    await copyAll(false)
    form.funding_confirmed = false
    await loadList()
    await loadMeta()
  } finally {
    issuing.value = false
  }
}

async function syncFromCardplatform() {
  syncingUpstream.value = true
  try {
    const r = await authFetch('/api/v1/admin/cardplatform/cdks/sync', {
      method: 'POST',
      body: JSON.stringify({
        status: listStatus.value || 'unused',
        plan: listPlan.value || form.plan || '',
      }),
    })
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      dialog.toast(d.error || d.msg || '从卡台同步失败', 'err')
      return
    }
    if (d.need_cardplatform_upgrade) {
      dialog.toast(d.msg || '卡台列表仍只回前缀，没有完整码可同步。不要重复购买。', 'warn')
    } else {
      dialog.toast(d.msg || `已同步 ${d.imported || 0} 张完整码`, (d.imported || 0) > 0 ? 'ok' : 'info')
    }
    if (d.full_code_in_store != null) storeStats.fullInStore = Number(d.full_code_in_store)
    await loadList()
  } finally {
    syncingUpstream.value = false
  }
}

function searchList() {
  page.value = 1
  return loadList()
}

async function loadList() {
  loadingList.value = true
  listError.value = ''
  clearSelection()
  try {
    if (listMode.value === 'stored') {
      const qs = new URLSearchParams({
        page: String(page.value),
        page_size: String(pageSize.value),
      })
      if (listQ.value.trim()) qs.set('q', listQ.value.trim())
      if (listPlan.value) qs.set('plan', listPlan.value)
      if (listStatus.value) qs.set('status', listStatus.value)
      if (listCodeKind.value) qs.set('code_kind', listCodeKind.value)
      if (listBindingState.value) qs.set('binding_state', listBindingState.value)
      if (listFulfilledAccount.value) qs.set('fulfilled_account_id', listFulfilledAccount.value)
      if (listFailover.value) qs.set('failover', listFailover.value)
      const r = await authFetch(`/api/v1/admin/cardplatform/cdks/stored?${qs.toString()}`)
      const d = await r.json().catch(() => ({}))
      if (!r.ok) {
        listError.value = d.error || d.msg || '加载本站完整码失败'
        rows.value = []
        total.value = 0
        return
      }
      const list = Array.isArray(d.list) ? d.list : []
      rememberIssued(list, form.plan)
      rows.value = list
      total.value = Number(d.total) || 0
      if (d.page) page.value = Number(d.page) || page.value
      storeStats.fullOnPage = list.filter((it: any) => isFullCode(extractFullCode(it) || it.code)).length
      storeStats.fullInStore = d.full_code_in_store != null ? Number(d.full_code_in_store) : null
      return
    }

    const qs = new URLSearchParams({
      page: String(page.value),
      page_size: String(pageSize.value),
    })
    if (listQ.value.trim()) qs.set('q', listQ.value.trim())
    if (listStatus.value) qs.set('status', listStatus.value)
    if (listPlan.value) qs.set('plan', listPlan.value)
    const r = await authFetch(`/api/v1/admin/cardplatform/cdks?${qs.toString()}`)
    const d = await r.json().catch(() => ({}))
    if (!r.ok) {
      listError.value = d.error || d.msg || '列表失败'
      rows.value = []
      total.value = 0
      return
    }
    const list = Array.isArray(d.list) ? d.list : []
    rememberIssued(list, form.plan)
    rows.value = list
    total.value = d.total || 0
    storeStats.fullOnPage = d.full_code_on_page != null ? Number(d.full_code_on_page) : null
    storeStats.fullInStore = d.full_code_in_store != null ? Number(d.full_code_in_store) : null
  } finally {
    loadingList.value = false
  }
}

async function copyAll(showToast = true) {
  if (!recentCodes.value.length) return
  const ok = await copyToClipboard(recentCodes.value.join('\n'))
  if (showToast) dialog.toast(ok ? `已复制 ${recentCodes.value.length} 张完整码` : '复制失败，请用下方文本框全选', ok ? 'ok' : 'err')
}

function downloadCodes() {
  if (!recentCodes.value.length) return
  const blob = new Blob([recentCodes.value.join('\n') + '\n'], { type: 'text/plain;charset=utf-8' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = `cdk-${form.plan || recentMeta.value?.plan || 'batch'}-${Date.now()}.txt`
  a.click()
  URL.revokeObjectURL(a.href)
  dialog.toast('已导出 .txt', 'ok')
}

async function refreshAll() {
  await loadMeta()
  await loadList()
}

onMounted(async () => {
  loadCodeCache()
  loadPersistedRecent()
  if (recentCodes.value.length) issueOpen.value = true
  await refreshAll()
  // 若本机有历史完整码而服务器库为空，自动静默回填一次
  const localN = Object.values(codeCache.value).filter((v) => isFullCode(v?.code || '')).length
  if (localN > 0 && storeStats.fullInStore === 0) {
    try {
      await syncLocalCacheToServer({ quiet: true })
    } catch {
      /* ignore */
    }
  }
})
</script>

<style scoped>
.fold-head {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  background: transparent;
  border: 0;
  cursor: pointer;
  text-align: left;
  color: inherit;
}
.fold-head:hover { background: var(--primary-soft); }
.fold-caret {
  margin-left: auto;
  font-size: 12px;
  color: var(--ink-2);
}
.binding-fold-head {
  display: flex;
  justify-content: space-between;
  width: 100%;
  padding: 8px 0 4px;
  border: 0;
  border-top: 1px solid var(--brd);
  background: transparent;
  color: var(--ink-2);
  font-size: 12px;
  cursor: pointer;
}
.binding-debug-row {
  padding: 8px 10px;
  border: 1px solid var(--brd);
  border-radius: 8px;
  background: var(--surface);
}
.binding-summary {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.plan-card-sm {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid var(--brd);
  border-radius: 10px;
  background: var(--surface);
  cursor: pointer;
}
.plan-card-sm:hover { border-color: var(--brd-2); }
.plan-card-sm--on {
  border-color: var(--primary);
  box-shadow: 0 0 0 1px var(--primary-soft);
}
.sel-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-radius: 10px;
  background: var(--primary-soft);
  font-size: 13px;
}
.mono { font-variant-numeric: tabular-nums; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }

/* 列表卡密：可点复制 */
.code-cell {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
  width: 100%;
  text-align: left;
  background: transparent;
  border: none;
  padding: 2px 0;
  cursor: pointer;
}
.code-cell:hover .code-cell__text.is-full {
  text-decoration: underline;
  text-underline-offset: 2px;
}
.code-cell__text {
  font-size: 12px;
  line-height: 1.4;
  word-break: break-all;
}
.code-cell__text.is-full {
  color: var(--good);
}
.code-cell__text.is-prefix {
  color: var(--ink-2);
}
.code-cell__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  font-size: 11px;
}

.note-cell {
  display: block;
  width: 100%;
  text-align: left;
  background: transparent;
  border: 0;
  padding: 2px 0;
  cursor: pointer;
  color: inherit;
}
.note-cell__text {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  word-break: break-word;
  font-size: 12px;
  line-height: 1.35;
  color: var(--el-text-color-primary, #303133);
}
.note-cell__empty {
  font-size: 12px;
  color: var(--el-text-color-placeholder, #a8abb2);
}
</style>
