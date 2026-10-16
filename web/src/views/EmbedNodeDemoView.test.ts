// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import { embedDemoChannelName } from '@/lib/inbox/embedDemoPreview'
import { SANDBOX_ATTR } from '@/lib/shared/htmlPreviewSandbox'

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { runId: 'run-1', nodeId: 'ap1' } }),
}))

import EmbedNodeDemoView from './EmbedNodeDemoView.vue'

const channels: BroadcastChannel[] = []

function i18n() {
  return createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common } } })
}

function listen() {
  const ch = new BroadcastChannel(embedDemoChannelName('run-1', 'ap1'))
  channels.push(ch)
  const seen: unknown[] = []
  ch.onmessage = (ev) => seen.push(ev.data)
  return { ch, seen }
}

afterEach(() => {
  for (const ch of channels.splice(0)) ch.close()
  document.documentElement.classList.remove('light')
})

describe('EmbedNodeDemoView', () => {
  it('renders the one frame from the same-origin channel, not from the host message', async () => {
    const { ch, seen } = listen()
    const w = mount(EmbedNodeDemoView, { global: { plugins: [i18n()] }, attachTo: document.body })
    await vi.waitFor(() => expect(seen).toContainEqual({ kind: 'ready', runId: 'run-1', nodeId: 'ap1' }))
    window.dispatchEvent(
      new MessageEvent('message', { data: { type: 'grasp-embed:demo', action: 'open', html: '<p>nope</p>' } }),
    )
    await flushPromises()
    expect(document.body.textContent).not.toContain('nope')
    ch.postMessage({ kind: 'show', runId: 'run-1', nodeId: 'ap1', html: '<p>preview-b</p>', label: '方案 B' })
    await flushPromises()
    const frame = document.body.querySelector('iframe')
    expect(frame?.getAttribute('sandbox')).toBe(SANDBOX_ATTR)
    expect(frame?.getAttribute('srcdoc')).toContain('preview-b')
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    w.unmount()
  })
})
