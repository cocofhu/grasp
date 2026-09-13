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
  const start = params.get('start') || '/gates'

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/gates', component: stub('page-gates', '待审批页') },
      { path: '/projects', component: stub('page-projects', '项目页') },
      { path: '/runs/:id', component: stub('page-run', '运行详情'), meta: { full: true } },
      { path: '/workflows/:id/edit', component: stub('page-editor', '流水线编辑器'), meta: { full: true } },
      { path: '/sandboxes/:id/console', component: stub('page-console', '沙箱控制台'), meta: { full: true } },
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
