// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import { markMissing, resetBlobMissingCacheForTests } from '@/lib/shared/blobMissingCache'
import { embedImageChannelName } from '@/lib/inbox/embedImagePreview'

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { runId: 'run-1', nodeId: 'ap1' } }),
}))

import EmbedNodeImageView from './EmbedNodeImageView.vue'

const channels: BroadcastChannel[] = []

function i18n() {
  return createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common } } })
}

function listen() {
  const ch = new BroadcastChannel(embedImageChannelName('run-1', 'ap1'))
  channels.push(ch)
  const seen: unknown[] = []
  ch.onmessage = (ev) => seen.push(ev.data)
  return { ch, seen }
}

afterEach(() => {
  resetBlobMissingCacheForTests()
  for (const ch of channels.splice(0)) ch.close()
  document.documentElement.classList.remove('light')
})

describe('EmbedNodeImageView', () => {
  it('paints a usable picture inside the frame and not in a dialog', async () => {
    const { ch, seen } = listen()
    const w = mount(EmbedNodeImageView, { global: { plugins: [i18n()] }, attachTo: document.body })
    await vi.waitFor(() => expect(seen).toContainEqual({ kind: 'ready', runId: 'run-1', nodeId: 'ap1' }))
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    ch.postMessage({
      kind: 'show',
      runId: 'run-1',
      nodeId: 'ap1',
      src: 'data:image/png;base64,AAA',
      label: 'image.png',
      missing: false,
    })
    await vi.waitFor(() => expect(w.find('[data-testid="embed-image-preview-img"]').exists()).toBe(true))
    expect(w.get('[data-testid="embed-image-preview-img"]').attributes('src')).toBe('data:image/png;base64,AAA')
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    w.unmount()
  })

  it('shows the existing failure copy for a known-missing attachment without an image request', async () => {
    const { ch } = listen()
    const w = mount(EmbedNodeImageView, { global: { plugins: [i18n()] }, attachTo: document.body })
    await flushPromises()
    ch.postMessage({
      kind: 'show',
      runId: 'run-1',
      nodeId: 'ap1',
      src: '/api/blobs/e54381fb9ce8471dbe0765d99fc0239f',
      label: 'gone.png',
      missing: true,
    })
    await vi.waitFor(() => expect(w.find('[data-testid="embed-image-preview-failed"]').exists()).toBe(true))
    const failed = w.get('[data-testid="embed-image-preview-failed"]')
    expect(failed.text()).toContain('图片加载失败')
    expect(failed.text()).toContain('附件不可用')
    expect(w.find('[data-testid="embed-image-preview-img"]').exists()).toBe(false)
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    w.unmount()
  })

  it('shows load failure inside the frame after the image errors', async () => {
    const { ch } = listen()
    const w = mount(EmbedNodeImageView, { global: { plugins: [i18n()] }, attachTo: document.body })
    await flushPromises()
    ch.postMessage({
      kind: 'show',
      runId: 'run-1',
      nodeId: 'ap1',
      src: 'data:image/png;base64,AAA',
      label: 'image.png',
      missing: false,
    })
    await vi.waitFor(() => expect(w.find('[data-testid="embed-image-preview-img"]').exists()).toBe(true))
    await w.get('[data-testid="embed-image-preview-img"]').trigger('error')
    expect(w.get('[data-testid="embed-image-preview-failed"]').text()).toContain('图片加载失败')
    expect(w.find('[data-testid="embed-image-preview-img"]').exists()).toBe(false)
    expect(w.get('[data-testid="embed-image-preview-failed"]').text()).not.toContain('附件不可用')
    w.unmount()
  })

  it('ignores a picture address posted by the preview page', async () => {
    const parent = { postMessage: vi.fn() }
    Object.defineProperty(window, 'parent', { configurable: true, value: parent })
    const { seen } = listen()
    const w = mount(EmbedNodeImageView, { global: { plugins: [i18n()] }, attachTo: document.body })
    await vi.waitFor(() => expect(seen.some((m) => (m as { kind?: string }).kind === 'ready')).toBe(true))
    const readyBefore = seen.filter((m) => (m as { kind?: string }).kind === 'ready').length
    window.dispatchEvent(
      new MessageEvent('message', {
        data: { type: 'grasp-embed:image', action: 'open', src: 'https://evil.example/x.png', label: 'nope.png' },
        source: parent as never,
      }),
    )
    await flushPromises()
    expect(w.find('img').exists()).toBe(false)
    expect(w.text()).not.toContain('evil.example')
    await vi.waitFor(() =>
      expect(seen.filter((m) => (m as { kind?: string }).kind === 'ready').length).toBeGreaterThan(readyBefore),
    )
    w.unmount()
    Object.defineProperty(window, 'parent', { configurable: true, value: window })
  })

  it('follows the preview page theme from the fragment', async () => {
    history.replaceState(null, '', '/embed/runs/run-1/nodes/ap1/image#theme=light')
    const w = mount(EmbedNodeImageView, { global: { plugins: [i18n()] } })
    await flushPromises()
    expect(document.documentElement.classList.contains('light')).toBe(true)
    expect(window.location.hash).toBe('')
    w.unmount()
    expect(document.documentElement.classList.contains('light')).toBe(false)
  })

  it('treats a blob the chat already marked missing as unavailable', async () => {
    markMissing('e54381fb9ce8471dbe0765d99fc0239f')
    const { ch } = listen()
    const w = mount(EmbedNodeImageView, { global: { plugins: [i18n()] }, attachTo: document.body })
    await flushPromises()
    ch.postMessage({
      kind: 'show',
      runId: 'run-1',
      nodeId: 'ap1',
      src: '/api/blobs/e54381fb9ce8471dbe0765d99fc0239f',
      label: 'gone.png',
      missing: false,
    })
    await vi.waitFor(() => expect(w.find('[data-testid="embed-image-preview-failed"]').exists()).toBe(true))
    expect(w.find('img').exists()).toBe(false)
    expect(w.get('[data-testid="embed-image-preview-failed"]').text()).toContain('附件不可用')
    w.unmount()
  })
})
