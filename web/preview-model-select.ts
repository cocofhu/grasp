import './src/styles/global.css'
import './src/lib/shared/theme'
import { createApp, h, onMounted, ref } from 'vue'
import { i18n } from './src/lib/shared/i18n'
import { initLocale, setLocale } from './src/lib/shared/locale'
import { setTheme } from './src/lib/shared/theme'
import SoloModelSelect from './src/components/solo/SoloModelSelect.vue'
import type { ConfiguredModelChoice } from './src/lib/solo/configuredModels'

const stageStyle = document.createElement('style')
stageStyle.textContent = `
  html, body, #app { height: 100%; margin: 0; }
  .preview-stage { min-height: 100%; display: flex; align-items: flex-end; justify-content: center; padding: 28px 20px 36px; background: rgb(var(--c-base)); }
  .preview-composer { width: min(720px, 100%); border: 1px solid rgb(var(--c-line-strong) / 0.7); border-radius: 20px; background: rgb(var(--c-surface)); box-shadow: 0 2px 10px rgb(var(--c-txt) / 0.025); padding: 10px; }
  .preview-composer textarea { display: block; width: 100%; min-height: 48px; resize: none; border: 0; background: transparent; color: rgb(var(--c-txt)); font: inherit; font-size: 14px; line-height: 1.75; padding: 8px 4px; outline: none; }
  .preview-toolbar { display: flex; align-items: center; margin-top: 2px; }
`
document.head.appendChild(stageStyle)

const models: ConfiguredModelChoice[] = [
  { id: 'gemini-4-argon', name: 'Gemini 4 Argon Contributor', hint: 'gemini-4-argon', vendor: 'gemini', backend: false, unavailable: false, configurationId: 'cfg-opencode', configurationName: 'OpenCode', tool: 'opencode', source: '' },
  { id: 'kimi-k3.1-pro', name: 'Kimi K3.1 Pro', hint: 'kimi-k3.1-pro', vendor: 'kimi', backend: false, unavailable: false, configurationId: 'cfg-token', configurationName: 'TokenHub', tool: 'deepseek_harness', source: '腾讯混元 / TokenHub · TokenHub' },
  { id: 'deepseek-v4.1-flash-1017', name: 'DeepSeek-V4.1-Flash-1017', hint: 'deepseek-v4.1-flash-1017', vendor: 'deepseek', backend: false, unavailable: false, configurationId: 'cfg-token', configurationName: 'TokenHub', tool: 'deepseek_harness', source: '腾讯混元 / TokenHub · TokenHub' },
  { id: 'grok-4.7-high', name: 'Grok 4.7 High', hint: 'grok-4.7-high', vendor: 'grok', backend: false, unavailable: false, configurationId: 'cfg-cursor', configurationName: 'CursorX', tool: 'cursor', source: '' },
  { id: 'claude-opus', name: 'Claude Opus 4.7', hint: 'claude-opus-4.7', vendor: 'claude', backend: false, unavailable: false, configurationId: 'cfg-cursor', configurationName: 'CursorX', tool: 'cursor', source: '' },
  { id: 'gpt-5.4', name: 'GPT-5.4', hint: 'gpt-5.4', vendor: 'openai', backend: false, unavailable: false, configurationId: 'cfg-token-2', configurationName: 'TokenHub', tool: 'codex', source: 'OpenAI / TokenHub · 备用组' },
  { id: 'qwen-max', name: 'Qwen Max 长名称用来检查一行省略是否保持行高不变', hint: 'qwen-max', vendor: 'qwen', backend: false, unavailable: false, configurationId: 'cfg-opencode-2', configurationName: 'OpenCode', tool: 'opencode', source: '' },
]

async function bootstrap() {
  await initLocale()
  await setLocale('zh-CN')
  setTheme('light')
  createApp({
    setup() {
      const selected = ref('')
      onMounted(() => {
        document.querySelector<HTMLButtonElement>('[data-testid="solo-model-select-trigger"]')?.click()
      })
      return () => h('div', { class: 'preview-stage' }, [
        h('form', { class: 'preview-composer', onSubmit: (event: Event) => event.preventDefault() }, [
          h('textarea', { rows: 2, placeholder: '直接和 Agent 对话', 'aria-label': '消息' }),
          h('div', { class: 'preview-toolbar' }, [
            h(SoloModelSelect, {
              models,
              flat: true,
              modelValue: selected.value,
              'onUpdate:modelValue': (value: string) => { selected.value = value },
            }),
          ]),
        ]),
      ])
    },
  }).use(i18n).mount('#app')
}

void bootstrap()
