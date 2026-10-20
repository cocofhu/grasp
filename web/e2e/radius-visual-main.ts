import '../src/styles/global.css'
import { createApp, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import common from '../src/locales/zh-CN/common.json'
import pages from '../src/locales/zh-CN/pages.json'
import shell from '../src/locales/zh-CN/shell.json'
import AppSidebar from '../src/components/shell/AppSidebar.vue'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { useAuth } from '../src/lib/composables/useAuth'

installIdleScrollbar()

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  messages: { 'zh-CN': { ...common, ...pages, ...shell } },
})

const router = createRouter({
  history: createMemoryHistory(),
  routes: [
    { path: '/', component: { template: '<div />' }, meta: {} },
    { path: '/login', component: { template: '<div />' } },
    { path: '/dashboard', component: { template: '<div />' }, meta: {} },
  ],
})

const App = {
  setup() {
    const { setUser } = useAuth()
    setUser({ username: 'admin', isAdmin: true })
    const theme = ref<'light' | 'dark'>('light')
    const setTheme = (next: 'light' | 'dark') => {
      theme.value = next
      document.documentElement.classList.toggle('light', next === 'light')
    }
    setTheme('light')
    return () =>
      h(
        'div',
        {
          class: 'app-shell-dotgrid min-h-screen p-6',
          'data-testid': 'radius-visual-root',
          style: 'display:grid; grid-template-columns: 304px 1fr; gap: 24px; min-height: 100vh;',
        },
        [
          h('div', { style: 'height: 720px;' }, [h(AppSidebar)]),
          h('main', { class: 'space-y-4', 'data-testid': 'radius-samples' }, [
            h('div', { class: 'flex flex-wrap gap-2' }, [
              h(
                'button',
                {
                  class: 'rounded-md border border-line bg-surface px-3 py-2 text-sm text-txt',
                  'data-testid': 'sample-control',
                  onClick: () => setTheme(theme.value === 'light' ? 'dark' : 'light'),
                },
                `控件 8px · 切主题(${theme.value})`,
              ),
              h(
                'span',
                {
                  class:
                    'force-radius-pill inline-flex items-center rounded-full border border-line px-3 py-1 text-xs text-txt',
                  'data-testid': 'sample-nav-pill',
                },
                '导航胶囊 10px',
              ),
              h(
                'span',
                {
                  class:
                    'force-radius-full inline-flex h-8 w-8 items-center justify-center rounded-full bg-accent text-white text-xs',
                  'data-testid': 'sample-avatar',
                },
                'A',
              ),
            ]),
            h(
              'div',
              {
                class: 'card p-4 text-txt',
                'data-testid': 'sample-card',
              },
              '业务卡片 rounded-lg → 12px',
            ),
            h(
              'div',
              {
                class: 'force-radius-xl border border-line bg-surface p-4 text-txt shadow-lg',
                'data-testid': 'sample-modal',
                style: 'width: min(420px, 100%)',
              },
              '弹层壳 16px',
            ),
            h('input', {
              class: 'input max-w-xs',
              'data-testid': 'sample-input',
              placeholder: '输入框 8px',
            }),
          ]),
        ],
      )
  },
}

const app = createApp(App)
app.use(i18n)
app.use(router)
router.isReady().then(() => {
  router.push('/dashboard')
  app.mount('#app')
})
