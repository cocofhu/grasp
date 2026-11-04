// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Artifact, Run } from '@/lib/shared/types'
import { resetStageOpenStateForTests } from '@/lib/run/reactArtifactPreview'
import { resetConversationDrawers } from '@/lib/chat/conversationDrawer'
import { resetArtifactEditorsForTest, setArtifactSessionBusy, useArtifactEditor } from '@/lib/run/useArtifactEditor'
import { api } from '@/lib/api/api'
import ReactArtifactStage from './ReactArtifactStage.vue'

vi.mock('@/lib/api/api', () => ({
  api: {
    artifactContent: vi.fn(async () => ({ content: '{"title":"p"}', etag: 'e1', revision: 1 })),
    artifactVersions: vi.fn(async () => []),
    artifactVersionContent: vi.fn(),
    getRunNodeSandbox: vi.fn(async () => ({ id: 42 })),
    nodePreviews: vi.fn(async () => ({ ports: [] })),
    nodeSandboxLog: vi.fn(async () => ({ content: '', live: false, found: false })),
    sandboxIdeUrl: () => 'about:blank',
    sandboxTerminalWsUrl: () => 'ws://test.local',
  },
}))

const stubs = {
  Icon: true,
  HtmlPreview: true,
  ArtifactPreview: defineComponent({
    props: { artifact: Object, editable: Boolean, editState: Object, startInEdit: Boolean },
    emits: ['saved', 'edit-start'],
    template:
      '<div data-testid="artifact-preview" :data-editable="editable ? 1 : 0" :data-start-edit="startInEdit ? 1 : 0" :data-edit-reason="editState?.reason">' +
      '{{ artifact?.name }}' +
      '<button data-testid="stub-saved" @click="$emit(\'saved\', { name: artifact.name, revision: 3, changed: true })" />' +
      '<button data-testid="stub-edit-start" @click="$emit(\'edit-start\', artifact.name)" />' +
      '</div>',
  }),
}

function i18n() {
  return createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
}

function art(partial: Partial<Artifact> & Pick<Artifact, 'id' | 'name'>): Artifact {
  return {
    kind: 'json',
    nodeId: 'plan_1',
    runId: 'run-edit',
    workflowName: 'wf',
    sizeBytes: 12,
    createdAt: '2026-08-01T00:00:00Z',
    revision: 1,
    ...partial,
  }
}

const ARTIFACTS = [
  art({ id: 'p', name: 'plan.json' }),
  art({ id: 'img', name: 'shot.png', kind: 'image' }),
]

function waiting(extra: Partial<Run> = {}): Run {
  return { id: 'run-edit', status: 'waiting_human', nodes: [], ...extra } as unknown as Run
}

function mountStage(props: Record<string, unknown> = {}) {
  return mount(ReactArtifactStage, {
    props: { artifacts: ARTIFACTS, runId: 'run-edit', nodeId: 'review_1', run: waiting(), remoteKind: 'off', ...props },
    attachTo: document.body,
    global: { plugins: [i18n()], stubs },
  })
}

describe('ReactArtifactStage editing', () => {
  beforeEach(() => {
    resetStageOpenStateForTests(':run-')
    resetConversationDrawers()
    resetArtifactEditorsForTest()
    vi.mocked(api.artifactVersions).mockResolvedValue([])
  })
  afterEach(() => {
    resetStageOpenStateForTests(':run-')
    document.body.innerHTML = ''
    vi.restoreAllMocks()
  })

  it('renders exactly as before when editable is off', async () => {
    const w = mountStage()
    await flushPromises()
    expect(w.find('[data-testid^="react-artifact-card-edit-"]').exists()).toBe(false)
    // Another node's artifact keeps the old read-only badge.
    expect(w.findAll('[data-testid="react-artifact-card-readonly"]')).toHaveLength(2)
    await w.get('[data-testid="react-artifact-card-plan.json"]').trigger('click')
    expect(w.get('[data-testid="artifact-preview"]').attributes('data-editable')).toBe('0')
    w.unmount()
  })

  it('shows quick edit only on editable artifacts and badges only read-only ones', async () => {
    const w = mountStage({ editable: true })
    await flushPromises()
    expect(w.find('[data-testid="react-artifact-card-edit-plan.json"]').exists()).toBe(true)
    expect(w.find('[data-testid="react-artifact-card-edit-shot.png"]').exists()).toBe(false)
    const badges = w.findAll('[data-testid="react-artifact-card-readonly"]')
    expect(badges).toHaveLength(1)
    expect(w.get('[data-testid="react-artifact-card-shot.png"]').find('[data-testid="react-artifact-card-readonly"]').exists()).toBe(true)
    w.unmount()
  })

  it('quick edit opens the tab in edit mode; a plain card click only previews', async () => {
    const w = mountStage({ editable: true })
    await flushPromises()
    await w.get('[data-testid="react-artifact-card-plan.json"]').trigger('click')
    expect(w.get('[data-testid="artifact-preview"]').attributes('data-start-edit')).toBe('0')
    expect(w.get('[data-testid="artifact-preview"]').attributes('data-editable')).toBe('1')
    await w.get('[data-testid="react-artifact-tab-grid"]').trigger('click')
    await w.get('[data-testid="react-artifact-card-edit-plan.json"]').trigger('click')
    expect(w.get('[data-testid="react-artifact-tab-plan.json"]').attributes('aria-selected')).toBe('true')
    expect(w.get('[data-testid="artifact-preview"]').attributes('data-start-edit')).toBe('1')
    await w.get('[data-testid="stub-edit-start"]').trigger('click')
    expect(w.get('[data-testid="artifact-preview"]').attributes('data-start-edit')).toBe('0')
    w.unmount()
  })

  it('opens the draft a failed save-and-send points at', async () => {
    const w = mountStage({ editable: true })
    await flushPromises()
    expect(w.find('[data-testid="react-artifact-tab-plan.json"]').exists()).toBe(false)
    useArtifactEditor('run-edit').requestFocus('plan.json')
    await flushPromises()
    expect(w.get('[data-testid="react-artifact-tab-plan.json"]').attributes('aria-selected')).toBe('true')
    expect(w.get('[data-testid="artifact-preview"]').attributes('data-start-edit')).toBe('1')
    w.unmount()
  })

  it('blocks quick edit with a reason when the run or session is not editable', async () => {
    const w = mountStage({ editable: true, run: waiting({ status: 'completed' }) })
    await flushPromises()
    const btn = w.get('[data-testid="react-artifact-card-edit-plan.json"]')
    expect(btn.attributes('aria-disabled')).toBe('true')
    expect(btn.attributes('title')).toBe('运行已结束，产物只读')
    await btn.trigger('click')
    expect(w.find('[data-testid="react-artifact-tab-plan.json"]').exists()).toBe(false)

    await w.setProps({ run: waiting() })
    setArtifactSessionBusy('run-edit', 'chat:review_1', true)
    await flushPromises()
    expect(w.get('[data-testid="react-artifact-card-edit-plan.json"]').attributes('title')).toBe('Agent 正在回复，结束后可保存')
    await w.setProps({ sessionBusy: false })
    expect(w.get('[data-testid="react-artifact-card-edit-plan.json"]').attributes('aria-disabled')).toBeUndefined()
    w.unmount()
  })

  it('marks unsaved tabs and confirms before closing them', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true)
    const w = mountStage({ editable: true })
    await flushPromises()
    await w.get('[data-testid="react-artifact-card-plan.json"]').trigger('click')
    const ed = useArtifactEditor('run-edit')
    await ed.begin(ARTIFACTS[0]!)
    expect(w.find('[data-testid="react-artifact-tab-dirty-plan.json"]').exists()).toBe(false)
    ed.update('plan.json', '{"title":"q"}')
    await flushPromises()
    expect(w.find('[data-testid="react-artifact-tab-dirty-plan.json"]').exists()).toBe(true)
    const close = w.get('[data-testid="react-artifact-tab-close-plan.json"]')
    expect(close.classes()).toContain('is-dirty')
    await close.trigger('click')
    expect(confirm).toHaveBeenCalledTimes(1)
    expect(w.find('[data-testid="react-artifact-tab-plan.json"]').exists()).toBe(true)
    await close.trigger('click')
    expect(w.find('[data-testid="react-artifact-tab-plan.json"]').exists()).toBe(false)
    expect(ed.draft('plan.json')).toBeUndefined()
    w.unmount()
  })

  it('returns to the latest version and flashes the chip after a save', async () => {
    vi.mocked(api.artifactVersions).mockResolvedValue([
      { artifactId: 'p', revision: 1, nodeId: 'plan_1', sizeBytes: 3, createdAt: '2026-08-01T00:00:00Z' },
    ])
    const w = mountStage({ editable: true, artifacts: [art({ id: 'p', name: 'plan.json', revision: 2 })] })
    await flushPromises()
    expect(w.find('[data-testid="react-artifact-version-chip"]').exists()).toBe(true)
    await w.get('[data-testid="react-artifact-card-plan.json"]').trigger('click')
    const before = vi.mocked(api.artifactVersions).mock.calls.length
    await w.get('[data-testid="stub-saved"]').trigger('click')
    await flushPromises()
    expect(vi.mocked(api.artifactVersions).mock.calls.length).toBeGreaterThan(before)
    await w.get('[data-testid="react-artifact-tab-grid"]').trigger('click')
    expect(w.find('.ed-chip-flash').exists()).toBe(true)
    w.unmount()
  })
})

describe('stage hosts', () => {
  const src = (p: string) => readFileSync(resolve(__dirname, '../..', p), 'utf8')
  const stageBlocks = (text: string) => text.split('<ReactArtifactStage').slice(1).map((b) => b.split('/>')[0]!)

  it('logged-in run hosts enable editing', () => {
    for (const p of [
      'components/run/RunClarifyPanel.vue',
      'components/run/RunReviewPanel.vue',
      'views/RunLiveView.vue',
      'views/GatesInboxView.vue',
    ]) {
      const blocks = stageBlocks(src(p))
      expect(blocks.length, p).toBeGreaterThan(0)
      for (const b of blocks) expect(b, p).toMatch(/\n\s+editable\n/)
    }
  })

  it('share, embed and solo hosts never enable editing', () => {
    for (const p of ['views/PublicGateApprovalView.vue', 'views/EmbedNodeArtifactsView.vue', 'components/solo/SoloSessionPane.vue']) {
      for (const b of stageBlocks(src(p))) expect(b, p).not.toMatch(/\beditable\b/)
    }
  })
})
