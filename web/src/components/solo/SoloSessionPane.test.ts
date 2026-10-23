// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { computed, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { describe, expect, it, vi } from 'vitest'
import type { SoloTurn } from '@/lib/solo/acpTurn'

const turns = ref<SoloTurn[]>([])

vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }), RouterLink: { template: '<a><slot /></a>' } }))
vi.mock('@/lib/api/api', () => ({ api: { runArtifacts: vi.fn(async () => []) } }))
vi.mock('@/lib/solo/useSoloSessions', () => ({ useSoloSessions: () => ({ upsert: vi.fn(), remove: vi.fn() }) }))
vi.mock('@/lib/solo/useSoloChat', () => ({
  useSoloChat: () => ({
    session: ref({ id: 's1', title: 'Solo', agentName: 'Coder', status: 'running', artifactRunId: '' }),
    turns,
    queued: ref([]),
    busy: ref(false),
    phase: ref(''),
    link: ref('open'),
    loading: ref(false),
    loadError: ref(''),
    actionError: ref(''),
    status: ref('running'),
    closed: ref(false),
    canSend: computed(() => true),
    load: vi.fn(),
    send: vi.fn(),
    removeQueued: vi.fn(),
  }),
}))

import SoloSessionPane from './SoloSessionPane.vue'

function turn(id: string, role: SoloTurn['role'], text: string): SoloTurn {
  const steps: SoloTurn['steps'] = role === 'agent' ? [{ kind: 'message', text }] : []
  return { id, role, text, tools: [], steps, plan: [], streaming: false }
}

describe('SoloSessionPane', () => {
  it('renders the message rail with one mark per user turn', async () => {
    turns.value = [
      turn('u1', 'user', '修一下登录'),
      turn('a1', 'agent', '已修复'),
      turn('s1', 'system', '已重启'),
      turn('u2', 'user', '再加个测试'),
      turn('a2', 'agent', '已添加'),
    ]
    const wrapper = mount(SoloSessionPane, {
      attachTo: document.body,
      props: { sessionId: 's1' },
      global: {
        plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': {} } })],
        stubs: {
          Icon: true,
          ReviewShell: { template: '<div><slot name="sidebar" /></div>' },
          ReactArtifactStage: true,
          AgentTimeline: true,
          SoloStartupCard: true,
          SoloEnvPanel: true,
          SoloLimitModal: true,
          AppModal: true,
          ChatImagePreviewModal: true,
        },
      },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="chat-message-rail"]').exists()).toBe(true)
    const marks = wrapper.findAll('[data-testid="chat-message-rail-mark"]')
    expect(marks).toHaveLength(2)
    expect(marks.map(mark => mark.attributes('data-message-id'))).toEqual(['u1', 'u2'])
    expect(wrapper.findAll('[data-chat-message-id]').map(el => el.attributes('data-chat-message-id'))).toEqual(['u1', 'u2'])
    wrapper.unmount()
  })
})
