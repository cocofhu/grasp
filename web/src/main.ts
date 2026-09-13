import './lib/shared/locale'
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { i18n } from './lib/shared/i18n'
import { initLocale } from './lib/shared/locale'
import { installIdleScrollbar } from './lib/shared/idleScrollbar'
import { vHoverInk } from './lib/shared/hoverInkDirective'

import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import '@vue-flow/controls/dist/style.css'
import '@vue-flow/minimap/dist/style.css'
import './styles/global.css'
import './lib/shared/theme'

installIdleScrollbar()

async function bootstrap() {
  const localeReady = initLocale()
  const app = createApp(App).use(createPinia()).use(i18n).use(router)
  app.directive('hover-ink', vHoverInk)
  // Mount immediately so brand / login shell can paint; locale fills in as JSON arrives.
  app.mount('#app')
  await localeReady
}

void bootstrap()
