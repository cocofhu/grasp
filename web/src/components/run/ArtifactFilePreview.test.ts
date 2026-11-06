// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import common from '@/locales/zh-CN/common.json'
import type { ArtifactAttachment } from '@/lib/shared/types'
import ArtifactFilePreview from './ArtifactFilePreview.vue'

const file: ArtifactAttachment = { artifactId: 'file-id', revision: 2, name: 'report.pdf', kind: 'pdf', sizeBytes: 100 }
function preview(over: Partial<ArtifactAttachment> = {}) {
  return mount(ArtifactFilePreview, { props: { file: { ...file, ...over } }, global: {
    plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })],
    stubs: { HtmlPreview: { props: ['html'], template: '<div data-testid="html">{{ html }}</div>' } },
  } })
}
afterEach(() => vi.unstubAllGlobals())
describe('versioned file previews', () => {
  it.each([['image', 'img'], ['video', 'video'], ['audio', 'audio'], ['pdf', 'iframe']] as const)('uses the authorized raw URL for %s without reading a body', async (kind, tag) => {
    const fetcher = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetcher)
    const wrapper = preview({ kind })
    await flushPromises()
    expect(fetcher).toHaveBeenCalledWith(expect.stringContaining('/file-id/raw?revision=2'), expect.objectContaining({ method: 'HEAD', credentials: 'include' }))
    expect(wrapper.get(tag).attributes('src')).toContain('/file-id/raw?revision=2')
    expect(wrapper.find('video[autoplay], audio[autoplay]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="artifact-file-download"]').attributes('href')).toContain('/file-id/download?revision=2')
    wrapper.unmount()
  })
  it('limits text, escapes source and keeps the full download', async () => {
    const fetcher = vi.fn().mockResolvedValue({ ok: true, text: async () => '<script>alert(1)</script>' })
    vi.stubGlobal('fetch', fetcher)
    const wrapper = preview({ kind: 'text', sizeBytes: 3000000 })
    await flushPromises()
    expect(fetcher).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ headers: { Range: 'bytes=0-2097151' } }))
    expect(wrapper.get('pre').text()).toContain('<script>')
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.text()).toContain('预览')
    wrapper.unmount()
  })
  it('displays errors, retries, and ignores a stale response after switching versions', async () => {
    let old!: (value: unknown) => void
    const fetcher = vi.fn().mockImplementationOnce(() => new Promise(resolve => { old = resolve }))
      .mockResolvedValueOnce({ ok: false }).mockResolvedValue({ ok: true, text: async () => 'new version' })
    vi.stubGlobal('fetch', fetcher)
    const wrapper = preview({ kind: 'text' })
    await wrapper.setProps({ file: { ...file, kind: 'text', revision: 3 } })
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-file-error"]').exists()).toBe(true)
    await wrapper.get('button').trigger('click')
    await flushPromises()
    old({ ok: true, text: async () => 'old version' })
    await flushPromises()
    expect(wrapper.get('pre').text()).toBe('new version')
    wrapper.unmount()
  })
  it('offers a download for unsupported files and handles native media errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))
    const wrapper = preview({ kind: 'file' })
    await flushPromises()
    expect(wrapper.find('iframe, video, audio, img').exists()).toBe(false)
    await wrapper.setProps({ file: { ...file, kind: 'video', revision: 3 } })
    await flushPromises()
    await wrapper.get('video').trigger('error')
    expect(wrapper.find('[data-testid="artifact-file-error"]').exists()).toBe(true)
    wrapper.unmount()
  })
})
