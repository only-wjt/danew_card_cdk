// ───────── 代理端（已停用，留档）─────────
// index.html 不再加载本文件；数据里的 S.agents / S.swap / S.ui.partner 已移除，需要演示时要补回。
// 正式开发时代理功能只隐藏入口和路由，不删代码和数据。
const me = () => S.agents[0]
const xPrice = (p) => +(p.cap * me().discount).toFixed(2)

page('p-home', {
  group: 'partner', title: '概览', url: '/partner',
  render: () => `
    <div class="page-head"><div><h2>你好，${esc(me().name)}</h2><p>余额 ${usd(me().balance)} · 折扣 ${me().discount * 100}%</p></div>${btn('去购卡', 'go', 'p-buy', 'primary')}</div>
    <div class="grid g4">${kpi('可用 GPT 卡密', '42 张')}${kpi('可用 X 卡密', me().x ? '9 张' : '未开通')}${kpi('今日兑换', '17')}${kpi('今日失败', '1', '可在「兑换记录」换码')}</div>
    <div class="card" style="margin-top:12px"><b>可售套餐</b>
      <table class="t" style="margin-top:8px"><tr><th>产品</th><th>套餐</th><th>单价</th></tr>
      ${S.gptPlans.map((p) => `<tr><td>${tag('GPT', 'blue')}</td><td>${p.name}</td><td>${usd(p.price * me().discount)}</td></tr>`).join('')}
      ${me().x ? S.xPlans.filter((p) => p.on).map((p) => `<tr><td>${tag('X')}</td><td>${p.name}</td><td>${usd(xPrice(p))}</td></tr>`).join('') : ''}</table></div>`,
  notes: ['代理看到的套餐按产品分组：GPT 和 X。代理能不能卖 X，由后台「代理」页按代理单独打开。', '代理看不到卡台名，也看不到 X 码背后是哪家。'],
})

A.pBuyProduct = (v) => { S.ui.partner.buyProduct = v; S.ui.partner.buyPlan = v === 'gpt' ? 'plus' : S.xPlans.find((p) => p.on).key }
A.pBuyPlan = (k) => { S.ui.partner.buyPlan = k }
A.pBuy = () => {
  const u = S.ui.partner, isX = u.buyProduct === 'x'
  const name = isX ? xPlan(u.buyPlan).name : gptPlan(u.buyPlan).name
  modal('易支付下单', `<p>${name} × ${u.buyQty}</p><p class="muted small">原型：这里会跳转到支付页。</p>`,
    btn('取消', 'closeModal') + btn('模拟：支付成功', 'pPaid', '', 'primary'))
  return 'keep'
}
A.pPaid = () => {
  const u = S.ui.partner, isX = u.buyProduct === 'x'
  u.orders.unshift({ id: 'P-' + (3302 + u.orders.length), at: now(), plan: (isX ? 'X ' + xPlan(u.buyPlan).name : gptPlan(u.buyPlan).name), qty: u.buyQty, st: '已入库' })
  toast('已入库到「我的卡密」')
}

page('p-buy', {
  group: 'partner', title: '购卡下单', url: '/partner/orders',
  render: () => {
    const u = S.ui.partner, isX = u.buyProduct === 'x'
    const plans = isX ? S.xPlans.filter((p) => p.on).map((p) => [p.key, p.name, usd(xPrice(p))]) : S.gptPlans.map((p) => [p.key, p.name, usd(p.price * me().discount)])
    return `<div class="card stack"><div class="row between"><b>选择套餐，支付后自动入库</b>${seg([['gpt', 'ChatGPT'], ...(me().x ? [['x', 'X 会员']] : [])], u.buyProduct, 'pBuyProduct')}</div>
      <div class="grid g3">${plans.map(([k, n, p]) => `<button class="plan-card ${u.buyPlan === k ? 'on' : ''}" data-act="pBuyPlan" data-arg="${k}"><div class="n">${n}</div><div class="s">${p} / 张</div></button>`).join('')}</div>
      <div class="row"><label class="f" style="margin:0">数量</label>${input('ui.partner.buyQty', u.buyQty, '', '', 'number')}${btn('去支付', 'pBuy', '', 'primary')}</div>
      ${isX ? '<p class="small muted">X 卡密是 DNX- 开头。客户兑换时，页面会提示要填用户名还是 Cookie。</p>' : ''}</div>
      <div class="card" style="margin-top:12px"><b>我的购卡订单</b><table class="t" style="margin-top:8px"><tr><th>单号</th><th>时间</th><th>套餐</th><th>数量</th><th>状态</th></tr>
      ${u.orders.map((o) => `<tr><td class="mono">${o.id}</td><td>${o.at}</td><td>${esc(o.plan)}</td><td>${o.qty}</td><td>${tag(o.st, 'ok')}</td></tr>`).join('')}</table></div>`
  },
  notes: ['购卡先选产品再选套餐。X 套餐的价格 = 后台每单花费上限 × 代理折扣（示例规则，你可以改成单独定价）。', '代理购 GPT 卡走本站码 DN-，是否双绑跟随后台「GPT 发码策略」。'],
})

page('p-cdks', {
  group: 'partner', title: '我的卡密', url: '/partner/cdks',
  render: () => `<div class="card"><div class="row between"><b>我的卡密</b><div class="row">${seg([['', '全部'], ['gpt', 'GPT'], ['x', 'X']], S.ui.partner.cdkFilter, 'pCdkFilter')}${btn('导出', 'toastMsg', '已导出 CSV')}</div></div>
    <table class="t" style="margin-top:8px"><tr><th>卡密</th><th>产品</th><th>套餐</th><th>状态</th><th>时间</th></tr>
    ${[...S.gptCodes.slice(0, 3).map((c) => ['gpt', c.code, gptPlan(c.plan).name, c.status, c.at]), ...S.xCodes.slice(0, 3).map((c) => ['x', c.code, xPlan(c.plan).name, c.status, c.at])]
      .filter((r) => !S.ui.partner.cdkFilter || r[0] === S.ui.partner.cdkFilter)
      .map((r) => `<tr><td class="mono">${r[1]}</td><td>${tag(r[0] === 'x' ? 'X' : 'GPT', r[0] === 'x' ? '' : 'blue')}</td><td>${r[2]}</td><td>${statusTag(r[3])}</td><td>${r[4]}</td></tr>`).join('')}</table></div>`,
  notes: ['GPT 和 X 的卡密放在同一个列表里，按产品筛选。'],
})
A.pCdkFilter = (v) => { S.ui.partner.cdkFilter = v }
// statusTag 已移到 app.js（共用）

page('p-batch', {
  group: 'partner', title: '批量充值', url: '/partner/batch',
  render: () => `<div class="card stack"><b>批量充值</b><p class="small muted">每行：卡密 + 邮箱。DN- 本站码会自动走可用的卡台。</p>
    <textarea class="input mono" placeholder="DN-PLUS-… user@example.com"></textarea><div class="row">${btn('提交批次', 'toastMsg', '批次 #72 已创建', 'primary')}</div></div>`,
  notes: ['批量充值目前只支持 GPT。X 的 SpaceX 码要 Cookie，暂不支持批量。', '改造点：代理用 DN- 码批量充值时，后端走统一路由，主台不可用时能按策略切备台（现在只认主台）。'],
})
page('p-records', {
  group: 'partner', title: '兑换记录', url: '/partner/records',
  render: () => `<div class="card"><div class="row">${input('', '', '搜卡密 / 邮箱 / X 用户名', 'w-full')}</div>
    <table class="t" style="margin-top:8px"><tr><th>时间</th><th>卡密</th><th>产品</th><th>账号</th><th>状态</th><th></th></tr>
    ${S.orders.map((o) => `<tr><td>${o.at}</td><td class="mono">${o.code}</td><td>${o.product === 'x' ? 'X' : 'GPT'}</td><td>${esc(o.who)}</td><td>${statusTag(o.st === 'completed' ? 'done' : o.st === 'failed' ? 'failed' : 'running')}</td>
    <td>${o.st === 'failed' ? btn('换码', 'toastMsg', '已换成新码 DN-PLUS-' + rnd(4), 'sm') : ''}</td></tr>`).join('')}</table></div>`,
  notes: ['失败且未扣款的码可直接换码，不用再去隐藏换码页。换出来的是 DN- 本站码。'],
})
page('p-api', {
  group: 'partner', title: 'API 密钥 / 文档', url: '/partner/api-keys',
  render: () => `<div class="grid g2"><div class="card stack"><b>API 密钥</b><div class="mono small">ak_live_****91aa · 07-01 创建</div>${btn('新建密钥', 'toastMsg', '已生成，只显示一次')}</div>
    <div class="card stack"><b>接口</b><div class="small mono">POST /api/v1/agent/redeem<br/>POST /api/v1/agent/x/redeem<br/>GET  /api/v1/agent/plans?product=gpt|x</div>${btn('下载 OpenAPI', 'toastMsg', '已下载')}</div></div>`,
  notes: ['API 增加 product 参数。X 接口单独一组，请求体里带 username 或 cookie。'],
})
page('p-settings', {
  group: 'partner', title: '对接设置', url: '/partner/settings',
  render: () => `<div class="card stack" style="max-width:560px"><b>Webhook</b>${input('', 'https://shop.example/hook', '', 'w-full')}<label class="f">客户单号前缀</label>${input('', 'WANG-', '')}<div>${btn('保存', 'toastMsg', '已保存', 'primary')}</div></div>`,
  notes: ['不改。X 单子的状态变化也推到这个 Webhook，事件里带 product 字段。'],
})
