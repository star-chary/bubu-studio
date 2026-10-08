import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig(({ mode }) => ({
  base: '/admin/',
  plugins: [vue()],
  server: {
    port: 5176, strictPort: true,
    proxy: {
      '/api': { target: loadEnv(mode, '.', 'API_PROXY_TARGET').API_PROXY_TARGET || 'http://127.0.0.1:8080', changeOrigin: true },
    },
  },
}))
