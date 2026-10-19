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

function session(email: string, st: string) {
  return { user: { email }, accessToken: `eyJa.eyJb.${st}`, sessionToken: st }
}

const GOOD = session('good@mail.com', 'st-good')
const BAD = session('bad@mail.com', 'st-bad')
const OTHER = session('other@mail.com', 'st-other')

let precheckCalls = 0

function mockFetch() {
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const body = JSON.parse(String(init?.body || '{}'))
    const json = (data: unknown) => new Response(JSON.stringify(data), { status: 200 })
    if (url.endsWith('/session/precheck')) {
      precheckCalls += 1
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

async function mountView() {
  const wrapper = mount(SessionInspectView, {
    global: { plugins: [i18n], stubs: { RouterLink: true, LanguageToggle: true, ThemeToggle: true } },
  })
  return wrapper
}

async function paste(wrapper: VueWrapper, value: unknown) {
  await wrapper.find('textarea').setValue(JSON.stringify(value))
  vi.advanceTimersByTime(250)
  await flushPromises()
}

function rowCards(wrapper: VueWrapper) {
  return wrapper.findAll('.space-y-3 > div')
}

function rowText(wrapper: VueWrapper, email: string) {
  return rowCards(wrapper).find((c) => c.text().includes(email))?.text() || ''
}

async function clickButton(wrapper: VueWrapper, label: string | RegExp) {
  const btn = wrapper.findAll('button').find((b) => (typeof label === 'string' ? b.text() === label : label.test(b.text())))
  if (!btn) throw new Error(`button ${label} not found: ${wrapper.findAll('button').map((b) => b.text()).join(' | ')}`)
  await btn.trigger('click')
  await flushPromises()
}

describe('SessionInspectView row state', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    precheckCalls = 0
    mockFetch()
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('removes failed rows and keeps results on the rest', async () => {
    const wrapper = await mountView()
    await paste(wrapper, [GOOD, BAD])
    await clickButton(wrapper, '全部校验')

    expect(rowText(wrapper, 'good@mail.com')).toContain('校验通过')
    expect(rowText(wrapper, 'bad@mail.com')).toContain('未通过')

    await clickButton(wrapper, '移除未通过的 1 条')
    expect(rowCards(wrapper)).toHaveLength(1)
    expect(rowText(wrapper, 'good@mail.com')).toContain('校验通过')
    expect(wrapper.find('textarea').element.value).toBe('')

    // 输入框清空后的重建不能把移除的行带回来
    vi.advanceTimersByTime(250)
    await flushPromises()
    expect(rowCards(wrapper)).toHaveLength(1)
  })

  it('keeps verdicts when another session is pasted', async () => {
    const wrapper = await mountView()
    await paste(wrapper, GOOD)
    await clickButton(wrapper, '加入列表并清空输入框')
    await clickButton(wrapper, '全部校验')
    expect(rowText(wrapper, 'good@mail.com')).toContain('校验通过')

    await paste(wrapper, OTHER)
    expect(rowCards(wrapper)).toHaveLength(2)
    expect(rowText(wrapper, 'good@mail.com')).toContain('校验通过')
    expect(rowText(wrapper, 'other@mail.com')).not.toContain('校验通过')

    // 已通过的不再重复校验
    await clickButton(wrapper, '全部校验')
    expect(precheckCalls).toBe(2)
  })

  it('keeps the refreshed session after pasting more', async () => {
    const wrapper = await mountView()
    await paste(wrapper, GOOD)
    await clickButton(wrapper, '刷新 AT')
    expect(rowText(wrapper, 'good@mail.com')).toContain('已刷新')

    await paste(wrapper, OTHER)
    expect(rowCards(wrapper)).toHaveLength(2)
    expect(rowText(wrapper, 'good@mail.com')).toContain('已刷新')

    const writeText = vi.fn(async (_text: string) => {})
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    await clickButton(wrapper, '复制全部已刷新')
    expect(String(writeText.mock.calls[0]?.[0])).toContain('eyJa.eyJb.st-good-new')
  })
})
