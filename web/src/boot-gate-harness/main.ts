import { createApp } from 'vue'
import { i18n } from '@/lib/shared/i18n'
import { initLocale } from '@/lib/shared/locale'
import { vHoverInk } from '@/lib/shared/hoverInkDirective'
import '@/styles/global.css'
import '@/lib/shared/theme'
import Harness from './Harness.vue'

async function bootstrap() {
  await initLocale()
  const app = createApp(Harness)
  app.use(i18n)
  app.directive('hover-ink', vHoverInk)
  app.mount('#app')
}

void bootstrap()
