import { defineConfig, type Plugin } from 'vite'
import { fileURLToPath, URL } from 'node:url'

// PageController lazily imports its WebGL mask (ai-motion); Grasp draws its own
// mask, so that chunk is replaced with an empty module.
function stubSimulatorMask(): Plugin {
  const id = '\0grasp-simulator-mask-stub'
  return {
    name: 'grasp-stub-simulator-mask',
    enforce: 'pre',
    resolveId(source) {
      return /SimulatorMask-[\w-]+\.js$/.test(source) ? id : null
    },
    load(source) {
      return source === id ? 'export class SimulatorMask {}' : null
    },
  }
}

// Builds public/page-control.js: the executor preview-pick.js loads when the user
// lets the agent operate the page. Run `npm run build:page-control` to refresh it
// and its copies.
export default defineConfig({
  plugins: [stubSimulatorMask()],
  publicDir: false,
  build: {
    outDir: 'dist-page-control',
    emptyOutDir: true,
    minify: true,
    target: 'es2019',
    lib: {
      entry: fileURLToPath(new URL('./src/pagecontrol/entry.ts', import.meta.url)),
      name: 'GraspPageControl',
      formats: ['iife'],
      fileName: () => 'page-control.js',
    },
    rollupOptions: {
      output: { inlineDynamicImports: true },
      // The eval lives in PageController.executeJavascript, which the executor never calls.
      onwarn(warning, warn) {
        if (warning.code === 'EVAL' && warning.id?.includes('@page-agent/page-controller')) return
        warn(warning)
      },
    },
  },
})
