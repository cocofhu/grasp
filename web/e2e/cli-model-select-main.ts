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
    { id: 'grok-4.7', name: 'Grok 4.7', vendor: 'xai', context: 500, efforts: ['low', 'medium', 'high', 'xhigh'], defaultEffort: 'high', serviceTiers: ['fast'] },
    { id: 'grok-4.6', name: 'Grok 4.6', vendor: 'xai', efforts: ['low', 'medium', 'high', 'xhigh'], defaultEffort: 'high', serviceTiers: ['fast'] },
    { id: 'composer-2.5', name: 'Composer 2.5', vendor: 'cursor', serviceTiers: ['fast'] },
    { id: 'claude-opus-5-5', name: 'Claude Opus 5.5', vendor: 'anthropic', context: 1000, recommended: true, efforts: ['low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'medium', serviceTiers: ['fast'] },
    { id: 'claude-opus-5', name: 'Claude Opus 5', vendor: 'anthropic', context: 1000, efforts: ['low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'high', serviceTiers: ['fast'] },
    { id: 'claude-opus-4-8', name: 'Claude Opus 4.8', vendor: 'anthropic', context: 1000, efforts: ['low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'high', serviceTiers: ['fast'] },
    { id: 'gemini-4-neon', name: 'Gemini 4 Neon', vendor: 'google', context: 1000, efforts: ['low', 'medium', 'high', 'xhigh'], defaultEffort: 'high' },
    { id: 'gemini-4-carbon', name: 'Gemini 4 Carbon', vendor: 'google', context: 1000, efforts: ['low', 'medium', 'high'], defaultEffort: 'medium' },
    { id: 'gemini-4-hydrogen', name: 'Gemini 4 Hydrogen', vendor: 'google', context: 1000, efforts: ['low', 'medium', 'high', 'xhigh'], defaultEffort: 'high' },
    { id: 'gemini-4-argon', name: 'Gemini 4 Argon', vendor: 'google', context: 1000, efforts: ['low', 'medium', 'high'], defaultEffort: 'medium' },
    { id: 'muse-wonder-1.0', name: 'Muse Wonder', vendor: 'muse', context: 1000, efforts: ['minimal', 'low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'xhigh' },
    { id: 'gpt-5.6-sol', name: 'GPT-5.6 Sol', vendor: 'openai', context: 1000, recommended: true, efforts: ['none', 'low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'medium', serviceTiers: ['fast'] },
    { id: 'gpt-5.5', name: 'GPT-5.5', vendor: 'openai', context: 1000, efforts: ['none', 'low', 'medium', 'high', 'xhigh'], defaultEffort: 'medium', serviceTiers: ['fast'] },
    { id: 'claude-fable-5-2', name: 'Claude Fable 5.2', vendor: 'anthropic', context: 1000, efforts: ['low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'high', serviceTiers: ['fast'] },
    { id: 'claude-fable-5-1', name: 'Claude Fable 5.1', vendor: 'anthropic', context: 1000, efforts: ['low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'high' },
    { id: 'claude-fable-5', name: 'Claude Fable 5', vendor: 'anthropic', context: 1000, efforts: ['low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'high' },
    { id: 'grok-4.5', name: 'Grok 4.5', vendor: 'xai', efforts: ['low', 'medium', 'high'], defaultEffort: 'high', serviceTiers: ['fast'] },
    { id: 'gemini-3.8-flash', name: 'Gemini 3.8 Flash', vendor: 'google', efforts: ['low', 'medium', 'high'], defaultEffort: 'high' },
    { id: 'muse-spark-1.3', name: 'Muse Spark 1.3', vendor: 'muse', context: 1000, efforts: ['minimal', 'low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'high' },
    { id: 'gpt-5.6-terra', name: 'GPT-5.6 Terra', vendor: 'openai', context: 1000, efforts: ['none', 'low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'medium', serviceTiers: ['fast'] },
    { id: 'claude-sonnet-5-5', name: 'Claude Sonnet 5.5', vendor: 'anthropic', context: 1000, efforts: ['low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'high' },
    { id: 'claude-sonnet-5', name: 'Claude Sonnet 5', vendor: 'anthropic', context: 1000, recommended: true, efforts: ['low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'high' },
    { id: 'claude-sonnet-4-6', name: 'Claude Sonnet 4.6', vendor: 'anthropic', context: 1000, efforts: ['low', 'medium', 'high', 'max'], defaultEffort: 'medium' },
    { id: 'composer-2', name: 'Composer 2', vendor: 'cursor', serviceTiers: ['fast'] },
    { id: 'gpt-5.3-codex', name: 'Codex 5.3', vendor: 'openai', efforts: ['low', 'medium', 'high', 'xhigh'], defaultEffort: 'high', serviceTiers: ['fast'] },
    { id: 'claude-opus-4-7', name: 'Claude Opus 4.7', vendor: 'anthropic', context: 1000, efforts: ['low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'xhigh', serviceTiers: ['fast'] },
    { id: 'gpt-5.4', name: 'GPT-5.4', vendor: 'openai', context: 1000, efforts: ['none', 'low', 'medium', 'high', 'xhigh'], defaultEffort: 'medium', serviceTiers: ['fast'] },
    { id: 'claude-opus-4-6', name: 'Claude Opus 4.6', vendor: 'anthropic', context: 1000, efforts: ['low', 'medium', 'high', 'max'], defaultEffort: 'high' },
    { id: 'claude-opus-4-5', name: 'Claude Opus 4.5', vendor: 'anthropic' },
    { id: 'gpt-5.2', name: 'GPT-5.2', vendor: 'openai', efforts: ['low', 'medium', 'high', 'xhigh'], defaultEffort: 'high', serviceTiers: ['fast'] },
    { id: 'gpt-5.6-luna', name: 'GPT-5.6 Luna', vendor: 'openai', context: 1000, efforts: ['none', 'low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'medium', serviceTiers: ['fast'] },
    { id: 'gemini-3.1-pro', name: 'Gemini 3.1 Pro', vendor: 'google' },
    { id: 'gpt-5.4-mini', name: 'GPT-5.4 Mini', vendor: 'openai', efforts: ['none', 'low', 'medium', 'high', 'xhigh'], defaultEffort: 'medium' },
    { id: 'gpt-5.4-nano', name: 'GPT-5.4 Nano', vendor: 'openai', efforts: ['none', 'low', 'medium', 'high', 'xhigh'], defaultEffort: 'medium' },
    { id: 'claude-haiku-5-5', name: 'Claude Haiku 5.5', vendor: 'anthropic', context: 1000, efforts: ['low', 'medium', 'high', 'xhigh', 'max'], defaultEffort: 'high' },
    { id: 'claude-haiku-4-5', name: 'Claude Haiku 4.5', vendor: 'anthropic' },
    { id: 'claude-sonnet-4-5', name: 'Claude Sonnet 4.5', vendor: 'anthropic', context: 200 },
    { id: 'gpt-5.1', name: 'GPT-5.1', vendor: 'openai', efforts: ['low', 'medium', 'high'], defaultEffort: 'medium' },
    { id: 'gemini-3-flash', name: 'Gemini 3 Flash', vendor: 'google' },
    { id: 'gemini-3.5-flash', name: 'Gemini 3.5 Flash', vendor: 'google' },
    { id: 'claude-sonnet-4', name: 'Claude Sonnet 4', vendor: 'anthropic', context: 200 },
    { id: 'gpt-5-mini', name: 'GPT-5 Mini', vendor: 'openai' },
    { id: 'gemini-2.5-flash', name: 'Gemini 2.5 Flash', vendor: 'google' },
    { id: 'kimi-k3', name: 'Kimi K3', vendor: 'moonshot', efforts: ['low', 'high', 'max'], defaultEffort: 'max' },
    { id: 'kimi-k2.7-code', name: 'Kimi K2.7 Code', vendor: 'moonshot' },
    { id: 'glm-5.2', name: 'GLM 5.2', vendor: 'zhipu', efforts: ['high', 'max'], defaultEffort: 'high' },
    { id: 'glm-5p3', name: 'GLM 5.3', vendor: 'zhipu', efforts: ['low', 'high', 'max'], defaultEffort: 'max' },
    { id: 'glm-5p3-flash', name: 'GLM 5.3 Flash', vendor: 'zhipu', efforts: ['low', 'high', 'max'], defaultEffort: 'max' },
    { id: 'deepseek-v41-flash-1017', name: 'DeepSeek V4.1 Flash 1017', vendor: 'deepseek', efforts: ['none', 'low', 'medium', 'high', 'max'], defaultEffort: 'max' },
  ],
  codex: [
    { id: 'gpt-6.2-sol', name: 'GPT-6.2 Sol', vendor: 'openai', tags: [...vr], context: 500, recommended: true, efforts: [...codexEfforts], serviceTiers: ['fast'] },
    { id: 'gpt-6.2-astra', name: 'GPT-6.2 Astra', vendor: 'openai', tags: [...vr], context: 272, recommended: true, efforts: [...codexEfforts], serviceTiers: ['fast'] },
    { id: 'gpt-6.1-terra', name: 'GPT-6.1 Terra', vendor: 'openai', tags: [...vr], context: 500, efforts: [...codexEfforts], serviceTiers: ['fast'] },
    { id: 'gpt-6.1-sol', name: 'GPT-6.1 Sol', vendor: 'openai', tags: [...vr], context: 500, recommended: true, efforts: [...codexEfforts], serviceTiers: ['fast'] },
    { id: 'gpt-6-astra', name: 'GPT-6 Astra', vendor: 'openai', tags: [...vr], context: 272, recommended: true, efforts: [...codexEfforts], serviceTiers: ['fast'] },
    { id: 'gpt-6-sol', name: 'GPT-6 Sol', vendor: 'openai', tags: [...vr], context: 272, efforts: [...codexEfforts], serviceTiers: ['fast'] },
    { id: 'gpt-6-luna', name: 'GPT-6 Luna', vendor: 'openai', tags: [...vr], context: 272, efforts: codexEfforts.slice(0, 5), serviceTiers: ['fast'] },
    { id: 'gpt-5.6-sol', name: 'GPT-5.6 Sol', vendor: 'openai', tags: [...vr], context: 272, efforts: [...codexEfforts], serviceTiers: ['fast'] },
    { id: 'gpt-5.6-terra', name: 'GPT-5.6 Terra', vendor: 'openai', tags: [...vr], context: 272, efforts: [...codexEfforts], serviceTiers: ['fast'] },
    { id: 'gpt-5.6-luna', name: 'GPT-5.6 Luna', vendor: 'openai', tags: [...vr], context: 272, efforts: codexEfforts.slice(0, 5), serviceTiers: ['fast'] },
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
