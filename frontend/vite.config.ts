import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig(({ mode }) => ({
  plugins: [vue()],
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': {
        target: loadEnv(mode, '.', 'API_PROXY_TARGET').API_PROXY_TARGET || 'http://127.0.0.1:8080',
        changeOrigin: true,
        timeout: 330_000,
        proxyTimeout: 330_000,
      },
    },
  },
}))
