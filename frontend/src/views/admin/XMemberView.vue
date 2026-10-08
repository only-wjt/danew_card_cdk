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

    <template v-if="tab === 'issue'">
    <div class="card space-y-4">
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
      <div class="flex flex-wrap items-center gap-3">
        <span class="text-sm text-muted">数量</span>
        <el-button size="small" :disabled="issue.quantity <= 1" @click="issue.quantity = Math.max(1, issue.quantity - 1)">−</el-button>
        <input v-model.number="issue.quantity" type="number" min="1" max="200" class="input !w-16 text-center mono" />
        <el-button size="small" :disabled="issue.quantity >= 200" @click="issue.quantity = Math.min(200, issue.quantity + 1)">+</el-button>
        <el-button-group>
          <el-button v-for="n in [1, 10, 50, 100, 200]" :key="n" size="small" @click="issue.quantity = n">{{ n }}</el-button>
        </el-button-group>
        <template v-if="currentSupply?.source === 'spacex'">
          <span class="text-sm text-muted">付款地区</span>
          <el-select v-model="issue.payment_country" size="small" style="width: 120px">
            <el-option v-for="r in regions" :key="r" :label="r" :value="r" />
          </el-select>
        </template>
        <el-input v-model="issue.note" size="small" class="!w-48" placeholder="备注，客服可搜" />
        <el-button type="primary" :loading="issuing" :disabled="!currentSupply || planBlocked(currentSupply)" @click="doIssue">
          {{ issuing ? '生成中…' : `生成 ${issue.quantity} 张 ${currentSupply?.label || ''}` }}
        </el-button>
      </div>
      <p v-if="currentSupply && planBlocked(currentSupply)" class="text-xs" style="color: var(--warn, #b45309)">
        {{ !avanXAcc ? '这个套餐走 Avanfinity，但 Avanfinity X 还没接入，先在上方开通；或在「供货设置」里改成 SpaceX。' : 'X CDK 通道还没启用：先去卡台「付款卡」点「保存并启用通道」。' }}
      </p>
      <div v-if="issuedCodes.length" class="rounded-xl bg-soft p-3 space-y-2 border" style="border-color: var(--good)">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div class="text-sm font-medium" style="color: var(--good)">
            本批 {{ issuedCodes.length }} 张
            <span v-if="issuedMeta" class="text-xs text-muted font-normal">
              · {{ issuedMeta.plan }} · {{ issuedMeta.source }}<template v-if="issuedMeta.region"> · {{ issuedMeta.region }} 付款</template> · {{ issuedMeta.at }}
            </span>
          </div>
          <div class="flex gap-1">
            <el-button size="small" type="success" @click="copy(issuedCodes.join('\n'))">复制</el-button>
            <el-button size="small" @click="copy(links.join('\n'))">复制链接</el-button>
            <el-button size="small" @click="downloadText(issuedCodes, 'x-cdk')">导出</el-button>
            <el-button size="small" text type="danger" @click="clearIssued">清除</el-button>
          </div>
        </div>
        <textarea
          class="input mono text-sm !min-h-[88px] w-full"
          readonly
          :value="issuedCodes.join('\n')"
          @focus="($event.target as HTMLTextAreaElement).select()"
        />
      </div>
    </div>

    <section class="card space-y-3">
      <div>
        <h2 class="text-lg font-semibold text-ink">CDK 列表</h2>
        <p class="text-xs text-muted mt-0.5">共 {{ listTotal }} 条 · 只列 Avanfinity 出的码（DNX-），SpaceX 出的码在「GPT 会员」页</p>
      </div>
      <div class="toolbar-filters">
        <el-input v-model="listQ" clearable class="!w-[260px]" placeholder="搜索卡密 / 用户名 / 备注" @keyup.enter="loadList" @clear="loadList" />
        <el-select v-model="listGroup" placeholder="状态" class="!w-[130px]" @change="loadList">
          <el-option v-for="g in groups" :key="g.key" :label="g.label" :value="g.key" />
        </el-select>
        <el-select v-model="listPlan" clearable placeholder="套餐" class="!w-[160px]" @change="loadList">
          <el-option v-for="k in listPlans" :key="k" :label="planName(k)" :value="k" />
        </el-select>
        <el-button type="primary" :loading="loadingList" @click="loadList">查询</el-button>
        <el-button :loading="loadingList" @click="loadList(); loadBatches()">刷新</el-button>
        <span class="flex-1"></span>
        <el-dropdown trigger="click" @command="onCopyCommand">
          <el-button size="small">复制 / 导出<el-icon class="el-icon--right"><ArrowDown /></el-icon></el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="copySelected" :disabled="!listSelected.length">复制选中 ({{ listSelected.length }})</el-dropdown-item>
              <el-dropdown-item command="exportSelected" :disabled="!listSelected.length">导出选中 .txt</el-dropdown-item>
              <el-dropdown-item command="copyPage" :disabled="!pagedList.length">复制本页 ({{ pagedList.length }})</el-dropdown-item>
              <el-dropdown-item divided command="exportAll" :disabled="!filteredList.length">导出当前列表 ({{ filteredList.length }})</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-dropdown trigger="click" @command="onBatchCommand">
          <el-button size="small">批量操作<el-icon class="el-icon--right"><ArrowDown /></el-icon></el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="copyLinks" :disabled="!listSelected.length">复制兑换链接 ({{ listSelected.length }})</el-dropdown-item>
              <el-dropdown-item divided command="disable" :disabled="!selectedDisableable.length">批量作废 ({{ selectedDisableable.length }})</el-dropdown-item>
              <el-dropdown-item command="clearSel" :disabled="!listSelected.length">清空选择</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
      <div class="overflow-x-auto">
        <el-table
          :data="pagedList"
          v-loading="loadingList"
          size="small"
          stripe
          empty-text="暂无数据"
          row-key="code_id"
          @selection-change="(rows: RecordRow[]) => (listSelected = rows)"
        >
          <el-table-column type="selection" width="44" reserve-selection />
          <el-table-column label="ID" width="72">
            <template #default="{ row }">{{ row.code_id }}</template>
          </el-table-column>
          <el-table-column label="卡密" min-width="240">
            <template #default="{ row }">
              <button type="button" class="code-cell" title="点击复制完整码" @click="copy(row.code)">
                <span class="mono break-all code-cell__text is-full">{{ row.code }}</span>
                <span class="code-cell__meta">
                  <el-tag size="small" type="success" effect="plain">完整</el-tag>
                  <span class="text-subtle">{{ row.code.length }}字 · 点复制</span>
                </span>
              </button>
            </template>
          </el-table-column>
          <el-table-column label="套餐" min-width="130">
            <template #default="{ row }">{{ planName(row.plan) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="120">
            <template #default="{ row }">
              <el-tag size="small" :type="groupTagType(row.group)" :title="row.message || ''">{{ statusName(row.status) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="开通给" min-width="130">
            <template #default="{ row }">
              <span v-if="row.recipient" class="mono">{{ '@' + row.recipient }}</span>
              <span v-else class="text-subtle">—</span>
            </template>
          </el-table-column>
          <el-table-column label="参考金额" width="96">
            <template #default="{ row }"><span class="mono">{{ usd(row.estimated_usd_e4) }}</span></template>
          </el-table-column>
          <el-table-column label="服务费" width="88">
            <template #default="{ row }"><span class="mono">{{ usd(row.service_fee_e4) }}</span></template>
          </el-table-column>
          <el-table-column label="备注" min-width="140">
            <template #default="{ row }">
              <span v-if="row.note" class="note-cell__text">{{ row.note }}</span>
              <span v-else class="note-cell__empty">—</span>
            </template>
          </el-table-column>
          <el-table-column prop="created_at" label="时间" min-width="148" />
          <el-table-column label="" width="72" fixed="right" align="right">
            <template #default="{ row }">
              <el-dropdown trigger="click" @command="(cmd: string) => onRowCommand(cmd, row)">
                <el-button size="small" link>操作</el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item command="copy">复制卡密</el-dropdown-item>
                    <el-dropdown-item command="link">复制兑换链接</el-dropdown-item>
                    <el-dropdown-item command="detail">看兑换记录</el-dropdown-item>
                    <el-dropdown-item v-if="row.redemption_id" command="requery">重新查询</el-dropdown-item>
                    <el-dropdown-item v-if="canDisable(row)" command="disable" divided>作废</el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </template>
          </el-table-column>
        </el-table>
      </div>
      <div class="flex flex-wrap items-center justify-between gap-3 text-sm text-muted">
        <span>第 {{ listPage }} 页 · 共 {{ listTotal }} 条<template v-if="listTotal > listRows.length"> · 这里列出最新 {{ listRows.length }} 张，搜索卡密可以找到更早的</template></span>
        <el-pagination
          background
          layout="prev, pager, next, sizes"
          :total="filteredList.length"
          :page-size="listPageSize"
          :current-page="listPage"
          :page-sizes="[20, 50, 100]"
          @current-change="(p: number) => (listPage = p)"
          @size-change="(s: number) => { listPageSize = s; listPage = 1 }"
        />
      </div>
      <details v-if="batches.length" class="text-sm">
        <summary class="cursor-pointer text-muted">最近批次（{{ batches.length }}）</summary>
        <el-table :data="batches" size="small" stripe class="mt-2">
          <el-table-column label="批次" width="72" prop="id" />
          <el-table-column label="套餐" min-width="130">
            <template #default="{ row }">{{ planName(row.plan) }}</template>
          </el-table-column>
          <el-table-column label="已用 / 张数" width="110">
            <template #default="{ row }">{{ row.used }} / {{ row.quantity }}</template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default="{ row }">{{ batchStatus(row.status) }}</template>
          </el-table-column>
          <el-table-column label="备注" min-width="120" prop="note" />
          <el-table-column label="时间" min-width="148" prop="created_at" />
          <el-table-column label="" width="140" align="right">
            <template #default="{ row }">
              <el-button v-if="row.status === 'pending'" size="small" link @click="retryBatch(row.id)">重试</el-button>
              <el-button size="small" link @click="exportBatch(row.id)">导出</el-button>
            </template>
          </el-table-column>
        </el-table>
      </details>
    </section>
    </template>

    <div v-else-if="tab === 'records'" class="space-y-3">
      <div class="card !py-3 toolbar-filters text-sm">
        <span class="text-muted">共 <b class="mono text-ink">{{ recTotal }}</b> 笔</span>
        <el-select v-model="recGroup" style="width: 140px" @change="loadRecords">
          <el-option v-for="g in groups" :key="g.key" :label="g.label" :value="g.key" />
        </el-select>
        <el-input v-model="recQ" clearable class="!w-[240px]" placeholder="卡密 / 用户名 / 备注" @keyup.enter="loadRecords" @clear="loadRecords" />
        <el-button type="primary" :loading="loadingRecords" @click="loadRecords">查询</el-button>
      </div>
      <div class="card overflow-hidden !p-0">
        <el-table :data="pagedRecords" v-loading="loadingRecords" size="small" stripe empty-text="暂无兑换记录">
          <el-table-column label="记录" width="72">
            <template #default="{ row }">#{{ row.code_id }}</template>
          </el-table-column>
          <el-table-column label="卡密" min-width="160">
            <template #default="{ row }">
              <div class="mono text-xs">{{ shortCode(row.code) }}</div>
              <div class="text-xs text-subtle mt-1">{{ channelName(row.channel) }}</div>
            </template>
          </el-table-column>
          <el-table-column label="套餐" min-width="140">
            <template #default="{ row }">{{ planName(row.plan) }}</template>
          </el-table-column>
          <el-table-column label="开通给" min-width="120">
            <template #default="{ row }">
              <span class="mono text-sm">{{ row.recipient ? '@' + row.recipient : '—' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="金额" width="130">
            <template #default="{ row }">
              <div class="mono text-sm">{{ officialAmount(row) }}</div>
              <div v-if="row.service_fee_e4" class="text-xs text-subtle">费 {{ usd(row.service_fee_e4) }}</div>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="120">
            <template #default="{ row }">
              <el-tag size="small" :type="groupTagType(row.group)">{{ statusName(row.status) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="时间" min-width="148">
            <template #default="{ row }">{{ row.created_at || '—' }}</template>
          </el-table-column>
          <el-table-column label="操作" width="88" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" size="small" @click="openRecord(row)">详情</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
      <div class="flex flex-wrap items-center justify-between gap-3 text-sm text-muted">
        <span>第 {{ recPage }} 页 · 本页 {{ pagedRecords.length }} 条<template v-if="recTotal > records.length"> · 最新 {{ records.length }} 条，搜索可以找到更早的</template></span>
        <el-pagination
          background
          layout="prev, pager, next, sizes"
          :total="records.length"
          :page-size="recPageSize"
          :current-page="recPage"
          :page-sizes="[20, 50, 100]"
          @current-change="(p: number) => (recPage = p)"
          @size-change="(s: number) => { recPageSize = s; recPage = 1 }"
        />
      </div>
      <el-drawer v-model="detailOpen" title="兑换详情" size="420px">
        <div v-if="selected" class="space-y-4 text-sm">
          <div class="flex items-center gap-2">
            <el-tag :type="groupTagType(selected.group)">{{ statusName(selected.status) }}</el-tag>
            <span class="text-muted">{{ channelName(selected.channel) }}</span>
          </div>
          <p v-if="selected.message" class="rounded-lg p-3" style="background: var(--warn-soft, #fff7ed)">{{ selected.message }}</p>
          <el-descriptions :column="1" border size="small">
            <el-descriptions-item label="卡密"><span class="mono text-xs break-all">{{ selected.code }}</span></el-descriptions-item>
            <el-descriptions-item label="套餐">{{ planName(selected.plan) }}</el-descriptions-item>
            <el-descriptions-item label="开通给">{{ selected.recipient ? '@' + selected.recipient : '—' }}</el-descriptions-item>
            <el-descriptions-item label="官方金额">{{ officialAmount(selected) }}</el-descriptions-item>
            <el-descriptions-item label="参考美元">{{ usd(selected.estimated_usd_e4) }}</el-descriptions-item>
            <el-descriptions-item label="服务费">{{ usd(selected.service_fee_e4) }}</el-descriptions-item>
            <el-descriptions-item v-if="selected.note" label="备注">{{ selected.note }}</el-descriptions-item>
            <el-descriptions-item label="时间">{{ selected.created_at || '—' }}</el-descriptions-item>
          </el-descriptions>
          <div>
            <div class="font-medium mb-2">处理过程</div>
            <div v-for="(ev, i) in eventsOf(selected)" :key="i" class="text-muted">{{ ev.t }} · {{ ev.s }}</div>
            <p v-if="!eventsOf(selected).length" class="text-muted">还没有过程记录。</p>
          </div>
          <div class="flex flex-wrap gap-2">
            <el-button v-if="selected.redemption_id" size="small" @click="requery(selected.redemption_id)">重新查询</el-button>
            <el-button v-if="selected.group === 'todo' && selected.redemption_id" size="small" @click="resolve(selected)">人工处理</el-button>
            <el-button v-if="selected.status === 'unused'" size="small" @click="disableCode(selected)">作废</el-button>
            <el-button size="small" @click="copy(locationOrigin() + '/x?code=' + selected.code)">复制兑换链接</el-button>
          </div>
          <details class="text-xs text-muted">
            <summary>排障信息</summary>
            <div class="mt-2 space-y-1">
              <div>账户 {{ selected.account_id || '—' }}</div>
              <div class="break-all">上游订单 {{ selected.upstream_order_id || '—' }}</div>
              <div class="break-all">请求 {{ selected.client_request_id || '—' }}</div>
              <div>上游状态 {{ selected.upstream_status || '—' }} · 已查 {{ selected.poll_count || 0 }} 次</div>
              <div>注资 {{ selected.funding_dispatched ? '已发出' : '未发出' }} · 付款 {{ selected.payment_dispatched ? '已发出' : '未发出' }}</div>
            </div>
          </details>
        </div>
      </el-drawer>
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
import { ArrowDown } from '@element-plus/icons-vue'

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
  created_at: string
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
const detailOpen = ref(false)
const loadingRecords = ref(false)
const recTotal = ref(0)
const recPage = ref(1)
const recPageSize = ref(20)
const recGroup = ref('all')
const recQ = ref('')
const pagedRecords = computed(() => {
  const start = (recPage.value - 1) * recPageSize.value
  return records.value.slice(start, start + recPageSize.value)
})
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

// 发码页下方的 CDK 列表（布局和「CDK 卡密」页一致）
const issuedMeta = ref<{ plan: string; source: string; region: string; at: string } | null>(null)
const listQ = ref('')
const listGroup = ref('all')
const listPlan = ref('')
const listPage = ref(1)
const listPageSize = ref(50)
const listRows = ref<RecordRow[]>([])
const loadingList = ref(false)
const listSelected = ref<RecordRow[]>([])
const listTotal = ref(0)
const filteredList = computed(() => listRows.value)
const listPlans = computed(() => Array.from(new Set([...Object.keys(names), ...listRows.value.map((r) => r.plan)])))
const pagedList = computed(() => {
  const start = (listPage.value - 1) * listPageSize.value
  return filteredList.value.slice(start, start + listPageSize.value)
})
const selectedDisableable = computed(() => listSelected.value.filter(canDisable))
function canDisable(row: RecordRow) { return row.status === 'unused' }
function groupTagType(g: string) {
  if (g === 'done') return 'success'
  if (g === 'todo') return 'danger'
  if (g === 'running') return 'warning'
  return 'info'
}
function redeemLink(code: string) { return locationOrigin() + '/x?code=' + code }
async function loadList() {
  loadingList.value = true
  try {
    const params = new URLSearchParams({ group: listGroup.value, q: listQ.value.trim() })
    if (listPlan.value) params.set('plan', listPlan.value)
    const r = await authFetch(`/api/v1/admin/x/records?${params}`)
    const d = await r.json().catch(() => ({}))
    listRows.value = d.records || []
    listTotal.value = Number(d.total ?? listRows.value.length)
    listPage.value = 1
  } finally { loadingList.value = false }
}
function downloadText(lines: string[], prefix: string) {
  const blob = new Blob([lines.join('\n') + '\n'], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${prefix}-${Date.now()}.txt`
  a.click()
  URL.revokeObjectURL(url)
}
function clearIssued() {
  issuedCodes.value = []
  links.value = []
  issuedMeta.value = null
}
function onCopyCommand(cmd: string) {
  if (cmd === 'copySelected') void copy(listSelected.value.map((r) => r.code).join('\n'))
  else if (cmd === 'exportSelected') downloadText(listSelected.value.map((r) => r.code), 'x-cdk')
  else if (cmd === 'copyPage') void copy(pagedList.value.map((r) => r.code).join('\n'))
  else if (cmd === 'exportAll') downloadText(filteredList.value.map((r) => r.code), 'x-cdk')
}
async function onBatchCommand(cmd: string) {
  if (cmd === 'copyLinks') { void copy(listSelected.value.map((r) => redeemLink(r.code)).join('\n')); return }
  if (cmd === 'clearSel') { listSelected.value = []; return }
  if (cmd !== 'disable') return
  const rows = selectedDisableable.value
  const ok = await dialog.confirm(`作废选中的 ${rows.length} 张未使用卡密？CDK 通道会同时撤销还没动钱的上游码。付款未确认的码请到兑换记录里人工处理。`, { title: '批量作废', danger: true, okText: '作废' })
  if (!ok) return
  let fail = 0
  for (const row of rows) {
    const r = await authFetch(`/api/v1/admin/x/codes/${row.code_id}/disable`, { method: 'POST' })
    if (!r.ok) fail++
  }
  dialog.toast(fail ? `${rows.length - fail} 张已作废，${fail} 张失败` : `已作废 ${rows.length} 张`, fail ? 'warn' : 'ok')
  await loadList()
  await loadRecords()
}
function onRowCommand(cmd: string, row: RecordRow) {
  if (cmd === 'copy') void copy(row.code)
  else if (cmd === 'link') void copy(redeemLink(row.code))
  else if (cmd === 'requery') void requery(row.redemption_id)
  else if (cmd === 'disable') void disableCode(row)
  else if (cmd === 'detail') {
    recGroup.value = 'all'
    recQ.value = row.code
    tab.value = 'records'
    void loadRecords().then(() => openRecord(records.value.find((r) => r.code_id === row.code_id) || row))
  }
}

function planName(k: string) { return names[k] || k }
function statusName(k: string) { return statusNames[k] || k }
function channelName(k: string) { return k === 'x_cdk' ? 'X CDK' : k === 'x_direct' ? 'X 直充' : k }
function batchStatus(k: string) {
  if (k === 'pending') return '待确认'
  if (k === 'failed') return '失败'
  return '已生成'
}
function usd(e4: number) {
  if (!e4) return '—'
  return '$' + (e4 / 10000).toFixed(2)
}
function shortCode(code: string) {
  const s = String(code || '')
  if (s.length <= 18) return s
  return s.slice(0, 10) + '…' + s.slice(-6)
}
function officialAmount(row: RecordRow) {
  if (!row.amount_minor) return '—'
  const cur = String(row.currency || '').toUpperCase()
  return `${Number(row.amount_minor).toLocaleString()} ${cur}`.trim()
}
function openRecord(row: RecordRow) {
  selected.value = row
  detailOpen.value = true
}
function accountName(id: number) {
  return accounts.value.find((a) => a.id === id)?.name || (id ? `账户 ${id}` : '未绑定卡台')
}
function channelLine(ch: any) {
  const who = accountName(ch.account_id)
  if (ch.channel === 'x_direct') return `${who} · 付款卡 ${ch.card_id || '未选'}`
  const how = ch.pay_mode === 'existing' || (!ch.pay_mode && !ch.auto_card && !ch.card_id)
    ? '已有卡自动选'
    : ch.pay_mode === 'new' || ch.auto_card
      ? '每笔开新卡'
      : ch.card_id
        ? '固定卡 ' + ch.card_id
        : '未选付款方式'
  return `${who} · ${how}`
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
    issuedMeta.value = {
      plan: currentSupply.value?.label || planName(issue.plan),
      source: sourceName(d.source || currentSupply.value?.source || ''),
      region: (d.source || currentSupply.value?.source) === 'spacex' ? issue.payment_country : '',
      at: new Date().toLocaleString(),
    }
    dialog.toast(`已生成 ${issuedCodes.value.length} 张`, 'ok')
    await loadBatches()
    await loadOverview()
    await loadList()
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
  loadingRecords.value = true
  try {
    const params = new URLSearchParams({ group: recGroup.value, q: recQ.value.trim() })
    const r = await authFetch(`/api/v1/admin/x/records?${params}`)
    const d = await r.json().catch(() => ({}))
    records.value = d.records || []
    recTotal.value = Number(d.total ?? records.value.length)
    recPage.value = 1
    if (selected.value) {
      selected.value = records.value.find((row) => row.code_id === selected.value?.code_id) || selected.value
    }
  } finally {
    loadingRecords.value = false
  }
}
async function requery(id: number) {
  const r = await authFetch(`/api/v1/admin/x/records/${id}/requery`, { method: 'POST' })
  const d = await r.json().catch(() => ({}))
  if (!r.ok) { dialog.toast(d.error || '查询失败', 'err'); return }
  dialog.toast('已重新查询', 'ok')
  await loadRecords()
  await loadList()
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
  await loadList()
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
  await loadList()
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
  // 后端这一列是整数：清空的格子当 0，非整数直接提示，不发请求。
  for (const row of limits.value) {
    const raw = String(row.max_official_amount_minor ?? '').trim()
    if (raw === '') {
      row.max_official_amount_minor = 0
      continue
    }
    if (!/^\d+$/.test(raw)) {
      dialog.toast(`${planName(row.plan)}的「官方金额上限」要填整数（最小单位，比如 BDT 300.00 填 30000）`, 'warn')
      return
    }
    row.max_official_amount_minor = Number(raw)
  }
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
  void loadList()
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
.mono { font-variant-numeric: tabular-nums; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
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
.code-cell:hover .code-cell__text { text-decoration: underline; text-underline-offset: 2px; }
.code-cell__text { font-size: 12px; line-height: 1.4; word-break: break-all; color: var(--good); }
.code-cell__meta { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; font-size: 11px; }
.note-cell__text {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  word-break: break-word;
  font-size: 12px;
  line-height: 1.35;
}
.note-cell__empty { font-size: 12px; color: var(--el-text-color-placeholder, #a8abb2); }
</style>
