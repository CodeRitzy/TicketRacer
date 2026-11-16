import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// In dev, /api goes to the Go server; in Docker, nginx does the same job.
export default defineConfig({
  plugins: [react()],
  // never inline assets as data: URIs; the CSP only allows same-origin fonts and scripts
  build: { assetsInlineLimit: 0 },
  server: { port: 5173, proxy: { '/api': { target: 'http://127.0.0.1:8081', changeOrigin: false } } },
  test: { environment: 'node' },
})
