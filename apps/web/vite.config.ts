/// <reference types="vitest/config" />
import path from 'node:path'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

const apiProxy = process.env.VITE_API_PROXY || 'http://127.0.0.1:8080'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: Number(process.env.HIGHLAND_WEB_PORT || 5173),
    strictPort: true,
    proxy: {
      '/auth': {
        target: apiProxy,
        changeOrigin: true,
      },
      '/api': {
        target: apiProxy,
        changeOrigin: true,
      },
      '/healthz': {
        target: apiProxy,
        changeOrigin: true,
      },
      '/readyz': {
        target: apiProxy,
        changeOrigin: true,
      },
    },
  },
  preview: {
    port: Number(process.env.HIGHLAND_WEB_PORT || 4173),
    strictPort: true,
    proxy: {
      '/auth': { target: apiProxy, changeOrigin: true },
      '/api': { target: apiProxy, changeOrigin: true },
      '/healthz': { target: apiProxy, changeOrigin: true },
      '/readyz': { target: apiProxy, changeOrigin: true },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.ts',
    // Playwright specs live in e2e/ — do not run them under Vitest
    exclude: [
      '**/node_modules/**',
      '**/dist/**',
      '**/e2e/**',
      '**/*.e2e.*',
    ],
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
  },
})
