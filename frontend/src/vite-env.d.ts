/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** 'true' 时开放代理端 /partner 与代理换码（默认关闭） */
  readonly VITE_ENABLE_AGENT?: string
  readonly VITE_ENABLE_X_DIRECT?: string
}

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<object, object, unknown>
  export default component
}
