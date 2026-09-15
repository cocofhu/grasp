import '../src/styles/global.css'
import { createApp, defineComponent, h, nextTick, onMounted, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import ClarifyChat from '../src/components/run/ClarifyChat.vue'
import PreflightView from '../src/components/run/product/PreflightView.vue'
import type { ClarifyTurn } from '../src/lib/shared/types'

initLocale()
setTheme('dark')

const App = defineComponent({
  name: 'PreflightUiHarness',
  setup() {
    const turns = ref<ClarifyTurn[]>([
      {
        role: 'agent',
        text: '请确认后续测试所需环境（对照计划缺口）。',
        at: new Date().toISOString(),
        forms: [
          {
            title: '环境核对',
            fields: [
              {
                name: 'test_env_url',
                label: '测试环境地址',
                type: 'url',
                why: '计划 g2 联调需要可达的测试基址',
                required: true,
              },
              {
                name: 'db_password',
                label: '数据库密码',
                type: 'text',
                why: '计划要求写入 vars 的明文凭据',
                required: true,
              },
            ],
          },
        ],
      },
    ])

    const preflightDoc = {
      summary: '已确认测试环境与库密码',
      confirmed: true,
      fields: [
        {
          name: 'test_env_url',
          label: '测试环境地址',
          value: 'http://127.0.0.1:18080',
          verified: true,
          verification: 'sandbox_probe',
          source: 'form',
        },
        {
          name: 'db_password',
          label: '数据库密码',
          value: 's3cret!',
          verified: true,
          verification: 'user_attested',
          source: 'form',
          notes: '当面确认',
        },
      ],
      unresolved: [],
    }

    onMounted(async () => {
      await nextTick()
    })

    return () =>
      h('div', { class: 'min-h-screen p-4 max-w-3xl mx-auto space-y-8', 'data-testid': 'preflight-ux-root' }, [
        h('section', { 'data-testid': 'preflight-form-section' }, [
          h('h2', { class: 'mb-2 text-sm text-txt2' }, 'FormCard (nodeType=preflight)'),
          h(ClarifyChat, {
            runId: 'run-preflight-e2e',
            nodeId: 'preflight_1',
            iteration: 1,
            turns: turns.value,
            done: false,
            active: true,
            reviewMode: false,
            annotateEnabled: false,
            hideFinish: true,
            nodeType: 'preflight',
            sendLabel: '提交环境信息',
          }),
        ]),
        h('section', { 'data-testid': 'preflight-product-section', class: 'rounded-lg border border-line p-4' }, [
          h('h2', { class: 'mb-2 text-sm text-txt2' }, 'PreflightView plaintext'),
          h(PreflightView, { doc: preflightDoc, accent: '#2DD4BF' }),
        ]),
      ])
  },
})

createApp(App).use(i18n).mount('#app')
