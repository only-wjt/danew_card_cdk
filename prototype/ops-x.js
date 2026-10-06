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
A.xGoTodo = () => { S.page = 'ops-x'; S.ui.xTab = 'records'; S.ui.xGroup = 'todo'; S.ui.xSel = S.xCodes.find((c) => c.group === 'todo')?.id ?? null }
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
A.xIssue = () => {
  const f = S.ui.xIssue, p = xPlan(f.plan)
  if (srcState(p.source) === 'err') { toast(`${srcLabel(p.source)} 现在不可用，先在「供货设置」切走`); return }
  const n = Math.min(f.qty, 50)
  const codes = Array.from({ length: n }, () => `DNX-${p.key.split('_')[1].slice(0, 1).toUpperCase()}${p.key.split('_')[2].toUpperCase()}-${rnd(4)}-${srcPrefix(p.source)}`)
  codes.forEach((code, i) => S.xCodes.unshift({ id: 600 + S.xCodes.length + i, code, plan: p.key, source: p.source, status: 'unused', group: 'unused', user: '', msg: '未兑换', usd: 0, at: now(), ev: [] }))
  S.xBatches.unshift({ id: 32 + S.xBatches.length, at: now(), plan: p.key, source: p.source, qty: n, used: 0, note: f.note })
  S.ui.xIssued = codes
  toast(`已生成 ${n} 张，供货 ${srcLabel(p.source)}`)
}
A.xGroup = (g) => { S.ui.xGroup = g; S.ui.xSel = S.xCodes.find((c) => c.group === g)?.id ?? null }
A.xSel = (id) => { S.ui.xSel = Number(id) }
A.xResolve = (arg) => {
  const [id, r] = arg.split(':'); const c = S.xCodes.find((x) => x.id == id)
  if (r === 'done') { c.status = 'done'; c.group = 'done'; c.msg = '人工确认已开通' }
  else if (r === 'retry') { c.status = 'unused'; c.group = 'unused'; c.msg = '已退回，客户可重新提交' }
  c.ev.push(now() + ' 人工处理：' + (r === 'done' ? '确认已开通' : '退回重提'))
  toast('已处理')
}
A.xVoid = (id) => { const c = S.xCodes.find((x) => x.id == id); c.status = 'void'; c.group = 'void'; c.msg = '已作废'; toast('已作废') }

const X_GROUPS = [['todo', '待处理'], ['running', '进行中'], ['failed', '失败'], ['unused', '未兑'], ['done', '已开通']]

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

function xIssueTab() {
  const f = S.ui.xIssue, p = xPlan(f.plan), bad = srcState(p.source) === 'err'
  return `<div class="card stack"><b>发 X 卡密</b>
    <div class="grid g3">${S.xPlans.filter((x) => x.on).map((x) => `<button class="plan-card ${f.plan === x.key ? 'on' : ''}" data-act="xIssuePlan" data-arg="${x.key}"><div class="n">${x.name}</div><div class="s">${dot(srcState(x.source))} 供货 ${srcLabel(x.source)} · 约 ${usd(x.cost[x.source])}</div></button>`).join('')}</div>
    <div class="row">${[1, 10, 50].map((n) => btn(n, 'xQty', n, 'sm')).join('')}${input('ui.xIssue.qty', f.qty, '', '', 'number')}${input('ui.xIssue.note', f.note, '备注，客服可搜')}${btn('生成', 'xIssue', '', 'primary', bad ? 'disabled' : '')}</div>
    <p class="small">${f.qty} 张 ${p.name} · 供货 <b>${srcLabel(p.source)}</b> · 客户兑换时${S.xSources[p.source].sub.replace('客户', '')} ${btn('改供货', 'xTab', 'supply', 'sm')}</p>
    ${bad ? `<div class="alert err small">这个套餐的供货卡台现在不可用，先去「供货设置」切走。</div>` : ''}
    ${S.ui.xIssued.length ? `<div class="card" style="background:var(--surface-2)"><div class="row between"><b>刚生成 ${S.ui.xIssued.length} 张</b><span class="row">${btn('复制兑换链接', 'toastMsg', '已复制', 'sm')}${btn('复制卡密', 'toastMsg', '已复制', 'sm')}</span></div><div class="mono small">${S.ui.xIssued.slice(0, 5).join('<br/>')}</div></div>` : ''}</div>
    <div class="card" style="margin-top:12px"><b>最近批次</b><table class="t" style="margin-top:8px"><tr><th>时间</th><th>套餐</th><th>供货</th><th>已用/总数</th><th>备注</th><th></th></tr>
    ${S.xBatches.map((b) => `<tr><td>${b.at}</td><td>${xPlan(b.plan).name}</td><td>${srcLabel(b.source)}</td><td>${b.used}/${b.qty}</td><td>${esc(b.note)}</td><td>${btn('导出', 'toastMsg', '已导出', 'sm')}</td></tr>`).join('')}</table></div>`
}
A.xQty = (n) => { S.ui.xIssue.qty = Number(n) }

function xRecordsTab() {
  const rows = S.xCodes.filter((c) => c.group === S.ui.xGroup)
  const c = S.xCodes.find((x) => x.id === S.ui.xSel && x.group === S.ui.xGroup)
  return `<div class="row" style="margin-bottom:10px">${X_GROUPS.map(([g, l]) => btn(`${l} ${S.xCodes.filter((x) => x.group === g).length}`, 'xGroup', g, S.ui.xGroup === g ? 'primary sm' : 'sm')).join('')}${input('ui.xQ', S.ui.xQ, '搜卡密 / 用户名 / 备注')}</div>
    <div class="grid" style="grid-template-columns:minmax(0,1fr) 340px">
    <div class="card">${rows.length ? `<table class="t"><tr><th>卡密</th><th>套餐</th><th>供货</th><th>开通给</th><th>说明</th><th>时间</th></tr>
      ${rows.map((r) => `<tr class="click ${c?.id === r.id ? 'sel' : ''}" data-act="xSel" data-arg="${r.id}"><td class="mono">${r.code}</td><td>${xPlan(r.plan).name}</td><td>${srcLabel(r.source)}</td><td>${r.user ? '@' + esc(r.user) : '—'}</td><td class="small">${esc(r.msg)}</td><td>${r.at}</td></tr>`).join('')}</table>` : '<div class="empty">这一组是空的</div>'}</div>
    ${c ? `<div class="card stack small"><div class="row between"><b>${statusTag(c.status)} ${c.code}</b></div>
      ${c.group === 'todo' ? `<div class="alert small">${esc(c.msg)}</div>` : ''}
      <div>套餐 ${xPlan(c.plan).name} · 供货 ${srcLabel(c.source)}</div><div>开通给 ${c.user ? '@' + esc(c.user) : '—'} · 花费 ${usd(c.usd)}</div>
      <b>处理过程</b><div class="timeline">${c.ev.length ? c.ev.map((e) => `<div>${esc(e)}</div>`).join('') : '<div class="muted">还没有记录</div>'}</div>
      <div class="row">${c.group === 'todo' ? btn('确认已开通', 'xResolve', c.id + ':done', 'primary sm') + btn('没开通，退回重提', 'xResolve', c.id + ':retry', 'sm') : ''}
        ${c.status === 'running' || c.status === 'todo' ? btn('重新查询', 'toastMsg', '上游：处理中', 'sm') : ''}${c.status === 'unused' ? btn('作废', 'xVoid', c.id, 'danger sm') : ''}${btn('复制兑换链接', 'toastMsg', '已复制', 'sm')}</div>
      <details><summary class="muted">排障信息</summary><div class="muted">卡台 ${plat(S.xSources[c.source].platform).name} · 上游订单 up_${c.id}88 · 请求 req_${c.id}</div></details></div>` : '<div></div>'}</div>`
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
    const strip = (name, d, line, act) => `<div class="card"><div class="row between"><span class="row">${dot(d)}<b>${name}</b></span>${btn('在卡台查看', 'openPlat', act, 'sm')}</div><div class="small muted" style="margin-top:4px">${line}</div></div>`
    return `<div class="page-head"><div><h2>X 会员</h2><p>X 只在这里发码、选卡台、处理单子。凭证和调用记录在「卡台」页。</p></div>${btn('刷新', 'toastMsg', '已刷新')}</div>
    <div class="grid g2" style="margin-bottom:12px">
      ${strip('SpaceX', srcState('spacex'), `钱包 ${usd(sx.x.wallet)} · 未兑 ${sx.x.unused} 张 · 客户填 X Cookie`, 'spacex:x')}
      ${strip('Avanfinity · CDK', srcState('avan_cdk'), srcState('avan_cdk') === 'err' ? '<span style="color:var(--err)">买 CDK 被拒（出口 IP 不在白名单）</span>' : `钱包 ${usd(av.x.wallet)} · 未兑负债 ${usd(av.x.liability)} · 客户填 X 用户名`, 'avan:x')}
    </div>
    <div class="row" style="margin-bottom:12px">${seg([['supply', '供货设置'], ['issue', '发码'], ['records', `兑换记录${S.xCodes.filter((c) => c.group === 'todo').length ? ' · 待处理 ' + S.xCodes.filter((c) => c.group === 'todo').length : ''}`], ['limits', '上限与告警']], S.ui.xTab, 'xTab')}</div>
    ${{ supply: xSupplyTab, issue: xIssueTab, records: xRecordsTab, limits: xLimitsTab }[S.ui.xTab]()}`
  },
  notes: () => ({
    supply: [
      '「供货设置」是运营唯一要动的地方：每个套餐一行，选一家卡台。点圆点就切换，会记审计。',
      '切换只影响之后新发的码。每张 DNX- 码在发出那一刻就绑定了卡台（码尾 SX / AC 只是原型里方便你看，正式版不带）。',
      '每个套餐只有两个选项：SpaceX（客户填 X Cookie）或 Avanfinity CDK（客户填 X 用户名）。Avan 直充不再提供，后端代码保留，界面隐藏。',
      '某家卡台挂了，顶部红条会列出受影响的套餐，「把受影响的套餐切走」按成本自动挑一个可用的。试试点一下。',
      'X 不做双绑：两家兑换要填的东西不同（用户名 / Cookie），X 每一步都可能动钱，自动切台容易重复扣款。',
      '「不卖」表示这家卡台没有这个套餐，不能选。停售开关关掉后，发码页就看不到这个套餐。',
    ],
    issue: ['发码只选套餐、数量、备注。供货卡台跟着「供货设置」走，发码时不用再选，下面一行写清楚这批走哪家、客户要填什么。', '供货卡台不可用时，生成按钮置灰，提示先去切。'],
    records: ['兑换记录按「待处理 / 进行中 / 失败 / 未兑 / 已开通」分组，默认打开待处理。右侧是处理面板，人工确认或退回重提一步完成。', '「待处理」的数量同时显示在顶部导航的红点上。'],
    limits: ['花费上限改成按套餐一行，不再按通道拆成好几列（原来 CDK 钱包上限、CDK 注资、服务费上限分开设，运营很难理解）。'],
  })[S.ui.xTab],
})
