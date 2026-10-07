// ───────── 后台：TG 会员（只走 Avanfinity CDK） ─────────
const TG_GROUPS = [['all', '全部'], ['todo', '待处理'], ['running', '进行中'], ['unused', '未兑'], ['done', '已开通'], ['void', '已作废']]

A.tgTab = (v) => { S.ui.tgTab = v }
A.tgGoTodo = () => { S.page = 'ops-tg'; S.ui.tgTab = 'records'; S.ui.tgGroup = 'todo' }
A.tgGoIssue = () => { S.page = 'ops-tg'; S.ui.tgTab = 'issue' }
A.tgGroup = (g) => { S.ui.tgGroup = g }
A.tgIssuePlan = (k) => { S.ui.tgIssue.plan = k }
A.tgQty = (n) => { S.ui.tgIssue.qty = Number(n) }
A.tgQtyStep = (d) => { S.ui.tgIssue.qty = Math.min(200, Math.max(1, Number(S.ui.tgIssue.qty) + Number(d))) }
A.tgListPage = (n) => { S.ui.tgList.page = Math.max(1, Number(n) || 1) }
A.tgRecPage = (n) => { S.ui.tgRecPage = Math.max(1, Number(n) || 1) }
A.tgListQuery = () => { S.ui.tgList.page = 1; toast('已查询') }
A.tgToggle = (id) => {
  const set = new Set(S.ui.tgSelIds)
  const n = Number(id)
  if (set.has(n)) set.delete(n); else set.add(n)
  S.ui.tgSelIds = [...set]
}
A.tgTogglePage = (ids) => {
  const pageIds = String(ids).split(',').filter(Boolean).map(Number)
  const all = pageIds.every((id) => S.ui.tgSelIds.includes(id))
  S.ui.tgSelIds = all ? S.ui.tgSelIds.filter((id) => !pageIds.includes(id)) : [...new Set([...S.ui.tgSelIds, ...pageIds])]
}
A.tgPlanOn = (k) => { tgPlan(k).on = !tgPlan(k).on }

A.tgIssue = () => {
  const f = S.ui.tgIssue, p = tgPlan(f.plan)
  if (!p.on) { toast('这个套餐已停售'); return }
  const pre = { premium_3m: 'P3M', premium_6m: 'P6M', premium_12m: 'P12' }[p.key]
  const n = Math.min(Math.max(Number(f.qty) || 1, 1), 200)
  const codes = Array.from({ length: n }, () => `DNT-${pre}-${rnd(4)}-AC`)
  codes.forEach((code, i) => S.tgCodes.unshift({
    id: 900 + S.tgCodes.length + i, code, plan: p.key, status: 'unused', group: 'unused',
    user: '', amt: '—', fee: '', note: f.note, at: now(), ev: [],
  }))
  S.tgBatches.unshift({ id: 20 + S.tgBatches.length, at: now(), plan: p.key, qty: n, used: 0, note: f.note })
  S.ui.tgIssued = codes
  plat('avan').tg.unused += n
  toast(`已生成 ${n} 张，供货 Avanfinity CDK`)
}

function tgRows() {
  const g = S.ui.tgGroup
  const q = (S.ui.tgQ || '').trim().toLowerCase()
  return S.tgCodes.filter((c) => (g === 'all' || c.group === g) && (!q || `${c.code} ${c.user}`.toLowerCase().includes(q)))
}

A.tgDetail = (id) => {
  const c = S.tgCodes.find((x) => x.id == id)
  if (!c) return
  const p = tgPlan(c.plan)
  drawer('兑换详情', `<div class="stack small">
      <div class="row">${c.group === 'done' ? tag('已开通', 'ok') : statusTag(c.status)}<span class="muted">TG CDK</span></div>
      <table class="t"><tr><td class="muted">卡密</td><td class="mono">${esc(c.code)}</td></tr>
        <tr><td class="muted">套餐</td><td>${esc(p.name)}</td></tr>
        <tr><td class="muted">开通给</td><td>${c.user ? '@' + esc(c.user) : '—'}</td></tr>
        <tr><td class="muted">官方金额</td><td>${esc(c.amt || '—')}</td></tr>
        <tr><td class="muted">参考美元</td><td>${c.usd ? usd(c.usd) : '—'}</td></tr>
        <tr><td class="muted">服务费</td><td>${c.fee || '—'}</td></tr>
        <tr><td class="muted">时间</td><td>${esc(c.at)}</td></tr></table>
      <b>处理过程</b>
      <div class="timeline">${c.ev.length ? c.ev.map((e) => `<div>${esc(e)}</div>`).join('') : '<div class="muted">还没有过程记录。</div>'}</div>
      <details><summary class="muted">排障信息</summary><div class="muted">供货 Avanfinity CDK · 发码不扣钱。确认后先注资，再派发 Telegram 付款。查询不会自动往下付钱。<br/>上游订单 up_${c.id}88 · 请求 req_${c.id}</div></details>
    </div>`,
    `<div class="row">${c.status === 'running' || c.status === 'todo' ? btn('重新查询', 'tgRefresh', '', 'sm') : ''}${c.group === 'todo' ? btn('人工处理', 'tgResolve', c.id + ':done', 'primary sm') : ''}${c.status === 'unused' ? btn('作废', 'tgVoid', c.id, 'danger sm') : ''}${btn('复制兑换链接', 'toastMsg', '已复制', 'sm')}</div>`)
  return 'keep'
}
A.tgRefresh = () => { toast('上游：付款中'); return 'keep' }
A.tgResolve = (arg) => {
  const [id, r] = String(arg).split(':')
  const c = S.tgCodes.find((x) => x.id == id)
  if (!c) return
  if (r === 'done') { c.status = 'done'; c.group = 'done' }
  else { c.status = 'unused'; c.group = 'unused'; c.user = '' }
  c.ev.push(now() + ' 人工处理：' + (r === 'done' ? '确认已开通' : '退回重提'))
  toast('已处理')
}
A.tgVoid = (id) => {
  const c = S.tgCodes.find((x) => x.id == id)
  if (!c || c.status !== 'unused') { toast('只有未使用的码可以作废'); return }
  c.status = 'void'; c.group = 'void'
  c.ev.push(now() + ' 已作废')
  toast('已作废')
}
A.tgFillCap = () => { S.tgPlans.forEach((p) => { p.cap = +(p.cost * 1.15).toFixed(2) }); toast('已填入') }

function tgMenu(label, items) {
  return `<details class="menu"><summary>${label}</summary><div class="pop">${items.map(([t, act, arg]) => `<button data-act="${act}" data-arg="${esc(arg)}">${t}</button>`).join('')}</div></details>`
}
function tgCardLine(p) {
  if (!p.cap) return `<button class="link" data-act="tgTab" data-arg="limits">还没填上限，去填</button>`
  return `每张最多 ${usd(p.cap)}`
}
function tgIssueTab() {
  const f = S.ui.tgIssue, p = tgPlan(f.plan)
  const q = (S.ui.tgList.q || '').trim().toLowerCase()
  const rows = S.tgCodes.filter((c) => (S.ui.tgList.group === 'all' || c.group === S.ui.tgList.group) && (!S.ui.tgList.plan || c.plan === S.ui.tgList.plan) && (!q || `${c.code} ${c.user} ${c.note || ''}`.toLowerCase().includes(q)))
  const page = S.ui.tgList.page || 1
  const pageRows = rows.slice((page - 1) * 20, page * 20)
  const ids = pageRows.map((r) => r.id).join(',')
  return `<div class="card stack">
    <div class="grid g3">${S.tgPlans.filter((x) => x.on).map((x) => `<div class="plan-card ${f.plan === x.key ? 'on' : ''}" data-act="tgIssuePlan" data-arg="${x.key}"><div class="top"><span class="n">${x.name}</span><span class="src-tag avan">Avanfinity</span></div><div class="s">${tgCardLine(x)}</div></div>`).join('')}</div>
    <p class="small muted">Avanfinity：发码不扣钱，兑换时从钱包出，客户只填 Telegram 用户名。上限在发码时锁死。套餐只有 Premium 3 / 6 / 12 个月。</p>
    <div class="row"><span class="small muted">数量</span>${btn('−', 'tgQtyStep', -1, 'sm')}${input('ui.tgIssue.qty', f.qty, '', '', 'number')}${btn('+', 'tgQtyStep', 1, 'sm')}${[1, 10, 50, 100, 200].map((n) => btn(n, 'tgQty', n, 'sm')).join('')}${input('ui.tgIssue.note', f.note, '备注，客服可搜')}${btn(`生成 ${f.qty} 张 ${p.name}`, 'tgIssue', '', 'primary')}</div>
    ${S.ui.tgIssued.length ? `<div class="card" style="background:var(--ok-soft)"><div class="row between"><b>本批 ${S.ui.tgIssued.length} 张</b><span class="row">${btn('复制', 'toastMsg', '已复制', 'sm')}${btn('复制链接', 'toastMsg', '已复制', 'sm')}${btn('导出', 'toastMsg', '已导出', 'sm')}</span></div><div class="mono small">${S.ui.tgIssued.slice(0, 5).join('<br/>')}</div></div>` : ''}</div>
    <div class="card" style="margin-top:12px">
      <b>CDK 列表</b>
      <p class="small muted">共 ${rows.length} 条 · 只列 Telegram 的 DNT- 码，不进「GPT 会员」页</p>
      <div class="row" style="margin:8px 0">${input('ui.tgList.q', S.ui.tgList.q, '搜索卡密 / 用户名 / 备注')}${select('ui.tgList.group', S.ui.tgList.group, TG_GROUPS)}${select('ui.tgList.plan', S.ui.tgList.plan, [['', '套餐'], ...S.tgPlans.map((x) => [x.key, x.name])])}${btn('查询', 'tgListQuery', '', 'primary sm')}${btn('刷新', 'toastMsg', '已刷新', 'sm')}<span style="flex:1"></span>${tgMenu('复制 / 导出', [['复制选中', 'toastMsg', '已复制'], ['导出当前列表', 'toastMsg', '已导出']])}${tgMenu('批量操作', [['复制兑换链接', 'toastMsg', '已复制'], ['批量作废未使用', 'toastMsg', '已作废未使用的码']])}</div>
      ${pageRows.length ? `<table class="t"><tr><th><button class="link" data-act="tgTogglePage" data-arg="${ids}">选</button></th><th>ID</th><th>卡密</th><th>套餐</th><th>状态</th><th>开通给</th><th>参考金额</th><th>服务费</th><th>备注</th><th>时间</th><th></th></tr>
        ${pageRows.map((r) => `<tr><td><button class="link" data-act="tgToggle" data-arg="${r.id}">${S.ui.tgSelIds.includes(r.id) ? '☑' : '☐'}</button></td><td>${r.id}</td><td class="mono small">${esc(r.code)}<div class="muted">完整 · ${r.code.length}字 · 点复制</div></td><td>${tgPlan(r.plan).name}</td><td>${statusTag(r.status)}</td><td>${r.user ? '@' + esc(r.user) : '—'}</td><td class="mono">${r.usd ? usd(r.usd) : '—'}</td><td>${r.fee || '—'}</td><td>${esc(r.note) || '—'}</td><td>${r.at}</td><td>${tgMenu('操作', [['复制卡密', 'toastMsg', '已复制'], ['复制兑换链接', 'toastMsg', '已复制'], ['看兑换记录', 'tgDetail', r.id], ...(r.status === 'unused' ? [['作废', 'tgVoid', r.id]] : [])])}</td></tr>`).join('')}</table>` : '<div class="empty">暂无数据</div>'}
      ${pagerBar(page, rows.length, 'tgListPage', 20)}
      <details style="margin-top:8px"><summary class="small muted">最近批次</summary>
        <table class="t"><tr><th>时间</th><th>套餐</th><th>已用/总数</th><th>备注</th><th></th></tr>
        ${S.tgBatches.map((b) => `<tr><td>${b.at}</td><td>${tgPlan(b.plan).name}</td><td>${b.used}/${b.qty}</td><td>${esc(b.note)}</td><td>${btn('导出', 'toastMsg', '已导出', 'sm')}</td></tr>`).join('')}</table></details>
    </div>`
}

function tgRecordsTab() {
  const rows = tgRows()
  const page = S.ui.tgRecPage || 1
  const pageRows = rows.slice((page - 1) * 20, page * 20)
  return `<div class="card"><div class="row"><span class="small muted">共 <b>${rows.length}</b> 笔</span>${select('ui.tgGroup', S.ui.tgGroup, TG_GROUPS)}${input('ui.tgQ', S.ui.tgQ, '卡密 / 用户名 / 备注')}${btn('查询', 'tgGroup', S.ui.tgGroup, 'primary sm')}</div></div>
    <div class="card" style="margin-top:12px;padding:0">${pageRows.length ? `<table class="t"><tr><th>记录</th><th>卡密</th><th>套餐</th><th>开通给</th><th>金额</th><th>状态</th><th>时间</th><th>操作</th></tr>
      ${pageRows.map((r) => `<tr><td>#${r.id}</td><td class="mono small">${esc(r.code)}<div class="muted">TG CDK</div></td><td>${tgPlan(r.plan).name}</td><td class="mono">${r.user ? '@' + esc(r.user) : '—'}</td><td class="mono">${esc(r.amt || '—')}${r.fee ? `<div class="small muted">费 ${esc(r.fee)}</div>` : ''}</td><td>${r.status === 'done' ? tag('完成', 'ok') : statusTag(r.status)}</td><td>${r.at}</td><td>${btn('详情', 'tgDetail', r.id, 'sm')}</td></tr>`).join('')}</table>` : '<div class="empty">暂无兑换记录</div>'}
    <div style="padding:0 12px 12px">${pagerBar(page, rows.length, 'tgRecPage', 20)}</div></div>`
}

function tgLimitsTab() {
  return `<div class="card"><div class="row between"><b>每单花费上限</b>${btn('按实测 +15% 填入', 'tgFillCap', '', 'sm')}</div>
    <p class="small muted">买 CDK 时把钱包可扣金额、官方应付、注资金额锁进这张码。兑换时超出或报价变了就拒绝，不扣钱。</p>
    <table class="t"><tr><th>套餐</th><th>实测成本</th><th>官方应付</th><th>上限（美元）</th></tr>
    ${S.tgPlans.map((p, i) => `<tr><td>${p.name}</td><td>${usd(p.cost)}</td><td>${p.official.toLocaleString('en-US')} ${p.currency.toUpperCase()}</td><td>${input(`tgPlans.${i}.cap`, p.cap, '', '', 'number')}</td></tr>`).join('')}</table></div>
    <div class="card" style="margin-top:12px"><b>告警</b><div class="grid g2" style="margin-top:8px">
      <div><label class="f">钱包低于（美元）</label>${input('tgAlerts.wallet', S.tgAlerts.wallet, '', 'w-full', 'number')}</div>
      <div><label class="f">单子卡住超过（分钟）</label>${input('tgAlerts.stuck', S.tgAlerts.stuck, '', 'w-full', 'number')}</div></div></div>`
}

page('ops-tg', {
  group: 'ops', title: 'TG 会员', url: () => '/ops/tg?tab=' + S.ui.tgTab,
  render: () => {
    const av = plat('avan')
    const bad = av.conns.some((c) => c.error)
    const today = S.tgCodes.filter((c) => c.group === 'done').length
    const running = S.tgCodes.filter((c) => c.group === 'running').length
    const todo = S.tgCodes.filter((c) => c.group === 'todo').length
    const unused = S.tgCodes.filter((c) => c.status === 'unused').length
    const n = S.tgPlans.filter((p) => p.on).length
    return `<div class="page-head"><div><h2>TG 会员</h2><p>只走 Avanfinity CDK，客户填 Telegram 用户名。不卖直充，也没有第二家卡台。</p></div>${btn('刷新', 'toastMsg', '已刷新')}</div>
    <div class="grid g2" style="margin-bottom:8px">
      <div class="card stack"><div class="row">${dot(bad ? 'warn' : 'ok')}<b>Avanfinity TG</b>${tag('已启用', 'ok')}${tag(`供 ${n} 个套餐`, 'ok')}</div>
        <p class="small">avanfinity · TG</p>
        <p class="small muted">钱包 ${usd(av.tg.wallet)} · 未兑 ${av.tg.unused} 张 · 客户兑换时填 Telegram 用户名</p>
        ${bad ? `<p class="small" style="color:var(--warn)">${esc(av.conns.find((c) => c.error)?.error || '')}</p>` : ''}
        <div>${btn('在卡台查看', 'openPlat', 'avan:creds', 'sm')}</div></div>
    </div>
    <div class="statline">今日开通 ${today} · 进行中 ${running} · 待处理 ${todo} · 未兑 ${unused}</div>
    <div class="row" style="margin-bottom:12px">${seg([['issue', '发码'], ['records', '兑换记录'], ['limits', '上限与告警']], S.ui.tgTab, 'tgTab')}</div>
    ${{ issue: tgIssueTab, records: tgRecordsTab, limits: tgLimitsTab }[S.ui.tgTab]()}`
  },
  notes: () => ({
    issue: [
      '版式和 X 会员发码页同一套：套餐卡片、数量条、CDK 列表、右侧详情抽屉。没有供货设置，因为只有 Avanfinity。',
      '套餐只有 Premium 3 / 6 / 12 个月。发码不扣钱，上限在发码时锁死。',
      'DNT- 码只出现在这一页，不进「GPT 会员」。',
    ],
    records: [
      '兑换记录和 X 一样：状态下拉默认全部，点详情打开右侧抽屉。',
      '作废只针对未使用的码。超时不确定的单子留在待处理，人工确认或退回重提。',
    ],
    limits: [
      '兑换时必须原样带回报价里的金额和币种。对不上就拒绝（报价已变或已过期），避免按错价付款。',
      '结果查询是只读的，不会把「注资中」自动推进成付款。',
    ],
  })[S.ui.tgTab],
})
