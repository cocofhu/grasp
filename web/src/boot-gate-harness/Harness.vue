<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import ClarifyChat from '@/components/run/ClarifyChat.vue'
import { provideConfirmFlowCeremony } from '@/lib/inbox/confirmFlowCeremony'
import { __resetConfirmFlowWindowForTests } from '@/lib/inbox/confirmFlowWindow'
import {
  __resetSidebarHandoffForTests,
  armSidebarHandoff,
  noteSidebarHandoffOutcome,
} from '@/lib/inbox/sidebarHandoff'
import { beginBoot, bootKey, clearBootRecords, markBootLive, observeBootSandbox } from '@/lib/run/bootRecord'
import type { ClarifyTurn } from '@/lib/shared/types'

provideConfirmFlowCeremony()

const KEY = bootKey('run-1', 'implement', 1)
const human = '当前这一步的产物已经写好'
const reply = '已连接，开始执行这一步。'
const turns: ClarifyTurn[] = [
  { role: 'human', text: human, at: '2026-10-21T05:00:00Z' },
  { role: 'agent', text: reply, at: '2026-10-21T05:00:01Z' },
]

type Scenario = 'open' | 'hold' | 'auto' | 'live' | 'human' | 'cancel' | 'reduce' | 'unknown' | 'recovered'

const titles: Record<Scenario, string> = {
  open: '窗口打开，启动尚未完成',
  hold: '已流转完成态仍在屏上',
  auto: '窗口离开后，下一过程是自动过程',
  live: '窗口离开时已经连上',
  human: '窗口离开后，下一过程不是自动过程',
  cancel: '取消后从真实进度继续',
  reduce: '减少动态，窗口离开后仍未连上',
  unknown: '窗口离开时，下一过程尚未判定为自动',
  recovered: '随后 node_enter 确认是自动过程',
}

const scenario = ref<Scenario>('open')
const showChat = ref(false)
const chatKey = ref(0)
const handoff = ref<{ phase: 'pending' | 'done'; at: string; target?: string; targetAuto?: boolean } | null>(null)
const followTail = ref<{ kind: 'auto' | 'waiting' | 'ended' | 'error'; label?: string; at: string } | null>(null)
const phase = ref('准备')
const observed = ref('')

function clickComposerConfirm() {
  const button = [...document.querySelectorAll('button')].find((el) => (el.textContent || '').includes('确认并流转'))
  if (!button) throw new Error('找不到按钮：确认并流转')
  ;(button as HTMLButtonElement).click()
}

async function clickTestId(id: string) {
  for (let i = 0; i < 30; i += 1) {
    const button = document.querySelector(`[data-testid="${id}"]`) as HTMLButtonElement | null
    if (button) {
      button.click()
      return
    }
    await new Promise((resolve) => setTimeout(resolve, 50))
  }
  throw new Error(`找不到 ${id}`)
}

function readSurface() {
  const dialog = document.body.textContent?.includes('确认流转') && !!document.querySelector('[data-testid="confirm-flow-dialog-body"]')
  const done = (document.body.textContent || '').includes('已流转') && !!document.querySelector('[data-testid="confirm-flow-dialog-done"]')
  const starting = !!document.querySelector('[data-testid="clarify-boot-title"]')
  const ready = document.querySelector('[data-testid="clarify-boot-done-title"]')?.textContent?.trim() || ''
  const replyOn = (document.querySelector('[data-testid="clarify-scroller"]')?.textContent || '').includes(reply)
  const handoffOn = !!document.querySelector('[data-testid="clarify-node-task"]')
  const humanOn = (document.querySelector('[data-testid="clarify-scroller"]')?.textContent || '').includes(human)
  const steps = [...document.querySelectorAll('[data-testid="clarify-boot-step"]')].map((el) => el.getAttribute('data-state')).join('/')
  return `窗口=${dialog ? '开' : '关'} 完成态=${done ? '是' : '否'} 清单=${starting ? steps || '有' : '无'} 就绪=${ready || '无'} 交接=${handoffOn ? '有' : '无'} 回复=${replyOn ? '有' : '无'} 当前消息=${humanOn ? '有' : '无'}`
}

function onFinish() {
  const at = ''
  if (scenario.value === 'auto' || scenario.value === 'reduce') {
    handoff.value = { phase: 'pending', at, target: '实现', targetAuto: true }
    followTail.value = { kind: 'auto', label: '实现', at }
    return
  }
  if (scenario.value === 'live') {
    markBootLive(KEY)
    handoff.value = { phase: 'pending', at, targetAuto: true }
    followTail.value = { kind: 'auto', label: '实现', at }
    return
  }
  if (scenario.value === 'human') {
    handoff.value = { phase: 'pending', at, target: '审批', targetAuto: false }
    followTail.value = { kind: 'waiting', label: '审批', at }
    return
  }
  if (scenario.value === 'unknown' || scenario.value === 'recovered') {
    armSidebarHandoff('run-1', 'implement', Date.now(), null)
  }
}

async function run(name: Scenario) {
  scenario.value = name
  phase.value = '切换中'
  observed.value = ''
  document.documentElement.dataset.phase = 'busy'
  showChat.value = false
  await nextTick()
  clearBootRecords()
  __resetConfirmFlowWindowForTests()
  __resetSidebarHandoffForTests()
  handoff.value = null
  followTail.value = null
  beginBoot(KEY, '2026-10-21T05:00:00Z')
  if (name === 'cancel') {
    observeBootSandbox(KEY, { status: 'running', updatedAt: '2026-10-21T05:00:02Z' })
  }
  if (name === 'reduce') {
    window.matchMedia = ((query: string) => ({
      matches: query.includes('prefers-reduced-motion'),
      media: query,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
      onchange: null,
    })) as typeof window.matchMedia
  }
  chatKey.value += 1
  showChat.value = true
  await nextTick()
  await nextTick()
  clickComposerConfirm()
  await nextTick()
  if (name === 'open') {
    phase.value = titles[name]
    observed.value = readSurface()
    document.documentElement.dataset.phase = 'ready'
    return
  }
  if (name === 'cancel') {
    await clickTestId('confirm-flow-dialog-cancel')
    await nextTick()
    phase.value = titles[name]
    observed.value = readSurface()
    document.documentElement.dataset.phase = 'ready'
    return
  }
  await clickTestId('confirm-flow-dialog-accept')
  await nextTick()
  if (name === 'hold') {
    phase.value = titles[name]
    observed.value = readSurface()
    document.documentElement.dataset.phase = 'ready'
    return
  }
  await new Promise((resolve) => setTimeout(resolve, 1100))
  if (name === 'recovered') {
    noteSidebarHandoffOutcome('run-1', 'auto', { nodeId: 'next', name: '实现' })
    await nextTick()
  }
  phase.value = titles[name]
  observed.value = readSurface()
  document.documentElement.dataset.phase = 'ready'
}

onMounted(() => {
  const requested = new URLSearchParams(location.search).get('s') as Scenario | null
  const initial = requested && requested in titles ? requested : 'open'
  void run(initial)
})

defineExpose({ run })
</script>

<template>
  <div class="harness" data-testid="boot-gate-harness">
    <header class="bar">
      <strong>{{ phase }}</strong>
      <span class="obs">{{ observed }}</span>
      <nav>
        <button type="button" @click="run('open')">窗口打开</button>
        <button type="button" @click="run('hold')">完成态</button>
        <button type="button" @click="run('auto')">自动过程</button>
        <button type="button" @click="run('live')">已经就绪</button>
        <button type="button" @click="run('human')">非自动</button>
        <button type="button" @click="run('cancel')">取消</button>
        <button type="button" @click="run('reduce')">减少动态</button>
        <button type="button" @click="run('unknown')">尚未判定</button>
        <button type="button" @click="run('recovered')">随后确认自动</button>
      </nav>
    </header>
    <div class="stage">
      <ClarifyChat
        v-if="showChat"
        :key="chatKey"
        run-id="run-1"
        node-id="implement"
        :references-enabled="false"
        :iteration="1"
        :turns="turns"
        :done="false"
        :active="true"
        review-mode
        node-task="上游交接说明"
        :handoff="handoff"
        :follow-tail="followTail"
        @finish="onFinish"
      />
    </div>
  </div>
</template>

<style scoped>
.harness {
  display: flex;
  height: 100vh;
  min-height: 0;
  flex-direction: column;
  background: rgb(var(--c-base));
}
.bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 12px;
  border-bottom: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-surface));
  padding: 8px 12px;
  font-size: 13px;
}
.obs {
  min-width: 0;
  flex: 1;
  color: rgb(var(--c-txt2));
}
nav {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
nav button {
  border: 1px solid rgb(var(--c-line));
  border-radius: 6px;
  background: rgb(var(--c-elevated));
  padding: 4px 8px;
  font-size: 12px;
}
.stage {
  min-height: 0;
  flex: 1;
}
</style>
