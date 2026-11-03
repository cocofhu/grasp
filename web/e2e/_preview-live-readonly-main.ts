import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import ClarifyChat from '../src/components/run/ClarifyChat.vue'
import type { ClarifyTurn } from '../src/lib/shared/types'
import { applyRunLiveFrame, useRunLiveCardHost } from '../src/lib/run/runLiveCards'

initLocale()
void setLocale('zh-CN')
setTheme('light')

const RUN_ID = 'run-demo'
const NODE_ID = 'clarify'
let clock = Date.parse('2026-11-03T10:00:00Z')
const at = () => new Date((clock += 1000)).toISOString()
const variants = [{ n: 1, label: '卡片式' }, { n: 2, label: '紧凑列表' }, { n: 3, label: '分步引导' }]

const App = defineComponent({
  setup() {
    useRunLiveCardHost(RUN_ID, NODE_ID)
    const turns = ref<ClarifyTurn[]>([{ role: 'agent', text: '登录成功后，直接告诉我这次的需求就行。', at: at() }])
    const human = (text: string, live: NonNullable<ClarifyTurn['live']>) => turns.value.push({ role: 'human', text, at: at(), live })
    const agent = (text: string) => turns.value.push({ role: 'agent', text, at: at() })
    const frame = (session: Record<string, unknown>) => applyRunLiveFrame({ type: 'live', runId: RUN_ID, nodeId: NODE_ID, session: { updatedAt: at(), ...session } })
    let n = 0
    const sid = () => `demo${String(n).padStart(4, '0')}`

    const steps: [string, () => void][] = [
      ['选中 div 出 3 个候选', () => {
        n++
        frame({ sid: sid(), state: 'generating', mode: 'replace', summary: 'div', count: 3 })
        human('Live · 自由发挥 · 3 个变体 · div', { sid: sid(), op: 'generate', generated: true })
      }],
      ['候选生成完成 + Agent 回复', () => {
        frame({ sid: sid(), state: 'ready', mode: 'replace', summary: 'div', count: 3, variants })
        agent('已生成 3 个候选：卡片式、紧凑列表、分步引导。请在预览页切换查看。')
      }],
      ['采用候选 2', () => {
        frame({ sid: sid(), state: 'accepting', mode: 'replace', summary: 'div', count: 3, variants, selected: 2 })
        human('Live · 采用变体 2', { sid: sid(), op: 'accept', variant: 2, generated: true })
        setTimeout(() => {
          frame({ sid: sid(), state: 'accepted', mode: 'replace', summary: 'div', count: 3, variants, selected: 2 })
          agent('已把候选 2 写回源码，其余候选已清理。')
        }, 1500)
      }],
      ['放弃候选（卡片 / 浮层）', () => {
        frame({ sid: sid(), state: 'discarding', mode: 'replace', summary: 'div', count: 3, variants })
        human('Live · 放弃变体,恢复原样', { sid: sid(), op: 'discard', generated: true })
        setTimeout(() => {
          frame({ sid: sid(), state: 'discarded', mode: 'replace', summary: 'div', count: 3, variants })
          agent('已移除全部候选，页面恢复原样。')
        }, 1500)
      }],
      ['整页调整后关闭失败', () => {
        n++
        const s = sid()
        frame({ sid: s, state: 'failed', mode: 'steer', prompt: '整体更紧凑', error: 'compile failed' })
        human('Live · 整页调整:整体更紧凑', { sid: s, op: 'steer', prompt: '整体更紧凑', generated: true })
        agent('整页调整没有完成：编译失败。')
        setTimeout(() => frame({ sid: s, state: 'discarded', mode: 'steer', prompt: '整体更紧凑' }), 2500)
      }],
    ]

    return () =>
      h('div', { class: 'flex h-full' }, [
        h('aside', { class: 'flex w-[220px] shrink-0 flex-col gap-2 border-r border-line p-3 text-[12px]' }, [
          h('div', { class: 'font-semibold text-txt2' }, '模拟预览页抽屉操作'),
          ...steps.map(([label, run]) => h('button', { type: 'button', class: 'rounded-md border border-line px-2 py-1 text-left hover:bg-overlay', onClick: run }, label)),
        ]),
        h('div', { class: 'min-w-0 flex-1' }, [
          h(ClarifyChat, { runId: RUN_ID, nodeId: NODE_ID, iteration: 1, turns: turns.value, done: false, active: true, draft: '', attachments: [] }),
        ]),
      ])
  },
})

createApp(App).use(i18n).mount('#app')
