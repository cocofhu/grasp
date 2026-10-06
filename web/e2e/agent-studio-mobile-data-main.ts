import '../src/styles/global.css'
import { createApp } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { setTheme } from '../src/lib/shared/theme'
import AgentStudioView from '../src/views/AgentStudioView.vue'

const params = new URLSearchParams(window.location.search)
const agent = params.get('agent') || 'GraspPM'
const studioTab = params.get('studioTab') || 'data'
const sub = params.get('sub') || 'memory'

async function boot() {
  await initLocale()
  await setLocale('zh-CN')
  setTheme('dark')
  installIdleScrollbar()

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/agents', component: AgentStudioView }],
  })
  await router.push({ path: '/agents', query: { agent, studioTab, sub } })
  await router.isReady()

  const app = createApp(AgentStudioView)
  app.use(i18n)
  app.use(router)
  app.mount('#app')

  const root = document.getElementById('app')
  if (root) root.setAttribute('data-testid', 'agent-studio-mobile-data-root')
}

void boot()
