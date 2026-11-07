// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import { i18n } from '@/lib/shared/i18n'
import type { ClarifyImage } from '@/lib/shared/types'
import { SITE_ATTACH_MAX_BYTES } from '@/lib/shared/attachments'
import ChatFileAttachment from './ChatFileAttachment.vue'

beforeEach(() => { i18n.global.setLocaleMessage('zh-CN', common) })
function render(attachment: ClarifyImage) {
  return mount(ChatFileAttachment, {
    props: { attachment },
    attrs: { 'data-testid': 'file-chip' },
    global: { plugins: [i18n], stubs: { AppModal: { props: ['open'], template: '<section v-if="open"><slot /></section>' } } },
  })
}

describe('ChatFileAttachment', () => {
  it.each([['audio/wav', 'voice.wav', 'audio'], ['video/mp4', '演示.mp4', 'video'], ['application/octet-stream', 'clip.webm', 'video']])('previews %s using its existing blob URL and native controls', async (mimeType, name, tag) => {
    const w = render({ mimeType, name, ref: 'blob:abc123', sizeBytes: 1024 })
    expect(w.get('[data-testid="file-chip"]').attributes('data-attachment-kind')).toBe(tag)
    expect(w.text()).toContain('1.0 KiB')
    await w.get('button').trigger('click')
    const media = w.get(tag)
    expect(media.attributes()).toHaveProperty('controls')
    expect(media.attributes('preload')).toBe('metadata')
    expect(media.get('source').attributes('src')).toBe('/api/blobs/abc123')
    expect(w.get('a').attributes('download')).toBe(name)
    await media.trigger('error')
    expect(w.find(tag).exists()).toBe(false)
    expect(w.get('[data-testid="attachment-preview-fallback"]').text()).toContain('预览失败')
    expect(w.get('a').attributes('href')).toBe('/api/blobs/abc123')
    w.unmount()
  })

  it('keeps download available for unsupported or oversized files', async () => {
    for (const attachment of [
      { mimeType: 'application/pdf', name: 'spec.pdf', ref: 'blob:pdf123' },
      { mimeType: 'video/mp4', name: 'huge.mp4', ref: 'blob:vid123', sizeBytes: SITE_ATTACH_MAX_BYTES + 1 },
    ]) {
      const w = render(attachment)
      await w.get('button').trigger('click')
      expect(w.find('video').exists()).toBe(false)
      expect(w.get('[data-testid="attachment-preview-fallback"]').text()).toContain('下载')
      expect(w.get('a').attributes('download')).toBe(attachment.name)
      w.unmount()
    }
  })

  it('shows reading and unavailable states without requesting an empty source', async () => {
    const w = render({ mimeType: 'audio/mpeg', name: 'voice.mp3', readState: 'reading' })
    expect(w.get('button').attributes()).toHaveProperty('disabled')
    expect(w.text()).toContain('正在读取')
    await w.setProps({ attachment: { mimeType: 'audio/mpeg', name: 'voice.mp3' } })
    await w.get('button').trigger('click')
    expect(w.get('[data-testid="attachment-preview-fallback"]').text()).toContain('不可用')
    expect(w.find('audio').exists()).toBe(false)
    expect(w.find('a').exists()).toBe(false)
    w.unmount()
  })
})
