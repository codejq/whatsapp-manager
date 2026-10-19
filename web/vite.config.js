import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'

const outDir = resolve(__dirname, '../internal/ui/dist')

export default defineConfig({
  plugins: [
    vue(),
    tailwindcss(),
    {
      // Keep the placeholder so `go build` works on a clean checkout.
      name: 'keep-gitkeep',
      closeBundle() { writeFileSync(resolve(outDir, '.gitkeep'), '') },
    },
  ],
  base: './',
  build: {
    outDir,
    emptyOutDir: true,
    assetsInlineLimit: 0,
    chunkSizeWarningLimit: 1500,
  },
  server: {
    // `npm run dev` proxies the API to a running app (set WAM_API).
    proxy: { '/api': process.env.WAM_API || 'http://127.0.0.1:39200' },
  },
})
