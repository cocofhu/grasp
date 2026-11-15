import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  root: fileURLToPath(new URL('./.rail-harness', import.meta.url)),
  plugins: [vue()],
  css: { postcss: fileURLToPath(new URL('.', import.meta.url)) },
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: { fs: { allow: [fileURLToPath(new URL('.', import.meta.url))] } },
})
