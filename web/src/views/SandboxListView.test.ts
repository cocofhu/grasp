// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'
import type { SandboxView } from '@/lib/api/api'

const apiMocks = vi.hoisted(() => ({
  listSandboxes: vi.fn(),
  getSandbox: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listSandboxes: apiMocks.listSandboxes,
      getSandbox: apiMocks.getSandbox,
    },
  }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}))

vi.mock('@/lib/shared/copyToClipboard', () => ({
  copyToClipboard: vi.fn(async () => true),
}))

import SandboxListView from './SandboxListView.vue'

const LIST_ROW: SandboxView = {
  id: 7,
  name: 'sb-7f3a',
  profile: 'default',
  purpose: 'test',
  status: 'running',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  containerStatus: 'running',
  busy: false,
  connected: false,
  hasCodeServer: true,
  hasAcp: true,
}

const DETAIL: SandboxView = {
  ...LIST_ROW,
  password: 's3cret',
  endpoints: {
    session: '203.0.113.10:8765',
    ide: '203.0.113.10:8744',
    ssh: '203.0.113.10:22',
    cdp: '203.0.113.10:9222',
    novnc: '203.0.113.10:6080',
    '9222': '203.0.113.10:9222',
    '6080': '203.0.113.10:6080',
  },
}

function mountList(locale: 'zh-CN' | 'en' = 'zh-CN') {
  const i18n =
    locale === 'zh-CN'
      ? createI18n({
          legacy: false,
          locale: 'zh-CN',
          messages: { 'zh-CN': { ...common, ...pages } },
        })
      : createI18n({
          legacy: false,
          locale: 'en',
          messages: { en: { ...enCommon, ...enPages } },
        })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/sandboxes', component: SandboxListView },
      { path: '/sandboxes/:id/console', name: 'sandbox-console', component: { template: '<div data-testid="console" />' } },
    ],
  })
  router.push('/sandboxes')
  const wrapper = mount(SandboxListView, {
    global: {
      plugins: [i18n, router],
      stubs: {
        Icon: true,
        AppButton: { template: '<button type="button"><slot /></button>' },
        AppModal: {
          props: ['open', 'title', 'width'],
          template: '<div v-if="open" data-testid="sandbox-detail-modal"><slot /><slot name="footer" /></div>',
        },
      },
    },
  })
  return { wrapper, router }
}

beforeEach(() => {
  vi.clearAllMocks()
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      addEventListener() {},
      removeEventListener() {},
    }),
  })
  apiMocks.listSandboxes.mockResolvedValue([LIST_ROW])
  apiMocks.getSandbox.mockResolvedValue(DETAIL)
})

describe('SandboxListView detail endpoints (g3)', () => {
  it('passwordHint is limited to session/ide/ssh (g3.5)', () => {
    expect(pages.pages.sandboxConsole.passwordHint).toContain('session / ide / ssh')
    expect(pages.pages.sandboxConsole.passwordHint).toMatch(/CDP|noVNC/)
    expect(enPages.pages.sandboxConsole.passwordHint).toMatch(/session \/ ide \/ ssh/i)
    expect(enPages.pages.sandboxConsole.passwordHint).toMatch(/CDP|noVNC/i)
  })

  it('does not render direct cdp/novnc and shows info notice + VNC preview path', async () => {
    const { wrapper } = mountList('zh-CN')
    await flushPromises()
    const detailBtns = wrapper.findAll('button').filter((b) => b.text().includes('详情'))
    expect(detailBtns.length).toBeGreaterThan(0)
    await detailBtns[0]!.trigger('click')
    await flushPromises()

    const modal = wrapper.get('[data-testid="sandbox-detail-modal"]')
    const text = modal.text()
    expect(text).toContain('/sandbox-vnc/7/ws')
    expect(text).toContain('/sandbox/7')
    expect(text).toContain('/sandbox-bridge/7')
    expect(text).toContain('打开预览')
    expect(text).toContain('CDP / noVNC 不提供直连')
    expect(text).toContain('203.0.113.10:8765')
    expect(text).toContain('203.0.113.10:8744')
    expect(text).toContain('203.0.113.10:22')
    expect(text).not.toContain('203.0.113.10:9222')
    expect(text).not.toContain('203.0.113.10:6080')
    expect(modal.find('[data-testid="sandbox-endpoints-notice"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('open preview navigates to sandbox console noVNC via platform WS', async () => {
    const { wrapper, router } = mountList('zh-CN')
    await flushPromises()
    await wrapper.findAll('button').filter((b) => b.text().includes('详情'))[0]!.trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="sandbox-vnc-open-preview"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/sandboxes/7/console')
    expect(router.currentRoute.value.query.tab).toBe('novnc')
    wrapper.unmount()
  })

  it('en notice covers proxy-only CDP/noVNC and session/ide/ssh auth', async () => {
    const { wrapper } = mountList('en')
    await flushPromises()
    await wrapper.findAll('button').filter((b) => b.text().includes('Details'))[0]!.trigger('click')
    await flushPromises()
    const notice = wrapper.get('[data-testid="sandbox-endpoints-notice"]').text()
    expect(notice).toMatch(/CDP \/ noVNC are not directly reachable/i)
    expect(notice).toContain('/sandbox-vnc/:sandboxId/ws')
    expect(notice).not.toContain('/preview-vnc/')
    expect(notice).toMatch(/Session required/i)
    expect(notice).toMatch(/session\/ide/i)
    wrapper.unmount()
  })
})

describe('SandboxListView loading small-fix lock', () => {
  it('table-loading must not use pointer-events:none', () => {
    const sandboxSrc = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'SandboxListView.vue'), 'utf8')
    const blockStart = sandboxSrc.indexOf('.table-loading {')
    expect(blockStart).toBeGreaterThanOrEqual(0)
    const blockEnd = sandboxSrc.indexOf('}', blockStart)
    const block = sandboxSrc.slice(blockStart, blockEnd + 1)
    expect(block).toMatch(/opacity:\s*0\.55/)
    expect(block).not.toMatch(/pointer-events:\s*none/)
    expect(sandboxSrc).not.toMatch(/\.table-loading\s*\{[^}]*pointer-events:\s*none/)
    expect(sandboxSrc).toMatch(/admin-list-thin-bar bg-accent/)
    expect(sandboxSrc).toMatch(/data-testid="sandbox-list-retry"/)
    expect(sandboxSrc).toMatch(/data-testid="sandbox-list-denied"/)
  })
})

describe('SandboxListView page header copy removal (g1/g2)', () => {
  const subtitleZh = ['工作流运行时', '空闲测试沙箱', '所有沙箱容器']
  const subtitleEn = ['Run node sandboxes', 'idle test sandboxes', 'Agent chat test sandboxes']
  const demoReview = ['副标题已移除', '改前', '改后', 'ghost-sub', 'subtitle removed']

  function pageHead(wrapper: ReturnType<typeof mountList>['wrapper']) {
    return wrapper.find('.mb-5.flex.flex-col')
  }

  function mountBilingual(initial: 'zh-CN' | 'en' = 'zh-CN') {
    const i18n = createI18n({
      legacy: false,
      locale: initial,
      messages: {
        'zh-CN': { ...common, ...pages },
        en: { ...enCommon, ...enPages },
      },
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        { path: '/sandboxes', component: SandboxListView },
        { path: '/sandboxes/:id/console', name: 'sandbox-console', component: { template: '<div data-testid="console" />' } },
      ],
    })
    router.push('/sandboxes')
    const wrapper = mount(SandboxListView, {
      global: {
        plugins: [i18n, router],
        stubs: {
          Icon: true,
          AppButton: { template: '<button type="button"><slot /></button>' },
          AppModal: {
            props: ['open', 'title', 'width'],
            template: '<div v-if="open" data-testid="sandbox-detail-modal"><slot /><slot name="footer" /></div>',
          },
        },
      },
    })
    return { wrapper, i18n }
  }

  it('removes pages.sandboxes.subtitle i18n keys (g1.2)', () => {
    expect((pages as { pages: { sandboxes: Record<string, unknown> } }).pages.sandboxes.subtitle).toBeUndefined()
    expect((enPages as { pages: { sandboxes: Record<string, unknown> } }).pages.sandboxes.subtitle).toBeUndefined()
  })

  it('hides zh subtitle, keeps title + cleanup, and Demo-aligned header (g1.1/g1.3/g2.1)', async () => {
    const { wrapper } = mountList('zh-CN')
    await flushPromises()

    const text = wrapper.text()
    for (const s of subtitleZh) expect(text).not.toContain(s)
    for (const s of demoReview) expect(text).not.toContain(s)
    expect(text).toContain('沙箱')
    expect(text).toContain('清理空闲')

    const head = pageHead(wrapper)
    expect(head.exists()).toBe(true)
    expect(head.classes()).toEqual(
      expect.arrayContaining([
        'mb-5',
        'flex',
        'flex-col',
        'items-start',
        'gap-2.5',
        'md:flex-row',
        'md:items-end',
        'md:justify-between',
      ]),
    )
    expect(head.classes()).not.toContain('justify-end')
    expect(head.find('p').exists()).toBe(false)
    expect(head.find('h2').text()).toBe('沙箱')
    wrapper.unmount()
  })

  it('hides en subtitle and keeps Sandboxes + Clean up idle (g2.2)', async () => {
    const { wrapper } = mountList('en')
    await flushPromises()

    const text = wrapper.text()
    for (const s of subtitleEn) expect(text).not.toContain(s)
    for (const s of demoReview) expect(text).not.toContain(s)
    expect(text).toContain('Sandboxes')
    expect(text).toContain('Clean up idle')
    expect(pageHead(wrapper).find('h2').text()).toBe('Sandboxes')
    wrapper.unmount()
  })

  it('language switch does not revive subtitle (g2.2)', async () => {
    const { wrapper, i18n } = mountBilingual('zh-CN')
    await flushPromises()

    let text = wrapper.text()
    for (const s of subtitleZh) expect(text).not.toContain(s)
    expect(text).toContain('沙箱')
    expect(text).toContain('清理空闲')

    i18n.global.locale.value = 'en'
    await flushPromises()
    text = wrapper.text()
    for (const s of subtitleZh) expect(text).not.toContain(s)
    for (const s of subtitleEn) expect(text).not.toContain(s)
    expect(text).toContain('Sandboxes')
    expect(text).toContain('Clean up idle')
    wrapper.unmount()
  })

  it('empty list still shows header without subtitle (g2.3)', async () => {
    apiMocks.listSandboxes.mockResolvedValue([])
    const { wrapper } = mountList('zh-CN')
    await flushPromises()

    const text = wrapper.text()
    for (const s of subtitleZh) expect(text).not.toContain(s)
    for (const s of demoReview) expect(text).not.toContain(s)
    expect(text).toContain('沙箱')
    expect(text).toContain('清理空闲')
    expect(text).toContain('暂无沙箱')
    expect(pageHead(wrapper).find('p').exists()).toBe(false)
    wrapper.unmount()
  })
})
