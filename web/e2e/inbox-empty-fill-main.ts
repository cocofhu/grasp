import '../src/styles/global.css'
import { createApp, defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { setTheme } from '../src/lib/shared/theme'
import GatesInboxView from '../src/views/GatesInboxView.vue'

const params = new URLSearchParams(window.location.search)
const theme = params.get('theme') === 'light' ? 'light' : 'dark'
const filterWf = params.get('wf') || ''

const Fixture = defineComponent({
  name: 'InboxEmptyFillFixture',
  setup() {
    return () =>
      h(
        'div',
        {
          'data-testid': 'inbox-empty-shell',
          // Mirror AppShell non-full height chain: h-screen overflow-hidden → padded flex col.
          class: 'flex h-full min-h-0 flex-col overflow-hidden bg-base',
        },
        [
          h(
            'div',
            {
              class: 'flex h-full min-h-0 flex-col px-4 pb-4 pt-2 md:px-6 md:pb-6 md:pt-3',
            },
            [h(GatesInboxView)],
          ),
        ],
      )
  },
})

async function boot() {
  await initLocale()
  await setLocale('zh-CN')
  setTheme(theme)
  installIdleScrollbar()

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/inbox-fixture', component: { render: () => h('div') }, meta: { titleKey: 'route.gates' } },
      { path: '/runs/:id', component: { render: () => h('div', { 'data-testid': 'run-page' }, 'run') } },
    ],
  })

  const query: Record<string, string> = {}
  if (filterWf) query.wf = filterWf
  await router.push({ path: '/inbox-fixture', query })

  createApp({ render: () => h(Fixture) })
    .use(i18n)
    .use(router)
    .mount('#app')
}

void boot()
