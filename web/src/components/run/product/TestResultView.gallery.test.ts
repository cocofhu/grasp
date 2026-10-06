// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Artifact } from '@/lib/shared/types'
import TestResultView, { type TestResultDoc } from './TestResultView.vue'

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: { ...actual.api, artifactDownloadUrl: (id: string) => `/api/artifacts/${id}/download` },
  }
})

function artifact(name: string, id = name): Artifact {
  return { id, name, sizeBytes: 10, updatedAt: '2026-01-01T00:00:00Z' } as Artifact
}

function mountView(doc: TestResultDoc, props: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(TestResultView, {
    props: { doc, ...props },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        AnnotateBtn: true,
        AppModal: {
          props: ['open', 'title', 'width'],
          template: '<div v-if="open" data-testid="lightbox"><slot /></div>',
        },
      },
    },
  })
}

const SHOT_ARTIFACTS = [artifact('a.png', 'a1'), artifact('b.png', 'b1'), artifact('c.png', 'c1')]

beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => ({ ok: true, blob: async () => new Blob(['x']) })),
  )
  vi.spyOn(URL, 'createObjectURL').mockImplementation(() => 'blob:shot')
  vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('TestResultView', () => {
  it('summarises pass rate from the case counters', async () => {
    const w = mountView({ summary: '一切正常', passed: 8, failed: 0, skipped: 2 })
    await flushPromises()
    const vm = w.vm as any

    expect(vm.total).toBe(10)
    expect(vm.passPct).toBe(80)
    expect(vm.allPassed).toBe(true)
    expect(w.text()).toContain('一切正常')
    w.unmount()
  })

  it('marks the run as failed when any case failed', async () => {
    const w = mountView({ passed: 1, failed: 2 })
    await flushPromises()
    const vm = w.vm as any
    expect(vm.allPassed).toBe(false)
    expect(vm.passPct).toBe(33)
    w.unmount()
  })

  it('reports a zero pass rate with no counters at all', async () => {
    const w = mountView({})
    await flushPromises()
    const vm = w.vm as any
    expect(vm.total).toBe(0)
    expect(vm.passPct).toBe(0)
    expect(vm.allPassed).toBe(false)
    expect(vm.caseGroups).toEqual([])
    w.unmount()
  })

  it('keeps a flat case list when no name carries a repo prefix', async () => {
    const w = mountView({
      cases: [
        { id: 'c1', name: 'login works', status: 'passed' },
        { id: 'c2', name: 'logout works', status: 'failed', detail: 'timeout' },
      ],
    })
    await flushPromises()
    const vm = w.vm as any

    expect(vm.caseGroups).toHaveLength(1)
    expect(vm.caseGroups[0].repo).toBeNull()
    expect(w.text()).toContain('login works')
    expect(w.text()).toContain('timeout')
    w.unmount()
  })

  it('groups cases by repo prefix and strips it from the display name', async () => {
    const w = mountView({
      cases: [
        { name: '[api] create user', status: 'passed' },
        { name: '[api] delete user', status: 'skip' },
        { name: '[web] renders', status: 'failed' },
        { name: 'unprefixed', status: 'ok' },
        { name: '[api]', status: 'pass' },
      ],
    })
    await flushPromises()
    const vm = w.vm as any

    expect(vm.caseGroups.map((g: any) => g.repo)).toEqual(['api', 'web', null])
    expect(vm.caseGroups[0].cases.map((c: any) => c.name)).toEqual(['create user', 'delete user', '[api]'])

    // A prefix-only name has no remainder, so the raw name is kept.
    expect(vm.groupStats(vm.caseGroups[0].cases)).toEqual({ passed: 2, failed: 0, skipped: 1, total: 3 })
    expect(vm.groupStats(vm.caseGroups[1].cases)).toEqual({ passed: 0, failed: 1, skipped: 0, total: 1 })
    expect(w.text()).toContain('api')
    w.unmount()
  })

  it('treats an unknown or missing case status as failed', async () => {
    const w = mountView({ cases: [{ name: 'a' }, { name: 'b', status: 'weird' }] })
    await flushPromises()
    const vm = w.vm as any

    expect(vm.groupStats(vm.caseGroups[0].cases).failed).toBe(2)
    expect(vm.cs('passed').cls).toBe('text-ok')
    expect(vm.cs('skipped').cls).toBe('text-txt3')
    expect(vm.cs(undefined).cls).toBe('text-err')
    expect(vm.cs('nope').cls).toBe('text-err')
    w.unmount()
  })

  it('ignores a non-array cases payload', async () => {
    const w = mountView({ cases: { items: [] } as never })
    await flushPromises()
    expect((w.vm as any).caseGroups).toEqual([])
    w.unmount()
  })

  it('renders defects with severity styling and detail rows', async () => {
    const w = mountView({
      defects: [
        { id: 'd1', title: '崩溃', severity: 'critical', status: 'open', detail: '空指针' },
        { title: '样式', severity: 'unknown-sev' },
      ],
    })
    await flushPromises()

    expect(w.text()).toContain('崩溃')
    expect(w.text()).toContain('空指针')
    expect(w.text()).toContain('unknown-sev')
    expect(w.html()).toContain('bg-err/20')
    w.unmount()
  })

  it('renders the variances and assessment sections when present', async () => {
    const w = mountView({ variances: '与预期不符', assessment: '整体可发布' })
    await flushPromises()
    expect(w.text()).toContain('与预期不符')
    expect(w.text()).toContain('整体可发布')
    w.unmount()
  })

  it('renders artifact screenshots and opens the lightbox', async () => {
    const w = mountView(
      {
        screenshots: [
          { artifact: 'a.png', mimeType: 'image/jpeg', caption: '首页' },
          { artifact: 'b.png', caption: '详情页' },
        ],
      },
      { artifacts: SHOT_ARTIFACTS, runStatus: 'completed' },
    )
    await flushPromises()
    const vm = w.vm as any

    expect(vm.shotStates.map((s: any) => s.status)).toEqual(['success', 'success'])
    expect(vm.galleryIndices).toEqual([0, 1])
    expect(w.find('[data-testid="lightbox"]').exists()).toBe(false)

    vm.openLightbox(0)
    await flushPromises()
    expect(vm.lightboxGalleryPos).toBe(0)
    expect(vm.lightboxShotIndex).toBe(0)
    expect(vm.lightboxSrc()).toBe(vm.shotStates[0].src)
    expect(w.find('[data-testid="lightbox"]').exists()).toBe(true)

    vm.closeLightbox()
    await flushPromises()
    expect(vm.lightboxGalleryPos).toBeNull()
    expect(vm.lightboxShotIndex).toBeNull()
    expect(vm.lightboxState).toBeNull()
    expect(vm.lightboxSrc()).toBe('')
    w.unmount()
  })

  it('wraps around the gallery with arrow keys and ignores other keys', async () => {
    const w = mountView(
      { screenshots: [{ artifact: 'a.png' }, { artifact: 'b.png' }, { artifact: 'c.png' }] },
      { artifacts: SHOT_ARTIFACTS, runStatus: 'completed' },
    )
    await flushPromises()
    const vm = w.vm as any

    // Arrow keys do nothing while the lightbox is closed.
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight' }))
    await flushPromises()
    expect(vm.lightboxGalleryPos).toBeNull()

    vm.openLightbox(0)
    await flushPromises()

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft' }))
    await flushPromises()
    expect(vm.lightboxGalleryPos).toBe(2)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight' }))
    await flushPromises()
    expect(vm.lightboxGalleryPos).toBe(0)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(vm.lightboxGalleryPos).toBe(0)

    // step() is inert once the gallery is empty.
    vm.closeLightbox()
    vm.step(1)
    expect(vm.lightboxGalleryPos).toBeNull()
    w.unmount()
  })

  it('detaches the key listener on unmount', async () => {
    const w = mountView(
      { screenshots: [{ artifact: 'a.png' }, { artifact: 'b.png' }] },
      { artifacts: SHOT_ARTIFACTS, runStatus: 'completed' },
    )
    await flushPromises()
    const vm = w.vm as any
    vm.openLightbox(0)
    await flushPromises()
    w.unmount()

    // No listener remains, so a stray key press cannot mutate the torn-down state.
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight' }))
    expect(vm.lightboxGalleryPos).toBe(0)
  })

  it('refuses to open the lightbox for a screenshot that failed to load', async () => {
    const w = mountView(
      { screenshots: [{ artifact: 'missing.png' }] },
      { artifacts: [], runStatus: 'completed' },
    )
    await flushPromises()
    const vm = w.vm as any

    expect(vm.shotStates[0].status).toBe('error')
    expect(vm.galleryIndices).toEqual([])
    vm.openLightbox(0)
    expect(vm.lightboxGalleryPos).toBeNull()
    expect(w.text()).toContain('missing.png')
    w.unmount()
  })

  it('keeps a pending screenshot in the loading frame while the run is live', async () => {
    const w = mountView(
      { screenshots: [{ artifact: 'later.png', caption: '稍后' }] },
      { artifacts: [], runStatus: 'running' },
    )
    await flushPromises()
    const vm = w.vm as any

    expect(vm.shotStates[0].status).toBe('loading')
    expect(w.text()).toContain('later.png')
    w.unmount()
  })

  it('fetches an artifact-backed screenshot and shows it in the gallery', async () => {
    const w = mountView(
      { screenshots: [{ artifact: 'shot.png', caption: '结果页' }] },
      { artifacts: [artifact('shot.png', 'a1')], runStatus: 'completed', accent: '#f00' },
    )
    await flushPromises()
    const vm = w.vm as any

    expect(fetch).toHaveBeenCalledWith('/api/artifacts/a1/download', { credentials: 'include' })
    expect(vm.shotStates[0].status).toBe('success')
    expect(vm.galleryIndices).toEqual([0])
    expect(vm.accent).toBe('#f00')

    vm.openLightbox(0)
    await flushPromises()
    expect(vm.lightboxSrc()).toBe('blob:shot')
    w.unmount()
  })

  it('falls back to the error frame when the artifact fetch fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: false, status: 500, blob: async () => new Blob() })),
    )
    const w = mountView(
      { screenshots: [{ artifact: 'shot.png' }] },
      { artifacts: [artifact('shot.png', 'a1')], runStatus: 'failed' },
    )
    await flushPromises()
    expect((w.vm as any).shotStates[0].status).toBe('error')
    w.unmount()
  })

  it('derives stable keys and captions for each screenshot slot', async () => {
    const w = mountView({
      screenshots: [{ artifact: ' a.png ', caption: '有说明' }, {}],
    })
    await flushPromises()
    const vm = w.vm as any

    expect(vm.shotKey({ artifact: ' a.png ' }, 0)).toBe('a.png')
    expect(vm.shotKey({}, 2)).toBe('shot-2')

    expect(vm.shotCaption({ caption: '有说明' }, 0)).toBe('有说明')
    expect(vm.shotCaption({}, 1)).toBeTruthy()
    w.unmount()
  })
})
