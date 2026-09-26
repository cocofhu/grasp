// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import DirectPreviewLauncher from './DirectPreviewLauncher.vue'

const DIRECT = 'http://127.0.0.1:18081/'

function mountLauncher(issueTicket?: () => Promise<unknown>) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': pages } })
  return mount(DirectPreviewLauncher, {
    props: { directUrl: DIRECT, issueTicket: issueTicket as never },
    global: { plugins: [i18n] },
  })
}

function fakeTab() {
  return { opener: {} as unknown, closed: false, location: { href: '' } }
}

afterEach(() => vi.restoreAllMocks())

describe('DirectPreviewLauncher', () => {
  it('opens the shown address with the drawer ticket', async () => {
    const tab = fakeTab()
    vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window)
    const w = mountLauncher(() => Promise.resolve({ ticket: 'tk', runId: 'run-1', nodeId: 'ap1', expiresAt: '' }))
    await w.get('[data-testid="direct-preview-address"]').trigger('click')
    await flushPromises()
    expect(tab.location.href).toBe(`${DIRECT}#__grasp_embed&run=run-1&node=ap1&ticket=tk&theme=dark`)
  })

  it('opens the tab inside the click, then sends it to the preview with the ticket', async () => {
    const tab = fakeTab()
    const open = vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window)
    let resolve!: (v: unknown) => void
    const w = mountLauncher(() => new Promise((r) => (resolve = r)))
    await w.get('[data-testid="app-preview-direct-open"]').trigger('click')
    expect(open).toHaveBeenCalledWith('about:blank', '_blank')
    expect(tab.opener).toBeNull()
    expect(tab.location.href).toBe('')
    resolve({ ticket: 't/1', runId: 'run-1', nodeId: 'ap1', expiresAt: '' })
    await flushPromises()
    expect(tab.location.href).toBe(`${DIRECT}#__grasp_embed&run=run-1&node=ap1&ticket=t%2F1&theme=dark`)
    expect(w.find('[data-testid="direct-preview-tip"]').exists()).toBe(false)
  })

  it('still opens the preview when no drawer ticket can be issued', async () => {
    const tab = fakeTab()
    vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window)
    const w = mountLauncher(() => Promise.reject(new Error('409')))
    await w.get('[data-testid="app-preview-direct-open"]').trigger('click')
    await flushPromises()
    expect(tab.location.href).toBe(DIRECT)
    expect(w.get('[data-testid="direct-preview-tip"]').text()).toContain('没法附带对话抽屉')
  })

  it('opens without a drawer when the host offers no ticket', async () => {
    const tab = fakeTab()
    vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window)
    const w = mountLauncher()
    await w.get('[data-testid="app-preview-direct-open"]').trigger('click')
    await flushPromises()
    expect(tab.location.href).toBe(DIRECT)
  })

  it('falls back to a link when the popup is blocked', async () => {
    vi.spyOn(window, 'open').mockReturnValue(null)
    const w = mountLauncher(() => Promise.resolve({ ticket: 'tk', runId: 'r', nodeId: 'n', expiresAt: '' }))
    await w.get('[data-testid="app-preview-direct-open"]').trigger('click')
    await flushPromises()
    const link = w.get('[data-testid="direct-preview-tip"]')
    expect(link.attributes('href')).toBe(`${DIRECT}#__grasp_embed&run=r&node=n&ticket=tk&theme=dark`)
    expect(link.attributes('rel')).toBe('noopener')
  })
})
