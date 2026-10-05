import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import RunLaunchModal from '../src/components/workflow/RunLaunchModal.vue'
import RunSandboxEnvPanel from '../src/components/run/RunSandboxEnvPanel.vue'
import type { ProjectEnvEntry } from '../src/lib/shared/types'

async function boot() {
  await initLocale()
  await setLocale('zh-CN')
  setTheme('dark')

  const open = ref(true)
  const view = ref<'launch' | 'detail'>('launch')
  const snapshot = ref<ProjectEnvEntry[]>([
    { key: 'LOG_LEVEL', value: 'debug', secret: false },
    { key: 'FEATURE_FLAG', value: 'on', secret: false },
    { key: 'DB_PASSWORD', value: '••••••••', secret: true },
  ])
  const lastStart = ref<string>('')

  const App = defineComponent({
    name: 'RunSandboxEnvHarness',
    setup() {
      return () =>
        h(
          'div',
          {
            class: 'mx-auto flex min-h-screen max-w-5xl flex-col gap-4 p-4',
            'data-testid': 'run-sandbox-env-root',
          },
          [
            h('div', { class: 'flex items-center justify-between gap-3' }, [
              h('h1', { class: 'text-lg font-semibold' }, '运行级沙箱环境变量验收'),
              h('div', { class: 'flex gap-2' }, [
                h(
                  'button',
                  {
                    class: 'border border-line px-3 py-1 text-sm',
                    'data-testid': 'goto-launch',
                    onClick: () => {
                      view.value = 'launch'
                      open.value = true
                    },
                  },
                  '启动弹窗',
                ),
                h(
                  'button',
                  {
                    class: 'border border-line px-3 py-1 text-sm',
                    'data-testid': 'goto-detail',
                    onClick: () => {
                      view.value = 'detail'
                    },
                  },
                  'Run 快照',
                ),
              ]),
            ]),
            lastStart.value
              ? h(
                  'pre',
                  {
                    class: 'border border-line bg-elevated p-2 text-[11px] text-txt2',
                    'data-testid': 'last-start',
                  },
                  lastStart.value,
                )
              : null,
            view.value === 'launch'
              ? h(RunLaunchModal, {
                  open: open.value,
                  workflowId: 'wf-env-1',
                  workflowName: 'deploy-workflow',
                  fields: [{ key: 'topic', desc: '主题', required: true }],
                  runInputs: { topic: 'hotfix' },
                  runImages: {},
                  onClose: () => {
                    open.value = false
                  },
                  onStarted: (id: string) => {
                    lastStart.value = `started:${id}`
                  },
                  onViewRun: (id: string) => {
                    lastStart.value = `view-run:${id}`
                    view.value = 'detail'
                    open.value = false
                  },
                })
              : h(
                  'div',
                  {
                    class: 'min-h-[420px] border border-line bg-surface',
                    'data-testid': 'detail-shell',
                  },
                  [
                    h('div', { class: 'border-b border-line px-4 py-3 text-sm font-medium' }, 'run_env_e2e · 运行级 env 快照'),
                    h(RunSandboxEnvPanel, { entries: snapshot.value }),
                  ],
                ),
          ],
        )
    },
  })

  createApp(App).use(i18n).mount('#app')
}

void boot()
