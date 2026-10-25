// @vitest-environment happy-dom
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { i18n as appI18n } from '@/lib/shared/i18n'
import { loadLocaleMessages } from '@/lib/shared/loadLocaleMessages'
import { confirmFlowCeremonyKey } from '@/lib/inbox/confirmFlowCeremony'
import { __resetConfirmFlowWindowForTests } from '@/lib/inbox/confirmFlowWindow'
import { __resetSidebarHandoffForTests } from '@/lib/inbox/sidebarHandoff'
import { beginBoot, bootKey, clearBootRecords, markBootLive, observeBootSandbox } from '@/lib/run/bootRecord'
import type { ClarifyTurn } from '@/lib/shared/types'
import { ref } from 'vue'
import ClarifyChat from './ClarifyChat.vue'

const KEY = bootKey('run-1', 'implement', 1)

beforeAll(async () => {
  const zh = await loadLocaleMessages('zh-CN')
  appI18n.global.setLocaleMessage('zh-CN', zh)
  appI18n.global.locale.value = 'zh-CN'
})

const human = '当前这一步的产物已经写好'
const reply = '已连接，开始执行这一步。'
const turns: ClarifyTurn[] = [
  { role: 'human', text: human, at: '2026-10-21T05:00:00Z' },
  { role: 'agent', text: reply, at: '2026-10-21T05:00:01Z' },
]

function mountChat(props: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  const ceremony = {
    phase: ref('idle'),
    reduceMotion: ref(false),
    playToken: ref(0),
    playing: ref(false),
    play: vi.fn(async () => {}),
    reset: vi.fn(),
  }
  return mount(ClarifyChat, {
    props: {
      runId: 'run-1',
      nodeId: 'implement',
      iteration: 1,
      turns,
      done: false,
      active: true,
      reviewMode: true,
      nodeTask: '上游交接说明',
      ...props,
    },
    attachTo: document.body,
    global: {
      plugins: [i18n],
      stubs: { Icon: true, ClarifyDemoFrame: true },
      provide: { [confirmFlowCeremonyKey as symbol]: ceremony },
    },
  })
}

function bootStates(root: ParentNode = document.body) {
  return [...root.querySelectorAll('[data-testid="clarify-boot-step"]')].map((el) => el.getAttribute('data-state'))
}

async function openDialog(w: ReturnType<typeof mountChat>) {
  const confirm = w.findAll('button').find((b) => b.text().includes('确认并流转'))
  expect(confirm).toBeTruthy()
  await confirm!.trigger('click')
  await flushPromises()
}

function acceptDialog() {
  const accept = document.querySelector('[data-testid="confirm-flow-dialog-accept"]') as HTMLButtonElement
  accept.click()
}

function cancelDialog() {
  const cancel = document.querySelector('[data-testid="confirm-flow-dialog-cancel"]') as HTMLButtonElement
  cancel.click()
}

afterEach(() => {
  document.body.innerHTML = ''
  clearBootRecords()
  __resetConfirmFlowWindowForTests()
  __resetSidebarHandoffForTests()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('ClarifyChat confirm-window boot gate', () => {
  it('keeps messages that were already on a connected step when the window opens', async () => {
    clearBootRecords()
    beginBoot(KEY, '2026-10-21T05:00:00Z')
    markBootLive(KEY)
    const w = mountChat()
    await flushPromises()
    expect(w.get('[data-testid="clarify-boot-done-title"]').text()).toBe('环境已就绪')
    expect(w.text()).toContain(reply)
    expect(w.text()).toContain(human)
    await openDialog(w)
    expect(w.find('[data-testid="clarify-boot-loader"]').exists()).toBe(false)
    expect(w.get('[data-testid="clarify-boot-done-title"]').text()).toBe('环境已就绪')
    expect(w.text()).toContain(reply)
    expect(w.text()).toContain(human)
    w.unmount()
  })

  it('hides the checklist, handoff and agent reply while the window is open, and keeps the current step', async () => {
    clearBootRecords()
    beginBoot(KEY, '2026-10-21T05:00:00Z')
    const w = mountChat()
    await flushPromises()
    expect(w.get('[data-testid="clarify-boot-title"]').text()).toBe('正在启动 Agent')
    expect(w.text()).not.toContain(reply)
    expect(w.find('[data-testid="clarify-node-task"]').exists()).toBe(false)
    expect(w.text()).toContain(human)

    await openDialog(w)
    expect(document.body.textContent).toContain('确认流转')
    expect(w.find('[data-testid="clarify-boot-loader"]').exists()).toBe(false)
    expect(w.text()).not.toContain('正在启动 Agent')
    expect(w.text()).not.toContain(reply)
    expect(w.find('[data-testid="clarify-node-task"]').exists()).toBe(false)
    expect(w.text()).toContain(human)
    w.unmount()
  })

  it('keeps the done hold from revealing the checklist or this boot’s messages', async () => {
    vi.useFakeTimers()
    clearBootRecords()
    beginBoot(KEY, '2026-10-21T05:00:00Z')
    const w = mountChat()
    await flushPromises()
    await openDialog(w)
    acceptDialog()
    await w.setProps({
      handoff: { phase: 'pending', at: '', target: '实现', targetAuto: true },
      followTail: { kind: 'auto', label: '实现', at: '' },
    })
    await nextTick()
    expect(document.body.textContent).toContain('已流转')
    expect(w.find('[data-testid="clarify-boot-loader"]').exists()).toBe(false)
    expect(w.text()).not.toContain(reply)
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(document.body.querySelector('[data-testid="confirm-flow-dialog-body"]')).toBeNull()
    expect(bootStates()).toEqual(['running', 'pending', 'pending'])
    expect(w.text()).not.toContain(reply)
    expect(w.find('[data-testid="clarify-node-task"]').exists()).toBe(false)
    w.unmount()
  })

  it('shows 环境已就绪 and then the messages when the boot is already live as the window leaves', async () => {
    vi.useFakeTimers()
    clearBootRecords()
    beginBoot(KEY, '2026-10-21T05:00:00Z')
    const w = mountChat()
    await flushPromises()
    await openDialog(w)
    acceptDialog()
    await w.setProps({
      handoff: { phase: 'pending', at: '', targetAuto: true },
      followTail: { kind: 'auto', label: '实现', at: '' },
    })
    markBootLive(KEY)
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(w.find('[data-testid="clarify-boot-loader"]').exists()).toBe(false)
    expect(w.get('[data-testid="clarify-boot-done-title"]').text()).toBe('环境已就绪')
    const html = w.html()
    expect(html.indexOf('clarify-boot-done')).toBeLessThan(html.indexOf('clarify-node-task'))
    expect(html.indexOf('clarify-node-task')).toBeLessThan(html.indexOf('clarify-agent-message'))
    expect(w.text()).toContain(reply)
    expect(w.text()).toContain(human)
    w.unmount()
  })

  it('does not play the checklist when the next process is not automatic', async () => {
    vi.useFakeTimers()
    clearBootRecords()
    beginBoot(KEY, '2026-10-21T05:00:00Z')
    const w = mountChat()
    await flushPromises()
    await openDialog(w)
    acceptDialog()
    await w.setProps({
      handoff: { phase: 'pending', at: '', target: '审批', targetAuto: false },
      followTail: { kind: 'waiting', label: '审批', at: '' },
    })
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(w.find('[data-testid="clarify-boot-loader"]').exists()).toBe(false)
    expect(w.find('[data-testid="clarify-boot-done"]').exists()).toBe(false)
    expect(w.text()).toContain(human)
    expect(w.text()).toContain(reply)
    w.unmount()
  })

  it('after cancel, keeps the current step and continues the checklist from the real progress', async () => {
    clearBootRecords()
    beginBoot(KEY, '2026-10-21T05:00:00Z')
    observeBootSandbox(KEY, { status: 'running', updatedAt: '2026-10-21T05:00:02Z' })
    const w = mountChat()
    await flushPromises()
    await openDialog(w)
    expect(w.find('[data-testid="clarify-boot-loader"]').exists()).toBe(false)
    cancelDialog()
    await flushPromises()
    expect(document.body.textContent).not.toContain('已流转')
    expect(w.text()).toContain(human)
    expect(bootStates()).toEqual(['done', 'done', 'running'])
    expect(w.text()).not.toContain(reply)
    w.unmount()
  })

  it('does not reveal this boot’s messages early when motion is reduced', async () => {
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: query.includes('prefers-reduced-motion'),
      media: query,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }))
    vi.useFakeTimers()
    clearBootRecords()
    beginBoot(KEY, '2026-10-21T05:00:00Z')
    const w = mountChat()
    await flushPromises()
    expect(w.text()).not.toContain(reply)
    await openDialog(w)
    acceptDialog()
    await w.setProps({
      followTail: { kind: 'auto', label: '实现', at: '' },
      handoff: { phase: 'pending', at: '', targetAuto: true },
    })
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(w.text()).not.toContain(reply)
    expect(w.find('[data-testid="clarify-node-task"]').exists()).toBe(false)
    expect(w.find('[data-testid="clarify-boot-loader"]').exists()).toBe(true)
    markBootLive(KEY)
    await nextTick()
    expect(w.text()).toContain(reply)
    expect(w.find('[data-testid="clarify-boot-loader"]').exists()).toBe(false)
    w.unmount()
  })
})
