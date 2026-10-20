// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { i18n as appI18n } from '@/lib/shared/i18n'
import { loadLocaleMessages } from '@/lib/shared/loadLocaleMessages'
import type { ClarifyTurn } from '@/lib/shared/types'
import { EMBED_IMAGE_MESSAGE } from '@/lib/inbox/embedChat'
import { EMBED_IMAGE_PREVIEW_KEY, createEmbedImagePreview, embedImageChannelName } from '@/lib/inbox/embedImagePreview'
import ClarifyChat from './ClarifyChat.vue'

beforeAll(async () => {
  const zh = await loadLocaleMessages('zh-CN')
  appI18n.global.setLocaleMessage('zh-CN', zh)
  appI18n.global.locale.value = 'zh-CN'
})

const channels: BroadcastChannel[] = []
const turns: ClarifyTurn[] = [
  {
    role: 'human',
    text: '看图',
    at: '2026-10-14T00:00:00Z',
    images: [
      { data: 'AAA', mimeType: 'image/png', name: 'image.png' },
      { data: 'BBB', mimeType: 'image/png', name: 'other.png' },
    ],
  },
]

function mountChat(provide?: { open: (src: string, label: string) => void; close: () => void }) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(ClarifyChat, {
    props: {
      runId: 'run-1',
      nodeId: 'ap1',
      iteration: 1,
      turns,
      done: false,
      active: true,
    },
    attachTo: document.body,
    global: {
      plugins: [i18n],
      stubs: { Icon: true, ClarifyDemoFrame: true },
      provide: provide ? { [EMBED_IMAGE_PREVIEW_KEY as symbol]: provide } : {},
    },
  })
}

afterEach(() => {
  for (const ch of channels.splice(0)) ch.close()
  document.body.innerHTML = ''
})

describe('ClarifyChat embed image preview', () => {
  it('does not mount a dialog and sends open, replace, and close as one message each', async () => {
    const post = vi.fn()
    const api = createEmbedImagePreview({ runId: 'run-1', nodeId: 'ap1', post })
    const frame = new BroadcastChannel(embedImageChannelName('run-1', 'ap1'))
    channels.push(frame)
    const seen: unknown[] = []
    frame.onmessage = (ev) => seen.push(ev.data)
    const w = mountChat(api)
    const thumbs = w.findAll('[data-testid="clarify-history-image-thumb"]')
    expect(thumbs).toHaveLength(2)
    await thumbs[0].trigger('click')
    await thumbs[1].trigger('click')
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(post.mock.calls.map((c) => c[0])).toEqual([
      { type: EMBED_IMAGE_MESSAGE, action: 'open', label: 'image.png' },
      { type: EMBED_IMAGE_MESSAGE, action: 'open', label: 'other.png' },
    ])
    expect(JSON.stringify(post.mock.calls)).not.toContain('base64')
    frame.postMessage({ kind: 'ready', runId: 'run-1', nodeId: 'ap1' })
    await vi.waitFor(() => expect(seen.some((m) => (m as { kind?: string }).kind === 'show')).toBe(true))
    expect(seen.find((m) => (m as { kind?: string }).kind === 'show')).toMatchObject({
      src: 'data:image/png;base64,BBB',
      label: 'other.png',
    })
    api.close()
    expect(post.mock.calls[2][0]).toEqual({ type: EMBED_IMAGE_MESSAGE, action: 'close' })
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    api.dispose()
    w.unmount()
  })

  it('still opens the full-page dialog in the main app', async () => {
    const w = mountChat()
    await w.get('[data-testid="clarify-history-image-thumb"]').trigger('click')
    await flushPromises()
    const dialog = document.body.querySelector('[role="dialog"]')
    expect(dialog).toBeTruthy()
    expect(dialog?.getAttribute('aria-modal')).toBe('true')
    expect(dialog?.textContent).toContain('图片预览 · image.png')
    w.unmount()
  })
})
