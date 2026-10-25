import { createApp } from 'vue'
import { createI18n } from 'vue-i18n'
import { createRouter, createWebHistory } from 'vue-router'
import pages from '@/locales/zh-CN/pages.json'
import RollPreview from './RollPreview.vue'
import '@/styles/global.css'
import '@/lib/shared/theme'

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  messages: { 'zh-CN': { ...pages } },
})

const router = createRouter({
  history: createWebHistory(),
  routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }],
})

createApp(RollPreview).use(i18n).use(router).mount('#app')
