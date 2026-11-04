// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Artifact, Run } from '@/lib/shared/types'
import { resetArtifactEditorsForTest, useArtifactEditor } from '@/lib/run/useArtifactEditor'
import ArtifactPreview from './ArtifactPreview.vue'

const apiMocks = vi.hoisted(() => ({
  artifactContent: vi.fn(),
  artifactVersions: vi.fn(),
  artifactVersionContent: vi.fn(),
  artifactDownloadUrl: vi.fn((id: string) => `http://test/api/artifacts/${id}/download`),
  deleteArtifact: vi.fn(),
  saveRunArtifact: vi.fn(),
}))
const toastMocks = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn(), warn: vi.fn() }))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api, ...apiMocks } }
})
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => toastMocks }))
vi.mock('@/components/ui/CodeEditor.vue', () => ({
  __esModule: true,
  default: {
    props: ['modelValue', 'language', 'readonly'],
    emits: ['update:modelValue', 'ready'],
    template:
      '<textarea data-testid="code-editor-stub" :data-language="language" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
  },
}))

const PLAN = JSON.stringify(
  {
    title: '计划',
    goals: [
      { id: 'g1', title: 'A', status: 'pending' },
      { id: 'g2', title: 'B', status: 'pending' },
    ],
  },
  null,
  2,
)

function art(partial: Partial<Artifact> = {}): Artifact {
  return {
    id: 'a-plan',
    name: 'plan.json',
    kind: 'json',
    nodeId: 'plan_1',
    runId: 'run-1',
    sizeBytes: 10,
    createdAt: '2026-08-10T12:00:00Z',
    revision: 3,
    ...partial,
  } as Artifact
}

function waitingRun(extra: Partial<Run> = {}): Run {
  return { id: 'run-1', status: 'waiting_human', ...extra } as Run
}

function mountPreview(props: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(ArtifactPreview, {
    props: { artifact: art(), runId: 'run-1', run: waitingRun(), editable: true, hideFileRow: true, ...props },
    attachTo: document.body,
    global: {
      plugins: [i18n],
      stubs: {
        Teleport: false,
        Icon: true,
        HtmlPreview: {
          props: ['html'],
          template: '<div data-testid="html-preview-stub"><slot name="toolbar-actions" />{{ html }}</div>',
        },
        StructuredArtifactView: { template: '<div data-testid="structured-view-stub" />' },
        AppModal: true,
        SelectionAddToChat: true,
        RefreshStrip: true,
        HardLoadLayer: true,
        SlidingTabs: { template: '<div><slot /></div>' },
        ParagraphInput: {
          props: ['text'],
          emits: ['update:text'],
          template: '<textarea class="pi-stub" :value="text" @input="$emit(\'update:text\', $event.target.value)" />',
        },
        AppSelect: {
          props: ['modelValue', 'options'],
          emits: ['update:modelValue'],
          template:
            '<select class="select-stub" :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option></select>',
        },
      },
    },
  })
}

async function enterEdit(w: ReturnType<typeof mountPreview>) {
  await w.get('[data-testid="artifact-preview-edit"]').trigger('click')
  await flushPromises()
}

describe('ArtifactPreview edit mode', () => {
  beforeEach(() => {
    resetArtifactEditorsForTest()
    vi.clearAllMocks()
    apiMocks.artifactVersions.mockResolvedValue([])
    apiMocks.artifactContent.mockImplementation(async (id: string) => ({
      id,
      name: 'plan.json',
      content: PLAN,
      etag: 'etag-3',
      revision: 3,
    }))
  })
  afterEach(() => {
    document.body.innerHTML = ''
    vi.restoreAllMocks()
  })

  it('shows no edit entry unless the host allows editing', async () => {
    const w = mountPreview({ editable: false })
    await flushPromises()
    expect(w.find('[data-testid="artifact-preview-edit"]').exists()).toBe(false)
    await w.setProps({ editable: true, artifact: art({ id: 'fb', name: 'feedback_index.json', kind: 'json' }) })
    await flushPromises()
    expect(w.find('[data-testid="artifact-preview-edit"]').exists()).toBe(false)
    w.unmount()
  })

  it('greys out the entry with the reason when the run is not editable', async () => {
    const w = mountPreview({ run: waitingRun({ status: 'completed' }) })
    await flushPromises()
    const btn = w.get('[data-testid="artifact-preview-edit"]')
    expect(btn.attributes('aria-disabled')).toBe('true')
    expect(btn.attributes('title')).toBe('运行已结束，产物只读')
    await btn.trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="artifact-edit-toolbar"]').exists()).toBe(false)
    w.unmount()
  })

  it('edits in the form, saves with Ctrl+S and reports the new version', async () => {
    apiMocks.saveRunArtifact.mockImplementation(async (_r: string, name: string, content: string) => ({
      id: 'a-plan',
      name,
      kind: 'json',
      content,
      revision: 4,
      changed: true,
      etag: 'etag-4',
    }))
    const w = mountPreview()
    await flushPromises()
    await enterEdit(w)
    expect(w.find('[data-testid="artifact-editor-form"]').exists()).toBe(true)
    expect(w.get('[data-testid="artifact-edit-summary"]').text()).toBe('未修改')
    expect(w.get('[data-testid="artifact-edit-save"]').attributes('disabled')).toBeDefined()

    await w.get('[data-field-path="title"] input').setValue('新计划')
    expect(w.get('[data-testid="artifact-edit-summary"]').text()).toBe('已修改 1 处')
    expect(w.get('[data-testid="artifact-edit-save"]').attributes('disabled')).toBeUndefined()

    await w.get('[data-testid="artifact-preview"]').trigger('keydown', { key: 's', ctrlKey: true })
    await flushPromises()
    expect(apiMocks.saveRunArtifact).toHaveBeenCalledTimes(1)
    const [runId, name, body, ifMatch] = apiMocks.saveRunArtifact.mock.calls[0]!
    expect([runId, name, ifMatch]).toEqual(['run-1', 'plan.json', 'etag-3'])
    expect(JSON.parse(body as string).title).toBe('新计划')
    expect(w.find('[data-testid="artifact-edit-toolbar"]').exists()).toBe(false)
    expect(w.get('[data-testid="artifact-edit-saved-note"]').text()).toBe('已保存为 v4')
    expect(w.emitted('saved')?.[0]).toEqual([{ name: 'plan.json', revision: 4, changed: true }])
    w.unmount()
  })

  it('reports unchanged saves without a new version', async () => {
    apiMocks.saveRunArtifact.mockResolvedValue({ id: 'a-plan', name: 'plan.json', content: PLAN, revision: 3, changed: false })
    const w = mountPreview()
    await flushPromises()
    await enterEdit(w)
    useArtifactEditor('run-1').update('plan.json', PLAN.replace('"A"', '"A "'))
    await flushPromises()
    await w.get('[data-testid="artifact-edit-save"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="artifact-edit-saved-note"]').text()).toBe('内容未变化，仍为 v3')
    w.unmount()
  })

  it('removes a goal with undo and keeps the other goals', async () => {
    const w = mountPreview()
    await flushPromises()
    await enterEdit(w)
    const goals = () => w.findAll('[data-list-key="goals"] > [data-testid="artifact-editor-item"]')
    expect(goals()).toHaveLength(2)
    await goals()[1]!.get('[data-testid="artifact-editor-item-remove"]').trigger('click')
    expect(goals()).toHaveLength(1)
    expect(w.get('[data-testid="artifact-editor-undo-bar"]').text()).toContain('已删除「B」')
    await w.get('[data-testid="artifact-editor-undo"]').trigger('click')
    expect(goals()).toHaveLength(2)
    await goals()[1]!.get('[data-testid="artifact-editor-item-remove"]').trigger('click')
    const draft = useArtifactEditor('run-1').draft('plan.json')!
    expect(JSON.parse(draft.content).goals.map((g: { id: string }) => g.id)).toEqual(['g1'])
    w.unmount()
  })

  it('shows the conflict bar with load latest and save anyway', async () => {
    apiMocks.saveRunArtifact.mockRejectedValueOnce(
      Object.assign(new Error('已被更新'), { status: 409, code: 'artifact_conflict', revision: 5 }),
    )
    const w = mountPreview()
    await flushPromises()
    await enterEdit(w)
    await w.get('[data-field-path="title"] input').setValue('mine')
    await w.get('[data-testid="artifact-edit-save"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="artifact-edit-conflict"]').text()).toContain('v5')

    apiMocks.saveRunArtifact.mockResolvedValueOnce({ id: 'a-plan', name: 'plan.json', content: '{}', revision: 6, changed: true })
    await w.get('[data-testid="artifact-edit-force"]').trigger('click')
    await flushPromises()
    expect(apiMocks.saveRunArtifact).toHaveBeenLastCalledWith('run-1', 'plan.json', expect.any(String), undefined)
    expect(w.get('[data-testid="artifact-edit-saved-note"]').text()).toBe('已保存为 v6')

    await enterEdit(w)
    await w.get('[data-field-path="title"] input').setValue('again')
    apiMocks.saveRunArtifact.mockRejectedValueOnce(
      Object.assign(new Error('已被更新'), { status: 409, code: 'artifact_conflict', revision: 7 }),
    )
    await w.get('[data-testid="artifact-edit-save"]').trigger('click')
    await flushPromises()
    apiMocks.artifactContent.mockResolvedValueOnce({ content: '{"title":"theirs","goals":[]}', etag: 'etag-7', revision: 7 })
    await w.get('[data-testid="artifact-edit-reload"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="artifact-edit-conflict"]').exists()).toBe(false)
    expect((w.get('[data-field-path="title"] input').element as HTMLInputElement).value).toBe('theirs')
    w.unmount()
  })

  it('marks the server field error and scrolls to it', async () => {
    const scroll = vi.fn()
    Element.prototype.scrollIntoView = scroll
    apiMocks.saveRunArtifact.mockRejectedValueOnce(
      Object.assign(new Error('goals[1].title 不能为空'), { status: 400, code: 'invalid', field: 'goals[1].title' }),
    )
    const w = mountPreview()
    await flushPromises()
    await enterEdit(w)
    await w.get('[data-field-path="title"] input').setValue('x')
    await w.get('[data-testid="artifact-edit-save"]').trigger('click')
    await flushPromises()
    await flushPromises()
    const row = w.get('.ed-row[data-field-path="goals[1].title"]')
    expect(row.classes()).toContain('ed-row--invalid')
    expect(row.get('[data-testid="artifact-editor-field-error"]').text()).toBe('goals[1].title 不能为空')
    expect(w.get('[data-testid="artifact-edit-error"]').text()).toContain('保存失败')
    expect(scroll).toHaveBeenCalled()
    w.unmount()
  })

  it('keeps the draft but blocks saving while the agent is replying', async () => {
    const w = mountPreview()
    await flushPromises()
    await enterEdit(w)
    await w.get('[data-field-path="title"] input').setValue('x')
    await w.setProps({ editState: { editable: false, reason: 'session_busy' } })
    expect(w.get('[data-testid="artifact-edit-blocked"]').text()).toContain('Agent 正在回复')
    expect(w.get('[data-testid="artifact-edit-save"]').attributes('disabled')).toBeDefined()
    await w.setProps({ editState: { editable: true, reason: '' } })
    expect(w.find('[data-testid="artifact-edit-blocked"]').exists()).toBe(false)
    expect(w.get('[data-testid="artifact-edit-save"]').attributes('disabled')).toBeUndefined()
    w.unmount()
  })

  it('Esc asks before discarding edits and leaves edit mode', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true)
    const w = mountPreview()
    await flushPromises()
    await enterEdit(w)
    await w.get('[data-field-path="title"] input').setValue('x')
    await w.get('[data-testid="artifact-preview"]').trigger('keydown', { key: 'Escape' })
    expect(confirm).toHaveBeenCalledTimes(1)
    expect(w.find('[data-testid="artifact-edit-toolbar"]').exists()).toBe(true)
    await w.get('[data-testid="artifact-preview"]').trigger('keydown', { key: 'Escape' })
    expect(w.find('[data-testid="artifact-edit-toolbar"]').exists()).toBe(false)
    expect(useArtifactEditor('run-1').draft('plan.json')).toBeUndefined()
    w.unmount()
  })

  it('switches to JSON and refuses to go back to the form with invalid JSON', async () => {
    const w = mountPreview()
    await flushPromises()
    await enterEdit(w)
    await w.get('[data-testid="artifact-edit-mode-json"]').trigger('click')
    await flushPromises()
    const code = w.get('[data-testid="code-editor-stub"]')
    expect(code.attributes('data-language')).toBe('json')
    await code.setValue('{"title": ')
    await flushPromises()
    expect(w.find('[data-testid="artifact-editor-json-error"]').exists()).toBe(true)
    expect(w.get('[data-testid="artifact-edit-save"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="artifact-edit-mode-form"]').trigger('click')
    expect(toastMocks.error).toHaveBeenCalled()
    expect(w.find('[data-testid="artifact-editor-form"]').exists()).toBe(false)
    w.unmount()
  })

  it('edits plain text artifacts in the code editor and opens straight into edit mode', async () => {
    apiMocks.artifactContent.mockResolvedValue({ content: '# 笔记', etag: 'e1', revision: 1 })
    const w = mountPreview({ artifact: art({ id: 'md', name: 'notes.md', kind: 'markdown', revision: 1 }), startInEdit: true })
    await flushPromises()
    expect(w.emitted('edit-start')?.[0]).toEqual(['notes.md'])
    expect(w.find('[data-testid="artifact-edit-mode-form"]').exists()).toBe(false)
    expect(w.get('[data-testid="code-editor-stub"]').attributes('data-language')).toBe('markdown')
    w.unmount()
  })

  it('flags an external update while editing', async () => {
    const w = mountPreview()
    await flushPromises()
    await enterEdit(w)
    await w.setProps({ artifact: art({ revision: 4 }) })
    await flushPromises()
    expect(w.get('[data-testid="artifact-edit-external"]').text()).toContain('v4')
    w.unmount()
  })

  it('offers code and preview for page.html', async () => {
    apiMocks.artifactContent.mockResolvedValue({ content: '<p>hi</p>', etag: 'e1', revision: 1 })
    const w = mountPreview({ artifact: art({ id: 'pg', name: 'page.html', kind: 'html', revision: 1 }) })
    await flushPromises()
    await enterEdit(w)
    expect(w.get('[data-testid="code-editor-stub"]').attributes('data-language')).toBe('html')
    await w.get('[data-testid="artifact-editor-html-preview"]').trigger('click')
    expect(w.find('[data-testid="code-editor-stub"]').exists()).toBe(false)
    expect(w.get('[data-testid="html-preview-stub"]').text()).toContain('<p>hi</p>')
    w.unmount()
  })
})
