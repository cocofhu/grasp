// @vitest-environment happy-dom
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ConversationArtifactBrowser from './ConversationArtifactBrowser.vue'
import pages from '@/locales/zh-CN/pages.json'
import common from '@/locales/zh-CN/common.json'
import type { Artifact } from '@/lib/shared/types'
import type { ConversationArtifactTarget } from '@/lib/chat/conversationArtifacts'
const mocks = vi.hoisted(() => ({ artifactContent: vi.fn(), listArtifacts: vi.fn(), getRun: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: mocks }))
const file = (id: string, runId = 'run-a') => ({ id, name: 'plan.json', kind: 'json', runId, nodeId: 'plan', workflowName: '研发', sizeBytes: 2, createdAt: '2026-10-20T09:00:00Z', content: '{}' }) as Artifact
let wrapper: VueWrapper | undefined
function mountBrowser(target: ConversationArtifactTarget | null) {
  wrapper = mount(ConversationArtifactBrowser, { props: { target }, global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })], stubs: { ArtifactPreview: { name: 'ArtifactPreview', props: ['artifact'], template: '<div data-testid="preview">{{artifact.id}}</div>' } } } })
  return wrapper
}
beforeEach(() => { vi.clearAllMocks(); mocks.getRun.mockResolvedValue({ id: 'run-a', artifacts: [file('art-a')] }); mocks.artifactContent.mockResolvedValue(file('art-a')); mocks.listArtifacts.mockResolvedValue({ items: [file('art-a'), file('art-b', 'run-b')], total: 2 }) })
afterEach(() => { wrapper?.unmount(); wrapper = undefined })

describe('ConversationArtifactBrowser', () => {
  it('does not fetch until an actual artifact is requested', async () => {
    const w = mountBrowser(null)
    await flushPromises()
    expect(w.text()).toContain('在这里查看产物')
    expect(mocks.artifactContent).not.toHaveBeenCalled()
    expect(mocks.listArtifacts).not.toHaveBeenCalled()
  })
  it('resolves exact artifact IDs and loads their run for version navigation', async () => {
    const w = mountBrowser({ id: 'art-a' })
    await flushPromises()
    expect(w.get('[data-testid="preview"]').text()).toBe('art-a')
    expect(mocks.artifactContent).toHaveBeenCalledWith('art-a')
    expect(mocks.getRun).toHaveBeenCalledWith('run-a')
  })
  it('shows readable content while optional run metadata is still pending', async () => {
    mocks.getRun.mockReturnValue(new Promise(() => {}))
    const w = mountBrowser({ id: 'art-a' })
    await flushPromises()
    expect(w.get('[data-testid="preview"]').text()).toBe('art-a')
    expect(w.find('[data-testid="conversation-artifact-loading"]').exists()).toBe(false)
  })
  it('requires a deliberate run choice for a bare filename', async () => {
    const w = mountBrowser({ name: 'plan.json' })
    await flushPromises()
    expect(w.find('[data-testid="preview"]').exists()).toBe(false)
    expect(w.findAll('[data-artifact-id]')).toHaveLength(2)
    await w.get('[data-artifact-id="art-b"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="preview"]').text()).toBe('art-b')
    await w.get('[data-testid="conversation-artifact-back"]').trigger('click')
    expect(w.findAll('[data-artifact-id]')).toHaveLength(2)
  })
  it('does not silently pick a single filename result without run context', async () => {
    mocks.listArtifacts.mockResolvedValue({ items: [file('art-a')], total: 1 })
    const w = mountBrowser({ name: 'plan.json' })
    await flushPromises()
    expect(w.find('[data-testid="preview"]').exists()).toBe(false)
    expect(w.find('[data-artifact-id="art-a"]').exists()).toBe(true)
  })
  it('matches the filename case accepted by PM citation validation', async () => {
    const w = mountBrowser({ name: 'PLAN.JSON' })
    await flushPromises()
    expect(w.findAll('[data-artifact-id]')).toHaveLength(2)
  })
  it('never displays an exact ID from another requested run', async () => {
    const w = mountBrowser({ id: 'art-a', runId: 'run-other' })
    await flushPromises()
    expect(w.find('[data-testid="conversation-artifact-error"]').exists()).toBe(true)
    expect(w.find('[data-testid="preview"]').exists()).toBe(false)
  })
  it('ignores stale requests after changing the artifact target', async () => {
    let resolve!: (artifact: Artifact) => void
    mocks.artifactContent.mockImplementationOnce(() => new Promise(r => { resolve = r })).mockResolvedValueOnce(file('art-b'))
    const w = mountBrowser({ id: 'old' })
    await w.setProps({ target: { id: 'art-b' } })
    await flushPromises()
    resolve(file('old'))
    await flushPromises()
    expect(w.get('[data-testid="preview"]').text()).toBe('art-b')
  })
  it('supports retry after a failed source request', async () => {
    mocks.listArtifacts.mockRejectedValueOnce(new Error('network'))
    const w = mountBrowser({ name: 'plan.json' })
    await flushPromises()
    await w.get('[data-testid="conversation-artifact-error"] button').trigger('click')
    await flushPromises()
    expect(w.findAll('[data-artifact-id]')).toHaveLength(2)
  })
  it('reports deletion without leaving an empty source picker for an exact ID', async () => {
    const w = mountBrowser({ id: 'art-a' })
    await flushPromises()
    w.findComponent({ name: 'ArtifactPreview' }).vm.$emit('deleted', 'art-a')
    await flushPromises()
    expect(w.find('[data-testid="conversation-artifact-deleted"]').exists()).toBe(true)
    expect(w.find('[data-testid="preview"]').exists()).toBe(false)
  })
  it('returns to a populated source page after deleting the only file on the last page', async () => {
    const firstPage = Array.from({ length: 20 }, (_, i) => file(`art-${i}`))
    mocks.listArtifacts
      .mockResolvedValueOnce({ items: firstPage, total: 21 })
      .mockResolvedValueOnce({ items: [file('last')], total: 21 })
      .mockResolvedValueOnce({ items: [], total: 20 })
      .mockResolvedValueOnce({ items: firstPage, total: 20 })
    const w = mountBrowser({ name: 'plan.json' })
    await flushPromises()
    await w.get('button[aria-label="下一页"]').trigger('click')
    await flushPromises()
    await w.get('[data-artifact-id="last"]').trigger('click')
    await flushPromises()
    w.findComponent({ name: 'ArtifactPreview' }).vm.$emit('deleted', 'last')
    await flushPromises()
    expect(w.findAll('[data-artifact-id]')).toHaveLength(20)
    expect(w.find('[data-testid="conversation-artifact-no-match"]').exists()).toBe(false)
    expect(mocks.listArtifacts).toHaveBeenLastCalledWith({ q: 'plan.json', page: 1, pageSize: 20 })
  })
})
