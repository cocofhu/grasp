import '../src/styles/global.css'
import { createApp } from 'vue'
import { createMemoryHistory } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import { vHoverInk } from '../src/lib/shared/hoverInkDirective'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { useAuth } from '../src/lib/composables/useAuth'
import { createAppRouter } from '../src/router'
import App from '../src/App.vue'

async function bootstrap() {
  const params = new URLSearchParams(window.location.search)
  installIdleScrollbar()
  await initLocale()
  await setLocale(params.get('lang') === 'en' ? 'en' : 'zh-CN')
  setTheme(params.get('theme') === 'dark' ? 'dark' : 'light')
  useAuth().setUser({ username: 'workspace-config-test', isAdmin: true, expiresAt: '2099-01-01T00:00:00Z' })
  const router = createAppRouter(createMemoryHistory())
  await router.push(params.get('view') || '/settings/workspace')
  await router.isReady()
  createApp(App).directive('hover-ink', vHoverInk).use(i18n).use(router).mount('#app')
}
void bootstrap()
