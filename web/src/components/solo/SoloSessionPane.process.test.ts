// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { computed, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { turnsFromMessages, type SoloTurn } from '@/lib/solo/acpTurn'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

type ProcessPage = { next: number; hasMore: boolean; loading: boolean; error: string; legacyIncomplete: boolean }
const state = vi.hoisted(() => ({
  turns: undefined as unknown as import('vue').Ref<SoloTurn[]>,
  pages: undefined as unknown as import('vue').Ref<Record<string, ProcessPage>>,
  loadProcess: vi.fn(),
}))
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
}))
vi.mock('@/lib/api/api', () => ({ api: {
  getSoloResources: vi.fn(async () => ({ artifacts: [], ports: [] })),
  getSoloEnv: vi.fn(async () => []), listAgents: vi.fn(async () => []),
  listModelConfigurations: vi.fn(async () => ({ items: [] })),
  listModelConnections: vi.fn(async () => ({ items: [] })),
  cliModels: vi.fn(async () => ({ models: [] })),
} }))
vi.mock('@/lib/solo/useSoloSessions', () => ({ useSoloSessions: () => ({ upsert: vi.fn(), remove: vi.fn() }) }))
vi.mock('@/lib/solo/useSoloChat', () => ({ useSoloChat: () => ({
  session: ref({ id: 's1', userId: 'u', title: 'Solo', agentName: 'coder', status: 'running', artifactRunId: '', repos: [] }),
  turns: state.turns, processPages: state.pages, loadProcess: state.loadProcess,
  hasEarlier: ref(false), loadingEarlier: ref(false), earlierError: ref(''), loadEarlier: vi.fn(),
  queued: ref([]), displayQueued: ref([]), busy: ref(false), phase: ref(''), link: ref('open'),
  loading: ref(false), loadError: ref(''), actionError: ref(''), status: ref('running'),
  closed: computed(() => false), canSend: computed(() => true),
  currentModelId: ref(''), currentReasoningEffort: ref(''), modelChangePending: ref(false),
  load: vi.fn(), send: vi.fn(), setCurrentModel: vi.fn(), setCurrentReasoningEffort: vi.fn(), removeQueued: vi.fn(),
}) }))

import SoloSessionPane from './SoloSessionPane.vue'

const saved = {
  id: 'reply:q', sessionId: 's1', turnId: 'q', role: 'assistant' as const, content: '最终答案',
  createdAt: '2026-11-10T00:00:00Z', durationMs: 42_000,
  execution: { agentName: 'coder', backend: 'opencode', startedAt: '2026-11-10T00:00:00Z' },
  eventLog: { version: 1, from: 1, through: 3, count: 3, generation: 1 },
}

function mountPane() {
  return mount(SoloSessionPane, { props: { sessionId: 's1' }, global: {
    plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': { ...common, ...pages } } })],
    stubs: {
      Icon: true, SoloWorkflowPanel: true,
      ReviewShell: { template: '<div><slot name="stage" /><slot name="sidebar" /></div>' },
      ReactArtifactStage: true, SoloStartupCard: true, SoloModelSelect: true, SoloLimitModal: true,
      AppModal: true, AppDrawer: true, ChatImagePreviewModal: true, ChatMessageRail: true, ChatComposer: true,
    },
  } })
}

describe('SoloSessionPane persisted process', () => {
  beforeEach(() => {
    state.turns = ref(turnsFromMessages([saved], () => '', '工具'))
    state.pages = ref({})
    state.loadProcess.mockReset().mockImplementation((turn: SoloTurn) => {
      const id = turn.historyMessageId!
      state.pages.value[id] = { next: state.pages.value[id]?.next ?? -1, hasMore: true, loading: true, error: '', legacyIncomplete: false }
    })
  })

  it.each([false, true])('keeps the elapsed fold after loading a reply-only journal (legacy incomplete: %s)', async legacyIncomplete => {
    const w = mountPane()
    try {
      await flushPromises()
      const fold = () => w.get('[data-testid="agent-timeline-elapsed"]')
      expect(w.find('[data-testid="solo-process-empty"]').exists()).toBe(false)
      await fold().trigger('click')
      expect(state.loadProcess).toHaveBeenCalledTimes(1)
      state.turns.value = turnsFromMessages([{ ...saved, events: [
        { data: { type: 'prompt_begin' } },
        { data: { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: saved.content } } } },
        { data: { type: 'prompt_done' } },
      ] }], () => '', '工具').map(turn => ({ ...turn, processComplete: true }))
      state.pages.value['reply:q'] = { next: 3, hasMore: false, loading: false, error: '', legacyIncomplete }
      await flushPromises()
      expect(fold().text()).toBe('用时 42s')
      expect(fold().attributes('aria-expanded')).toBe('true')
      expect(w.findAll('[data-testid="solo-agent-message"]').map(row => row.text())).toEqual([saved.content])
      expect(w.find('[data-testid="solo-load-process"]').exists()).toBe(false)
      if (legacyIncomplete) {
        expect(w.find('[data-testid="solo-process-empty"]').exists()).toBe(false)
        expect(w.get('[data-testid="solo-process-status"]').text()).toContain('此记录来自旧版本')
      } else {
        expect(w.get('[data-testid="solo-process-empty"]').text()).toBe('本轮未记录额外的思考或工具调用过程。')
      }
      await fold().trigger('click')
      expect(w.find('[data-testid="agent-timeline-work"]').exists()).toBe(false)
      await fold().trigger('click')
      expect(fold().attributes('aria-expanded')).toBe('true')
      expect(state.loadProcess).toHaveBeenCalledTimes(1)
    } finally { w.unmount() }
  })

  it('uses the original elapsed fold for lazy history, retry and next page without a separate process button', async () => {
    const w = mountPane()
    try {
      await flushPromises()
      const fold = () => w.get('[data-testid="agent-timeline-elapsed"]')
      expect(fold().text()).toBe('用时 42s')
      expect(fold().attributes('aria-expanded')).toBe('false')
      expect(w.find('[data-testid="solo-load-process"]').exists()).toBe(false)
      expect(state.loadProcess).not.toHaveBeenCalled()
      const answer = () => w.findAll('[data-testid="solo-agent-message"]').filter(m => m.text() === '最终答案')
      expect(answer()).toHaveLength(1)
      await fold().trigger('click')
      expect(state.loadProcess).toHaveBeenCalledTimes(1)
      expect(w.get('[data-testid="agent-timeline-work"] [role="status"]').text()).toBe('加载中')
      expect(answer()).toHaveLength(1)

      state.pages.value['reply:q'] = { next: -1, hasMore: true, loading: false, error: 'storage temporarily unavailable', legacyIncomplete: false }
      await flushPromises()
      const retry = w.get('[data-testid="agent-timeline-work"] [data-testid="solo-load-process"]')
      expect(retry.text()).toBe('重试')
      expect(w.get('[data-testid="agent-timeline-work"] [role="alert"]').text()).toContain('storage temporarily unavailable')
      await retry.trigger('click')
      expect(state.loadProcess).toHaveBeenCalledTimes(2)
      expect(answer()).toHaveLength(1)

      state.turns.value = turnsFromMessages([{ ...saved, events: [
        { data: { type: 'session_update', update: { sessionUpdate: 'agent_thought_chunk', content: { text: '已核对真实文件' } } } },
        { data: { type: 'session_update', update: { sessionUpdate: 'tool_call', toolCallId: 'read', title: 'read_file', status: 'completed' } } },
      ] }], () => '', '工具')
      state.pages.value['reply:q'] = { next: 2, hasMore: true, loading: false, error: '', legacyIncomplete: true }
      await flushPromises()
      expect(fold().attributes('aria-expanded')).toBe('true')
      expect(w.get('[data-testid="agent-timeline-work"] [data-testid="agent-tool-group"]').exists()).toBe(true)
      expect(w.get('[data-testid="agent-timeline-work"]').text()).toContain('此记录来自旧版本')
      expect(w.get('[data-testid="solo-load-process"]').text()).toBe('加载更多过程')
      await w.get('[data-testid="solo-load-process"]').trigger('click')
      expect(state.loadProcess).toHaveBeenCalledTimes(3)
      state.pages.value['reply:q'] = { next: 3, hasMore: false, loading: false, error: '', legacyIncomplete: true }
      await flushPromises()
      expect(w.find('[data-testid="solo-load-process"]').exists()).toBe(false)
      expect(answer()).toHaveLength(1)
      await fold().trigger('click')
      expect(w.find('[data-testid="agent-timeline-work"]').exists()).toBe(false)
      await fold().trigger('click')
      expect(state.loadProcess).toHaveBeenCalledTimes(3)
      expect(w.get('[data-testid="agent-timeline-work"]').text()).toContain('此记录来自旧版本')
      expect(answer()).toHaveLength(1)
    } finally { w.unmount() }
  })

  it('keeps recorded time and the fold when a live reply is replaced by its persisted journal metadata', async () => {
    state.turns.value = [{ id: 'live:q', role: 'agent', text: '最终答案', tools: [],
      steps: [{ kind: 'thought', text: '恢复后执行的过程' }, { kind: 'message', text: '最终答案' }], plan: [], streaming: true }]
    const w = mountPane()
    try {
      await flushPromises()
      expect(w.find('[data-testid="solo-agent-activity"]').exists()).toBe(true)
      expect(w.find('[data-testid="agent-timeline-elapsed"]').exists()).toBe(false)
      state.turns.value = turnsFromMessages([saved], () => '', '工具')
      await flushPromises()
      expect(w.find('[data-testid="solo-agent-activity"]').exists()).toBe(false)
      expect(w.get('[data-testid="agent-timeline-elapsed"]').text()).toBe('用时 42s')
      expect(w.get('[data-testid="agent-timeline-elapsed"]').attributes('aria-expanded')).toBe('false')
      expect(w.get('[data-testid="solo-agent-message"]').text()).toBe('最终答案')
      expect(state.loadProcess).not.toHaveBeenCalled()
    } finally { w.unmount() }
  })
})
