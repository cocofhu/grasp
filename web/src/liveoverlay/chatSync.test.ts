// @vitest-environment happy-dom
import { mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createOverlay, LIVE_CMD, LIVE_SESSIONS, type LiveOverlay } from './overlay'
import { createLiveStore, LIVE_CARD_HOST, parseEmbedLiveMessage } from '@/lib/inbox/liveVariants'
import LiveVariantCard from '@/components/run/LiveVariantCard.vue'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const session = { sid: 'sid001', state: 'ready', mode: 'replace', variants: [{ n: 1 }, { n: 2 }] }
const markup = '<div data-grasp-live="sid001" style="display:contents"><section data-grasp-variant="0" hidden>original</section>' +
  '<section data-grasp-variant="1">one</section>' +
  `<section data-grasp-variant="2" data-grasp-params='[{"id":"gap","kind":"range","min":8,"max":48,"default":24,"unit":"px"},{"id":"tone","kind":"steps","options":[{"value":"soft"},{"value":"strong"}]}]' hidden>two</section></div>`

let overlay: LiveOverlay
let card: VueWrapper | undefined
beforeEach(() => {
  sessionStorage.clear()
  document.body.innerHTML = markup
})
afterEach(() => {
  card?.unmount()
  card = undefined
  overlay?.dispose()
  document.body.innerHTML = ''
})

function connect() {
  const live = createLiveStore()
  live.apply(session)
  const requests: Record<string, unknown>[] = []
  const post = vi.fn((data: Record<string, unknown>) => {
    const message = parseEmbedLiveMessage(data)
    if (message?.kind === 'state') live.setView(message.sid, message.view)
    else if (message?.kind === 'request') requests.push(data)
    return true
  })
  overlay = createOverlay({ post, theme: () => 'dark', notice: vi.fn(), stopPick: vi.fn(), changed: vi.fn(), isOwnUi: () => false })
  overlay.setEnabled(true)
  overlay.onDrawer({ type: LIVE_SESSIONS, replace: true, sessions: [session] })
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  card = mount(LiveVariantCard, {
    props: { liveRef: { sid: 'sid001', op: 'generate' } },
    global: {
      plugins: [i18n],
      provide: { [LIVE_CARD_HOST as symbol]: { store: live.store, interactive: true, command: (sid: string, cmd: string, variant?: number) => overlay.onDrawer({ type: LIVE_CMD, sid, cmd, variant }) } },
    },
  })
  return { live, requests, post }
}

describe('Live page and chat synchronization', () => {
  it('publishes the first mounted candidate before any page interaction', () => {
    const { live } = connect()
    expect(live.activeCtx()).toEqual({ sid: 'sid001', current: 1 })
    expect(card!.get('[data-testid="live-variant-viewing"]').text()).toContain('1 / 2')
    expect(card!.get('[data-testid="live-variant-accept"]').attributes('disabled')).toBeUndefined()
  })

  it('restores candidate and knobs, synchronizes the card, and adopts the visible values', async () => {
    sessionStorage.setItem('__grasp_live', JSON.stringify({ sid001: { current: 2, mode: 'inplace', params: { '2': { gap: 32 } }, at: 1 } }))
    const { live, requests } = connect()
    expect((document.querySelector('[data-grasp-variant="2"]') as HTMLElement).hidden).toBe(false)
    expect(live.activeCtx()).toEqual({ sid: 'sid001', current: 2, params: { gap: '32px', tone: 'soft' } })
    expect(card!.get('[data-testid="live-variant-viewing"]').text()).toContain('2 / 2')
    const slider = document.querySelector('grasp-live-overlay')!.shadowRoot!.querySelector('[data-param="sid001|2|gap"]') as HTMLInputElement
    slider.value = '40'
    slider.dispatchEvent(new Event('input', { bubbles: true }))
    expect(live.activeCtx()).toEqual({ sid: 'sid001', current: 2, params: { gap: '40px', tone: 'soft' } })
    await card!.get('[data-testid="live-variant-accept"]').trigger('click')
    expect(requests.at(-1)).toMatchObject({ op: 'accept', variant: 2, params: { gap: '40px', tone: 'soft' } })
  })

  it('resends views to a fresh drawer and follows HMR fallback and route removal', async () => {
    sessionStorage.setItem('__grasp_live', JSON.stringify({ sid001: { current: 2, mode: 'inplace', params: {}, at: 1 } }))
    const { live, post } = connect()
    live.store.views = {}
    post.mockClear()
    overlay.onDrawer({ type: LIVE_SESSIONS, replace: true, sessions: [session] })
    expect(live.activeCtx()).toMatchObject({ current: 2 })
    expect(post).toHaveBeenCalledOnce()
    document.querySelector('[data-grasp-variant="2"]')!.remove()
    await vi.waitFor(() => expect(live.activeCtx()).toEqual({ sid: 'sid001', current: 1 }))
    document.querySelector('[data-grasp-live]')!.remove()
    await vi.waitFor(() => expect(live.activeCtx()).toBeNull())
    await card!.vm.$nextTick()
    expect(card!.get('[data-testid="live-variant-accept"]').attributes('disabled')).toBeDefined()
  })
})
