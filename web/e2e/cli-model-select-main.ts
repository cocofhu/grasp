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
import WorkspaceSharedAgentPanel from '../src/components/workspace/WorkspaceSharedAgentPanel.vue'
import { api } from '../src/lib/api/api'
import type { CliCatalogModel } from '../src/lib/api/apiTypes'

const vr = ['vision', 'reasoning'] as const
const codexEfforts = ['low', 'medium', 'high', 'xhigh', 'max', 'ultra']
const catalog: Record<string, CliCatalogModel[]> = {
  cursor: [
    { id: 'claude-opus-5-5-medium', name: 'Claude Opus 5.5', vendor: 'anthropic', tags: [...vr], context: 1000, recommended: true },
    { id: 'gpt-5.6-sol-medium', name: 'GPT-5.6 Sol', vendor: 'openai', tags: [...vr], context: 1000, recommended: true },
    { id: 'claude-sonnet-5-high', name: 'Claude Sonnet 5', vendor: 'anthropic', tags: [...vr], context: 1000, recommended: true },
    { id: 'claude-fable-5-1-high', name: 'Claude Fable 5.1', vendor: 'anthropic', tags: [...vr], context: 1000 },
    { id: 'gpt-5.6-terra-medium', name: 'GPT-5.6 Terra', vendor: 'openai', tags: [...vr], context: 1000 },
    { id: 'gpt-5.6-luna-medium', name: 'GPT-5.6 Luna', vendor: 'openai', tags: [...vr], context: 1000 },
    { id: 'gpt-5.3-codex', name: 'Codex 5.3', vendor: 'openai', tags: ['reasoning'] },
    { id: 'gemini-4-argon-medium', name: 'Gemini 4 Argon', vendor: 'google', tags: [...vr] },
    { id: 'grok-4.7-medium', name: 'Grok 4.7', vendor: 'xai', tags: ['reasoning'] },
    { id: 'composer-2.5', name: 'Composer 2.5', vendor: 'cursor', tags: ['reasoning', 'fast'] },
    { id: 'kimi-k3-max', name: 'Kimi K3', vendor: 'moonshot', tags: ['reasoning'] },
  ],
  codex: [
    { id: 'gpt-6.1-sol', name: 'GPT-6.1 Sol', vendor: 'openai', tags: [...vr], context: 500, recommended: true, efforts: [...codexEfforts] },
    { id: 'gpt-6-astra', name: 'GPT-6 Astra', vendor: 'openai', tags: [...vr], context: 272, recommended: true, efforts: [...codexEfforts] },
    { id: 'gpt-6-sol', name: 'GPT-6 Sol', vendor: 'openai', tags: [...vr], context: 272, efforts: [...codexEfforts] },
    { id: 'gpt-6-luna', name: 'GPT-6 Luna', vendor: 'openai', tags: [...vr], context: 272, efforts: codexEfforts.slice(0, 5) },
    { id: 'gpt-5.6-sol', name: 'GPT-5.6 Sol', vendor: 'openai', tags: [...vr], context: 272, efforts: [...codexEfforts] },
    { id: 'gpt-5.6-terra', name: 'GPT-5.6 Terra', vendor: 'openai', tags: [...vr], context: 272, efforts: [...codexEfforts] },
    { id: 'gpt-5.6-luna', name: 'GPT-5.6 Luna', vendor: 'openai', tags: [...vr], context: 272, efforts: codexEfforts.slice(0, 5) },
  ],
  trae: [
    { id: 'claude-sonnet-4', name: 'Claude Sonnet 4', vendor: 'anthropic', tags: [...vr], context: 200, recommended: true },
    { id: 'gpt-5', name: 'GPT-5', vendor: 'openai', tags: [...vr], context: 400, recommended: true },
    { id: 'kimi-k2', name: 'Kimi K2', vendor: 'moonshot', tags: ['fast'], context: 128 },
    { id: 'doubao-seed-code', name: 'Doubao Seed Code', vendor: 'bytedance', tags: ['vision', 'fast'], context: 256 },
  ],
}

const harnessParams = new URLSearchParams(location.search)
Object.assign(api, {
  getSharedAgentConfig: async () => {
    const backend = harnessParams.get('backend') || 'cursor'
    const shared = harnessParams.get('shared') || ''
    return {
      acpBackend: backend,
      aiCredentialId: backend === 'codex' ? 'c2' : 'c1',
      files: [],
      mcp: [],
      env: shared ? { ACP_BRIDGE_MODEL: shared } : {},
      layout: { configRoot: '/root/.cursor', workspaceDir: '/root/workspace' },
    }
  },
  cliModels: async (backend: string) => {
    await new Promise((r) => setTimeout(r, new URLSearchParams(location.search).get('slow') ? 60000 : 150))
    return { models: catalog[backend] || [] }
  },
  listCredentials: async () => ({
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
      { path: '/:p(.*)*', component: { render: () => null } },
    ] })
  const app = createApp({
    setup() {
      return () =>
        h('div', { class: 'mx-auto flex max-w-[860px] flex-col', 'data-testid': 'harness' }, [
          view === 'meta'
            ? h('div', { class: 'flex h-[900px] flex-col', 'data-testid': 'shot' }, [
                h(AgentMetaPanel, { draft: draft as never, agentName: 'ImplementAgent' }),
              ])
            : view === 'shared'
              ? h('div', { class: 'flex h-[1100px] flex-col', 'data-testid': 'shot' }, [h(WorkspaceSharedAgentPanel)])
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
