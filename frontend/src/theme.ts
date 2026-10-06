// 整站主题包：颜色 + 字体 + 圆角 + 密度 + 导航 + 布局壳（原子应用，避免半套换肤）
import { ref, computed } from 'vue'

export type ThemeMode = 'light' | 'dark' | 'auto'
export type SkinId =
  | 'danew'
  | 'zovo'
  | 'terracotta'
  | 'ember'
  | 'ocean'
  | 'cyber'
  | 'forest'
  | 'violet'
  | 'slate'
  | 'rose'
  | 'noir'
  | 'paper'

const MODE_KEY = 'theme-mode'
const SKIN_KEY = 'site-skin'
const BRAND_KEY = 'site-brand'

export interface SiteBrand {
  name: string
  sub: string
}

export interface SkinMeta {
  id: SkinId
  label: string
  labelEn: string
  swatch: string
  swatch2: string
  blurb: string
  heading: 'serif' | 'sans' | 'display' | 'mono'
  density: 'comfy' | 'compact' | 'airy'
  nav: 'pill' | 'underline' | 'block'
  /** 管理端壳层布局 */
  layout: 'top' | 'sidebar' | 'rail'
  /** 是否默认走暗色（如 cyber/ember/noir） */
  preferDark?: boolean
  /** 主色，用于同步 Element Plus */
  primary: string
  primaryOn?: string
  /** 暗色下的主色覆盖。只有需要明暗分别取值的皮肤才填，未填则沿用 primary/primaryOn。 */
  primaryDark?: string
  primaryOnDark?: string
  /** 亮色模式下另用一套主色（同一皮肤明暗两套配色时，如卡台新版 曜夜/流金） */
  primaryLight?: string
  primaryOnLight?: string
  /** 该皮肤用到的 Google 字体（css2 family 参数），切换皮肤时按需加载 */
  fonts?: string[]
}

const MONO_FONT = 'JetBrains+Mono:wght@400;500;600'

export const SKINS: SkinMeta[] = [
  {
    id: 'danew',
    label: 'danew 靛蓝',
    labelEn: 'danew Indigo',
    swatch: '#4b3fce',
    swatch2: '#ff6a3d',
    blurb: '品牌默认 · 靛蓝主色 + 珊瑚橙强调 · 亮暗双模 · WCAG AA',
    heading: 'sans',
    density: 'comfy',
    nav: 'pill',
    layout: 'top',
    primary: '#4b3fce',
    primaryOn: '#ffffff',
    primaryDark: '#8e86f5',
    primaryOnDark: '#16132e',
    fonts: ['Plus+Jakarta+Sans:wght@600;700;800', 'Inter:wght@400;500;600;700'],
  },
  {
    id: 'zovo',
    label: '卡台新版',
    labelEn: 'Mist Gold',
    swatch: '#cbb079',
    swatch2: '#0c0c11',
    blurb: '曜夜雾金 · 日间流金 · 玻璃面板',
    heading: 'display',
    density: 'comfy',
    nav: 'pill',
    layout: 'top',
    primary: '#cbb079',
    primaryOn: '#07070a',
    primaryLight: '#9c7c3c',
    primaryOnLight: '#fffdf7',
    fonts: ['Manrope:wght@400;500;600;700', 'Sora:wght@500;600;700', 'Cormorant+Garamond:wght@500;600;700', 'Noto+Serif+SC:wght@500;600;700'],
  },
  {
    id: 'terracotta',
    label: '赤陶奶油',
    labelEn: 'Terracotta',
    swatch: '#c0563a',
    swatch2: '#f2ede4',
    blurb: '衬线标题 · 顶栏药丸 · 温暖运营台',
    heading: 'serif',
    density: 'comfy',
    nav: 'pill',
    layout: 'top',
    primary: '#c0563a',
  },
  {
    id: 'cyber',
    label: '赛博科技',
    labelEn: 'Cyber',
    swatch: '#22d3ee',
    swatch2: '#0b1020',
    blurb: '侧栏布局 · 网格底 · 霓虹描边 · 等宽数据',
    heading: 'mono',
    density: 'compact',
    nav: 'block',
    layout: 'sidebar',
    preferDark: true,
    primary: '#22d3ee',
    primaryOn: '#041016',
  },
  {
    id: 'ocean',
    label: '海洋控制台',
    labelEn: 'Ocean',
    swatch: '#2563eb',
    swatch2: '#eef4fb',
    blurb: '顶栏块状导航 · 直角克制 · 产品台',
    heading: 'sans',
    density: 'compact',
    nav: 'block',
    layout: 'top',
    primary: '#2563eb',
  },
  {
    id: 'ember',
    label: '余烬暗夜',
    labelEn: 'Ember',
    swatch: '#ff854a',
    swatch2: '#140b08',
    blurb: '暗色沉浸 · 暖橙霓虹',
    heading: 'display',
    density: 'comfy',
    nav: 'pill',
    layout: 'top',
    preferDark: true,
    primary: '#ff854a',
    primaryOn: '#1a0c08',
  },
  {
    id: 'forest',
    label: '森林清新',
    labelEn: 'Forest',
    swatch: '#047857',
    swatch2: '#eef6f1',
    blurb: '空气感留白 · 轻圆角',
    heading: 'sans',
    density: 'airy',
    nav: 'pill',
    layout: 'top',
    primary: '#047857',
  },
  {
    id: 'violet',
    label: '紫罗兰',
    labelEn: 'Violet',
    swatch: '#7c3aed',
    swatch2: '#f4f0fb',
    blurb: '展示字体 · 底栏强调',
    heading: 'display',
    density: 'comfy',
    nav: 'underline',
    layout: 'top',
    primary: '#7c3aed',
  },
  {
    id: 'slate',
    label: '石板极简',
    labelEn: 'Slate',
    swatch: '#475569',
    swatch2: '#f1f5f9',
    blurb: '细侧轨 · 数据密集',
    heading: 'mono',
    density: 'compact',
    nav: 'block',
    layout: 'rail',
    primary: '#475569',
  },
  {
    id: 'rose',
    label: '玫瑰杂志',
    labelEn: 'Rose',
    swatch: '#e11d48',
    swatch2: '#fdf2f4',
    blurb: '杂志衬线 · 大标题',
    heading: 'serif',
    density: 'airy',
    nav: 'underline',
    layout: 'top',
    primary: '#e11d48',
  },
  {
    id: 'noir',
    label: '墨黑工坊',
    labelEn: 'Noir',
    swatch: '#e5e5e5',
    swatch2: '#0a0a0a',
    blurb: '纯黑白 · 硬朗直角',
    heading: 'display',
    density: 'compact',
    nav: 'block',
    layout: 'top',
    preferDark: true,
    primary: '#fafafa',
    primaryOn: '#0a0a0a',
  },
  {
    id: 'paper',
    label: '纸感笔记',
    labelEn: 'Paper',
    swatch: '#8b7355',
    swatch2: '#f7f3ea',
    blurb: '纸纹 · 衬线正文',
    heading: 'serif',
    density: 'comfy',
    nav: 'underline',
    layout: 'top',
    primary: '#8b5e34',
  },
]

export const DEFAULT_SKIN: SkinId = 'danew'

/** 品牌名/副标题的中性兜底，必须与后端 settings.go 的 settingOr 默认值保持一致 */
export const DEFAULT_BRAND_NAME = 'Recharge Portal'
export const DEFAULT_BRAND_SUB = 'Account Upgrade Service'

function readSkin(): SkinId {
  const v = localStorage.getItem(SKIN_KEY) as SkinId
  return SKINS.some((s) => s.id === v) ? v : DEFAULT_SKIN
}

export const themeMode = ref<ThemeMode>((localStorage.getItem(MODE_KEY) as ThemeMode) || 'light')
export const siteSkin = ref<SkinId>(readSkin())

function loadBrand(): SiteBrand {
  try {
    const raw = localStorage.getItem(BRAND_KEY)
    if (raw) {
      const o = JSON.parse(raw)
      if (o?.name) return { name: String(o.name), sub: String(o.sub || '') }
    }
  } catch { /* ignore */ }
  return { name: DEFAULT_BRAND_NAME, sub: DEFAULT_BRAND_SUB }
}

export const siteBrand = ref<SiteBrand>(loadBrand())

function systemPrefersDark(): boolean {
  return !!(window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches)
}

export function isDark(): boolean {
  // preferDark 皮肤在 light 模式下仍用皮肤自身暗色 token；.dark 类只在用户选 dark/auto-dark 时加
  if (themeMode.value === 'auto') return systemPrefersDark()
  return themeMode.value === 'dark'
}

export const currentSkinMeta = computed(() => SKINS.find((s) => s.id === siteSkin.value) || SKINS[0])

/** 由主色生成 EP light/dark 阶梯 */
function hexToRgb(hex: string): [number, number, number] | null {
  const h = hex.replace('#', '').trim()
  if (h.length === 3) {
    return [parseInt(h[0] + h[0], 16), parseInt(h[1] + h[1], 16), parseInt(h[2] + h[2], 16)]
  }
  if (h.length !== 6) return null
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)]
}

function mix(a: number, b: number, t: number) {
  return Math.round(a + (b - a) * t)
}

function mixHex(hex: string, toward: 'white' | 'black', t: number): string {
  const rgb = hexToRgb(hex)
  if (!rgb) return hex
  const target = toward === 'white' ? 255 : 0
  const [r, g, b] = rgb.map((c) => mix(c, target, t))
  return `#${[r, g, b].map((x) => x.toString(16).padStart(2, '0')).join('')}`
}

function syncElementPlus(primary: string, primaryOn: string, dark: boolean) {
  const root = document.documentElement
  const set = (k: string, v: string) => root.style.setProperty(k, v)
  const toward = dark ? 'black' : 'white'
  set('--el-color-primary', primary)
  set('--el-color-primary-light-3', mixHex(primary, toward, 0.3))
  set('--el-color-primary-light-5', mixHex(primary, toward, 0.5))
  set('--el-color-primary-light-7', mixHex(primary, toward, 0.7))
  set('--el-color-primary-light-8', mixHex(primary, toward, 0.8))
  set('--el-color-primary-light-9', mixHex(primary, toward, 0.9))
  set('--el-color-primary-dark-2', mixHex(primary, dark ? 'white' : 'black', 0.2))
  // 按钮跟主色，禁止写死赤陶
  set('--el-button-bg-color', primary)
  set('--el-button-border-color', primary)
  set('--el-button-hover-bg-color', mixHex(primary, 'white', 0.12))
  set('--el-button-hover-border-color', mixHex(primary, 'white', 0.12))
  set('--el-button-active-bg-color', mixHex(primary, 'black', 0.12))
  set('--el-button-active-border-color', mixHex(primary, 'black', 0.12))
  set('--el-button-text-color', primaryOn)
  set('--el-color-white', primaryOn)
  set('--el-font-family', 'var(--font-sans)')
  set('--el-border-radius-base', 'var(--radius-md)')
  // 同步语义色给 EP
  set('--el-bg-color', 'var(--surface)')
  set('--el-bg-color-page', 'var(--bg)')
  set('--el-bg-color-overlay', 'var(--surface-solid, var(--surface))')
  set('--el-fill-color-blank', 'var(--surface)')
  set('--el-text-color-primary', 'var(--ink)')
  set('--el-text-color-regular', 'var(--ink-2)')
  set('--el-text-color-secondary', 'var(--ink-3)')
  set('--el-border-color', 'var(--brd)')
  set('--el-border-color-light', 'var(--brd)')
  set('--el-border-color-lighter', 'var(--brd)')
  set('--el-mask-color', 'rgba(0,0,0,.45)')
}

function applyMode() {
  document.documentElement.classList.toggle('dark', isDark())
}

function applySkin() {
  const meta = currentSkinMeta.value
  const root = document.documentElement
  // 原子写入：同一帧内改完所有属性，减少半套样式
  root.setAttribute('data-skin', meta.id)
  root.setAttribute('data-heading', meta.heading)
  root.setAttribute('data-density', meta.density)
  root.setAttribute('data-nav', meta.nav)
  root.setAttribute('data-layout', meta.layout)
  // preferDark 皮肤：若用户未显式选 light，自动保证暗色 class 与皮肤一致
  if (meta.preferDark && themeMode.value === 'light') {
    // 允许 light，但皮肤 CSS 本身已是暗色 token；不强制改 mode
  }
  const dark = isDark() || !!meta.preferDark
  const primary = dark && meta.primaryDark
    ? meta.primaryDark
    : !dark && meta.primaryLight
      ? meta.primaryLight
      : meta.primary
  const primaryOn = dark && meta.primaryOnDark
    ? meta.primaryOnDark
    : !dark && meta.primaryOnLight
      ? meta.primaryOnLight
      : (meta.primaryOn || '#ffffff')
  syncElementPlus(primary, primaryOn, dark)
  root.style.colorScheme = dark ? 'dark' : 'light'
  ensureSkinFonts(meta)
}

const loadedFontHref = new Set<string>()
function ensureSkinFonts(meta: SkinMeta) {
  const families = [...(meta.fonts || []), MONO_FONT].map((f) => `family=${f}`).join('&')
  const href = `https://fonts.googleapis.com/css2?${families}&display=swap`
  if (loadedFontHref.has(href)) return
  loadedFontHref.add(href)
  const link = document.createElement('link')
  link.rel = 'stylesheet'
  link.href = href
  link.dataset.skinFonts = meta.id
  document.head.appendChild(link)
}

/** 统一入口：皮肤 + 明暗 一次刷完 */
export function applyThemeAll() {
  applyMode()
  applySkin()
}

export function setTheme(mode: ThemeMode) {
  themeMode.value = mode
  localStorage.setItem(MODE_KEY, mode)
  applyThemeAll()
}

export function toggleTheme() {
  setTheme(isDark() ? 'light' : 'dark')
}

export function setSkin(id: SkinId) {
  if (!SKINS.some((s) => s.id === id)) return
  siteSkin.value = id
  localStorage.setItem(SKIN_KEY, id)
  const meta = SKINS.find((s) => s.id === id)!
  // 切到 preferDark 皮肤时，若当前是 light 且用户没刻意锁 light，可自动转 dark 以完整适配
  if (meta.preferDark && themeMode.value === 'light') {
    themeMode.value = 'dark'
    localStorage.setItem(MODE_KEY, 'dark')
  }
  applyThemeAll()
}

export function setSiteBrand(patch: Partial<SiteBrand>) {
  siteBrand.value = {
    name: (patch.name ?? siteBrand.value.name).trim() || DEFAULT_BRAND_NAME,
    sub: (patch.sub ?? siteBrand.value.sub).trim(),
  }
  localStorage.setItem(BRAND_KEY, JSON.stringify(siteBrand.value))
}

export function initTheme() {
  applyThemeAll()
  if (window.matchMedia) {
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
      if (themeMode.value === 'auto') applyThemeAll()
    })
  }
}
