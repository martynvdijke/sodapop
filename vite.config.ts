import { defineConfig } from 'vite'
import { fileURLToPath } from 'node:url'

export default defineConfig({
  root: 'web',
  base: '/static/',
  build: {
    outDir: '../internal/web/static',
    emptyOutDir: false,
    manifest: true,
    sourcemap: false,
    target: 'es2022',
    rollupOptions: {
      input: {
        main: fileURLToPath(new URL('./web/src/main.ts', import.meta.url)),
        sw: fileURLToPath(new URL('./web/src/sw.ts', import.meta.url)),
      },
      output: {
        entryFileNames: (chunk) =>
          chunk.name === 'sw' ? 'sw.js' : 'assets/[name]-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]',
      },
    },
  },
})
