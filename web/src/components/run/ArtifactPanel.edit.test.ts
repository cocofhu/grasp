// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Artifact, Run } from '@/lib/shared/types'
import { resetArtifactEditorsForTest, useArtifactEditor } from '@/lib/run/useArtifactEditor'
import ArtifactPanel from './ArtifactPanel.vue'

vi.mock('@/lib/api/api', () => ({
  api: { artifactContent: vi.fn(async () => ({ content: '{"title":"p"}', etag: 'e1', revision: 1 })) },
}))

const a = (id: string, name: string): Artifact =>
  ({ id, name, kind: 'json', nodeId: 'n', runId: 'run-p', sizeBytes: 1, createdAt: '' }) as Artifact

const stubs = {
  ArtifactList: defineComponent({
    props: { artifacts: Array, activeId: String },
    emits: ['select'],
    template:
      '<div><button v-for="x in artifacts" :key="x.id" :data-testid="`pick-${x.name}`" @click="$emit(\'select\', x)" /></div>',
  }),
  ArtifactPreview: defineComponent({
    props: { artifact: Object, editable: Boolean, run: Object },
    template: '<div data-testid="preview" :data-editable="editable ? 1 : 0">{{ artifact?.name }}|{{ run?.status }}</div>',
  }),
}

describe('ArtifactPanel editing', () => {
  beforeEach(() => resetArtifactEditorsForTest())
  afterEach(() => vi.restoreAllMocks())

  it('passes editable and run through and guards switching away from unsaved edits', async () => {
    const run = { id: 'run-p', status: 'failed' } as Run
    const w = mount(ArtifactPanel, {
      props: { artifacts: [a('1', 'plan.json'), a('2', 'review.json')], editable: true, run },
      global: { stubs },
    })
    await flushPromises()
    expect(w.get('[data-testid="preview"]').attributes('data-editable')).toBe('1')
    expect(w.get('[data-testid="preview"]').text()).toBe('plan.json|failed')

    const ed = useArtifactEditor('run-p')
    await ed.begin(a('1', 'plan.json'))
    ed.update('plan.json', '{"title":"q"}')
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true)
    await w.get('[data-testid="pick-review.json"]').trigger('click')
    expect(w.get('[data-testid="preview"]').text()).toContain('plan.json')
    await w.get('[data-testid="pick-review.json"]').trigger('click')
    expect(confirm).toHaveBeenCalledTimes(2)
    expect(w.get('[data-testid="preview"]').text()).toContain('review.json')
    expect(ed.draft('plan.json')).toBeUndefined()

    await ed.begin(a('2', 'review.json'))
    const exposed = w.vm as unknown as { releaseActive: () => boolean }
    expect(exposed.releaseActive()).toBe(true)
    expect(ed.draft('review.json')).toBeUndefined()
  })

  it('hides the list while the active artifact is being edited', async () => {
    const run = { id: 'run-p', status: 'failed' } as Run
    const w = mount(ArtifactPanel, {
      props: { artifacts: [a('1', 'plan.json'), a('2', 'review.json')], editable: true, run },
      global: { stubs },
    })
    await flushPromises()
    const list = () => (w.get('[data-testid="artifact-panel-list"]').element as HTMLElement).style.display
    expect(list()).toBe('')
    const ed = useArtifactEditor('run-p')
    await ed.begin(a('2', 'review.json'))
    await flushPromises()
    expect(list()).toBe('')
    await ed.begin(a('1', 'plan.json'))
    await flushPromises()
    expect(list()).toBe('none')
    ed.discard('plan.json')
    await flushPromises()
    expect(list()).toBe('')
  })

  it('stays read-only by default', async () => {
    const w = mount(ArtifactPanel, { props: { artifacts: [a('1', 'plan.json')] }, global: { stubs } })
    await flushPromises()
    expect(w.get('[data-testid="preview"]').attributes('data-editable')).toBe('0')
    expect((w.vm as unknown as { releaseActive: () => boolean }).releaseActive()).toBe(true)
  })
})

describe('artifact editor styles', () => {
  const css = readFileSync(resolve(__dirname, 'editor/artifactEditor.css'), 'utf8')

  it('maps --ed-* to stage and app tokens', () => {
    expect(css).toMatch(/\.artifact-stage-surface \.artifact-editor[\s\S]*--ed-surface: var\(--ra-surface\)/)
    expect(css).toMatch(/--ed-surface: rgb\(var\(--c-surface\)\)/)
  })

  it('collapses to one column when the editor itself is narrow and honors reduced motion', () => {
    expect(css).toMatch(/\.artifact-editor \{[^}]*container: ed-editor \/ inline-size;/)
    expect(css).toMatch(/@container ed-editor \(max-width: 560px\)[\s\S]*\.ed-row \{\s*grid-template-columns: minmax\(0, 1fr\);/)
    expect(css).toMatch(/@media \(prefers-reduced-motion: reduce\)[\s\S]*\.ed-chip-flash \{\s*animation: none;/)
  })
})
