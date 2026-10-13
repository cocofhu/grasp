import '../src/styles/global.css'
import { createApp, h, reactive } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import AgentMetaPanel from '../src/components/agent/AgentMetaPanel.vue'
import AgentEnvPanel from '../src/components/agent/AgentEnvPanel.vue'
import CliModelSelect from '../src/components/agent/CliModelSelect.vue'
import ToastHost from '../src/components/ui/ToastHost.vue'
import { api } from '../src/lib/api/api'
import type { CliCatalogModel } from '../src/lib/api/apiTypes'

const vr = ['vision', 'reasoning'] as const
const catalog: Record<string, CliCatalogModel[]> = {
  cursor: [
    { id: 'claude-4.6-opus-max', name: 'Claude Opus 4.6 Max', vendor: 'anthropic', tags: [...vr], context: 200, recommended: true },
    { id: 'claude-4.5-sonnet', name: 'Claude Sonnet 4.5', vendor: 'anthropic', tags: [...vr], context: 200, recommended: true },
    { id: 'gpt-5', name: 'GPT-5', vendor: 'openai', tags: [...vr], context: 400, recommended: true },
    { id: 'gpt-5-codex', name: 'GPT-5 Codex', vendor: 'openai', tags: ['reasoning'], context: 400 },
    { id: 'gemini-2.5-pro', name: 'Gemini 2.5 Pro', vendor: 'google', tags: [...vr], context: 1000 },
    { id: 'grok-4.5-high-fast', name: 'Grok 4.5 Fast', vendor: 'xai', tags: ['reasoning', 'fast'], context: 256 },
  ],
  codex: [
    { id: 'gpt-5-codex', name: 'GPT-5 Codex', vendor: 'openai', tags: ['reasoning'], context: 400, recommended: true },
    { id: 'gpt-5', name: 'GPT-5', vendor: 'openai', tags: [...vr], context: 400, recommended: true },
    { id: 'gpt-5-mini', name: 'GPT-5 mini', vendor: 'openai', tags: ['vision', 'reasoning', 'fast'], context: 400 },
    { id: 'o3', name: 'o3', vendor: 'openai', tags: [...vr], context: 200 },
    { id: 'gpt-4.1', name: 'GPT-4.1', vendor: 'openai', tags: ['vision'], context: 1000 },
  ],
  trae: [
    { id: 'claude-sonnet-4', name: 'Claude Sonnet 4', vendor: 'anthropic', tags: [...vr], context: 200, recommended: true },
    { id: 'gpt-5', name: 'GPT-5', vendor: 'openai', tags: [...vr], context: 400, recommended: true },
    { id: 'kimi-k2', name: 'Kimi K2', vendor: 'moonshot', tags: ['fast'], context: 128 },
    { id: 'doubao-seed-code', name: 'Doubao Seed Code', vendor: 'bytedance', tags: ['vision', 'fast'], context: 256 },
  ],
}

Object.assign(api, {
  cliModels: async (backend: string) => {
    await new Promise((r) => setTimeout(r, new URLSearchParams(location.search).get('slow') ? 60000 : 150))
    return { models: catalog[backend] || [] }
  },
  getProjectCredentials: async () => ({
    items: [
      { id: 'c1', type: 'ai', provider: 'cursor', name: '团队 Cursor', envKey: 'GRASP_CURSOR_API_KEY', configured: true, masked: 'key_••••a91' },
      { id: 'c2', type: 'ai', provider: 'codex', name: 'Codex 登录', envKey: 'GRASP_CODEX_AUTH_JSON', configured: true },
    ],
  }),
})

async function boot() {
  await initLocale()
  const params = new URLSearchParams(location.search)
  await setLocale(params.get('lang') === 'en' ? 'en' : 'zh-CN')
  setTheme(params.get('theme') === 'light' ? 'light' : 'dark')
  const view = params.get('view') || 'meta'
  const backend = params.get('backend') || 'cursor'
  const model = params.get('model') || ''

  const env = [{ k: 'FOO', v: 'bar' }]
  if (model) env.unshift({ k: 'ACP_BRIDGE_MODEL', v: model })
  if (params.get('effort')) env.push({ k: 'ACP_BRIDGE_REASONING_EFFORT', v: params.get('effort')! })
  const draft = reactive({
    name: 'ImplementAgent',
    projectId: 'p1',
    acpBackend: backend,
    aiCredentialId: backend === 'codex' ? 'c2' : 'c1',
    openCodeCredentialId: '',
    gitCredentialId: '',
    sshHostsCredentialId: '',
    gitCredentialType: undefined,
    files: [],
    mcp: [],
    env,
    layout: { configRoot: '/root/.cursor', workspaceDir: '/workspace' },
    capabilities: null,
  })
  const compact = reactive({ value: model })

  const router = createRouter({ history: createMemoryHistory(), routes: [
      { path: '/projects/:id', name: 'project-detail', component: { render: () => null } },
      { path: '/:p(.*)*', component: { render: () => null } },
    ] })
  const app = createApp({
    setup() {
      return () =>
        h('div', { class: 'mx-auto flex max-w-[860px] flex-col', 'data-testid': 'harness' }, [
          view === 'meta'
            ? h('div', { class: 'flex h-[900px] flex-col', 'data-testid': 'shot' }, [
                h(AgentMetaPanel, { draft: draft as never, agentName: 'ImplementAgent', projects: [{ id: 'p1', name: 'Grasp' }] }),
              ])
            : view === 'env'
              ? h('div', { class: 'flex h-[560px] flex-col', 'data-testid': 'shot' }, [h(AgentEnvPanel, { draft: draft as never })])
              : h('div', { class: 'w-[440px] p-6', 'data-testid': 'shot' }, [
                  h('div', { class: 'mb-2 text-[12px] font-medium text-txt2' }, '模型'),
                  h(CliModelSelect, {
                    modelValue: compact.value,
                    backend,
                    variant: 'compact',
                    'onUpdate:modelValue': (v: string) => (compact.value = v),
                  }),
                ]),
          h(ToastHost),
        ])
    },
  })
  app.use(i18n)
  app.use(router)
  app.mount('#app')
}

void boot()
