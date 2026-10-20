import '../src/styles/global.css'
import { createApp, defineComponent, h, nextTick, onMounted, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import ReviewShell from '../src/components/run/ReviewShell.vue'
import ReactConnectingState from '../src/components/run/ReactConnectingState.vue'
import ClarifyChat from '../src/components/run/ClarifyChat.vue'
import type { AgentPart, ClarifyTurn } from '../src/lib/shared/types'

/**
 * Post-launch chat shell harness (GatesInbox clarify layout: ReviewShell + sidebar).
 * ?scene=connecting|organizing|react|streaming|done|scroll  ·  ?play=1 runs start → streaming → done.
 */
const params = new URLSearchParams(location.search)
const scene = params.get('scene') || 'connecting'
const play = params.get('play') === '1'
const sleep = (ms: number) => new Promise(r => setTimeout(r, ms))
type Chat = { applyReviewFrame: (f: Record<string, unknown>) => void; applyAcpEvents: (e: unknown[]) => boolean }

const HUMAN = '完成首次引导后没有刷新工作流'
const STEPS: AgentPart[] = [
  { kind: 'thought', text: '先确认用户描述的现象：首次引导完成后侧栏工作流列表没有更新。' },
  { kind: 'message', text: '先对齐需求：我会读取检查清单、附件截图和仓库现状，确认「首次引导完成后未刷新工作流」具体指什么。' },
  { kind: 'tool', title: '读取文件', status: 'completed' },
  { kind: 'tool', title: '查询工具定义', status: 'completed' },
  { kind: 'thought', text: '截图里侧栏仍显示「还没有工作流」，需要看引导完成后是否触发了目录刷新。' },
  { kind: 'message', text: '截图显示首次引导结束后侧栏仍是空的「还没有工作流」。接下来我会查仓库里的引导完成逻辑，以及工作流列表何时刷新。' },
  { kind: 'tool', title: '搜索代码', status: 'completed' },
  { kind: 'tool', title: '读取文件', status: 'completed' },
]
const FINAL: AgentPart = { kind: 'message', text: '已定位：引导向导完成后没有触发 `workflowCatalogRevision`，侧栏目录不会重新加载。修复方式是在向导完成回调里 bump revision。' }

function history(n: number): ClarifyTurn[] {
  const out: ClarifyTurn[] = []
  for (let i = 0; i < n; i++) {
    out.push({ role: 'human', text: `第 ${i + 1} 轮补充：${HUMAN}`, at: new Date(Date.now() - (n - i) * 600_000).toISOString() })
    out.push({ role: 'agent', text: `第 ${i + 1} 轮回复：已记录，接下来检查引导完成后的目录刷新路径，并核对侧栏工作流列表的加载时机与缓存。`, at: new Date(Date.now() - (n - i) * 590_000).toISOString() })
  }
  return out
}

const App = defineComponent({
  setup() {
    const connecting = ref(['connecting', 'organizing'].includes(scene) || play)
    const turns = ref<ClarifyTurn[]>(scene === 'scroll' ? history(8) : [])
    const chat = ref<Chat | null>(null)
    async function event(parts: AgentPart[], streaming = true) {
      const thought = parts.filter(p => p.kind === 'thought').map(p => p.text).pop() || ''
      const message = parts.filter(p => p.kind === 'message').map(p => p.text).pop() || ''
      chat.value?.applyAcpEvents([
        ...(thought ? [{ kind: 'thought', text: thought }] : []),
        ...(message ? [{ kind: 'message', text: message }] : []),
        ...parts.filter(p => p.kind === 'tool').map(p => ({ kind: 'tool_call', title: p.title, status: streaming ? p.status : 'completed' })),
        { kind: 'timeline', parts: parts.map(p => ({ ...p })) },
      ])
      await nextTick()
    }
    async function begin() {
      chat.value?.applyReviewFrame({ event: 'queue_state', nodeId: 'clarify', waiting: 0, items: [], busy: true })
      chat.value?.applyReviewFrame({ event: 'turn_begin', nodeId: 'clarify', item: { text: HUMAN } })
      await nextTick()
    }
    async function stream(stepMs: number) {
      for (let i = 1; i <= STEPS.length; i++) { await event(STEPS.slice(0, i)); await sleep(stepMs) }
    }
    async function finish() {
      await event([...STEPS, FINAL], false)
      await sleep(200)
      chat.value?.applyReviewFrame({ event: 'turn_done', nodeId: 'clarify' })
      chat.value?.applyReviewFrame({ event: 'queue_state', nodeId: 'clarify', waiting: 0, items: [], busy: false })
    }
    onMounted(async () => {
      ;(window as unknown as { __scene: string }).__scene = 'mounted'
      if (play) { await sleep(9000); connecting.value = false; await nextTick(); await nextTick() }
      if (connecting.value || scene === 'scroll') { (window as unknown as { __scene: string }).__scene = 'ready'; return }
      await nextTick(); await nextTick()
      await begin()
      if (scene === 'react') { (window as unknown as { __scene: string }).__scene = 'ready'; return }
      await stream(play ? 1500 : 40)
      if (scene === 'streaming') { (window as unknown as { __scene: string }).__scene = 'ready'; return }
      if (play) await sleep(1500)
      await finish()
      ;(window as unknown as { __scene: string }).__scene = 'ready'
    })
    return () => h('div', { class: 'flex h-screen min-h-0 flex-col bg-surface' }, [
      h(ReviewShell, { scopeKey: 'harness', runId: 'run-1', hostConfirmFlow: false, class: 'min-h-0 flex-1' }, {
        stage: () => h('div', { class: 'p-6 text-sm text-txt3' }, '产物'),
        sidebar: () => connecting.value
          ? h(ReactConnectingState, { showConfirm: true })
          : h(ClarifyChat, { ref: chat, runId: 'run-1', nodeId: 'clarify', iteration: 1, turns: turns.value, done: false, active: true, reviewMode: false, class: 'min-h-0 flex-1' }),
      }),
    ])
  },
})

async function boot() {
  installIdleScrollbar()
  await initLocale(); await setLocale('zh-CN'); setTheme('light')
  createApp(App).use(i18n).mount('#app')
}
void boot()
