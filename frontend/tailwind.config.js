/** @type {import('tailwindcss').Config} */
export default {
  darkMode: 'class',
  content: [
    "./index.html",
    "./src/**/*.{vue,js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      fontFamily: {
        serif: [
          'Source Serif 4', 'Georgia', 'Noto Serif SC', 'Songti SC', 'STSong', 'SimSun', 'serif',
        ],
        // 正文与表格走 Inter：Jakarta 的 x-height 在 12–13px 密集表格里辨识度明显下降
        sans: [
          'Inter', '-apple-system', 'BlinkMacSystemFont', 'PingFang SC',
          'HarmonyOS Sans SC', 'Microsoft YaHei', 'Noto Sans SC', 'Arial', 'sans-serif',
        ],
        // 标题与 logo 专用
        display: [
          'Plus Jakarta Sans', 'Inter', 'PingFang SC',
          'HarmonyOS Sans SC', 'Microsoft YaHei', 'Noto Sans SC', 'sans-serif',
        ],
        mono: [
          'JetBrains Mono', 'ui-monospace', 'SFMono-Regular', 'Menlo', 'monospace',
        ],
      },
      transitionTimingFunction: {
        standard: 'cubic-bezier(.2,0,0,1)',
        entrance: 'cubic-bezier(.16,1,.3,1)',
      },
      colors: {
        // ── danew brand ramps ──
        // 只加静态色阶，不加 primary / surface 这类语义别名：
        // 现有页面里已经存在同名的 scoped class（RechargeView 的 .text-good /
        // .border-primary / .bg-primary\/10 等），加语义别名会让 Tailwind 生成同名
        // utility 并改变这些页面的既有表现。语义色统一走 CSS 变量（见 style.css）。
        brand: {
          50: '#eef0fe',
          100: '#dde0fd',
          200: '#bec3fa',
          300: '#9a9ef5',
          400: '#7a79ec',
          500: '#6055e0',
          600: '#4b3fce',
          700: '#3b31a8',
          800: '#2e2782',
          900: '#221d5f',
          950: '#15123a',
        },
        // 品牌强调色：500 仅做大色块，700 才是可用于文字与图标的值（白底 5.65:1）
        coral: {
          50: '#fff3ee',
          100: '#ffe3d8',
          200: '#ffc7b1',
          300: '#ffa485',
          400: '#ff8a5c',
          500: '#ff6a3d',
          600: '#e8501f',
          700: '#b93c13',
          800: '#8f2f10',
          900: '#6b240e',
        },
        // 微冷中性，同时压得住靛蓝和珊瑚橙；不叫 slate 以免覆盖 Tailwind 内建色
        slate2: {
          0: '#ffffff',
          50: '#f7f8fa',
          100: '#eff1f5',
          200: '#e3e6ec',
          300: '#cbd0da',
          400: '#9aa1b1',
          500: '#828a9b',
          600: '#616878',
          700: '#545b6b',
          800: '#3e4451',
          900: '#1a1d25',
          950: '#101218',
        },
        // warm terracotta brand
        terracotta: {
          50: '#f7e9e2',
          100: '#efcfc4',
          200: '#e7bbac',
          300: '#d9947d',
          400: '#ce7558',
          500: '#c0563a',
          600: '#9c4530',
          700: '#7d3626',
        },
        // cream light surfaces
        cream: {
          50: '#fbf8f1',
          100: '#f6f1e7',
          200: '#f2ede4',
          300: '#e7ddcb',
          400: '#dccfb8',
        },
        // warm charcoal dark surfaces
        warm: {
          950: '#17120e',
          900: '#221a14',
          800: '#2b2118',
          700: '#372c21',
          600: '#46382b',
        },
        ink: {
          DEFAULT: '#2a2622',
          muted: '#6f6657',
          subtle: '#9a9183',
        },
      },
    },
  },
  plugins: [],
}
