import '../src/styles/global.css'
import { createApp, h, reactive } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import AgentMetaPanel from '../src/components/agent/AgentMetaPanel.vue'
import type { AgentStudioDraft } from '../src/lib/agent/agentStudioDraft'
import type { AgentOrg } from '../src/lib/api/api'

async function boot() {
  await initLocale()
  await setLocale('zh-CN')
  setTheme('dark')

  const draft = reactive<AgentStudioDraft>({
    name: 'ssh-meta-agent',
    projectId: 'proj-1',
    acpBackend: 'cursor',
    gitCredentialType: 'ssh',
    gitSshKnownHosts: '',
    gitSshPrivateKey: '',
    files: [],
    mcp: [],
    env: [
      { k: 'GITHUB_TOKEN', v: '${vars.github_token}' },
      { k: 'GITLAB_TOKEN', v: '${vars.gitlab_token}' },
    ],
    layout: { configRoot: '/root/.cursor', workspaceDir: '/root/workspace' },
    capabilities: null,
  })
  const org = reactive<AgentOrg>({ revision: 0, groups: [], agents: {} })

  const app = createApp({
    setup() {
      return () =>
        h(AgentMetaPanel, {
          draft,
          org,
          agentName: 'ssh-meta-agent',
          agentNames: ['ssh-meta-agent'],
          projects: [{ id: 'proj-1', name: 'Demo' }],
          isProjectBound: true,
          'onUpdate:org': (v: AgentOrg) => Object.assign(org, v),
        })
    },
  })
  app.use(i18n)
  app.mount('#app')
  document.getElementById('app')?.setAttribute('data-testid', 'agent-ssh-meta-root')
}

boot()
