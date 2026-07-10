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
  build: {
    // Split heavy libraries into their own cacheable chunks so the main app
    // bundle stays small and vendor code is fetched in parallel / cached across
    // deploys.
    rollupOptions: {
      output: {
        manualChunks(id: string) {
          if (!id.includes('node_modules')) return
          if (id.includes('recharts') || id.includes('d3-')) return 'recharts'
          if (id.includes('@tanstack')) return 'tanstack'
          if (id.includes('react-router') || id.includes('react-dom') || /node_modules\/react\//.test(id))
            return 'react-vendor'
        },
      },
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
