// ───────── 后台：总览 / GPT 卡密 / 批量充值 / 对账 / 外观 / 审计 ─────────
page('ops-dash', {
  group: 'ops', title: '总览', url: '/ops',
  render: () => {
    const sx = plat('spacex'), av = plat('avan')
    const xTodo = S.xCodes.filter((c) => c.group === 'todo').length
    const probs = platformProblems()
    return `<div class="page-head"><div><h2>总览</h2><p>今天的情况，按产品线看。</p></div></div>
    ${probs.length || xTodo ? `<div class="alert"><b>需要处理</b>
      ${probs.map((p) => `<div class="row"><span>${p.text}</span><button class="link" data-act="${p.act}" data-arg="${p.arg}">${p.btn}</button></div>`).join('')}
      ${xTodo ? `<div class="row"><span>X 会员有 ${xTodo} 单需要人工确认</span><button class="link" data-act="xGoTodo">去处理</button></div>` : ''}</div>` : ''}
    <div class="grid g2">
      <div class="card stack"><div class="row between"><b>ChatGPT</b>${tag('SpaceX 主台', 'blue')}</div>
        <div class="grid g3">${kpi('今日兑换', sx.gpt.ok24)}${kpi('失败', sx.gpt.fail24)}${kpi('SpaceX 可用余额', usd(sx.gpt.spendable))}</div>
        <div class="small muted">备用台：${S.gptPolicy.backupOn ? 'Avanfinity（已开启）' : '未开启'} · 未用本站码 ${sx.gpt.unused} 张</div>
        <div class="row">${btn('发 GPT 卡密', 'go', 'ops-cdk', 'primary sm')}${btn('看 SpaceX', 'openPlat', 'spacex:overview', 'sm')}</div></div>
      <div class="card stack"><div class="row between"><b>X 会员</b>${tag('按套餐选卡台')}</div>
        <div class="grid g3">${kpi('今日开通', sx.x.ok24 + av.x.ok24)}${kpi('待处理', xTodo, '', xTodo ? 'var(--warn)' : '')}${kpi('未兑负债', usd(av.x.liability), 'Avan CDK')}</div>
        <div class="small muted">SpaceX 供 ${S.xPlans.filter((p) => p.on && p.source === 'spacex').length} 个套餐 · Avanfinity 供 ${S.xPlans.filter((p) => p.on && p.source !== 'spacex').length} 个</div>
        <div class="row">${btn('发 X 卡密', 'xGoIssue', '', 'primary sm')}${btn('供货设置', 'xGoSupply', '', 'sm')}</div></div>
    </div>
    <div class="card" style="margin-top:12px"><b>卡台</b>${matrixTable()}</div>`
  },
  notes: ['总览按产品线分两块：ChatGPT、X 会员。所有问题汇总在顶部黄条，每条都直接跳到能修它的地方。', '下面的「产品 × 卡台」小表和卡台页顶部是同一张，一眼看出哪个产品在哪家卡台上有问题。'],
})

// ── GPT 卡密 ──
A.cdkTab = (v) => { S.ui.cdkTab = v }
A.cdkPlan = (k) => { S.ui.cdkIssue.plan = k }
A.cdkDual = () => { S.ui.cdkIssue.dual = !S.ui.cdkIssue.dual }
A.cdkIssue = () => {
  const f = S.ui.cdkIssue, dual = S.gptPolicy.backupOn && f.dual
  const pre = { plus: 'PLUS', pro_5x: 'PRO5', pro_20x: 'PR20', go: 'GO01', credit_1000: 'CR1K' }[f.plan]
  const list = Array.from({ length: Math.min(f.qty, 50) }, () => `DN-${pre}-${rnd(4)}-${rnd(4)}`)
  list.forEach((code, i) => S.gptCodes.unshift({ id: 1300 + S.gptCodes.length + i, code, plan: f.plan, region: f.region, status: 'unused', bind: { spacex: true, avan: dual }, ful: '—', note: f.note, at: now() }))
  S.ui.cdkIssued = list
  plat('spacex').gpt.spendable -= gptPlan(f.plan).price * list.length
  toast(`已生成 ${list.length} 张${dual ? '（双绑）' : ''}`)
}
A.cdkFilter = (v) => { const [k, val] = v.split(':'); S.ui.cdkFilter[k] = val }
A.cdkVoid = (id) => { const c = S.gptCodes.find((x) => x.id == id); c.status = 'void'; toast('已作废，卡台上的码同步撤销') }

page('ops-cdk', {
  group: 'ops', title: 'GPT 卡密', url: '/ops/cdkeys',
  render: () => {
    const f = S.ui.cdkIssue, fl = S.ui.cdkFilter
    const tabs = seg([['site', '本站码 DN-'], ['legacy', '老码（卡台原生）'], ['stock', '白号库存']], S.ui.cdkTab, 'cdkTab')
    const issue = `<div class="card stack"><b>发 GPT 卡密</b>
      <div class="grid g4" style="grid-template-columns:repeat(5,minmax(0,1fr))">${S.gptPlans.map((p) => `<button class="plan-card ${f.plan === p.key ? 'on' : ''}" data-act="cdkPlan" data-arg="${p.key}"><div class="n">${p.name}</div><div class="s">SpaceX 服务费 ${usd(p.price)}</div></button>`).join('')}</div>
      <div class="row">${select('ui.cdkIssue.region', f.region, S.regions.map((r) => [r, r]))}${input('ui.cdkIssue.qty', f.qty, '', '', 'number')}${input('ui.cdkIssue.note', f.note, '备注，客服可搜')}
        <span class="row small">${sw(f.dual && S.gptPolicy.backupOn, 'cdkDual')}<span class="${S.gptPolicy.backupOn ? '' : 'muted'}">同时在 Avanfinity 备一张</span></span>
        ${btn('生成', 'cdkIssue', '', 'primary')}</div>
      <p class="small muted">${S.gptPolicy.backupOn ? '备用台已开启。勾选后，SpaceX 挂了会自动用 Avanfinity 那张兜底。' : '备用台没开，只在 SpaceX 买一张。要开去「卡台 → GPT 发码策略」。'} X 卡密不在这里发，去「X 会员」。</p>
      ${S.ui.cdkIssued.length ? `<div class="card" style="background:var(--surface-2)"><div class="row between"><b>刚生成 ${S.ui.cdkIssued.length} 张</b>${btn('复制全部', 'toastMsg', '已复制', 'sm')}</div><div class="mono small">${S.ui.cdkIssued.slice(0, 5).join('<br/>')}${S.ui.cdkIssued.length > 5 ? '<br/>…' : ''}</div></div>` : ''}</div>`
    let list = ''
    if (S.ui.cdkTab === 'site') {
      const rows = S.gptCodes.filter((c) => c.bind && (!fl.status || c.status === fl.status))
      list = `<div class="row" style="margin:10px 0">${seg([['status:', '全部'], ['status:unused', '未用'], ['status:used', '已用'], ['status:failed', '失败']], 'status:' + fl.status, 'cdkFilter')}${input('', '', '搜卡密 / 备注')}</div>
      <table class="t"><tr><th>卡密</th><th>套餐</th><th>区域</th><th>状态</th><th>绑定</th><th>履约台</th><th>备注</th><th>时间</th><th></th></tr>
      ${rows.map((c) => `<tr><td class="mono">${c.code}</td><td>${gptPlan(c.plan).name}</td><td>${c.region}</td><td>${statusTag(c.status)}</td>
        <td>${tag('SpaceX ✓', 'ok')} ${c.bind.avan ? tag('Avan ✓', 'ok') : ''}</td><td>${c.ful}</td><td>${esc(c.note)}</td><td>${c.at}</td>
        <td>${c.status === 'unused' ? btn('作废', 'cdkVoid', c.id, 'sm danger') : ''}</td></tr>`).join('')}</table>`
    } else if (S.ui.cdkTab === 'legacy') {
      list = `<p class="small muted" style="margin:10px 0">改造前发出的卡台原生码，照常能兑（固定走 SpaceX）。这里只读，不再新发。</p>
      <table class="t"><tr><th>卡密</th><th>套餐</th><th>状态</th><th>时间</th></tr>${S.gptCodes.filter((c) => !c.bind).map((c) => `<tr><td class="mono">${c.code}</td><td>${gptPlan(c.plan).name}</td><td>${statusTag(c.status)}</td><td>${c.at}</td></tr>`).join('')}</table>`
    } else {
      list = `<div class="row between" style="margin:10px 0"><span class="small muted">白号（已有 Plus 的现成账号）。兑换 Plus 时优先出白号，不消耗卡台余额。</span>${btn('导入白号', 'stockImport', '', 'primary sm')}</div>
      <table class="t"><tr><th>账号</th><th>套餐</th><th>状态</th><th>导入</th></tr>${S.localStock.map((s) => `<tr><td>${s.email}</td><td>${gptPlan(s.plan).name}</td><td>${tag(s.status, s.status === '可用' ? 'ok' : '')}</td><td>${s.at}</td></tr>`).join('')}</table>`
    }
    return `<div class="page-head"><div><h2>GPT 卡密</h2><p>只管 ChatGPT。默认在 SpaceX 买码，对外发 DN- 本站码。</p></div></div>${issue}<div class="card" style="margin-top:12px"><div class="row between"><b>卡密列表</b>${tabs}</div>${list}</div>`
  },
  notes: [
    '原「CDK卡密」改名「GPT 卡密」，只发 GPT。原来在这里能发的 X 套餐挪到「X 会员」，三条 X 链路合成一条。',
    '「同时在 Avanfinity 备一张」只有在 GPT 发码策略里开了备用台才能点；默认关，GPT 就是单台出货。',
    '列表拆成三个页签：本站码、老码（只读）、白号库存。原来一张表混三类码，靠筛选区分，很难看懂。',
    '白号导入入口挪到这里（原 AgentManagement.vue 没挂路由，进不去）。',
  ],
})
A.stockImport = () => { modal('导入白号', '<textarea class="input mono" placeholder="email----password----2fa"></textarea>', btn('取消', 'closeModal') + btn('导入', 'stockDone', '', 'primary')); return 'keep' }
A.stockDone = () => { S.localStock.unshift({ id: 9, email: 'white0' + (S.localStock.length + 1) + '@example.com', plan: 'plus', status: '可用', at: now().slice(0, 5) }); toast('导入 1 个') }

// ── 批量充值 ──
page('ops-batch', {
  group: 'ops', title: '批量充值', url: '/ops/batch-recharge',
  render: () => `<div class="page-head"><div><h2>批量充值</h2><p>GPT 专用。每行：卡密 + 邮箱 / Session。</p></div></div>
    <div class="card stack"><textarea class="input mono" placeholder="DN-PLUS-… user@example.com"></textarea>
    <div class="row">${btn('预检', 'toastMsg', '40 行：DN- 38、老码 2，全部可兑')}${btn('提交', 'toastMsg', '批次 #72 已创建', 'primary')}</div></div>
    <div class="card" style="margin-top:12px"><b>批次</b><table class="t" style="margin-top:8px"><tr><th>#</th><th>时间</th><th>总数</th><th>成功</th><th>失败</th><th>状态</th></tr>
    ${S.batches.map((b) => `<tr><td>${b.id}</td><td>${b.at}</td><td>${b.total}</td><td>${b.ok}</td><td>${b.fail}</td><td>${tag(b.st, b.st === '已完成' ? 'ok' : 'warn')}</td></tr>`).join('')}</table></div>`,
  notes: ['页面不变。后端改造：DN- 码走统一兑换路由（和单张兑换同一套），现在批量只认主台原生码。'],
})

// ── 兑换对账 ──
A.ordFilter = (v) => { const [k, val] = v.split(':'); S.ui.ordFilter[k] = val }
A.ordSel = (id) => { S.ui.ordSel = S.ui.ordSel == id ? null : Number(id) }
page('ops-orders', {
  group: 'ops', title: '兑换对账', url: '/ops/orders',
  render: () => {
    const f = S.ui.ordFilter
    const rows = S.orders.filter((o) => (!f.product || o.product === f.product) && (!f.st || o.st === f.st))
    const d = S.orders.find((o) => o.id === S.ui.ordSel)
    return `<div class="page-head"><div><h2>兑换对账</h2><p>GPT 和 X 的所有开通单，一张表看钱花在哪。</p></div>${btn('导出', 'toastMsg', '已导出 CSV')}</div>
    <div class="row" style="margin-bottom:10px">${seg([['product:', '全部产品'], ['product:gpt', 'GPT'], ['product:x', 'X']], 'product:' + f.product, 'ordFilter')}
      ${seg([['st:', '全部状态'], ['st:completed', '成功'], ['st:failed', '失败'], ['st:uncertain', '不确定']], 'st:' + f.st, 'ordFilter')}</div>
    <div class="grid" style="grid-template-columns:minmax(0,1fr) ${d ? '300px' : '0'}">
    <div class="card"><table class="t"><tr><th>订单</th><th>产品</th><th>卡密</th><th>套餐</th><th>账号</th><th>卡台</th><th>卡片</th><th>金额</th><th>状态</th><th>时间</th></tr>
    ${rows.map((o) => `<tr class="click ${o.id === S.ui.ordSel ? 'sel' : ''}" data-act="ordSel" data-arg="${o.id}"><td>${o.id}</td><td>${o.product === 'x' ? 'X' : 'GPT'}</td><td class="mono">${o.code}</td><td>${o.plan}</td><td>${esc(o.who)}</td><td>${o.plat}</td><td>${o.card}</td><td>${o.amt}</td>
      <td>${tag({ completed: '成功', failed: '失败', uncertain: '不确定' }[o.st], { completed: 'ok', failed: 'err', uncertain: 'warn' }[o.st])}</td><td>${o.at}</td></tr>`).join('')}</table></div>
    ${d ? `<div class="card stack small"><b>订单 ${d.id}</b><div>卡台 ${d.plat}</div><div>卡片 ${d.card}</div><div>金额 ${d.amt}</div><div class="row">${btn('向上游查询', 'toastMsg', '上游状态：' + d.st, 'sm')}${d.product === 'x' ? btn('去 X 会员处理', 'xGoTodo', '', 'sm') : ''}</div></div>` : '<div></div>'}</div>`
  },
  notes: ['对账加「产品」和「卡台」两列，GPT 和 X 放一起看。X 单子点进去可以跳到 X 会员的处理面板。'],
})

// 代理功能已停用：原型不展示代理页。开发时只隐藏路由和入口，不删代码和数据。

page('ops-appearance', {
  group: 'ops', title: '外观', url: '/ops/appearance',
  render: () => `<div class="page-head"><div><h2>外观</h2><p>保持现状。</p></div></div><div class="grid g4">${['默认', 'Slate', 'Cyber', 'Paper'].map((s) => `<div class="card"><b>${s}</b><div class="small muted">皮肤预览</div></div>`).join('')}</div>`,
  notes: ['不改，保持现状。'],
})
page('ops-audit', {
  group: 'ops', title: '审计', url: '/ops/audit',
  render: () => `<div class="page-head"><div><h2>操作审计</h2><p>供货切换、发码、改策略都会记一笔。</p></div></div>
    <div class="card"><table class="t"><tr><th>时间</th><th>操作人</th><th>操作</th><th>IP</th></tr>${S.audit.map((a) => `<tr><td>${a.at}</td><td>${a.who}</td><td>${esc(a.act)}</td><td class="mono">${a.ip}</td></tr>`).join('')}</table></div>`,
  notes: [
    '新增几类审计：X 供货切换、GPT 主台/备用台变更、卡台启停。原型里在 X 供货设置切一次，这里会多出一行。',
    '已停用但要保留的功能（开发时只隐藏，不删代码、不删数据、不删接口）：代理端 /partner 全部页面、后台代理管理、代理订单、代理换码 /partner/swap、Avan X 直充和付款卡。做法是去掉导航入口、路由加开关，后端接口保持可用。',
  ],
})
