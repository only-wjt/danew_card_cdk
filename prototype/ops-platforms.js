// ───────── 后台：卡台 ─────────
// 问题汇总：顶部导航角标、总览黄条、卡台页黄条共用
function platformProblems() {
  const out = []
  S.platforms.forEach((p) => {
    if (!p.enabled) return
    if (!p.webhook.secret && (p.gptRole !== 'off' || p.id === 'avan')) out.push({ text: `<b>${p.name}</b>：回调 Secret 没配，回调会被拒，订单状态只能靠轮询。`, btn: '去配置回调', act: 'openPlat', arg: `${p.id}:webhook` })
    p.conns.filter((c) => c.error).forEach((c) => out.push({ text: `<b>${p.name} · ${c.protoLabel.split(' · ')[0]}</b>：${esc(c.error)}。`, btn: '去看凭证', act: 'openPlat', arg: `${p.id}:creds` }))
  })
  return out
}

// 产品 × 卡台
function matrixCell(prod, pid) {
  const p = plat(pid)
  if (!p.enabled) return `<span class="muted">已停用</span>`
  if (prod === 'gpt') {
    if (p.gptRole === 'primary') return `${dot('ok')} 主台`
    if (p.gptRole === 'backup') return `${dot(p.conns[0].state === 'ok' ? 'ok' : 'warn')} 备用`
    return `<span class="muted">不用</span>`
  }
  if (prod === 'tg') {
    if (pid !== 'avan') return `<span class="muted">不用</span>`
    const n = S.tgPlans.filter((x) => x.on).length
    const bad = S.xSources.avan_cdk.state === 'err'
    return `${dot(bad ? 'err' : 'ok')} 供 ${n} 个套餐 · 共用钱包${bad ? ' · <span style="color:var(--err)">凭证不可用</span>' : ''}`
  }
  const keys = Object.keys(S.xSources).filter((k) => S.xSources[k].platform === pid)
  const n = S.xPlans.filter((x) => x.on && keys.includes(x.source)).length
  const bad = keys.some((k) => S.xSources[k].state === 'err')
  return `${dot(bad ? 'err' : 'ok')} 供 ${n} 个套餐${bad ? ' · <span style="color:var(--err)">X 不可用</span>' : ''}`
}
function matrixTable() {
  return `<table class="t matrix" style="margin-top:8px"><tr><th></th>${S.platforms.map((p) => `<th>${p.name}</th>`).join('')}</tr>
    <tr><td><b>ChatGPT</b></td>${S.platforms.map((p) => `<td><button class="link" data-act="openPlat" data-arg="${p.id}:gpt">${matrixCell('gpt', p.id)}</button></td>`).join('')}</tr>
    <tr><td><b>X 会员</b></td>${S.platforms.map((p) => `<td><button class="link" data-act="openPlat" data-arg="${p.id}:x">${matrixCell('x', p.id)}</button></td>`).join('')}</tr>
    <tr><td><b>TG 会员</b></td>${S.platforms.map((p) => `<td>${p.id === 'avan' ? `<button class="link" data-act="openPlat" data-arg="avan:tg">${matrixCell('tg', p.id)}</button>` : matrixCell('tg', p.id)}</td>`).join('')}</tr></table>`
}

A.openPlat = (arg) => { const [id, tab] = arg.split(':'); S.page = 'ops-platforms'; S.ui.platSide = id; S.ui.plat = id; S.ui.platTab = tab || 'overview' }
A.platSide = (v) => { S.ui.platSide = v; if (plat(v)) { S.ui.plat = v; S.ui.platTab = 'overview' } }
A.platTab = (v) => { S.ui.platTab = v }
A.copyIp = () => toast('已复制 ' + S.egressIp)
A.platEnable = (id) => {
  const p = plat(id)
  if (p.enabled && p.gptRole === 'primary') { toast('它是 GPT 主台，先在「GPT 发码策略」换主台再停用'); return }
  p.enabled = !p.enabled; toast(`${p.name} 已${p.enabled ? '启用' : '停用'}`)
}
A.gptRole = (arg) => {
  const [id, role] = arg.split(':'); const p = plat(id)
  if (role === 'primary') S.platforms.forEach((x) => { if (x.gptRole === 'primary') x.gptRole = 'backup' })
  if (p.gptRole === 'primary' && role !== 'primary') { toast('GPT 必须有一个主台，请把另一家设为主台'); return }
  p.gptRole = role
  S.gptPolicy.backupOn = S.platforms.some((x) => x.gptRole === 'backup')
  S.audit.unshift({ at: now(), who: 'admin', act: `GPT 角色 · ${p.name} → ${{ primary: '主台', backup: '备用', off: '不用' }[role]}`, ip: '198.51.100.7' })
  toast('已保存，只影响之后新发的码')
}
A.testConn = (arg) => {
  const [pid, cid] = arg.split(':'); const c = plat(pid).conns.find((x) => x.id == cid)
  if (c.error) { toast('仍失败：' + c.error.split('：')[0]); return }
  c.state = 'ok'; c.lastOk = '刚刚'; toast('连通正常')
}
A.fixWhitelist = () => {
  const c = plat('avan').conns.find((x) => x.id === 22)
  c.error = undefined; c.state = 'ok'; c.lastOk = '刚刚'; S.xSources.avan_cdk.state = 'ok'
  toast('Avan 会员连通正常，X 和 TG 的 CDK 都恢复了')
}
A.saveSecret = (id) => { plat(id).webhook.secret = true; toast('Secret 已保存') }
A.cardMove = (arg) => {
  const [pid, code, d] = arg.split(':'); const cs = plat(pid).cards.sort((a, b) => a.order - b.order)
  const i = cs.findIndex((c) => c.code === code), j = i + Number(d)
  if (j < 0 || j >= cs.length) return
  ;[cs[i].order, cs[j].order] = [cs[j].order, cs[i].order]
}
A.cardEnabled = (arg) => { const [pid, code] = arg.split(':'); const c = plat(pid).cards.find((x) => x.code === code); c.enabled = !c.enabled }
A.spacexPolicy = (key) => { const p = plat('spacex').policy; p[key] = !p[key] }
A.spacexHealth = (key) => { const h = plat('spacex').health; h[key] = !h[key] }
A.unblock = (arg) => { const [pid, id] = arg.split(':'); const p = plat(pid); p.blocked = p.blocked.filter((b) => b.id != id); toast('已解冻') }
A.payMode = (mode) => { plat('avan').payMode = mode }
A.payFallback = () => { const p = plat('avan'); p.payFallbackNew = !p.payFallbackNew }
A.payCardOn = (id) => { const c = plat('avan').payCards.find((x) => x.id == id); if (c.status === '冻结') { toast('冻结的卡先解冻再启用'); return } c.enabled = !c.enabled }
A.payCardMove = (arg) => {
  const [id, d] = arg.split(':'); const cs = plat('avan').payCards.sort((a, b) => a.order - b.order)
  const i = cs.findIndex((c) => c.id == id), j = i + Number(d)
  if (j < 0 || j >= cs.length) return
  ;[cs[i].order, cs[j].order] = [cs[j].order, cs[i].order]
}
A.payFix = (id) => { const p = plat('avan'); p.fixedCardId = Number(id); p.payMode = 'fixed' }
A.payUnblock = (id) => {
  const p = plat('avan')
  p.payBlocked = p.payBlocked.filter((b) => b.id != id)
  const c = p.payCards.find((x) => x.id == id)
  if (c) { c.status = '正常'; c.enabled = true }
  toast('已解冻，回到自动选卡')
}
A.addPlat = () => {
  modal('添加卡台', `<label class="f">名称</label>${input('', '', '例如 新卡台 C', 'w-full')}
    <label class="f">协议</label>${select('', '', [['spacexcard-legacy', 'SpaceX 旧 OpenAPI'], ['avanfinity-2026-08', 'Avanfinity OpenAI'], ['avanfinity-api-v1', 'Avanfinity 会员 CDK（X + TG）']])}
    <label class="f">接口地址</label>${input('', '', 'https://', 'w-full')}<label class="f">凭证</label>${input('', '', 'API Key / AppId:Secret', 'w-full', 'password')}
    <p class="small muted">一家卡台可以加多个连接。Avanfinity 的 X 和 TG 用同一套 AppId，不要再建一个 TG 连接。</p>`,
    btn('取消', 'closeModal') + btn('保存并测连通', 'toastMsg', '原型：已保存', 'primary'))
  return 'keep'
}
A.editConn = (arg) => {
  const [pid, cid] = arg.split(':'); const c = plat(pid).conns.find((x) => x.id == cid)
  modal('编辑连接 · ' + c.protoLabel, `<label class="f">凭证</label>${input('', '', '留空不改（当前 ' + c.cred + '）', 'w-full', 'password')}`, btn('取消', 'closeModal') + btn('保存', 'toastMsg', '已保存', 'primary'))
  return 'keep'
}

function platTabs(p) {
  const t = [['overview', '概览'], ['gpt', 'GPT 选卡'], ['x', 'X'], ['webhook', '回调'], ['creds', '凭证']]
  if (p.tg) t.splice(3, 0, ['tg', 'TG'])
  return seg(t, S.ui.platTab, 'platTab')
}
function tabOverview(p) {
  const g = p.gpt, x = p.x
  const walletTitle = p.tg ? '会员钱包' : 'X 钱包'
  const walletSub = p.tg ? 'X 和 TG 共用，不要加两次' : (x.liability ? `未兑负债 ${usd(x.liability)}` : '')
  const sync = p.tg ? '已同步 5 个 GPT 套餐、10 个 X 套餐、3 个 TG 套餐' : '已同步 5 个 GPT 套餐、4 个 X 套餐'
  return `<div class="grid g3">${kpi('GPT 可用余额', usd(g.spendable), g.reserve ? `含 ${usd(g.reserve)} 风险保证金` : '')}${kpi('GPT 服务费', g.fee, g.feeNote)}${kpi('连通', p.conns.every((c) => c.state === 'ok') ? '正常' : '有问题', p.conns.map((c) => c.lastOk).join(' · '), p.conns.every((c) => c.state === 'ok') ? 'var(--ok)' : 'var(--warn)')}</div>
    <div class="grid" style="margin-top:12px;grid-template-columns:repeat(${p.tg ? 4 : 3},minmax(0,1fr))">${kpi(walletTitle, usd(x.wallet), walletSub)}${kpi('近 24 小时 GPT', `成功 ${g.ok24} · 失败 ${g.fail24}`)}${kpi('近 24 小时 X', `成功 ${x.ok24} · 失败 ${x.fail24}`)}${p.tg ? kpi('近 24 小时 TG', `成功 ${p.tg.ok24} · 失败 ${p.tg.fail24}`) : ''}</div>
    <div class="card stack" style="margin-top:12px"><b>这家卡台用在哪</b>
      <div class="row"><span style="width:90px">ChatGPT</span>${seg([['primary', '主台'], ['backup', '备用'], ['off', '不用']], p.gptRole, 'gptRoleOf')}</div>
      <div class="row"><span style="width:90px">X 会员</span><span>${matrixCell('x', p.id)}</span>${btn('去 X 供货设置', 'xGoSupply', '', 'sm')}</div>
      <div class="row"><span style="width:90px">TG 会员</span><span>${matrixCell('tg', p.id)}</span>${p.tg ? btn('去 TG 会员', 'tgGoIssue', '', 'sm') : ''}</div></div>
    <div class="row" style="margin-top:12px">${btn('一键检测（连通 + 余额 + 服务费）', 'toastMsg', '检测完成', 'primary')}${btn('同步套餐', 'toastMsg', sync)}</div>`
}
A.gptRoleOf = (role) => A.gptRole(S.ui.plat + ':' + role)
function tabGpt(p) {
  if (p.id !== 'spacex') return `<div class="card"><b>这家的 GPT 不在本站选卡</b><p class="small muted">Avanfinity 的 OpenAI 接口不提供卡头顺序。选卡由上游自己做。卡头优先级、兑换换卡、坏卡归因只属于 SpaceX。</p></div>`
  const cs = [...p.cards].sort((a, b) => a.order - b.order)
  const pol = p.policy
  const health = p.health
  const first = cs.find((c) => c.enabled && c.online)
  const online = cs.filter((c) => c.online).length
  const how = !pol.localOn
    ? '本站策略关着。发码和兑换都走卡台自己的级联，下面的卡头顺序不会盖过它。'
    : `发码时把「${first ? first.label : '没有可用卡头'}」写成这张码的偏好。兑换时${pol.strict ? '严格按下面的顺序，卡台默认的 537872 / 星链不能盖过。' : '仍可能被卡台默认卡头盖过。'}${pol.switchOnFail ? `确认没扣款之后，按顺序换下一张，这一单最多换 ${pol.maxCards} 张。` : '失败了不自动换卡。'}${pol.autoOpen ? '顺序里没有合格卡头时，允许开新卡。' : '没有合格卡头就停，不开新卡。'}`
  return `<div class="card stack"><b>这台现在怎么选卡</b><p class="small" style="margin:0">${how}</p><p class="small muted" style="margin:0">改完只影响之后新发的码。已经发出的码不追溯。</p></div>

    <div class="card" style="margin-top:12px"><div class="row between"><div><b>1. 卡头优先级</b><div class="small muted">选的是开卡产品，不是一张张已开出的实体卡。在线 ${online} / ${cs.length}。靠前且启用、在线的先用；关掉或下线的跳过。默认只收美卡，香港卡可以加进来但默认关。保存后同步到这台的 select_priority。</div></div>${btn('立即同步', 'toastMsg', '已同步', 'sm')}</div>
    <table class="t"><tr><th>#</th><th>参与</th><th>状态</th><th>卡头</th><th>地区</th><th></th></tr>${cs.map((c, i) => `<tr><td>${i + 1}</td><td>${sw(c.enabled, 'cardEnabled', p.id + ':' + c.code)}</td><td>${c.online ? tag('在线', 'ok') : tag('下线')}</td>
      <td>${esc(c.label)} <span class="mono small muted">${c.bin}</span></td><td>${c.area}</td>
      <td>${btn('↑', 'cardMove', `${p.id}:${c.code}:-1`, 'sm')}${btn('↓', 'cardMove', `${p.id}:${c.code}:1`, 'sm')}</td></tr>`).join('')}</table>
    <p class="small muted">当前发码产品：${first ? `${esc(first.label)}（优先级里第一条启用且在线的）` : '没有'}。不用单独再填一个产品码。</p></div>

    <div class="card stack" style="margin-top:12px"><div class="row between"><b>2. 兑换时换不换卡</b><span class="row">${sw(pol.localOn, 'spacexPolicy', 'localOn')}<span class="small">${pol.localOn ? '本站策略开' : '本站策略关'}</span></span></div>
      <p class="small muted" style="margin:0">这一组只有本站策略开着才生效。开着时，发码写入选卡偏好，兑换向卡台声明按本站来，不跟卡台账户里的 ACC 换卡策略。</p>
      <div class="row">${sw(pol.strict, 'spacexPolicy', 'strict')}<span class="small">严格按上面的卡头顺序（strict_card_preference）。关掉之后，卡台默认卡头可以盖过 CDK。</span></div>
      <div class="row">${sw(pol.switchOnFail, 'spacexPolicy', 'switchOnFail')}<span class="small">${pol.switchOnFail ? '确认未扣款或失败后，按优先级换下一张卡。' : '不自动换卡（no_auto_card_switch）。这张失败了就停在这张，等人工或本站拉黑后再跳过。'}</span></div>
      <div class="grid g3">
        <div><label class="f">这一单最多用几张卡</label>${input('platforms.0.policy.maxCards', pol.maxCards, '', 'w-full')}</div>
        <div><label class="f">失败后冷却（小时）</label>${input('platforms.0.policy.cooldown', pol.cooldown, '', 'w-full')}<div class="small muted">预留，界面上有，兑换还没按它拦截。</div></div>
        <div><label class="f">每张卡新账号上限</label>${input('platforms.0.policy.maxNew', pol.maxNew, '', 'w-full')}<div class="small muted">一卡几付的硬限制仍在卡台。这里是本站记下的上限。</div></div>
      </div></div>

    <div class="card stack" style="margin-top:12px"><b>3. 没有合格卡时</b>
      <div class="row">${sw(pol.autoOpen, 'spacexPolicy', 'autoOpen')}<span class="small">${pol.autoOpen ? '顺序里没有能用的卡头时，允许开新卡。' : '没有合格卡头就停止，不开新卡。'}</span></div>
      <div class="grid g3">
        <div><label class="f">限定发卡地区</label>${input('platforms.0.policy.area', pol.area, 'United States', 'w-full')}</div>
        <div><label class="f">新卡持卡人名</label>${input('platforms.0.policy.holderFirst', pol.holderFirst, 'GPT', 'w-full')}</div>
        <div><label class="f">新卡持卡人姓</label>${input('platforms.0.policy.holderLast', pol.holderLast, 'Direct', 'w-full')}</div>
      </div></div>

    <div class="card" style="margin-top:12px"><div class="row between"><div><b>4. 坏卡</b><div class="small muted">看的是已经开出来的那张实体卡，不是卡头。同一张卡失败到阈值：不同邮箱判卡的问题，本站拉黑，下次兑换排除它；同一个邮箱判号的问题，不拉黑。没有邮箱时${health.requireEmail ? '不拉黑' : '也会拉黑'}。</div></div><span class="row">${sw(health.enabled, 'spacexHealth', 'enabled')}<span class="small">${health.enabled ? '启用' : '停用'}</span></span></div>
      <div class="grid g3">
        <div><label class="f">失败几次算坏卡</label>${input('platforms.0.health.threshold', health.threshold, '', 'w-full')}</div>
        <div class="row" style="align-items:flex-end">${sw(health.freeze, 'spacexHealth', 'freeze')}<span class="small">判定后冻结卡台上的卡。现在实际不冻结，只在本站排除，直充还能用这张。</span></div>
        <div class="row" style="align-items:flex-end">${sw(health.requireEmail, 'spacexHealth', 'requireEmail')}<span class="small">没有邮箱就不拉黑。</span></div>
      </div>
      <div style="margin-top:10px"><b class="small">已拉黑</b></div>
      ${p.blocked.length ? `<table class="t"><tr><th>卡</th><th>原因</th><th></th></tr>${p.blocked.map((b) => `<tr><td class="mono">#${b.id} ****${b.last4}</td><td>${b.reason} · 失败 ${b.fails} · 邮箱 ${b.emails} · ${b.freeze}</td><td>${btn('解禁', 'unblock', p.id + ':' + b.id, 'sm')}</td></tr>`).join('')}</table>` : '<div class="empty">没有</div>'}
      <div style="margin-top:10px"><b class="small">最近失败</b></div>
      <table class="t"><tr><th>时间</th><th>卡</th><th>订单</th><th>邮箱</th><th>判定</th><th>状态</th></tr>${(p.failEvents || []).map((e) => `<tr><td>${e.at}</td><td class="mono">#${e.card}</td><td class="mono">${e.order}</td><td class="mono">${e.email}</td><td>${e.verdict === '卡的问题' ? tag(e.verdict, 'err') : tag(e.verdict, 'warn')}</td><td>${e.status}</td></tr>`).join('')}</table></div>`
}
function tabPayCards(p) {
  const rows = [...p.payCards].sort((a, b) => a.order - b.order)
  const next = p.payMode === 'existing' ? rows.find((c) => c.enabled && c.status === '正常' && c.balance >= 1) : null
  return `<div class="card stack"><div class="row between"><div><b>付款从哪张已有卡出</b><div class="small muted">X 和 TG 共用这一池卡、同一个顺序。发码不扣钱。客户兑换时按顺序用已经开好的卡，不每笔新开一张。</div></div></div>
    ${seg([['existing', '从已有卡自动选'], ['fixed', '固定一张'], ['new', '每笔开新卡']], p.payMode, 'payMode')}
    ${p.payMode === 'existing' ? `<p class="small">下一笔会用 ${next ? `<b>****${next.mask}</b>（${next.product}，余额 ${usd(next.balance)}）` : '<b>没有合格卡，这笔会停住</b>'}。条件：已启用、状态正常、余额够付、没拉黑。余额不够或冻结的自动跳过。</p>
      <div class="row">${sw(p.payFallbackNew, 'payFallback')}<span class="small">${p.payFallbackNew ? '池子里没有合格卡时，才按卡种开一张新卡。' : '池子里没有合格卡就停止，不开新卡。'}</span></div>` : ''}
    ${p.payMode === 'new' ? '<p class="small" style="color:var(--warn)">每笔兑换都新开一张卡。卡之间互不影响，但开卡费每笔都扣。这不是默认。</p>' : ''}
    ${p.payMode === 'fixed' ? '<p class="small">只付这一张。它冻结、余额不够或被拉黑时，X 和 TG 一起停，不会改去别的卡。</p>' : ''}
    <table class="t"><tr><th>#</th><th>启用</th><th>卡</th><th>余额</th><th>状态</th><th></th></tr>${rows.map((c, i) => `<tr><td>${i + 1}</td><td>${sw(c.enabled && c.status === '正常', 'payCardOn', c.id)}</td>
      <td class="mono">****${c.mask} <span class="small muted">${c.product}</span>${p.payMode === 'fixed' && p.fixedCardId === c.id ? ' ' + tag('固定', 'blue') : ''}${next && next.id === c.id ? ' ' + tag('下一笔', 'ok') : ''}</td>
      <td>${usd(c.balance)}</td><td>${c.status === '正常' ? tag('正常', 'ok') : tag(c.status, 'err')}</td>
      <td>${btn('↑', 'payCardMove', c.id + ':-1', 'sm')}${btn('↓', 'payCardMove', c.id + ':1', 'sm')}${p.payMode === 'fixed' ? btn('用这张', 'payFix', c.id, 'sm') : ''}</td></tr>`).join('')}</table></div>
    <div class="card" style="margin-top:12px"><b>拉黑的卡</b>${p.payBlocked.length ? `<table class="t"><tr><th>卡</th><th>原因</th><th></th></tr>${p.payBlocked.map((b) => `<tr><td class="mono">****${b.mask}</td><td>${b.reason}</td><td>${btn('解冻', 'payUnblock', b.id, 'sm')}</td></tr>`).join('')}</table>` : '<div class="empty">没有</div>'}</div>`
}
function callTable(rows, empty) {
  if (!rows.length) return `<div class="empty">${empty}</div>`
  return `<table class="t"><tr><th>时间</th><th>方法</th><th>路径</th><th>状态</th><th>说明</th></tr>${rows.map((c) => `<tr><td>${c.at}</td><td>${c.m}</td><td class="mono small">${c.p}</td><td>${tag(c.s, c.s < 300 ? 'ok' : 'err')}</td><td class="small">${c.d}</td></tr>`).join('')}</table>`
}
function tabX(p) {
  if (p.id === 'spacex') return `<div class="card stack"><b>SpaceX X 会员</b><div class="small">客户兑换时提交 X Cookie，SpaceX 负责下单付款。Telegram 不走这家。</div>
    <div class="grid g3">${kpi('X 钱包', usd(p.x.wallet))}${kpi('未兑 X 码', p.x.unused)}${kpi('近 24 小时', `成功 ${p.x.ok24}`)}</div>${btn('去 X 供货设置', 'xGoSupply', '', 'sm')}</div>`
  const x = p.x
  const rows = (p.calls || []).filter((c) => c.prod !== 'tg')
  return `<div class="card stack"><b>Avanfinity X · CDK</b><div class="small">和 TG 共用钱包、同一套 AppId，也共用下面这池已有卡。这里只看 X 的未兑和调用。</div>
    <div class="grid g3">${kpi('共用钱包', usd(x.wallet), 'TG 也扣这里')}${kpi('X 未兑负债', usd(x.liability), `已买未兑 ${x.unusedCdk} 张`)}${kpi('近 24 小时 X', `成功 ${x.ok24} · 失败 ${x.fail24}`)}</div>${btn('去 X 供货设置', 'xGoSupply', '', 'sm')}</div>
    <div style="margin-top:12px">${tabPayCards(p)}</div>
    <div class="card" style="margin-top:12px"><b>X 的最近调用</b><p class="small muted">没有回调。TG 的请求在「TG」页签，不混在这里。改卡序在哪边改都是同一份。</p>
    ${callTable(rows, '还没有 X 调用')}</div>`
}
function tabTg(p) {
  if (!p.tg) return `<div class="card"><b>这家卡台不卖 Telegram</b><p class="small muted">TG 只走 Avanfinity CDK，和 X 的 CDK 共用钱包和凭证。</p></div>`
  const rows = (p.calls || []).filter((c) => c.prod === 'tg')
  return `<div class="card stack"><b>Avanfinity TG · CDK</b><div class="small">没有第二套凭证，也没有第二池卡。客户填 Telegram 用户名。付款卡和 X 是同一份顺序。</div>
    <div class="grid g3">${kpi('共用钱包', usd(p.x.wallet), '和 X 是同一个余额')}${kpi('TG 未兑', p.tg.unused + ' 张', '不计入 X 的未兑负债')}${kpi('近 24 小时 TG', `成功 ${p.tg.ok24} · 失败 ${p.tg.fail24}`)}</div>
    <div class="row">${btn('去 TG 会员', 'tgGoIssue', '', 'sm')}${btn('上限与告警', 'tgGoLimits', '', 'sm')}</div></div>
    <div style="margin-top:12px">${tabPayCards(p)}</div>
    <div class="card" style="margin-top:12px"><b>TG 的最近调用</b><p class="small muted">路径是 /tg-direct 和 /api/public/tg-cdk。白名单失败会和 X 同时出现，因为是同一套 AppId。</p>
    ${callTable(rows, '还没有 TG 调用')}</div>`
}
A.tgGoLimits = () => { S.page = 'ops-tg'; S.ui.tgTab = 'limits' }
function tabWebhook(p) {
  const w = p.webhook
  return `<div class="card stack" style="max-width:640px"><b>回调（GPT 订单）</b>
    <label class="f">回调地址，填到卡台后台</label><div class="row">${input('', w.url, '', 'w-full mono')}${btn('复制', 'toastMsg', '已复制')}</div>
    <label class="f">Secret ${w.secret ? tag('已配置', 'ok') : tag('未配置', 'warn')}</label><div class="row">${input('', '', w.secret ? '已设置，留空不改' : '从卡台后台复制', 'w-full', 'password')}${btn('保存', 'saveSecret', p.id, 'primary')}</div>
    <div class="small muted">最近一次：${w.last}</div></div>`
}
function tabCreds(p) {
  return `<div class="card"><div class="row between"><b>连接</b>${btn('添加连接', 'addPlat', '', 'sm')}</div>
    <p class="small muted">一家卡台下面可能有几套接口，每套单独测连通。</p>
    <table class="t"><tr><th>接口</th><th>用于</th><th>凭证</th><th>状态</th><th></th></tr>${p.conns.map((c) => `<tr><td>${c.protoLabel}<div class="mono small muted">${c.proto}</div></td>
      <td>${c.caps.map((k) => tag({ gpt: 'GPT', x_spacex: 'X', x_cdk: 'X CDK', tg_cdk: 'TG CDK' }[k] || k)).join(' ')}</td><td class="mono small">${c.cred}</td>
      <td>${dot(c.state === 'ok' ? 'ok' : c.state === 'idle' ? '' : 'err')} ${c.lastOk}${c.error ? `<div class="small" style="color:var(--err)">${esc(c.error)}</div>` : ''}</td>
      <td>${btn('测连通', 'testConn', p.id + ':' + c.id, 'sm')}${btn('编辑', 'editConn', p.id + ':' + c.id, 'sm')}${c.error ? btn('模拟：已加白名单', 'fixWhitelist', '', 'sm') : ''}</td></tr>`).join('')}</table>
    <div class="row" style="margin-top:10px">${btn(p.enabled ? '停用这家卡台' : '启用', 'platEnable', p.id, p.enabled ? 'danger sm' : 'primary sm')}</div></div>`
}
function platSideBar() {
  const it = (v, name, sub, d, right = '') => `<button class="side-item ${S.ui.platSide === v ? 'on' : ''}" data-act="platSide" data-arg="${v}">${d != null ? dot(d) : ''}<span style="flex:1">${name}<span class="sub">${sub}</span></span><span class="small muted">${right}</span></button>`
  return `<div class="side-group">卡台</div>${S.platforms.map((p) => {
      const bad = platformProblems().some((x) => x.arg.startsWith(p.id + ':'))
      const role = { primary: 'GPT 主台', backup: 'GPT 备用', off: '' }[p.gptRole]
      const money = p.tg ? `钱包 ${usd(p.x.wallet)} · X+TG` : `GPT ${usd(p.gpt.spendable)} · X ${usd(p.x.wallet)}`
      const marks = [role, 'X', p.tg ? 'TG' : ''].filter(Boolean).join(' / ')
      return it(p.id, p.name, p.enabled ? (bad ? '<span style="color:var(--warn)">需要处理</span>' : money) : '已停用', p.enabled ? (bad ? 'warn' : 'ok') : '', marks)
    }).join('')}
    <div class="side-group">规则</div>${it('policy', 'GPT 发码策略', S.gptPolicy.backupOn ? '主台 + 备用' : '只用主台', null)}
    <button class="side-item" data-act="xGoSupply"><span style="flex:1">X 供货设置 →<span class="sub">在 X 会员页，TG 不在这里选</span></span></button>
    <button class="side-item" data-act="tgGoIssue"><span style="flex:1">TG 会员 →<span class="sub">只走这家的 CDK</span></span></button>
    <div class="side-group">其他</div>${it('orphans', '未归属回调', '找不到卡台的回调', null, S.orphans.length)}`
}
function platMain() {
  const v = S.ui.platSide
  if (v === 'policy') {
    const pri = S.platforms.find((p) => p.gptRole === 'primary')
    return `<h3>GPT 发码策略</h3><div class="card stack" style="max-width:640px">
      <div class="row"><span style="width:90px">主台</span>${seg(S.platforms.map((p) => [p.id, p.name]), pri.id, 'gptPrimary')}</div>
      <div class="row"><span style="width:90px">备用台</span>${sw(S.gptPolicy.backupOn, 'gptBackup')}<span class="small">${S.gptPolicy.backupOn ? `开：发码页可勾「同时在 ${S.platforms.find((p) => p.gptRole === 'backup')?.name} 备一张」` : '关：GPT 单台出货'}</span></div>
      <ol class="small muted"><li>默认只用主台：在主台买一张码，DN- 码只绑主台。</li><li>开了备用台：发码时可勾双绑，同时在备用台买一张；主台熔断时客户兑换自动走备用台，另一张退回。</li><li>改策略只影响之后新发的码。</li></ol></div>`
  }
  if (v === 'orphans') return `<h3>未归属回调</h3><div class="card"><p class="small muted">回调地址对不上任何卡台。多半是卡台后台还填着旧地址。</p><table class="t"><tr><th>时间</th><th>路径</th><th>事件</th></tr>${S.orphans.map((o) => `<tr><td>${o.at}</td><td class="mono small">${o.path}</td><td>${o.ev}</td></tr>`).join('')}</table></div>`
  const p = plat(v)
  return `<div class="row between"><div class="row"><h3 style="margin:0">${p.name}</h3>${tag(p.enabled ? '启用' : '停用', p.enabled ? 'ok' : '')}${p.gptRole !== 'off' ? tag(p.gptRole === 'primary' ? 'GPT 主台' : 'GPT 备用', 'blue') : ''}<span class="small muted">${p.site}</span></div>${btn('一键检测', 'toastMsg', '检测完成', 'sm')}</div>
    <div style="margin:12px 0">${platTabs(p)}</div>${{ overview: tabOverview, gpt: tabGpt, x: tabX, tg: tabTg, webhook: tabWebhook, creds: tabCreds }[S.ui.platTab](p)}`
}
A.gptPrimary = (id) => A.gptRole(id + ':primary')
A.gptBackup = () => {
  const other = S.platforms.find((p) => p.gptRole !== 'primary')
  A.gptRole(other.id + ':' + (S.gptPolicy.backupOn ? 'off' : 'backup'))
}

page('ops-platforms', {
  group: 'ops', title: '卡台', url: () => '/ops/platforms/' + S.ui.platSide + (plat(S.ui.platSide) ? '?tab=' + S.ui.platTab : ''),
  render: () => {
    const probs = platformProblems()
    return `<div class="page-head"><div><h2>卡台</h2><p>接了哪几家、各自用在哪、凭证和回调。</p></div>
      <div class="row"><span class="card small" style="padding:6px 10px">出口 IP（填白名单） <span class="mono">${S.egressIp}</span> <button class="link" data-act="copyIp">复制</button></span>${btn('添加卡台', 'addPlat', '', 'primary')}</div></div>
    ${probs.length ? `<div class="alert"><b>${probs.length} 项需要处理</b>${probs.map((p) => `<div class="row"><span>${p.text}</span><button class="link" data-act="${p.act}" data-arg="${p.arg}">${p.btn}</button></div>`).join('')}</div>` : ''}
    <div class="card" style="margin-bottom:14px"><b>产品 × 卡台</b>${matrixTable()}</div>
    <div class="split"><div>${platSideBar()}</div><div>${platMain()}</div></div>`
  },
  notes: [
    '左边按「卡台」列，不再按 OPENAI / X 分组。一家卡台一行，右边页签里同时看到它的 GPT 和 X。现在 Avan 被拆成「备台 B」「Avan-X-CDK」「Avan-X-Direct」三行，其实是一家。',
    'GPT 走 SpaceX 主台；Avanfinity 做 GPT 备台，默认关闭，需要时在「GPT 发码策略」打开。',
    '顶部「产品 × 卡台」回答 GPT、X、TG 现在走哪家。TG 只有 Avanfinity 一格能点，进「TG」页签；SpaceX 那格是不用。',
    'GPT 角色（主台 / 备用 / 不用）在卡台概览或「GPT 发码策略」里改，两处是同一个设置。主台不能直接停用。',
    'X 走哪家不在这里改，统一在「X 会员 → 供货设置」。TG 没有供货切换。',
    'Avanfinity 的 X CDK 和 TG CDK 是同一套 AppId、同一个钱包。凭证上打两个标签，概览里钱包只出现一次。调用记录按产品拆开，白名单失败会在两边同时看到。',
    'SpaceX「GPT 选卡」按兑换顺序分成四块：卡头优先级、失败了换不换卡、没有合格卡头开不开新卡、实体卡坏了怎么拉黑。现在线上卡台页把这些收成一张上下移的表，对不上。',
    'Avanfinity 的 X 和 TG 不再默认「每笔开一张新卡」，也不默认钉死一张。两边看到的是同一池已开出的卡，按顺序自动选；没有合格卡就停，除非打开「才开新卡」。',
    '「凭证」页签列出这家卡台下的每套接口，各自测连通。试试在 Avan 的凭证里点「模拟：已加白名单」，X 会员页的红条会消失。',
    '代理功能已停用：代理端 /partner、代理管理、代理换码在开发时只隐藏路由和入口，不删代码和数据。原来「其他」里的「代理换码」入口去掉。',
  ],
})
