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
  const keys = Object.keys(S.xSources).filter((k) => S.xSources[k].platform === pid)
  const n = S.xPlans.filter((x) => x.on && keys.includes(x.source)).length
  const bad = keys.some((k) => S.xSources[k].state === 'err')
  return `${dot(bad ? 'err' : 'ok')} 供 ${n} 个套餐${bad ? ' · <span style="color:var(--err)">X 不可用</span>' : ''}`
}
function matrixTable() {
  return `<table class="t matrix" style="margin-top:8px"><tr><th></th>${S.platforms.map((p) => `<th>${p.name}</th>`).join('')}</tr>
    <tr><td><b>ChatGPT</b></td>${S.platforms.map((p) => `<td><button class="link" data-act="openPlat" data-arg="${p.id}:gpt">${matrixCell('gpt', p.id)}</button></td>`).join('')}</tr>
    <tr><td><b>X 会员</b></td>${S.platforms.map((p) => `<td><button class="link" data-act="openPlat" data-arg="${p.id}:x">${matrixCell('x', p.id)}</button></td>`).join('')}</tr></table>`
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
  toast('Avan X 连通正常，CDK 恢复可用')
}
A.saveSecret = (id) => { plat(id).webhook.secret = true; toast('Secret 已保存') }
A.cardMove = (arg) => {
  const [pid, code, d] = arg.split(':'); const cs = plat(pid).cards.sort((a, b) => a.order - b.order)
  const i = cs.findIndex((c) => c.code === code), j = i + Number(d)
  if (j < 0 || j >= cs.length) return
  ;[cs[i].order, cs[j].order] = [cs[j].order, cs[i].order]
}
A.cardOnline = (arg) => { const [pid, code] = arg.split(':'); const c = plat(pid).cards.find((x) => x.code === code); c.online = !c.online }
A.unblock = (arg) => { const [pid, id] = arg.split(':'); const p = plat(pid); p.blocked = p.blocked.filter((b) => b.id != id); toast('已解冻') }
A.addPlat = () => {
  modal('添加卡台', `<label class="f">名称</label>${input('', '', '例如 新卡台 C', 'w-full')}
    <label class="f">协议</label>${select('', '', [['spacexcard-legacy', 'SpaceX 旧 OpenAPI'], ['avanfinity-2026-08', 'Avanfinity OpenAI'], ['avanfinity-api-v1', 'Avanfinity X 会员']])}
    <label class="f">接口地址</label>${input('', '', 'https://', 'w-full')}<label class="f">凭证</label>${input('', '', 'API Key / AppId:Secret', 'w-full', 'password')}
    <p class="small muted">一家卡台可以加多个连接（比如 Avan 的 OpenAI 和 X 是两套接口）。保存后先测连通，再决定用于哪个产品。</p>`,
    btn('取消', 'closeModal') + btn('保存并测连通', 'toastMsg', '原型：已保存', 'primary'))
  return 'keep'
}
A.editConn = (arg) => {
  const [pid, cid] = arg.split(':'); const c = plat(pid).conns.find((x) => x.id == cid)
  modal('编辑连接 · ' + c.protoLabel, `<label class="f">凭证</label>${input('', '', '留空不改（当前 ' + c.cred + '）', 'w-full', 'password')}`, btn('取消', 'closeModal') + btn('保存', 'toastMsg', '已保存', 'primary'))
  return 'keep'
}

function platTabs(p) {
  const t = [['overview', '概览'], ['gpt', 'GPT 选卡'], ['x', 'X · 调用记录'], ['webhook', '回调'], ['creds', '凭证']]
  return seg(t, S.ui.platTab, 'platTab')
}
function tabOverview(p) {
  const g = p.gpt, x = p.x
  return `<div class="grid g3">${kpi('GPT 可用余额', usd(g.spendable), g.reserve ? `含 ${usd(g.reserve)} 风险保证金` : '')}${kpi('GPT 服务费', g.fee, g.feeNote)}${kpi('连通', p.conns.every((c) => c.state === 'ok') ? '正常' : '有问题', p.conns.map((c) => c.lastOk).join(' · '), p.conns.every((c) => c.state === 'ok') ? 'var(--ok)' : 'var(--warn)')}</div>
    <div class="grid g3" style="margin-top:12px">${kpi('X 钱包', usd(x.wallet), x.liability ? `未兑负债 ${usd(x.liability)}` : '')}${kpi('近 24 小时 GPT', `成功 ${g.ok24} · 失败 ${g.fail24}`)}${kpi('近 24 小时 X', `成功 ${x.ok24} · 失败 ${x.fail24}`)}</div>
    <div class="card stack" style="margin-top:12px"><b>这家卡台用在哪</b>
      <div class="row"><span style="width:90px">ChatGPT</span>${seg([['primary', '主台'], ['backup', '备用'], ['off', '不用']], p.gptRole, 'gptRoleOf')}</div>
      <div class="row"><span style="width:90px">X 会员</span><span>${matrixCell('x', p.id)}</span>${btn('去 X 供货设置', 'xGoSupply', '', 'sm')}</div></div>
    <div class="row" style="margin-top:12px">${btn('一键检测（连通 + 余额 + 服务费）', 'toastMsg', '检测完成', 'primary')}${btn('同步套餐', 'toastMsg', '已同步 5 个 GPT 套餐、6 个 X 套餐')}</div>`
}
A.gptRoleOf = (role) => A.gptRole(S.ui.plat + ':' + role)
function tabGpt(p) {
  const cs = [...p.cards].sort((a, b) => a.order - b.order)
  return `<div class="card"><div class="row between"><b>GPT 选卡顺序</b>${btn('从卡台同步卡片', 'toastMsg', '已同步', 'sm')}</div>
    <p class="small muted">开通 GPT 时按顺序挑第一张在线、没拉黑的卡。</p>
    <table class="t"><tr><th>#</th><th>卡</th><th>在线</th><th></th></tr>${cs.map((c, i) => `<tr><td>${i + 1}</td><td>${c.label} <span class="mono small muted">${c.code}</span></td><td>${sw(c.online, 'cardOnline', p.id + ':' + c.code)}</td>
      <td>${btn('↑', 'cardMove', `${p.id}:${c.code}:-1`, 'sm')}${btn('↓', 'cardMove', `${p.id}:${c.code}:1`, 'sm')}</td></tr>`).join('')}</table>
    <div class="row" style="margin-top:8px">${sw(p.forceNewCard, 'toastMsg', '原型：开关')}<span class="small">每次都开新卡（不复用）</span></div></div>
    <div class="card" style="margin-top:12px"><b>拉黑的卡</b>${p.blocked.length ? `<table class="t"><tr><th>卡 ID</th><th>原因</th><th></th></tr>${p.blocked.map((b) => `<tr><td class="mono">${b.id}</td><td>${b.reason}</td><td>${btn('解冻', 'unblock', p.id + ':' + b.id, 'sm')}</td></tr>`).join('')}</table>` : '<div class="empty">没有</div>'}</div>`
}
function tabX(p) {
  if (p.id === 'spacex') return `<div class="card stack"><b>SpaceX X 会员</b><div class="small">客户兑换时提交 X Cookie，SpaceX 负责下单付款。</div>
    <div class="grid g3">${kpi('X 钱包', usd(p.x.wallet))}${kpi('未兑 X 码', p.x.unused)}${kpi('近 24 小时', `成功 ${p.x.ok24}`)}</div>${btn('去 X 供货设置', 'xGoSupply', '', 'sm')}</div>`
  const x = p.x
  return `<div class="card stack"><b>Avanfinity X 会员 · CDK</b><div class="small">兑换时本站先用钱包买一张 Avan CDK，再拿客户填的 X 用户名去兑。</div>
    <div class="grid g3">${kpi('CDK 钱包', usd(x.wallet))}${kpi('未兑负债', usd(x.liability), `已买未兑 ${x.unusedCdk} 张`)}${kpi('近 24 小时', `成功 ${x.ok24} · 失败 ${x.fail24}`)}</div>${btn('去 X 供货设置', 'xGoSupply', '', 'sm')}</div>
    <div class="card" style="margin-top:12px"><b>最近调用</b><p class="small muted">X 接口没有回调，这里看本站发给 Avan 的请求。</p>
    <table class="t"><tr><th>时间</th><th>方法</th><th>路径</th><th>状态</th><th>说明</th></tr>${p.calls.map((c) => `<tr><td>${c.at}</td><td>${c.m}</td><td class="mono small">${c.p}</td><td>${tag(c.s, c.s < 300 ? 'ok' : 'err')}</td><td class="small">${c.d}</td></tr>`).join('')}</table></div>`
}
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
      <td>${c.caps.map((k) => tag({ gpt: 'GPT', x_spacex: 'X', x_cdk: 'X CDK' }[k])).join(' ')}</td><td class="mono small">${c.cred}</td>
      <td>${dot(c.state === 'ok' ? 'ok' : c.state === 'idle' ? '' : 'err')} ${c.lastOk}${c.error ? `<div class="small" style="color:var(--err)">${esc(c.error)}</div>` : ''}</td>
      <td>${btn('测连通', 'testConn', p.id + ':' + c.id, 'sm')}${btn('编辑', 'editConn', p.id + ':' + c.id, 'sm')}${c.error ? btn('模拟：已加白名单', 'fixWhitelist', '', 'sm') : ''}</td></tr>`).join('')}</table>
    <div class="row" style="margin-top:10px">${btn(p.enabled ? '停用这家卡台' : '启用', 'platEnable', p.id, p.enabled ? 'danger sm' : 'primary sm')}</div></div>`
}
function platSideBar() {
  const it = (v, name, sub, d, right = '') => `<button class="side-item ${S.ui.platSide === v ? 'on' : ''}" data-act="platSide" data-arg="${v}">${d != null ? dot(d) : ''}<span style="flex:1">${name}<span class="sub">${sub}</span></span><span class="small muted">${right}</span></button>`
  return `<div class="side-group">卡台</div>${S.platforms.map((p) => {
      const bad = platformProblems().some((x) => x.arg.startsWith(p.id + ':'))
      const role = { primary: 'GPT 主台', backup: 'GPT 备用', off: '' }[p.gptRole]
      return it(p.id, p.name, p.enabled ? (bad ? '<span style="color:var(--warn)">需要处理</span>' : `GPT ${usd(p.gpt.spendable)} · X ${usd(p.x.wallet)}`) : '已停用', p.enabled ? (bad ? 'warn' : 'ok') : '', [role, 'X'].filter(Boolean).join(' / '))
    }).join('')}
    <div class="side-group">规则</div>${it('policy', 'GPT 发码策略', S.gptPolicy.backupOn ? '主台 + 备用' : '只用主台', null)}
    <button class="side-item" data-act="xGoSupply"><span style="flex:1">X 供货设置 →<span class="sub">在 X 会员页</span></span></button>
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
    <div style="margin:12px 0">${platTabs(p)}</div>${{ overview: tabOverview, gpt: tabGpt, x: tabX, webhook: tabWebhook, creds: tabCreds }[S.ui.platTab](p)}`
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
    '顶部「产品 × 卡台」小表回答一个问题：GPT 和 X 现在分别走哪家。点格子跳到对应页签。',
    'GPT 角色（主台 / 备用 / 不用）在卡台概览或「GPT 发码策略」里改，两处是同一个设置。主台不能直接停用。',
    'X 走哪家不在这里改，统一在「X 会员 → 供货设置」。卡台页只管凭证、钱包和调用记录。Avan X 只用 CDK，直充付款卡不再展示（后端保留）。',
    '「凭证」页签列出这家卡台下的每套接口，各自测连通。试试在 Avan 的凭证里点「模拟：已加白名单」，X 会员页的红条会消失。',
    '代理功能已停用：代理端 /partner、代理管理、代理换码在开发时只隐藏路由和入口，不删代码和数据。原来「其他」里的「代理换码」入口去掉。',
  ],
})
