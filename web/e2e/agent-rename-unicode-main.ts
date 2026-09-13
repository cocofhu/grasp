import '../src/styles/global.css'
import { computed, createApp, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import { normalizeAgentName, validateAgentName } from '../src/lib/agent/agentIO'
import { vHoverInk } from '../src/lib/shared/hoverInkDirective'
import AppModal from '../src/components/ui/AppModal.vue'
import AppButton from '../src/components/ui/AppButton.vue'

async function boot() {
  await initLocale()
  await setLocale('zh-CN')
  setTheme('dark')

  const existing = ref(['research-agent', 'clarify.v1'])
  const open = ref(true)
  const promptValue = ref('Approve需求澄清视觉研发')
  const promptError = ref('')
  const promptOkMsg = ref('')
  const renamedTo = ref('')
  const t = i18n.global.t

  function validate(v: string): string {
    const code = validateAgentName(v)
    if (code === 'required') return String(t('pages.agentStudio.dialogs.nameRequired'))
    if (code === 'invalid') return String(t('pages.agentStudio.dialogs.nameInvalid'))
    const normalized = normalizeAgentName(v)
    if (existing.value.some((a) => a === normalized)) {
      return String(t('pages.agentStudio.dialogs.nameExists'))
    }
    return ''
  }

  function refresh() {
    const v = promptValue.value
    if (!v.trim()) {
      promptError.value = ''
      promptOkMsg.value = ''
      return
    }
    const err = validate(v)
    if (err) {
      promptError.value = err
      promptOkMsg.value = ''
    } else {
      promptError.value = ''
      promptOkMsg.value = String(t('pages.agentStudio.dialogs.nameValid'))
    }
  }

  const promptCanSubmit = computed(() => {
    const v = promptValue.value
    if (!v.trim()) return false
    return !validate(v)
  })

  refresh()

  const app = createApp({
    setup() {
      return () =>
        h('div', { 'data-testid': 'agent-rename-unicode-root', class: 'p-6' }, [
          renamedTo.value
            ? h('p', { 'data-testid': 'renamed-to' }, renamedTo.value)
            : null,
          h(
            AppModal,
            {
              open: open.value,
              title: String(t('pages.agentStudio.dialogs.renameTitle')),
              width: 420,
              onClose: () => {
                open.value = false
              },
            },
            {
              default: () => [
                h('label', { class: 'mb-1 block text-[12px] text-txt2' }, String(t('pages.agentStudio.dialogs.renameLabel'))),
                h('input', {
                  'data-testid': 'rename-input',
                  value: promptValue.value,
                  class: [
                    'w-full rounded-md border border-line bg-base px-3 py-2 text-[13px] text-txt outline-none focus:border-accent',
                    promptError.value ? 'border-err' : '',
                    promptOkMsg.value && !promptError.value ? 'border-ok/55' : '',
                  ]
                    .filter(Boolean)
                    .join(' '),
                  onInput: (e: Event) => {
                    promptValue.value = (e.target as HTMLInputElement).value
                    refresh()
                  },
                }),
                promptError.value
                  ? h('p', { 'data-testid': 'rename-error', class: 'mt-2 text-[12px] text-err' }, promptError.value)
                  : promptOkMsg.value
                    ? h('p', { 'data-testid': 'rename-ok', class: 'mt-2 text-[12px] text-ok' }, promptOkMsg.value)
                    : null,
                h(
                  'p',
                  { class: 'mt-3 text-[12px] leading-relaxed text-txt2' },
                  String(t('pages.agentStudio.dialogs.renameCascadeHint')),
                ),
              ],
              footer: () => [
                h(
                  AppButton,
                  {
                    size: 'sm',
                    variant: 'ghost',
                    onClick: () => {
                      open.value = false
                    },
                  },
                  () => String(t('common.buttons.cancel')),
                ),
                h(
                  AppButton,
                  {
                    size: 'sm',
                    variant: 'primary',
                    disabled: !promptCanSubmit.value,
                    'data-testid': 'rename-confirm',
                    onClick: () => {
                      if (!promptCanSubmit.value) return
                      renamedTo.value = normalizeAgentName(promptValue.value)
                      open.value = false
                    },
                  },
                  () => String(t('pages.agentStudio.dialogs.confirm')),
                ),
              ],
            },
          ),
        ])
    },
  })
  app.directive('hover-ink', vHoverInk)
  app.use(i18n)
  app.mount('#app')
}

void boot()
