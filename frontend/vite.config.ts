import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
        timeout: 200000,
        proxyTimeout: 200000,
      },
    },
  },
  build: {
    outDir: 'dist',
    // 生产包不再带 sourcemap，避免把完整源码跟构建产物一起发出去。
    sourcemap: false,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) return
          if (id.includes('element-plus') || id.includes('@element-plus')) return 'vendor-element'
          if (id.includes('chart.js') || id.includes('vue-chartjs')) return 'vendor-chart'
          if (id.includes('xlsx')) return 'vendor-xlsx'
          if (/[\\/]node_modules[\\/](vue|@vue|vue-router|pinia|vue-i18n|@intlify)[\\/]/.test(id)) return 'vendor-vue'
        },
      },
    },
  },
})
