import '../src/styles/global.css'
import { createApp, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import AgentCreateWizard from '../src/components/agent/AgentCreateWizard.vue'
import type { Agent } from '../src/lib/api/api'

async function boot() {
  await initLocale()
  await setLocale(new URLSearchParams(location.search).get('lang') === 'en' ? 'en' : 'zh-CN')
  setTheme('dark')

  const open = ref(true)
  const createdName = ref('')
  const app = createApp({
    setup() {
      return () =>
        h('div', [
          createdName.value
            ? h('p', { 'data-testid': 'created-name' }, createdName.value)
            : null,
          h(AgentCreateWizard, {
            open: open.value,
            existingNames: [],
            projectId: new URLSearchParams(location.search).get('projectId') || undefined,
            onClose: () => {
              open.value = false
            },
            onCreated: (agent: Agent) => {
              createdName.value = agent.name
              open.value = false
            },
          }),
        ])
    },
  })
  app.use(i18n)
  app.mount('#app')
  document.getElementById('app')?.setAttribute('data-testid', 'agent-create-wizard-root')
}

void boot()
