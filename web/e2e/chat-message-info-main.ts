import '../src/styles/global.css'
import { createApp, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import VendorMark from '../src/components/agent/VendorMark.vue'
import CredentialProviderLogo from '../src/components/workspace/CredentialProviderLogo.vue'
import AgentTimeline from '../src/components/run/AgentTimeline.vue'
import ChatUserMessage from '../src/components/run/ChatUserMessage.vue'
import SoloModelSelect from '../src/components/solo/SoloModelSelect.vue'
import type { ChatMessageMetadata } from '../src/lib/shared/types'

const marks = [
  ['anthropic', 'claude'], ['openai', 'openai'], ['google', 'google'], ['xai', 'xai'],
  ['deepseek', 'deepseek'], ['moonshot', 'moonshot'], ['kimi', 'kimi'], ['bytedance', 'bytedance'],
  ['qwen', 'qwen'], ['doubao', 'doubao'], ['siliconflow', 'siliconflow'], ['minimax', 'minimax'],
  ['zhipu', 'zhipu'], ['zai', 'zai'], ['hunyuan', 'hunyuan'], ['qianfan', 'qianfan'], ['openrouter', 'openrouter'],
  ['cursor', 'cursor'], ['codex', 'codex'], ['claude_code', 'claude_code'], ['codebuddy', 'codebuddy'],
  ['trae', 'trae'], ['opencode', 'opencode'], ['deepseek_harness', 'deepseek_harness'], ['github', 'github'], ['gitlab', 'gitlab'],
] as const
const backends = new Set(['cursor', 'codex', 'claude_code', 'codebuddy', 'trae', 'opencode', 'deepseek_harness'])
const metadata: ChatMessageMetadata = {
  createdAt: '2026-10-28T08:00:25Z', startedAt: '2026-10-28T08:00:00Z', endedAt: '2026-10-28T08:00:25Z', durationMs: 25000,
  execution: { agentName: 'Recorded Agent', backend: 'opencode', configurationId: 'recorded-group', modelId: 'glm-recorded', reasoningEffort: 'high' },
  usage: { inputTokens: 1000, outputTokens: 200, cacheReadTokens: 300, cacheWriteTokens: 0 },
  usageByModel: { 'glm-recorded': { inputTokens: 1000, outputTokens: 200, cacheReadTokens: 300, cacheWriteTokens: 0 } },
}
async function bootstrap() {
  await initLocale(); await setLocale('en')
  setTheme(new URLSearchParams(location.search).get('theme') === 'light' ? 'light' : 'dark')
  createApp({ setup() {
    const selected = ref('glm-current')
    return () => h('main', { class: 'mx-auto max-w-[1280px] p-5', 'data-testid': 'shared-chat-audit' }, [
      h('h1', { class: 'mb-4 text-lg font-semibold' }, 'Model and provider marks'),
      h('div', { class: 'mb-8 grid grid-cols-2 gap-3 md:grid-cols-4' }, marks.map(([vendor, provider]) => h('section', { class: 'rounded-lg border border-line p-3', 'data-brand': vendor }, [
        h('div', { class: 'mb-2 text-xs text-txt2' }, vendor),
        h('div', { class: 'flex items-center gap-3' }, [
          h('button', { type: 'button', 'data-testid': 'compact-mark' }, [h(VendorMark, { vendor, backend: backends.has(vendor), size: 18, flat: true })]),
          h(VendorMark, { vendor, backend: backends.has(vendor), size: 32 }),
          h(CredentialProviderLogo, { provider }),
        ]),
      ]))),
      h('section', { class: 'mb-8 rounded-lg border border-line p-4', 'data-testid': 'actual-selector' }, [
        h(SoloModelSelect, { modelValue: selected.value, 'onUpdate:modelValue': (value: string) => { selected.value = value }, flat: true, models: [
          { id: 'glm-current', name: 'GLM current selection', vendor: 'zhipu', backend: false, unavailable: false },
          { id: 'deepseek-current', name: 'DeepSeek', vendor: 'deepseek', backend: false, unavailable: false },
        ] }),
      ]),
      ...['Solo', 'Workflow'].map(kind => h('section', { class: 'mb-8 rounded-lg border border-line p-4', 'data-testid': 'chat-'+kind.toLowerCase() }, [
        h('h2', { class: 'mb-4 text-sm text-txt2' }, kind),
        h(ChatUserMessage, { text: 'Continue the task.', createdAt: '2026-10-28T08:00:00Z' }),
        h(AgentTimeline, { parts: [{ kind: 'message', text: 'This reply records its own execution and token usage.' }], completed: true, metadata, startedAt: metadata.startedAt, endedAt: metadata.endedAt }),
      ])),
    ])
  } }).use(i18n).mount('#app')
}
void bootstrap()
