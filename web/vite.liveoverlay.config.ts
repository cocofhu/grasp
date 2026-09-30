import { defineConfig } from 'vite'
import { fileURLToPath, URL } from 'node:url'

// Builds public/live-overlay.js: the Live variants overlay preview-pick.js loads
// when the drawer reports Live is on for the node. Run `npm run build:live-overlay`
// to refresh it and its copies.
export default defineConfig({
  publicDir: false,
  build: {
    outDir: 'dist-live-overlay',
    emptyOutDir: true,
    minify: true,
    target: 'es2019',
    lib: {
      entry: fileURLToPath(new URL('./src/liveoverlay/entry.ts', import.meta.url)),
      name: 'GraspLiveOverlay',
      formats: ['iife'],
      fileName: () => 'live-overlay.js',
    },
    rollupOptions: { output: { inlineDynamicImports: true } },
  },
})
