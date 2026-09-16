import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// 后端地址:默认本机 8081,可用 HME_API_TARGET 覆盖(./dev.sh 会按选定端口自动注入)。
const apiTarget = process.env.HME_API_TARGET ?? 'http://127.0.0.1:8081'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
    // 前端整体内嵌进 Go 二进制后单文件分发,拆包只会增加请求数,
    // 因此这里直接放宽体积告警阈值(Ant Design 运行时约 1.2 MB / gzip 375 KB)。
    chunkSizeWarningLimit: 1500,
  },
  server: {
    proxy: {
      '/api': {
        target: apiTarget,
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    globals: true,
  },
})
