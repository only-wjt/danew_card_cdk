// ───────── 后台：X 会员 ─────────
// Avan 直充不再提供（后端保留、界面隐藏），X 只在 SpaceX 和 Avan CDK 之间二选一
const SRC_KEYS = ['spacex', 'avan_cdk']
const srcLabel = (k) => S.xSources[k].label
const srcState = (k) => S.xSources[k].state
const srcPrefix = (k) => (k === 'spacex' ? 'SX' : 'AC')

function xAffected() { return S.xPlans.filter((p) => p.on && srcState(p.source) === 'err') }
function cheapestOk(p, exclude) {
  return SRC_KEYS.filter((k) => k !== exclude && p.cost[k] != null && srcState(k) !== 'err').sort((a, b) => p.cost[a] - p.cost[b])[0]
}

A.xTab = (v) => { S.ui.xTab = v }
A.xGoTodo = () => { S.page = 'ops-x'; S.ui.xTab = 'records'; S.ui.xGroup = 'todo'; S.ui.xRecPage = 1 }
A.xGoIssue = () => { S.page = 'ops-x'; S.ui.xTab = 'issue' }
A.xGoSupply = () => { S.page = 'ops-x'; S.ui.xTab = 'supply' }

A.xSetSource = (arg) => {
  const [key, src] = arg.split(':'); const p = xPlan(key)
  if (p.source === src) return
  const old = p.source; p.source = src
  S.audit.unshift({ at: now(), who: 'admin', act: `X 供货 · ${p.name}：${srcLabel(old)} → ${srcLabel(src)}`, ip: '198.51.100.7' })
  toast(`${p.name} 之后新发的码走 ${srcLabel(src)}，已发的不受影响`)
}
A.xPlanOn = (key) => { const p = xPlan(key); p.on = !p.on }
A.xAllTo = (src) => {
  const ok = S.xPlans.filter((p) => p.cost[src] != null)
  const skip = S.xPlans.filter((p) => p.cost[src] == null).map((p) => p.name)
  modal(`全部切到 ${srcLabel(src)}？`, `<p>会改 ${ok.length} 个套餐的供货卡台，只影响之后新发的码。</p>${skip.length ? `<p class="small muted">${srcLabel(src)} 不卖：${skip.join('、')}，这几个保持不变。</p>` : ''}`,
    btn('取消', 'closeModal') + btn('确认切换', 'xAllToDo', src, 'primary'))
  return 'keep'
}
A.xAllToDo = (src) => { S.xPlans.forEach((p) => { if (p.cost[src] != null) p.source = src }); toast('已切换') }
A.xFixAffected = () => {
  const list = xAffected().map((p) => [p, cheapestOk(p, p.source)])
  modal('把受影响的套餐切走', `<table class="t"><tr><th>套餐</th><th>现在</th><th>改成</th></tr>${list.map(([p, to]) => `<tr><td>${p.name}</td><td>${srcLabel(p.source)}</td><td>${to ? srcLabel(to) + ' · ' + usd(p.cost[to]) : '<span style="color:var(--err)">没有可用卡台，先停售</span>'}</td></tr>`).join('')}</table>
    <p class="small muted">按最近成本挑最便宜的可用来源。已发的码仍在原卡台兑换，原卡台修好前这些码会排队。</p>`,
    btn('取消', 'closeModal') + btn('确认', 'xFixDo', '', 'primary'))
  return 'keep'
}
A.xFixDo = () => { xAffected().forEach((p) => { const to = cheapestOk(p, p.source); if (to) p.source = to; else p.on = false }); toast('已切换') }

A.xIssuePlan = (k) => { S.ui.xIssue.plan = k }
A.xQty = (n) => { S.ui.xIssue.qty = Number(n) }
A.xQtyStep = (d) => { S.ui.xIssue.qty = Math.min(200, Math.max(1, Number(S.ui.xIssue.qty) + Number(d))) }
A.xIssue = () => {
  const f = S.ui.xIssue, p = xPlan(f.plan)
  if (!p.on) { toast('这个套餐已停售'); return }
  if (srcState(p.source) === 'err') { toast(`${srcLabel(p.source)} 现在不可用，先在「供货设置」切走`); return }
  const n = Math.min(Math.max(Number(f.qty) || 1, 1), 200)
  const codes = Array.from({ length: n }, () => `DNX-${rnd(8)}-${rnd(8)}-${rnd(6)}`)
  codes.forEach((code, i) => S.xCodes.unshift({ id: 600 + S.xCodes.length + i, code, plan: p.key, source: p.source, status: 'unused', group: 'unused', user: '', msg: '未兑换', official: '—', usd: 0, fee: '', note: f.note, at: now(), ev: [] }))
  S.xBatches.unshift({ id: 32 + S.xBatches.length, at: now(), plan: p.key, source: p.source, qty: n, used: 0, note: f.note })
  S.ui.xIssued = codes
  toast(`已生成 ${n} 张，供货 ${srcLabel(p.source)}`)
}
A.xGroup = (g) => { S.ui.xGroup = g; S.ui.xRecPage = 1 }
A.xRecPage = (n) => { S.ui.xRecPage = Math.max(1, Number(n) || 1) }
A.xListPage = (n) => { S.ui.xList.page = Math.max(1, Number(n) || 1) }
A.xListQuery = () => { S.ui.xList.page = 1; toast('已查询') }
A.xToggle = (id) => {
  const set = new Set(S.ui.xSelIds)
  const n = Number(id)
  if (set.has(n)) set.delete(n); else set.add(n)
  S.ui.xSelIds = [...set]
}
A.xTogglePage = (ids) => {
  const pageIds = String(ids).split(',').filter(Boolean).map(Number)
  const all = pageIds.every((id) => S.ui.xSelIds.includes(id))
  S.ui.xSelIds = all ? S.ui.xSelIds.filter((id) => !pageIds.includes(id)) : [...new Set([...S.ui.xSelIds, ...pageIds])]
}
A.xResolve = (arg) => {
  const [id, r] = arg.split(':'); const c = S.xCodes.find((x) => x.id == id)
  if (r === 'done') { c.status = 'done'; c.group = 'done'; c.msg = '人工确认已开通' }
  else if (r === 'retry') { c.status = 'unused'; c.group = 'unused'; c.msg = '已退回，客户可重新提交' }
  c.ev.push(now() + ' 人工处理：' + (r === 'done' ? '确认已开通' : '退回重提'))
  toast('已处理')
}
A.xVoid = (id) => { const c = S.xCodes.find((x) => x.id == id); c.status = 'void'; c.group = 'void'; c.msg = '已作废'; toast('已作废') }

const X_GROUPS = [['all', '全部'], ['todo', '待处理'], ['running', '进行中'], ['done', '已完成'], ['unused', '未使用'], ['failed', '失败已退回']]
const srcTag = (k) => `<span class="src-tag ${k === 'spacex' ? 'spacex' : 'avan'}">${k === 'spacex' ? 'SpaceX' : 'Avanfinity'}</span>`
const xChannel = (c) => (c.source === 'spacex' ? 'SpaceX' : 'X CDK')
function xCardLine(p) {
  if (p.source === 'spacex') return '兑换时客户填 X Cookie'
  if (!p.cap) return `<button class="link" data-act="xTab" data-arg="limits">还没填上限，去填</button>`
  return `每张最多 ${usd(p.cap)}`
}
function xMatch(c, group, q, plan) {
  const text = `${c.code} ${c.user} ${c.note || ''}`.toLowerCase()
  return (group === 'all' || c.group === group) && (!plan || c.plan === plan) && (!q || text.includes(q))
}

function xSupplyTab() {
  const aff = xAffected()
  return `${aff.length ? `<div class="alert err"><div class="row"><span><b>${srcLabel(aff[0].source)} 不可用</b>：${esc(plat('avan').conns[1].error)}。受影响 ${aff.length} 个套餐，新发的码会兑换失败。</span>
      <span class="row">${btn('把受影响的套餐切走', 'xFixAffected', '', 'warn sm')}${btn('去卡台修', 'openPlat', 'avan:creds', 'sm')}</span></div></div>` : ''}
    <div class="card"><div class="row between"><div><b>供货设置</b><div class="small muted">每个套餐选一家卡台。只影响之后新发的码，已发的码在原卡台兑换。</div></div>
      <div class="row">${btn('全部切到 SpaceX', 'xAllTo', 'spacex', 'sm')}${btn('全部切到 Avanfinity', 'xAllTo', 'avan_cdk', 'sm')}</div></div>
    <table class="t" style="margin-top:10px"><tr><th>在售</th><th>套餐</th>${SRC_KEYS.map((k) => `<th>${dot(srcState(k))} ${srcLabel(k)}<div class="small muted" style="font-weight:400">${S.xSources[k].sub}</div></th>`).join('')}<th>未兑</th></tr>
    ${S.xPlans.map((p) => `<tr><td>${sw(p.on, 'xPlanOn', p.key)}</td><td><b>${p.name}</b></td>
      ${SRC_KEYS.map((k) => p.cost[k] == null ? `<td class="muted small">不卖</td>` : `<td><button class="radio ${p.source === k ? 'on' : ''}" data-act="xSetSource" data-arg="${p.key}:${k}" ${p.on ? '' : 'disabled'}>${p.source === k ? '●' : '○'} ${usd(p.cost[k])}${cheapestOk(p) === k ? ' <span class="small">最低</span>' : ''}</button></td>`).join('')}
      <td>${S.xCodes.filter((c) => c.plan === p.key && c.status === 'unused').length}</td></tr>`).join('')}</table>
    <p class="small muted" style="margin-top:8px">金额是最近一次实测成本（含服务费）。点「卡台」页的「测试连接」会刷新。</p></div>`
}

function xMenu(label, items) {
  return `<details class="menu"><summary>${label}</summary><div class="pop">${items.map(([t, act, arg]) => `<button data-act="${act}" data-arg="${esc(arg)}">${t}</button>`).join('')}</div></details>`
}
function xIssueTab() {
  const f = S.ui.xIssue, p = xPlan(f.plan), bad = srcState(p.source) === 'err'
  const q = (S.ui.xList.q || '').trim().toLowerCase()
  const rows = S.xCodes.filter((c) => c.source === 'avan_cdk' && xMatch(c, S.ui.xList.group, q, S.ui.xList.plan))
  const page = S.ui.xList.page || 1
  const pageRows = rows.slice((page - 1) * 20, page * 20)
  const ids = pageRows.map((r) => r.id).join(',')
  return `<div class="card stack">
    <div class="grid g3">${S.xPlans.filter((x) => x.on).map((x) => `<div class="plan-card ${f.plan === x.key ? 'on' : ''}" data-act="xIssuePlan" data-arg="${x.key}"><div class="top"><span class="n">${x.name}</span>${srcTag(x.source)}</div><div class="s">${xCardLine(x)}</div></div>`).join('')}</div>
    <p class="small muted">${p.source === 'spacex' ? 'SpaceX：发码时锁定付款地区，客户兑换时要填 X 的 Cookie（auth_token / ct0）。' : 'Avanfinity：发码不扣钱，兑换时从钱包出，客户只填 X 用户名。上限在发码时锁死。'}</p>
    <div class="row"><span class="small muted">数量</span>${btn('−', 'xQtyStep', -1, 'sm')}${input('ui.xIssue.qty', f.qty, '', '', 'number')}${btn('+', 'xQtyStep', 1, 'sm')}${[1, 10, 50, 100, 200].map((n) => btn(n, 'xQty', n, 'sm')).join('')}${p.source === 'spacex' ? `<span class="small muted">付款地区</span>${select('ui.xIssue.region', f.region, ['日本', '美国', '菲律宾', '土耳其'].map((r) => [r, r]))}` : ''}${input('ui.xIssue.note', f.note, '备注，客服可搜')}${btn(`生成 ${f.qty} 张 ${p.name}`, 'xIssue', '', 'primary', bad ? 'disabled' : '')}</div>
    ${bad ? `<div class="alert err small">这个套餐的供货卡台现在不可用，先去「供货设置」切走。</div>` : ''}
    ${S.ui.xIssued.length ? `<div class="card" style="background:var(--ok-soft)"><div class="row between"><b>本批 ${S.ui.xIssued.length} 张</b><span class="row">${btn('复制', 'toastMsg', '已复制', 'sm')}${btn('复制链接', 'toastMsg', '已复制', 'sm')}${btn('导出', 'toastMsg', '已导出', 'sm')}</span></div><div class="mono small">${S.ui.xIssued.slice(0, 5).join('<br/>')}</div></div>` : ''}</div>
    <div class="card" style="margin-top:12px">
      <b>CDK 列表</b>
      <p class="small muted">共 ${rows.length} 条 · 只列 Avanfinity 出的码（DNX-），SpaceX 出的码在「GPT 会员」页</p>
      <div class="row" style="margin:8px 0">${input('ui.xList.q', S.ui.xList.q, '搜索卡密 / 用户名 / 备注')}${select('ui.xList.group', S.ui.xList.group, X_GROUPS)}${select('ui.xList.plan', S.ui.xList.plan, [['', '套餐'], ...S.xPlans.map((x) => [x.key, x.name])])}${btn('查询', 'xListQuery', '', 'primary sm')}${btn('刷新', 'toastMsg', '已刷新', 'sm')}<span style="flex:1"></span>${xMenu('复制 / 导出', [['复制选中', 'toastMsg', '已复制'], ['导出当前列表', 'toastMsg', '已导出']])}${xMenu('批量操作', [['复制兑换链接', 'toastMsg', '已复制'], ['批量作废未使用', 'toastMsg', '已作废未使用的码']])}</div>
      ${pageRows.length ? `<table class="t"><tr><th><button class="link" data-act="xTogglePage" data-arg="${ids}">选</button></th><th>ID</th><th>卡密</th><th>套餐</th><th>状态</th><th>开通给</th><th>参考金额</th><th>服务费</th><th>备注</th><th>时间</th><th></th></tr>
        ${pageRows.map((r) => `<tr><td><button class="link" data-act="xToggle" data-arg="${r.id}">${S.ui.xSelIds.includes(r.id) ? '☑' : '☐'}</button></td><td>${r.id}</td><td class="mono small">${esc(r.code)}<div class="muted">完整 · ${r.code.length}字 · 点复制</div></td><td>${xPlan(r.plan).name}</td><td>${statusTag(r.status)}</td><td>${r.user ? '@' + esc(r.user) : '—'}</td><td class="mono">${r.usd ? usd(r.usd) : '—'}</td><td>${r.fee || '—'}</td><td>${esc(r.note) || '—'}</td><td>${r.at}</td><td>${xMenu('操作', [['复制卡密', 'toastMsg', '已复制'], ['复制兑换链接', 'toastMsg', '已复制'], ['看兑换记录', 'xDetail', r.id], ...(r.status === 'unused' ? [['作废', 'xVoid', r.id]] : [])])}</td></tr>`).join('')}</table>` : '<div class="empty">暂无数据</div>'}
      ${pagerBar(page, rows.length, 'xListPage', 20)}
      <details style="margin-top:8px"><summary class="small muted">最近批次</summary>
        <table class="t"><tr><th>时间</th><th>套餐</th><th>供货</th><th>已用/总数</th><th>备注</th><th></th></tr>
        ${S.xBatches.map((b) => `<tr><td>${b.at}</td><td>${xPlan(b.plan).name}</td><td>${srcLabel(b.source)}</td><td>${b.used}/${b.qty}</td><td>${esc(b.note)}</td><td>${btn('导出', 'toastMsg', '已导出', 'sm')}</td></tr>`).join('')}</table></details>
    </div>`
}

A.xDetail = (id) => {
  const c = S.xCodes.find((x) => x.id == id)
  if (!c) return
  const p = xPlan(c.plan)
  drawer('兑换详情', `<div class="stack small">
      <div class="row">${c.group === 'done' ? tag('已开通', 'ok') : statusTag(c.status)}<span class="muted">${xChannel(c)}</span></div>
      ${c.msg ? `<div class="alert small">${esc(c.msg)}</div>` : ''}
      <table class="t"><tr><td class="muted">卡密</td><td class="mono">${esc(c.code)}</td></tr>
        <tr><td class="muted">套餐</td><td>${esc(p.name)}</td></tr>
        <tr><td class="muted">开通给</td><td>${c.user ? '@' + esc(c.user) : '—'}</td></tr>
        <tr><td class="muted">官方金额</td><td>${esc(c.official || '—')}</td></tr>
        <tr><td class="muted">参考美元</td><td>${c.usd ? usd(c.usd) : '—'}</td></tr>
        <tr><td class="muted">服务费</td><td>${c.fee || '—'}</td></tr>
        ${c.note ? `<tr><td class="muted">备注</td><td>${esc(c.note)}</td></tr>` : ''}
        <tr><td class="muted">时间</td><td>${esc(c.at)}</td></tr></table>
      <b>处理过程</b>
      <div class="timeline">${c.ev.length ? c.ev.map((e) => `<div>${esc(e)}</div>`).join('') : '<div class="muted">还没有过程记录。</div>'}</div>
      <details><summary class="muted">排障信息</summary><div class="muted">卡台 ${plat(S.xSources[c.source].platform).name} · 上游订单 up_${c.id}88 · 请求 req_${c.id}<br/>注资 ${c.status === 'unused' ? '未发出' : '已发出'} · 付款 ${c.status === 'done' ? '已发出' : '未发出'}</div></details>
    </div>`,
    `<div class="row">${c.status === 'running' || c.status === 'todo' ? btn('重新查询', 'toastMsg', '上游：付款中', 'sm') : ''}${c.group === 'todo' ? btn('人工处理', 'xResolve', c.id + ':done', 'primary sm') : ''}${c.status === 'unused' ? btn('作废', 'xVoid', c.id, 'danger sm') : ''}${btn('复制兑换链接', 'toastMsg', '已复制', 'sm')}</div>`)
  return 'keep'
}

function xRecordsTab() {
  const q = (S.ui.xQ || '').trim().toLowerCase()
  const rows = S.xCodes.filter((c) => xMatch(c, S.ui.xGroup, q, ''))
  const page = S.ui.xRecPage || 1
  const pageRows = rows.slice((page - 1) * 20, page * 20)
  return `<div class="card"><div class="row"><span class="small muted">共 <b>${rows.length}</b> 笔</span>${select('ui.xGroup', S.ui.xGroup, X_GROUPS)}${input('ui.xQ', S.ui.xQ, '卡密 / 用户名 / 备注')}${btn('查询', 'xGroup', S.ui.xGroup, 'primary sm')}</div></div>
    <div class="card" style="margin-top:12px;padding:0">${pageRows.length ? `<table class="t"><tr><th>记录</th><th>卡密</th><th>套餐</th><th>开通给</th><th>金额</th><th>状态</th><th>时间</th><th>操作</th></tr>
      ${pageRows.map((r) => `<tr><td>#${r.id}</td><td class="mono small">${esc(r.code)}<div class="muted">${xChannel(r)}</div></td><td>${xPlan(r.plan).name}</td><td class="mono">${r.user ? '@' + esc(r.user) : '—'}</td><td class="mono">${esc(r.official || (r.usd ? usd(r.usd) : '—'))}${r.fee ? `<div class="small muted">费 ${esc(r.fee)}</div>` : ''}</td><td>${r.status === 'done' ? tag('完成', 'ok') : statusTag(r.status)}</td><td>${r.at}</td><td>${btn('详情', 'xDetail', r.id, 'sm')}</td></tr>`).join('')}</table>` : '<div class="empty">暂无兑换记录</div>'}
    <div style="padding:0 12px 12px">${pagerBar(page, rows.length, 'xRecPage', 20)}</div></div>`
}

function xLimitsTab() {
  return `<div class="card"><div class="row between"><b>每单花费上限</b>${btn('按实测 +15% 填入', 'xFillCap', '', 'sm')}</div>
    <p class="small muted">买 CDK 或下单前，成本超过上限就不下单，不扣钱。按套餐设一个上限，不管用哪家卡台。</p>
    <table class="t"><tr><th>套餐</th><th>当前供货</th><th>实测成本</th><th>上限（美元）</th></tr>
    ${S.xPlans.map((p, i) => `<tr><td>${p.name}</td><td>${srcLabel(p.source)}</td><td>${usd(p.cost[p.source])}</td><td>${input(`xPlans.${i}.cap`, p.cap, '', '', 'number')}</td></tr>`).join('')}</table></div>
    <div class="card" style="margin-top:12px"><b>告警</b><div class="grid g3" style="margin-top:8px">
      <div><label class="f">钱包低于（美元）</label>${input('xAlerts.wallet', S.xAlerts.wallet, '', 'w-full', 'number')}</div>
      <div><label class="f">Avan 未兑 CDK 低于（张）</label>${input('xAlerts.card', S.xAlerts.card, '', 'w-full', 'number')}</div>
      <div><label class="f">单子卡住超过（分钟）</label>${input('xAlerts.stuck', S.xAlerts.stuck, '', 'w-full', 'number')}</div></div></div>`
}
A.xFillCap = () => { S.xPlans.forEach((p) => { if (p.cost[p.source] != null) p.cap = +(p.cost[p.source] * 1.15).toFixed(2) }); toast('已填入') }

page('ops-x', {
  group: 'ops', title: 'X 会员', url: () => '/ops/x?tab=' + S.ui.xTab,
  render: () => {
    const sx = plat('spacex'), av = plat('avan')
    const nSx = S.xPlans.filter((p) => p.on && p.source === 'spacex').length
    const nAv = S.xPlans.filter((p) => p.on && p.source === 'avan_cdk').length
    const today = S.xCodes.filter((c) => c.group === 'done').length
    const running = S.xCodes.filter((c) => c.group === 'running').length
    const todo = S.xCodes.filter((c) => c.group === 'todo').length
    const unused = S.xCodes.filter((c) => c.status === 'unused').length
    const strip = (name, d, tags, lines, act) => `<div class="card stack"><div class="row">${dot(d)}<b>${name}</b>${tags}</div>${lines}<div>${btn('在卡台查看', 'openPlat', act, 'sm')}</div></div>`
    return `<div class="page-head"><div><h2>X 会员</h2><p>每个套餐只从一家卡台出码，在「供货设置」里二选一。卡台凭证在「卡台」页改。</p></div>${btn('刷新', 'toastMsg', '已刷新')}</div>
    <div class="grid g2" style="margin-bottom:8px">
      ${strip('SpaceX', srcState('spacex'), tag(nSx ? `供 ${nSx} 个套餐` : '没有套餐走这里', nSx ? 'ok' : ''), `<p class="small">主台 A（现网）（GPT 主台，同一套凭证）</p><p class="small muted">发码时锁定付款地区，客户兑换时填 X Cookie。</p>`, 'spacex:x')}
      ${strip('Avanfinity X', srcState('avan_cdk') === 'err' ? 'warn' : 'ok', tag('已启用', 'ok') + tag(nAv ? `供 ${nAv} 个套餐` : '没有套餐走这里', nAv ? 'ok' : ''), `<p class="small">avanfinity · X</p><p class="small muted">钱包 ${usd(av.x.wallet)} · 未兑负债 ${usd(av.x.liability)} · ${av.x.unusedCdk} 张未兑</p>${srcState('avan_cdk') === 'err' ? `<p class="small" style="color:var(--warn)">${esc(plat('avan').conns[1].error)}</p>` : ''}`, 'avan:x')}
    </div>
    <div class="statline">今日开通 ${today} · 进行中 ${running} · 待处理 ${todo} · 未兑 ${unused}</div>
    <div class="row" style="margin-bottom:12px">${seg([['issue', '发码'], ['records', '兑换记录'], ['supply', '供货设置'], ['limits', '上限与告警']], S.ui.xTab, 'xTab')}</div>
    ${{ supply: xSupplyTab, issue: xIssueTab, records: xRecordsTab, limits: xLimitsTab }[S.ui.xTab]()}`
  },
  notes: () => ({
    supply: [
      '供货设置仍是每个套餐二选一。发码页不再选卡台，角标直接显示这套餐现在走 SpaceX 还是 Avanfinity。',
      '切换只影响之后新发的码。SpaceX 出的码在「GPT 会员」页，Avanfinity 的 DNX- 留在本页 CDK 列表。',
    ],
    issue: [
      '发码页和现网一致：套餐卡片右上角是供货来源，下面一行是客户要填什么，或「还没填上限，去填」。',
      '数量用加减和 1 / 10 / 50 / 100 / 200。走 SpaceX 时多一个付款地区。按钮文案是「生成 N 张 套餐名」。',
      'CDK 列表只列 Avanfinity 的码。点「详情」或操作里的「看兑换记录」打开右侧抽屉，时间线不铺在表格上。',
    ],
    records: [
      '兑换记录默认「全部」，用下拉筛选状态。表格列是记录、卡密、套餐、开通给、金额、状态、时间、详情。',
      '详情是右侧抽屉：官方金额、参考美元、服务费、处理过程、重新查询 / 人工处理 / 复制链接、排障信息。',
    ],
    limits: ['没填上限的 Avanfinity 套餐，发码卡片上会提示去这里填。已经发出去的码按发码当时的上限执行。'],
  })[S.ui.xTab],
})
