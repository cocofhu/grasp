// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { computed, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { describe, expect, it, vi } from 'vitest'
import type { SoloTurn } from '@/lib/solo/acpTurn'

const box = vi.hoisted(() => ({
  turns: undefined as undefined | import('vue').Ref<SoloTurn[]>,
  session: undefined as undefined | import('vue').Ref<{
    id: string
    title: string
    agentName: string
    status: string
    artifactRunId: string
    projectId: string
    project?: { name?: string }
    repos: Array<{ position: number; projectId: string; name: string; repoUrl: string; repoDirName: string; branch: string }>
  }>,
  status: undefined as undefined | import('vue').Ref<string>,
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
}))
vi.mock('@/lib/api/api', () => ({ api: { runArtifacts: vi.fn(async () => []), getSoloEnv: vi.fn(async () => []) } }))
vi.mock('@/lib/solo/useSoloSessions', () => ({ useSoloSessions: () => ({ upsert: vi.fn(), remove: vi.fn() }) }))
vi.mock('@/lib/solo/useSoloChat', () => ({
  useSoloChat: () => ({
    session: box.session,
    turns: box.turns,
    queued: ref([]),
    busy: ref(false),
    phase: ref(''),
    link: ref('open'),
    loading: ref(false),
    loadError: ref(''),
    actionError: ref(''),
    status: box.status,
    closed: ref(false),
    canSend: computed(() => true),
    load: vi.fn(),
    send: vi.fn(),
    removeQueued: vi.fn(),
  }),
}))

import SoloSessionPane from './SoloSessionPane.vue'
import SoloEnvPanel from './SoloEnvPanel.vue'
import pages from '@/locales/zh-CN/pages.json'

const turns = (box.turns = ref<SoloTurn[]>([]))
const sessionState = (box.session = ref({
  id: 's1',
  title: 'Solo',
  agentName: 'Coder',
  status: 'running',
  artifactRunId: '',
  projectId: '',
  project: undefined as { name?: string } | undefined,
  repos: [] as Array<{ position: number; projectId: string; name: string; repoUrl: string; repoDirName: string; branch: string }>,
}))
const statusState = (box.status = ref('running'))

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
          ReviewShell: { template: '<div><slot name="actions" /><slot name="sidebar" /></div>' },
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

  it('opens the environment panel and lists every repository on a sandbox error', async () => {
    turns.value = []
    statusState.value = 'error'
    sessionState.value = {
      id: 's1',
      title: 'Solo',
      agentName: 'Coder',
      status: 'error',
      artifactRunId: '',
      projectId: '',
      repos: [
        { position: 0, projectId: 'web', name: 'webapp', repoUrl: 'https://github.com/acme/webapp.git', repoDirName: 'webapp', branch: 'main' },
        { position: 1, projectId: 'api', name: 'api', repoUrl: 'https://github.com/acme/api.git', repoDirName: 'api', branch: 'main' },
      ],
    }
    const wrapper = mount(SoloSessionPane, {
      attachTo: document.body,
      props: { sessionId: 's1' },
      global: {
        plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': { ...pages } } })],
        stubs: {
          Icon: true,
          ReviewShell: { template: '<div><slot name="actions" /><slot name="sidebar" /></div>' },
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

    const toggle = wrapper.get('[data-testid="solo-env-toggle"]')
    expect(toggle.text()).toContain('会话环境')
    expect(wrapper.getComponent(SoloEnvPanel).props('open')).toBe(false)
    await toggle.trigger('click')
    expect(wrapper.getComponent(SoloEnvPanel).props('open')).toBe(true)

    const links = wrapper.findAll('[data-testid^="solo-error-edit-"]')
    expect(links.map((link) => link.attributes('href'))).toEqual([
      '/settings/projects?edit=web',
      '/settings/projects?edit=api',
    ])
    expect(links[0]?.text()).toContain('修改 webapp')
    expect(links[1]?.text()).toContain('修改 api')
    wrapper.unmount()
    statusState.value = 'running'
  })
})
