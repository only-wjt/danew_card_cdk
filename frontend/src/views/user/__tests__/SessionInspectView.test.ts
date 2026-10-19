// @vitest-environment happy-dom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh'
import SessionInspectView from '../SessionInspectView.vue'

// 不加载 src/i18n：它在 import 时读 localStorage，Node 自带的 localStorage 会盖掉 happy-dom 的
vi.mock('../../../components/LanguageToggle.vue', () => ({ default: { render: () => null } }))
vi.mock('../../../components/ThemeToggle.vue', () => ({ default: { render: () => null } }))

const i18n = createI18n({ legacy: false, globalInjection: true, locale: 'zh', messages: { zh } })

function b64url(obj: unknown) {
  return btoa(JSON.stringify(obj)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

// exp 在过去：本地解析会判定 AT 已过期
function session(email: string, st: string, expired = false) {
  const exp = Math.floor(Date.now() / 1000) + (expired ? -3600 : 3600)
  return { user: { email }, accessToken: `eyJhbGciOiJub25lIn0.${b64url({ exp })}.${st}`, sessionToken: st }
}

const GOOD = session('good@mail.com', 'st-good')
const BAD = session('bad@mail.com', 'st-bad')
const OTHER = session('other@mail.com', 'st-other')
const EXPIRED_GOOD = session('onlyWjt.441@gmail.com', 'st-expired', true)

let precheckCalls = 0
let holdPrecheck: Promise<void> | null = null

function mockFetch() {
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const body = JSON.parse(String(init?.body || '{}'))
    const json = (data: unknown) => new Response(JSON.stringify(data), { status: 200 })
    if (url.endsWith('/session/precheck')) {
      precheckCalls += 1
      if (holdPrecheck) await holdPrecheck
      return String(body.session).includes('st-bad')
        ? json({ valid: false, reason_code: 'GPT_SESSION_INVALID', message: 'bad session' })
        : json({ valid: true, email: 'x@mail.com', current_plan: 'free' })
    }
    if (url.endsWith('/session/refresh')) {
      return json({
        results: body.sessions.map((raw: string) => {
          const s = JSON.parse(raw)
          return { ok: true, session: { ...s, accessToken: `${s.accessToken}-new` } }
        }),
      })
    }
    return new Response('{}', { status: 404 })
  }))
}

function mountView() {
  return mount(SessionInspectView, {
    global: { plugins: [i18n], stubs: { RouterLink: true } },
  })
}

async function paste(wrapper: VueWrapper, value: unknown) {
  await wrapper.find('textarea').setValue(JSON.stringify(value))
  vi.advanceTimersByTime(250)
  await flushPromises()
}

function rowCards(wrapper: VueWrapper) {
  return wrapper.findAll('[data-session-row]')
}

function rowCard(wrapper: VueWrapper, email: string) {
  const card = rowCards(wrapper).find((c) => c.text().includes(email))
  if (!card) throw new Error(`row ${email} not found`)
  return card
}

function buttonIn(scope: VueWrapper | ReturnType<VueWrapper['find']>, label: string) {
  const btn = scope.findAll('button').find((b) => b.text() === label)
  if (!btn) throw new Error(`button ${label} not found: ${scope.findAll('button').map((b) => b.text()).join(' | ')}`)
  return btn
}

async function checkRow(wrapper: VueWrapper, email: string) {
  await buttonIn(rowCard(wrapper, email), '校验').trigger('click')
  await flushPromises()
}

describe('SessionInspectView', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    precheckCalls = 0
    holdPrecheck = null
    mockFetch()
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('has no batch check buttons', async () => {
    const wrapper = mountView()
    await paste(wrapper, [GOOD, BAD])
    const labels = wrapper.findAll('button').map((b) => b.text())
    expect(labels).not.toContain('全部校验')
    expect(labels).not.toContain('查全部订阅')
    expect(labels).not.toContain('刷新全部可刷新')
  })

  it('locks every network button while one request is running', async () => {
    const wrapper = mountView()
    await paste(wrapper, [GOOD, OTHER])
    let release!: () => void
    holdPrecheck = new Promise((r) => (release = r))

    await buttonIn(rowCard(wrapper, 'good@mail.com'), '校验').trigger('click')
    await flushPromises()
    const other = rowCard(wrapper, 'other@mail.com')
    for (const label of ['校验', '查订阅', '刷新 AT']) {
      expect(buttonIn(other, label).attributes('disabled')).toBeDefined()
    }
    expect(precheckCalls).toBe(1)

    release()
    await flushPromises()
    expect(buttonIn(rowCard(wrapper, 'other@mail.com'), '校验').attributes('disabled')).toBeUndefined()
    expect(rowCard(wrapper, 'good@mail.com').text()).toContain('校验通过')
  })

  it('treats an expired AT as a soft hint and drops it once the check passes', async () => {
    const wrapper = mountView()
    await paste(wrapper, EXPIRED_GOOD)
    const before = rowCard(wrapper, 'onlyWjt.441@gmail.com').text()
    expect(before).toContain('accessToken 已过期，不影响校验')
    expect(before).not.toContain('accessToken 已过期\n')

    await checkRow(wrapper, 'onlyWjt.441@gmail.com')
    const after = rowCard(wrapper, 'onlyWjt.441@gmail.com').text()
    expect(after).toContain('校验通过')
    expect(after).not.toContain('accessToken 已过期')
  })

  it('removes failed rows and keeps results on the rest', async () => {
    const wrapper = mountView()
    await paste(wrapper, [GOOD, BAD])
    await checkRow(wrapper, 'good@mail.com')
    await checkRow(wrapper, 'bad@mail.com')
    expect(rowCard(wrapper, 'bad@mail.com').text()).toContain('未通过')

    await buttonIn(wrapper, '移除未通过的 1 条').trigger('click')
    await flushPromises()
    expect(rowCards(wrapper)).toHaveLength(1)
    expect(rowCard(wrapper, 'good@mail.com').text()).toContain('校验通过')
    expect(wrapper.find('textarea').element.value).toBe('')

    // 输入框清空后的重建不能把移除的行带回来
    vi.advanceTimersByTime(250)
    await flushPromises()
    expect(rowCards(wrapper)).toHaveLength(1)
  })

  it('keeps verdicts when another session is pasted', async () => {
    const wrapper = mountView()
    await paste(wrapper, GOOD)
    await buttonIn(wrapper, '加入列表并清空输入框').trigger('click')
    await flushPromises()
    await checkRow(wrapper, 'good@mail.com')

    await paste(wrapper, OTHER)
    expect(rowCards(wrapper)).toHaveLength(2)
    expect(rowCard(wrapper, 'good@mail.com').text()).toContain('校验通过')
    expect(rowCard(wrapper, 'other@mail.com').text()).toContain('未校验')
  })

  it('keeps the refreshed session after pasting more', async () => {
    const wrapper = mountView()
    await paste(wrapper, GOOD)
    await buttonIn(rowCard(wrapper, 'good@mail.com'), '刷新 AT').trigger('click')
    await flushPromises()
    expect(rowCard(wrapper, 'good@mail.com').text()).toContain('已刷新')

    await paste(wrapper, OTHER)
    expect(rowCards(wrapper)).toHaveLength(2)
    expect(rowCard(wrapper, 'good@mail.com').text()).toContain('已刷新')

    const writeText = vi.fn(async (_text: string) => {})
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    await buttonIn(wrapper, '复制全部已刷新').trigger('click')
    await flushPromises()
    expect(String(writeText.mock.calls[0]?.[0])).toContain('st-good-new')
  })
})
