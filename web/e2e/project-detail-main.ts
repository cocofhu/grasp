import '../src/styles/global.css'
import { createApp, h } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { setTheme } from '../src/lib/shared/theme'
import { vHoverInk } from '../src/lib/shared/hoverInkDirective'
import AppShell from '../src/components/shell/AppShell.vue'
import ProjectDetailView from '../src/views/ProjectDetailView.vue'

installIdleScrollbar()

async function bootstrap() {
  const params = new URLSearchParams(window.location.search)
  await initLocale()
  // Match project-list / sandbox-list harnesses: ?lang=en for English UI acceptance.
  await setLocale(params.get('lang') === 'en' ? 'en' : 'zh-CN')

  if (params.get('theme') === 'light') {
    setTheme('light')
  }
  const startTab = params.get('tab') || ''
  const path = startTab
    ? `/projects/proj-1?tab=${encodeURIComponent(startTab)}`
    : '/projects/proj-1'

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/projects/:id', component: ProjectDetailView },
      { path: '/projects', component: { render: () => h('div', { 'data-testid': 'projects-page' }, 'projects') } },
      { path: '/workflows/:id/edit', component: { render: () => h('div', { 'data-testid': 'edit-page' }, 'edit') } },
      { path: '/workflows/new/edit', component: { render: () => h('div', { 'data-testid': 'new-edit-page' }, 'new') } },
      { path: '/runs/:id', component: { render: () => h('div', { 'data-testid': 'run-page' }, 'run') } },
      { path: '/runs', component: { render: () => h('div', { 'data-testid': 'runs-page' }, 'runs') } },
    ],
  })

  await router.push(path)

  // Mirror in-memory route tab onto the real page URL so Playwright can assert ?tab=.
  router.afterEach((to) => {
    const url = new URL(window.location.href)
    const tab = typeof to.query.tab === 'string' ? to.query.tab : ''
    if (tab) url.searchParams.set('tab', tab)
    else url.searchParams.delete('tab')
    window.history.replaceState(window.history.state, '', url.toString())
  })
  // Sync current route once after initial push/ensureTabQuery settles.
  {
    const url = new URL(window.location.href)
    const tab = typeof router.currentRoute.value.query.tab === 'string' ? router.currentRoute.value.query.tab : ''
    if (tab) url.searchParams.set('tab', tab)
    else url.searchParams.delete('tab')
    window.history.replaceState(window.history.state, '', url.toString())
  }

  createApp({
    render: () => h(AppShell, null, { default: () => h(RouterView) }),
  })
    .directive('hover-ink', vHoverInk)
    .use(i18n)
    .use(router)
    .mount('#app')
}

void bootstrap()
