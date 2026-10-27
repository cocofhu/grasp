import { createApp } from 'vue'
import { createI18n } from 'vue-i18n'
import common from './src/locales/zh-CN/common.json'
import pages from './src/locales/zh-CN/pages.json'
import Harness from './TimelineHarness.vue'
import './src/styles/global.css'

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  fallbackLocale: 'zh-CN',
  missingWarn: false,
  fallbackWarn: false,
  messages: { 'zh-CN': { ...common, ...pages } },
})

createApp(Harness).use(i18n).mount('#app')
