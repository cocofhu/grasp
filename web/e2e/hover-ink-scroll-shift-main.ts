import '../src/styles/global.css'
import { createApp, h } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { vHoverInk } from '../src/lib/shared/hoverInkDirective'
import AppShell from '../src/components/shell/AppShell.vue'
import { useAuth } from '../src/lib/composables/useAuth'

installIdleScrollbar()

function stub(testid: string, label: string) {
  return { render: () => h('div', { 'data-testid': testid }, label) }
}

async function bootstrap() {
  await initLocale()
  await setLocale('zh-CN')
  useAuth().setUser({ username: 'admin', expiresAt: '2099-01-01T00:00:00Z', isAdmin: true })

  const params = new URLSearchParams(window.location.search)
  const start = params.get('start') || '/dashboard'

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/dashboard', component: stub('page-dashboard', '开始页') },
      { path: '/gates', component: stub('page-gates', '待办页') },
      { path: '/runs', component: stub('page-runs', '运行页') },
      { path: '/settings', component: stub('page-settings', '设置页') },
      { path: '/settings/platform-rules', component: stub('page-platform-rules', '平台规则') },
      { path: '/notifications', component: stub('page-notifications', '通知页') },
    ],
  })
  await router.push(start)

  const app = createApp({
    render: () => h(AppShell, null, { default: () => h(RouterView) }),
  })
  app.directive('hover-ink', vHoverInk)
  app.use(i18n).use(router).mount('#app')
}

void bootstrap()
