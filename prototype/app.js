// 原型框架：页面注册、渲染、事件分发。各页面在 site.js / partner.js / ops-*.js 里注册。
const PAGES = {}
const A = {} // 动作：data-act="名字" data-arg="参数"
const GROUPS = [
  { id: 'site', label: '客户端 danew.cc' },
  { id: 'ops', label: '后台 /ops' },
  // 代理端 /partner 已停用：原型不再展示（partner.js 留档不加载），开发时只隐藏入口，不删代码
]

function page(id, def) { PAGES[id] = { id, ...def } }
const $ = (s) => document.querySelector(s)
const esc = (v) => String(v ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]))
const usd = (n) => (n == null ? '—' : '$' + Number(n).toFixed(2))
const plat = (id) => S.platforms.find((p) => p.id === id)
const xPlan = (k) => S.xPlans.find((p) => p.key === k)
const tgPlan = (k) => S.tgPlans.find((p) => p.key === k)
const gptPlan = (k) => S.gptPlans.find((p) => p.key === k)
const rnd = (n) => Array.from({ length: n }, () => 'ABCDEFGHJKLMNPQRSTUVWXYZ23456789'[Math.floor(Math.random() * 32)]).join('')
const now = () => { const d = new Date(); return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}` }

// 小组件
const btn = (label, act, arg = '', cls = '', extra = '') => `<button class="btn ${cls}" data-act="${act}" data-arg="${esc(arg)}" ${extra}>${label}</button>`
const tag = (t, cls = '') => `<span class="tag ${cls}">${t}</span>`
const dot = (cls) => `<span class="dot ${cls}"></span>`
const sw = (on, act, arg = '') => `<button class="switch ${on ? 'on' : ''}" data-act="${act}" data-arg="${esc(arg)}" aria-pressed="${on}"></button>`
const seg = (items, cur, act) => `<div class="seg">${items.map(([v, l]) => `<button class="${v === cur ? 'on' : ''}" data-act="${act}" data-arg="${esc(v)}">${l}</button>`).join('')}</div>`
const input = (bind, val, ph = '', cls = '', type = 'text') => `<input class="input ${cls}" type="${type}" data-bind="${bind}" value="${esc(val)}" placeholder="${esc(ph)}" />`
const select = (bind, val, opts) => `<select class="input" data-bind="${bind}">${opts.map(([v, l]) => `<option value="${esc(v)}" ${String(v) === String(val) ? 'selected' : ''}>${esc(l)}</option>`).join('')}</select>`
function statusTag(s) {
  const m = { unused: ['未用', ''], used: ['已用', 'ok'], done: ['已开通', 'ok'], running: ['开通中', 'warn'], failed: ['失败', 'err'], todo: ['待处理', 'warn'], void: ['已作废', ''] }[s] || [s, '']
  return tag(m[0], m[1])
}
const kpi = (t, v, s = '', color = '') => `<div class="card kpi"><div class="t">${t}</div><div class="v" ${color ? `style="color:${color}"` : ''}>${v}</div><div class="s">${s}</div></div>`

function toast(msg) {
  const el = $('#toast'); el.textContent = msg; el.hidden = false
  clearTimeout(toast._t); toast._t = setTimeout(() => (el.hidden = true), 2200)
}
function modal(title, body, foot = '') {
  $('#modal-mask').classList.remove('as-drawer')
  $('#modal-title').textContent = title
  $('#modal-body').innerHTML = body
  $('#modal-foot').innerHTML = foot || btn('关闭', 'closeModal')
  $('#modal-mask').hidden = false
}
function drawer(title, body, foot = '') {
  modal(title, body, foot)
  $('#modal-mask').classList.add('as-drawer')
}
A.closeModal = () => { $('#modal-mask').hidden = true; $('#modal-mask').classList.remove('as-drawer') }
function pagerBar(page, total, act, size = 20) {
  const pages = Math.max(1, Math.ceil(total / size))
  const p = Math.min(page, pages)
  return `<div class="pager"><span>第 ${p} 页 · 共 ${total} 条</span><span class="row">${btn('‹', act, String(p - 1), 'sm', p <= 1 ? 'disabled' : '')}<span class="tag">${p}</span>${btn('›', act, String(p + 1), 'sm', p >= pages ? 'disabled' : '')}<span class="small">20/page</span></span></div>`
}

// 按路径写状态，例如 "ui.xIssue.qty"
function setPath(path, val) {
  const ks = path.split('.'); let o = S
  for (let i = 0; i < ks.length - 1; i++) o = o[ks[i]]
  const old = o[ks.at(-1)]
  o[ks.at(-1)] = typeof old === 'number' && val !== '' && !isNaN(val) ? Number(val) : val
}

// 管理端外壳
function opsNav() {
  const todo = S.xCodes.filter((c) => c.group === 'todo').length
  const tgTodo = S.tgCodes.filter((c) => c.group === 'todo').length
  const probs = platformProblems().length
  return [
    ['ops-dash', '总览'], ['ops-cdk', 'GPT 会员'], ['ops-x', 'X 会员', todo], ['ops-tg', 'TG 会员', tgTodo],
    ['ops-batch', '批量充值'], ['ops-orders', '兑换对账'], ['ops-platforms', '卡台', probs], ['ops-appearance', '外观'], ['ops-audit', '审计'],
  ]
}
function opsShell(inner) {
  return `<div class="main"><div class="topnav"><span class="brand">danew 运营</span>${opsNav().map(([id, l, n]) => `<button class="pill ${S.page === id ? 'on' : ''}" data-act="go" data-arg="${id}">${l}${n ? `<span class="badge-n">${n}</span>` : ''}</button>`).join('')}
    <span style="margin-left:auto" class="muted small">admin</span></div><div class="page">${inner}</div></div>`
}
function siteShell(inner) {
  const items = [['s-home', '首页'], ['s-redeem', '兑换'], ['s-batch', '批量兑换'], ['s-history', '查询'], ['s-tools', '工具']]
  return `<div class="main"><div class="topnav"><span class="brand">danew</span>${items.map(([id, l]) => `<button class="pill ${S.page === id ? 'on' : ''}" data-act="go" data-arg="${id}">${l}</button>`).join('')}
    <span style="margin-left:auto" class="muted small">中文 · 主题</span></div><div class="page">${inner}</div></div>`
}

function render() {
  const p = PAGES[S.page]
  $('#entry').innerHTML = GROUPS.map((g) => `<button class="chip ${g.id === p.group ? 'on' : ''}" data-act="group" data-arg="${g.id}">${g.label}</button>`).join('')
  const opsOrder = ['ops-dash', 'ops-cdk', 'ops-x', 'ops-tg', 'ops-batch', 'ops-orders', 'ops-platforms', 'ops-appearance', 'ops-audit']
  const pages = Object.values(PAGES).filter((x) => x.group === p.group && !x.hidden)
  if (p.group === 'ops') pages.sort((a, b) => opsOrder.indexOf(a.id) - opsOrder.indexOf(b.id))
  $('#pages').innerHTML = pages.map((x) => `<button class="chip ${x.id === S.page ? 'on' : ''}" data-act="go" data-arg="${x.id}">${x.title}</button>`).join('')
  $('#url').textContent = 'danew.cc' + (typeof p.url === 'function' ? p.url() : p.url)
  const inner = p.render()
  $('#frame').innerHTML = p.group === 'ops' ? opsShell(inner) : siteShell(inner)
  const notes = typeof p.notes === 'function' ? p.notes() : p.notes
  $('#notes').innerHTML = notes?.length ? `<h3>设计说明 · ${p.title}</h3><ol>${notes.map((n) => `<li>${n}</li>`).join('')}</ol>` : ''
  $('#notes').hidden = !notes?.length
}

A.go = (id) => { S.page = id; window.scrollTo({ top: 0 }) }
A.group = (g) => { S.page = Object.values(PAGES).find((x) => x.group === g && !x.hidden).id }

document.addEventListener('click', (e) => {
  const el = e.target.closest('[data-act]')
  if (!el || el.disabled) return
  const fn = A[el.dataset.act]
  if (!fn) return toast('原型：' + el.dataset.act)
  const keepModal = fn(el.dataset.arg, el) === 'keep'
  if (!keepModal && el.closest('.modal') && el.dataset.act !== 'closeModal') A.closeModal()
  render()
})
document.addEventListener('change', (e) => {
  const el = e.target.closest('[data-bind]')
  if (!el) return
  setPath(el.dataset.bind, el.type === 'checkbox' ? el.checked : el.value)
  if (!el.closest('.modal')) render()
})
document.addEventListener('keydown', (e) => { if (e.key === 'Escape') A.closeModal() })
