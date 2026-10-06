// ───────── 客户端 ─────────
page('s-home', {
  group: 'site', title: '首页', url: '/',
  render: () => `
    <div class="hero"><h2>ChatGPT / X 会员 自助兑换</h2><p class="muted">输入卡密，按提示提交，几分钟内开通。</p>
      <div class="row" style="justify-content:center;margin-top:16px">${btn('去兑换', 'go', 's-redeem', 'primary')}${btn('查询卡密', 'go', 's-history')}</div></div>
    <div class="grid g3" style="margin-top:20px">
      <div class="card"><b>ChatGPT Plus / Pro</b><p class="muted small">DN- 开头的卡密，提交 ChatGPT 登录凭证。</p></div>
      <div class="card"><b>X Premium</b><p class="muted small">DNX- 开头的卡密。页面会按卡密告诉你要填用户名还是 Cookie。</p></div>
      <div class="card"><b>批量兑换</b><p class="muted small">一次提交多张卡密，适合团队。</p></div>
    </div>`,
  notes: ['首页只放一个兑换入口，不再区分 GPT 页和 X 页。客户不需要知道背后有几家卡台。'],
})

const REDEEM_SAMPLES = [
  ['DN-PLUS-7K2M-QX9A', 'GPT 未用'],
  ['DNX-B1M-PO0A-AC', 'X · 背后是 Avan'],
  ['DNX-P3M-NEW1-SX', 'X · 背后是 SpaceX'],
  ['DNX-P3M-K1LM-SX', 'X 已用'],
  ['LEG-8812-KQ', '老码'],
]
function lookupCode(code) {
  code = code.trim().toUpperCase()
  if (!code) return { err: '请输入卡密' }
  if (code.startsWith('DNX-')) {
    const c = S.xCodes.find((x) => x.code === code) || (code.endsWith('-SX') ? { code, plan: 'x_premium_3m', source: 'spacex', status: 'unused' } : null)
    if (!c) return { err: '卡密不存在，请检查是否输错' }
    if (c.status === 'done') return { err: '这张卡密已经兑换过了，可以去「查询」页看记录' }
    return { product: 'x', code, plan: xPlan(c.plan).name, needs: S.xSources[c.source].platform === 'spacex' ? 'cookie' : 'username', rec: c }
  }
  const g = S.gptCodes.find((x) => x.code === code)
  if (!g) return { err: '卡密不存在，请检查是否输错' }
  if (g.status === 'used') return { err: '这张卡密已经兑换过了' }
  return { product: 'gpt', code, plan: gptPlan(g.plan).name, needs: 'session', rec: g }
}
A.redeemSample = (c) => { S.ui.redeem = { step: 1, code: c, info: null, cred: '', err: '' } }
A.redeemCheck = () => {
  const r = S.ui.redeem, info = lookupCode(r.code)
  if (info.err) { r.err = info.err; return }
  r.err = ''; r.info = info; r.step = 2
}
A.redeemNext = () => {
  const r = S.ui.redeem
  if (!r.cred.trim()) { r.err = r.info.needs === 'username' ? '请填写 X 用户名' : r.info.needs === 'cookie' ? '请粘贴 X Cookie' : '请粘贴 ChatGPT Session'; return }
  r.err = ''; r.step = 3
}
A.redeemSubmit = () => {
  const r = S.ui.redeem; r.step = 4
  if (r.info.product === 'x' && r.info.rec.id) { r.info.rec.status = 'running'; r.info.rec.group = 'running'; r.info.rec.user = r.cred.replace(/^@/, '').slice(0, 20); r.info.rec.ev.push(now() + ' 客户提交') }
  if (r.info.product === 'gpt') r.info.rec.status = 'used'
}
A.redeemFinish = () => { S.ui.redeem.step = 5 }
A.redeemReset = () => { S.ui.redeem = { step: 1, code: '', info: null, cred: '', err: '' } }
A.redeemBack = () => { S.ui.redeem.step = Math.max(1, S.ui.redeem.step - 1) }

page('s-redeem', {
  group: 'site', title: '兑换（统一入口）', url: () => '/recharge' + (S.ui.redeem.code ? '?code=' + S.ui.redeem.code : ''),
  render: () => {
    const r = S.ui.redeem, i = r.info
    const steps = ['输入卡密', '填写信息', '确认', '开通中', '完成']
    const head = `<div class="steps">${steps.map((s, k) => `<span class="${k + 1 === r.step ? 'on' : k + 1 < r.step ? 'done' : ''}">${k + 1}. ${s}</span>`).join('')}</div>`
    const err = r.err ? `<div class="alert err">${esc(r.err)}</div>` : ''
    let body = ''
    if (r.step === 1) body = `
      <div class="card stack"><b>输入卡密</b>
        <div class="row">${input('ui.redeem.code', r.code, 'DN-… 或 DNX-…', 'w-full mono')}</div>
        <div class="row">${btn('下一步', 'redeemCheck', '', 'primary')}</div>
        <div class="small muted">试试这些示例：${REDEEM_SAMPLES.map(([c, l]) => `<button class="link" data-act="redeemSample" data-arg="${c}">${l}</button>`).join(' · ')}</div>
      </div>`
    if (r.step === 2) {
      const lab = { session: ['ChatGPT 凭证', '登录 chatgpt.com 后打开 /api/auth/session，整段复制粘贴到这里。', '{"accessToken":"…"}'],
        username: ['X 用户名', '要开通会员的 X 账号用户名，不带 @ 也行。', 'alice_dev'],
        cookie: ['X Cookie', '在电脑浏览器登录 x.com，按教程复制 auth_token 和 ct0。只用来开通这一次，开通后自动删除。', 'auth_token=…; ct0=…'] }[i.needs]
      body = `<div class="card stack">
        <div class="row">${tag(i.product === 'x' ? 'X 会员' : 'ChatGPT', 'blue')}<b>${esc(i.plan)}</b><span class="mono muted small">${esc(i.code)}</span></div>
        <div><label class="f">${lab[0]}</label>${i.needs === 'username' ? input('ui.redeem.cred', r.cred, lab[2], 'w-full') : `<textarea class="input mono" data-bind="ui.redeem.cred" placeholder="${esc(lab[2])}">${esc(r.cred)}</textarea>`}
        <div class="small muted" style="margin-top:4px">${lab[1]} ${i.needs === 'cookie' ? '<button class="link" data-act="cookieHelp">查看图文教程</button>' : ''}</div></div>
        <div class="row">${btn('上一步', 'redeemBack')}${btn('下一步', 'redeemNext', '', 'primary')}</div></div>`
    }
    if (r.step === 3) body = `<div class="card stack"><b>确认兑换</b>
      <table class="t"><tr><td class="muted">套餐</td><td>${esc(i.plan)}</td></tr><tr><td class="muted">卡密</td><td class="mono">${esc(i.code)}</td></tr>
      <tr><td class="muted">${i.needs === 'username' ? '开通给' : '账号凭证'}</td><td>${i.needs === 'username' ? '@' + esc(r.cred.replace(/^@/, '')) : '已填写（' + r.cred.length + ' 字符）'}</td></tr></table>
      <div class="alert info small">提交后卡密会锁定在这个账号上，开通前可以在「查询」页看进度。</div>
      <div class="row">${btn('上一步', 'redeemBack')}${btn('确认开通', 'redeemSubmit', '', 'primary')}</div></div>`
    if (r.step === 4) body = `<div class="card stack"><b>正在开通</b>
      <div class="timeline"><div>✓ 已收到</div><div>✓ ${i.product === 'x' ? '已提交开通' : '已校验账号'}</div><div>… 开通中，一般 1～3 分钟</div></div>
      <p class="small muted">可以关掉页面，稍后用卡密在「查询」页看结果。</p>
      <div class="row">${btn('模拟：开通成功', 'redeemFinish', '', 'primary')}${btn('去查询页', 'go', 's-history')}</div></div>`
    if (r.step === 5) body = `<div class="card stack" style="text-align:center"><h3 style="margin:6px 0">开通成功</h3>
      <p class="muted">${esc(i.plan)} 已经开到 ${i.needs === 'username' ? '@' + esc(r.cred.replace(/^@/, '')) : '你的账号'}，刷新 ${i.product === 'x' ? 'X' : 'ChatGPT'} 即可看到。</p>
      <div class="row" style="justify-content:center">${btn('再兑一张', 'redeemReset', '', 'primary')}</div></div>`
    return `<div class="center">${head}${err}${body}</div>`
  },
  notes: [
    '一个兑换页，按前缀自动分流：DN- 走 GPT，DNX- 走 X。原来的 /x 地址跳转到这里。',
    'X 码背后是哪家卡台，客户看不到；页面只根据这张码要填用户名（Avanfinity）还是 Cookie（SpaceX）切换输入框。',
    '老 LEG- 码（卡台原生码）照常能兑，走 SpaceX。',
    '已确认：SpaceX 的 X 必须填 Cookie。所以第 2 步的输入框按码切换，Cookie 页附图文教程，并写明「只用这一次，开通后删除」。',
  ],
})
A.cookieHelp = () => modal('如何复制 X Cookie', '<ol class="small"><li>电脑浏览器登录 x.com</li><li>F12 → Application → Cookies → https://x.com</li><li>复制 auth_token 和 ct0 两项的值</li><li>按 auth_token=…; ct0=… 的格式粘贴</li></ol><p class="muted small">原型里只是示意，正式版会放截图。</p>')

page('s-batch', {
  group: 'site', title: '批量兑换', url: '/batch',
  render: () => `<div class="center"><div class="card stack"><b>批量兑换</b>
    <div class="small muted">每行一组：卡密 + 空格 + 邮箱 / X 用户名。GPT 和 X 可以混在一起，系统按前缀分开处理。</div>
    <textarea class="input mono" placeholder="DN-PLUS-7K2M-QX9A user@example.com&#10;DNX-B1M-PO0A-AC alice_dev"></textarea>
    <div class="row">${btn('校验', 'toastMsg', '共 2 行：GPT 1 张、X 1 张，全部可兑', '')}${btn('提交', 'toastMsg', '已提交，去「查询」页看进度', 'primary')}</div></div></div>`,
  notes: ['批量兑换也按前缀分流。SpaceX 的 X 码需要 Cookie，不适合批量，校验时会单独提示这几行。'],
})
A.toastMsg = (m) => toast(m)

page('s-history', {
  group: 'site', title: '查询', url: '/history',
  render: () => `<div class="center"><div class="card stack"><b>查询卡密</b>
    <div class="row">${input('ui.redeem.code', S.ui.redeem.code || 'DNX-P1M-7HQ2-AC', '卡密', 'w-full mono')}${btn('查询', 'toastMsg', '已刷新', 'primary')}</div></div>
    <div class="card stack" style="margin-top:12px"><div class="row between"><b>DNX-P1M-7HQ2-AC</b>${tag('开通中', 'warn')}</div>
    <div class="timeline"><div>10:31 已提交 @alice_dev</div><div>10:31 已提交开通</div><div>10:32 等待开通</div></div>
    <p class="small muted">X 码开通失败时，如果没有扣款，可以在这里直接重新提交。</p></div></div>`,
  notes: ['查询页展示的是客户视角的状态：已提交、开通中、成功、失败可重试。看不到卡台名和上游订单号。'],
})

page('s-tools', {
  group: 'site', title: '工具', url: '/billing',
  render: () => `<div class="grid g3">
    <div class="card"><b>账单检查</b><p class="muted small">/billing：看 ChatGPT 账号当前订阅。</p></div>
    <div class="card"><b>Session 转换</b><p class="muted small">/convert：把各种格式转成兑换需要的 Session。</p></div>
    <div class="card"><b>Session 检查</b><p class="muted small">/inspect：检查 Session 是否有效、是否已有订阅。</p></div></div>`,
  notes: ['这三个工具页保持现状，只是收进一个「工具」入口。'],
})
