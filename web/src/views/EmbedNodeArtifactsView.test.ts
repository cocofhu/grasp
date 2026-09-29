// @vitest-environment happy-dom
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { saveEmbedSession } from '@/lib/inbox/embedChat'

const mocks = vi.hoisted(() => ({
  preview: vi.fn(),
  artifacts: vi.fn(),
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { runId: 'run-1', nodeId: 'ap1' } }),
}))

vi.mock('@/lib/inbox/gateShareLink', async () => {
  const actual = await vi.importActual<typeof import('@/lib/inbox/gateShareLink')>('@/lib/inbox/gateShareLink')
  return {
    ...actual,
    publicGateApi: {
      ...actual.publicGateApi,
      preview: mocks.preview,
      artifacts: mocks.artifacts,
    },
  }
})

vi.mock('@/components/run/ReactArtifactStage.vue', () => ({
  default: defineComponent({
    name: 'ReactArtifactStage',
    props: {
      artifacts: { type: Array, default: () => [] },
      previewArtifact: { type: String, default: '' },
      hideAppPreview: { type: Boolean, default: false },
      remoteKind: { type: String, default: '' },
      token: { type: String, default: '' },
      nodeId: { type: String, default: '' },
      nodeType: { type: String, default: '' },
    },
    setup(props) {
      return () =>
        h('div', {
          'data-testid': 'stage-stub',
          'data-hide-app': props.hideAppPreview ? '1' : '0',
          'data-remote': props.remoteKind,
          'data-token': props.token,
          'data-count': String((props.artifacts as unknown[]).length),
          'data-pin': String(props.previewArtifact || ''),
          'data-names': (props.artifacts as { name?: string }[]).map((a) => a.name).join(','),
        })
    },
  }),
}))

import EmbedNodeArtifactsView from './EmbedNodeArtifactsView.vue'

function i18n() {
  return createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
}

beforeEach(() => {
  localStorage.clear()
  sessionStorage.clear()
  history.replaceState(null, '', '/embed/runs/run-1/nodes/ap1/artifacts')
  mocks.preview.mockResolvedValue({
    status: 'active',
    kind: 'review',
    nodeType: 'approve',
    productName: 'plan.json',
  })
  mocks.artifacts.mockResolvedValue({
    status: 'active',
    artifacts: [
      {
        id: 'a1',
        name: 'plan.json',
        kind: 'json',
        nodeId: 'ap1',
        sizeBytes: 12,
        createdAt: '2026-09-01T00:00:00Z',
        revision: 1,
      },
    ],
    nodes: [{ id: 'ap1', type: 'approve', label: 'Approve' }],
  })
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('EmbedNodeArtifactsView', () => {
  it('reuses the drawer session and mounts the artifact stage without app preview', async () => {
    saveEmbedSession('run-1', 'ap1', { token: 'gse_' + 'ab'.repeat(16), expiresAt: '2099-01-01T00:00:00Z' })
    const w = mount(EmbedNodeArtifactsView, {
      global: { plugins: [i18n()], stubs: { Icon: true } },
    })
    await flushPromises()
    const stage = w.get('[data-testid="stage-stub"]')
    expect(stage.attributes('data-hide-app')).toBe('1')
    expect(stage.attributes('data-remote')).toBe('off')
    expect(stage.attributes('data-token')).toContain('gse_')
    expect(stage.attributes('data-count')).toBe('1')
    expect(mocks.artifacts).toHaveBeenCalled()
    w.unmount()
  })

  it('shows expired when no session is available', async () => {
    const w = mount(EmbedNodeArtifactsView, {
      global: { plugins: [i18n()], stubs: { Icon: true } },
    })
    await flushPromises()
    expect(w.find('[data-testid="embed-artifacts-expired"]').exists()).toBe(true)
    expect(w.find('[data-testid="stage-stub"]').exists()).toBe(false)
    w.unmount()
  })

  it('filters feedback artifacts and does not default-pin them (g2.1/g2.2)', async () => {
    saveEmbedSession('run-1', 'ap1', { token: 'gse_' + 'ab'.repeat(16), expiresAt: '2099-01-01T00:00:00Z' })
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nodeType: 'approve',
      productName: '',
    })
    mocks.artifacts.mockResolvedValue({
      status: 'active',
      artifacts: [
        {
          id: 'fb',
          name: 'feedback.clarify.approve_7gl6.i1.json',
          kind: 'json',
          nodeId: 'ap1',
          sizeBytes: 8,
          createdAt: '2026-09-01T00:00:00Z',
          revision: 1,
        },
        {
          id: 'idx',
          name: 'feedback_index.json',
          kind: 'json',
          nodeId: 'ap1',
          sizeBytes: 4,
          createdAt: '2026-09-01T00:00:00Z',
          revision: 1,
        },
        {
          id: 'a1',
          name: 'clarified_requirement.json',
          kind: 'json',
          nodeId: 'ap1',
          sizeBytes: 12,
          createdAt: '2026-09-01T00:00:00Z',
          revision: 1,
        },
      ],
      nodes: [{ id: 'ap1', type: 'approve', label: 'Approve' }],
    })
    const w = mount(EmbedNodeArtifactsView, {
      global: { plugins: [i18n()], stubs: { Icon: true } },
    })
    await flushPromises()
    const stage = w.get('[data-testid="stage-stub"]')
    expect(stage.attributes('data-count')).toBe('1')
    expect(stage.attributes('data-names')).toBe('clarified_requirement.json')
    expect(stage.attributes('data-pin')).toBe('clarified_requirement.json')
    expect(stage.attributes('data-names')).not.toContain('feedback')
    w.unmount()
  })

  it('shows empty pipeline products when only feedback remains (g2.2)', async () => {
    saveEmbedSession('run-1', 'ap1', { token: 'gse_' + 'ab'.repeat(16), expiresAt: '2099-01-01T00:00:00Z' })
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nodeType: 'approve',
      productName: 'feedback_index.json',
    })
    mocks.artifacts.mockResolvedValue({
      status: 'active',
      artifacts: [
        {
          id: 'idx',
          name: 'feedback_index.json',
          kind: 'json',
          nodeId: 'ap1',
          sizeBytes: 4,
          createdAt: '2026-09-01T00:00:00Z',
          revision: 1,
        },
        {
          id: 'fb',
          name: 'feedback.clarify.x.json',
          kind: 'json',
          nodeId: 'ap1',
          sizeBytes: 8,
          createdAt: '2026-09-01T00:00:00Z',
          revision: 1,
        },
      ],
      nodes: [{ id: 'ap1', type: 'approve', label: 'Approve' }],
    })
    const w = mount(EmbedNodeArtifactsView, {
      global: { plugins: [i18n()], stubs: { Icon: true } },
    })
    await flushPromises()
    const stage = w.get('[data-testid="stage-stub"]')
    expect(stage.attributes('data-count')).toBe('0')
    expect(stage.attributes('data-pin')).toBe('')
    expect(stage.attributes('data-names')).toBe('')
    w.unmount()
  })
})
