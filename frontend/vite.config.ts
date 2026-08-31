import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
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
    sourcemap: true,
  },
})
