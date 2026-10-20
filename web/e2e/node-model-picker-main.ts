import '../src/styles/global.css'
import '../src/components/canvas/canvas.css'
import { computed, createApp, h, reactive, ref } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import NodeInspector from '../src/components/canvas/NodeInspector.vue'
import type { CanvasAgent } from '../src/components/canvas/composables/outlets'
import { api, type CredentialItem } from '../src/lib/api/api'
import type { WFNode } from '../src/lib/shared/types'

const caps = { interaction: 'auto', tools: [], reads: [], writes: [] }

const agents: CanvasAgent[] = [
  { name: 'ImplementAgent', acpBackend: 'opencode', openCodeCredentialId: 'cred-ds', capabilities: caps as never },
  { name: 'ReviewAgent', acpBackend: 'opencode', openCodeCredentialId: '', capabilities: caps as never },
  { name: 'CursorAgent', acpBackend: 'cursor', capabilities: caps as never },
]

const model = (id: string, name: string, provider: string, m: string, vision = false): CredentialItem => ({
  id,
  type: 'ai',
  provider: 'opencode',
  name,
  envKey: 'GRASP_OPENCODE_API_KEY',
  configured: true,
  masked: 'sk-••••' + id.slice(-3),
  metadata: { provider, model: m, vision },
})

const credentials: CredentialItem[] = [
  model('cred-ds', '工作号', 'deepseek', 'deepseek/deepseek-chat'),
  model('cred-oa', '视觉评审', 'openai', 'openai/gpt-4o', true),
  model('cred-or', '便宜跑量', 'openrouter', 'openrouter/qwen/qwen-2.5-coder-32b'),
  model('cred-own', '内网网关', 'custom', 'custom/glm-4-plus'),
  { id: 'cred-gh', type: 'git', provider: 'github', name: 'GitHub', envKey: 'GITHUB_TOKEN', configured: true },
]

const catalogs: Record<string, { id: string; name?: string }[]> = {
  deepseek: [
    { id: 'deepseek-chat', name: 'DeepSeek V3' },
    { id: 'deepseek-reasoner', name: 'DeepSeek R1' },
  ],
  openai: [
    { id: 'gpt-4o', name: 'GPT-4o' },
    { id: 'gpt-4o-mini', name: 'GPT-4o mini' },
    { id: 'gpt-4.1', name: 'GPT-4.1' },
    { id: 'o3', name: 'o3' },
    { id: 'o4-mini', name: 'o4-mini' },
  ],
  openrouter: Array.from({ length: 140 }, (_, i) =>
    i === 0 ? { id: 'qwen/qwen-2.5-coder-32b', name: 'Qwen2.5 Coder 32B' } : { id: `vendor${i % 9}/model-${i}`, name: `Model ${i}` },
  ),
}
Object.assign(api, {
  openCodeModels: async (id: string) => {
    await new Promise((r) => setTimeout(r, 250))
    return { models: catalogs[id] || [] }
  },
})

type Scenario = { id: string; label: string; agent: string; model?: string; modelId?: string }
const scenarios: Scenario[] = [
  { id: 'follow', label: '跟随 Agent', agent: 'ImplementAgent' },
  { id: 'override', label: '节点指定凭据', agent: 'ImplementAgent', model: 'cred-oa' },
  { id: 'override-model', label: '凭据 + 模型', agent: 'ImplementAgent', model: 'cred-oa', modelId: 'openai/o3' },
  { id: 'unlisted', label: '目录外模型', agent: 'ImplementAgent', modelId: 'deepseek/deepseek-v9' },
  { id: 'custom', label: '自建网关', agent: 'ImplementAgent', model: 'cred-own' },
  { id: 'follow-empty', label: 'Agent 未选模型', agent: 'ReviewAgent' },
  { id: 'stale', label: '已失效', agent: 'ImplementAgent', model: 'cred-deleted' },
  { id: 'own-auth', label: '非 OpenCode', agent: 'CursorAgent', model: 'cred-ds' },
  { id: 'no-agent', label: '未选 Agent', agent: '' },
]

async function boot() {
  await initLocale()
  const params = new URLSearchParams(location.search)
  await setLocale(params.get('lang') === 'en' ? 'en' : 'zh-CN')
  setTheme(params.get('theme') === 'light' ? 'light' : 'dark')

  const current = ref(params.get('s') || 'follow')
  const empty = ref(params.get('empty') === '1')
  const nodes = reactive<Record<string, WFNode>>({})
  for (const s of scenarios) {
    nodes[s.id] = {
      id: `agent_${s.id.replace(/-/g, '_')}`,
      type: 'agent',
      label: s.agent || 'agent',
      position: { x: 0, y: 0 },
      config: { agent_profile: s.agent, prompt: '实现需求', ...(s.model ? { model_credential_id: s.model } : {}), ...(s.modelId ? { model_id: s.modelId } : {}) },
    } as WFNode
  }
  const node = computed(() => nodes[current.value]!)

  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { render: () => null } }] })

  const app = createApp({
    setup() {
      return () =>
        h('div', { class: 'flex h-full' }, [
          h('div', { class: 'flex-1 space-y-4 p-6' }, [
            h('h1', { class: 'text-lg font-semibold' }, '节点模型选择 · 交互预览'),
            h(
              'div',
              { class: 'flex flex-wrap gap-2' },
              scenarios.map((s) =>
                h(
                  'button',
                  {
                    class: ['rounded-md border px-3 py-1.5 text-[12px]', current.value === s.id ? 'border-accent bg-accent-dim text-txt' : 'border-line text-txt2'],
                    'data-testid': `scenario-${s.id}`,
                    onClick: () => (current.value = s.id),
                  },
                  s.label,
                ),
              ),
            ),
            h('label', { class: 'flex items-center gap-2 text-[12px] text-txt2' }, [
              h('input', { type: 'checkbox', checked: empty.value, onChange: (e: Event) => (empty.value = (e.target as HTMLInputElement).checked) }),
              '工作区里没有模型 Key',
            ]),
            h('pre', { class: 'rounded-lg border border-line bg-surface p-3 text-[12px]', 'data-testid': 'node-config' }, JSON.stringify(node.value.config, null, 2)),
          ]),
          h(NodeInspector, {
            key: current.value,
            node: node.value,
            allNodes: [node.value],
            edges: [],
            agents,
            agentsLoaded: true,
            credentials: empty.value ? [] : credentials,
          }),
        ])
    },
  })
  app.use(i18n)
  app.use(router)
  app.mount('#app')
}

void boot()
