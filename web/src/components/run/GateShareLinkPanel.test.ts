// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import GateShareLinkPanel from './GateShareLinkPanel.vue'
import type { GateInboxItem } from '@/lib/shared/types'
import { forgetShareUrl, rememberShareUrl } from '@/lib/inbox/gateShareLink'

const mocks = vi.hoisted(() => {
  const toasts = { value: [] as Array<{ id: number; message: string; type?: string }> }
  let toastId = 0
  return {
    create: vi.fn(),
    regen: vi.fn(),
    revoke: vi.fn(),
    createReview: vi.fn(),
    regenReview: vi.fn(),
    revokeReview: vi.fn(),
    copyToClipboard: vi.fn(async (_text: string) => true),
    toasts,
    toastSuccess: vi.fn((message: string) => {
      const id = ++toastId
      toasts.value.push({ id, message, type: 'success' })
      return id
    }),
    toastShow: vi.fn((message: string) => {
      const id = ++toastId
      toasts.value.push({ id, message })
      return id
    }),
    toastDismiss: vi.fn((id: number) => {
      toasts.value = toasts.value.filter((t) => t.id !== id)
    }),
    resetToasts() {
      toasts.value = []
      toastId = 0
    },
  }
})

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      createGateShareLink: mocks.create,
      regenGateShareLink: mocks.regen,
      revokeGateShareLink: mocks.revoke,
      createReviewShareLink: mocks.createReview,
      regenReviewShareLink: mocks.regenReview,
      revokeReviewShareLink: mocks.revokeReview,
    },
  }
})

vi.mock('@/lib/shared/copyToClipboard', () => ({
  copyToClipboard: (text: string) => mocks.copyToClipboard(text),
}))

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({
    toasts: mocks.toasts,
    success: mocks.toastSuccess,
    show: mocks.toastShow,
    dismiss: mocks.toastDismiss,
    error: vi.fn(),
    warn: vi.fn(),
  }),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  messages: { 'zh-CN': { ...common, ...pages } },
})

function item(over: Partial<GateInboxItem> = {}): GateInboxItem {
  return {
    type: 'gate',
    nodeType: 'human_gate',
    runId: 'run-1',
    nodeId: 'hg1',
    iteration: 1,
    workflowName: 'wf',
    title: '审阅',
    bodyMd: '',
    actions: [{ id: 'approve', label: '批准' }],
    requestedAt: '2026-08-01T00:00:00Z',
    shareLink: { state: 'none', canCreate: true },
    ...over,
  }
}

beforeEach(() => {
  mocks.create.mockReset()
  mocks.regen.mockReset()
  mocks.revoke.mockReset()
  mocks.createReview.mockReset()
  mocks.regenReview.mockReset()
  mocks.revokeReview.mockReset()
  mocks.copyToClipboard.mockReset()
  mocks.copyToClipboard.mockResolvedValue(true)
  mocks.toastSuccess.mockClear()
  mocks.toastShow.mockClear()
  mocks.toastDismiss.mockClear()
  mocks.resetToasts()
  forgetShareUrl('run-1', 'hg1', 1)
  forgetShareUrl('run-1', 'research1', 1)
})

describe('GateShareLinkPanel', () => {
  it('creates with default 24h, masks URL, auto-copies full link (plan g1.1 / g2.2)', async () => {
    const token = 'ab'.repeat(32)
    const url = `https://app.example/public/gate-approvals#t=${token}`
    mocks.create.mockResolvedValue({ id: 'gsl-1', url, ttlTier: '24h', expiresAt: '2026-08-10T00:00:00Z', state: 'active' })

    const w = mount(GateShareLinkPanel, {
      props: { open: true, target: item() },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    await flushPromises()
    expect(w.get('[data-testid="gate-share-panel-body"]').text()).toContain('信任')
    expect(w.get('[data-testid="gate-share-panel-body"]').text()).toContain('审批工作台')
    expect(w.get('[data-testid="gate-share-panel-body"]').text()).toContain('可取点')
    expect(w.get('[data-testid="gate-share-panel-body"]').text()).not.toMatch(/不是内部审批工作台|不可取点|外部一次确认页/)
    const ttl24 = w.findAll('[data-testid="gate-share-ttl"]').find((b) => b.attributes('data-tier') === '24h')
    expect(ttl24).toBeTruthy()

    expect(w.findAll('[data-testid="gate-share-permission"]')).toHaveLength(2)
    const fullOpt = w.findAll('[data-testid="gate-share-permission"]').find((b) => b.attributes('data-preset') === 'full')
    expect(fullOpt?.attributes('aria-checked')).toBe('true')

    await w.get('[data-testid="gate-share-create"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith('run-1', 'hg1', '24h', 'full')
    expect(mocks.copyToClipboard).toHaveBeenCalledWith(url)
    expect(mocks.toastSuccess).toHaveBeenCalledWith('已自动复制新链接')
    const shown = (w.get('[data-testid="gate-share-url"]').element as HTMLTextAreaElement).value
    expect(shown).toContain('••••')
    expect(shown).not.toContain(token)
  })

  it('reveals full URL when clipboard fails (plan g3.1)', async () => {
    const token = 'cd'.repeat(32)
    const url = `https://app.example/public/gate-approvals#t=${token}`
    mocks.create.mockResolvedValue({ id: 'gsl-2', url, ttlTier: '24h', expiresAt: '2026-08-10T00:00:00Z', state: 'active' })
    mocks.copyToClipboard.mockResolvedValue(false)

    const w = mount(GateShareLinkPanel, {
      props: { open: true, target: item() },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    await w.get('[data-testid="gate-share-create"]').trigger('click')
    await flushPromises()
    const shown = (w.get('[data-testid="gate-share-url"]').element as HTMLTextAreaElement).value
    expect(shown).toBe(url)
    expect(mocks.toastShow).toHaveBeenCalledWith('无法写入剪贴板，请全选下方链接手动复制')
  })

  it('dedupes clipboardFallback toast on repeated failure (plan g3.2)', async () => {
    const token = 'ff'.repeat(32)
    const url = `https://app.example/public/gate-approvals#t=${token}`
    rememberShareUrl('run-1', 'hg1', 1, url)
    mocks.copyToClipboard.mockResolvedValue(false)

    const w = mount(GateShareLinkPanel, {
      props: {
        open: true,
        target: item({ shareLink: { state: 'active', ttlTier: '24h', canManage: true, canCreate: false } }),
      },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    await flushPromises()
    await w.get('[data-testid="gate-share-copy"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="gate-share-copy"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="gate-share-copy"]').trigger('click')
    await flushPromises()

    const fallback = '无法写入剪贴板，请全选下方链接手动复制'
    expect(mocks.copyToClipboard).toHaveBeenCalledTimes(3)
    expect(mocks.toastShow).toHaveBeenCalledTimes(3)
    expect(mocks.toastDismiss).toHaveBeenCalled()
    expect(mocks.toasts.value.filter((t) => t.message === fallback)).toHaveLength(1)
  })

  it('manual copy success uses copied toast (plan g1.2 / g2.2)', async () => {
    const token = 'ef'.repeat(32)
    const url = `https://app.example/public/gate-approvals#t=${token}`
    rememberShareUrl('run-1', 'hg1', 1, url)
    let resolveCopy!: (v: boolean) => void
    mocks.copyToClipboard.mockImplementation(
      () =>
        new Promise<boolean>((resolve) => {
          resolveCopy = resolve
        }),
    )

    const w = mount(GateShareLinkPanel, {
      props: {
        open: true,
        target: item({ shareLink: { state: 'active', ttlTier: '24h', canManage: true, canCreate: false } }),
      },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    await flushPromises()
    const copyBtn = w.get('[data-testid="gate-share-copy"]')
    await copyBtn.trigger('click')
    await copyBtn.trigger('click')
    expect(copyBtn.attributes('disabled')).toBeDefined()
    expect(copyBtn.attributes('aria-busy')).toBe('true')
    expect(copyBtn.text()).toMatch(/复制中/)
    expect(mocks.copyToClipboard).toHaveBeenCalledTimes(1)
    resolveCopy(true)
    await flushPromises()
    expect(mocks.toastSuccess).toHaveBeenCalledWith('已复制到剪贴板')
    w.unmount()
  })

  it('createAndCopy shows 创建中… and does not create twice', async () => {
    const token = 'ab'.repeat(32)
    const url = `https://app.example/public/gate-approvals#t=${token}`
    let resolveCreate!: (v: unknown) => void
    mocks.create.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveCreate = resolve
        }),
    )
    const w = mount(GateShareLinkPanel, {
      props: { open: true, target: item() },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    await flushPromises()
    const createBtn = w.get('[data-testid="gate-share-create"]')
    await createBtn.trigger('click')
    await createBtn.trigger('click')
    expect(createBtn.text()).toMatch(/创建中/)
    expect(mocks.create).toHaveBeenCalledTimes(1)
    resolveCreate({ id: 'gsl-1', url, ttlTier: '24h', expiresAt: '2026-08-10T00:00:00Z', state: 'active' })
    await flushPromises()
    w.unmount()
  })

  it('manage mode copies remembered URL without regenerating', async () => {
    const token = 'ef'.repeat(32)
    const url = `https://app.example/public/gate-approvals#t=${token}`
    rememberShareUrl('run-1', 'hg1', 1, url)

    const w = mount(GateShareLinkPanel, {
      props: {
        open: true,
        target: item({ shareLink: { state: 'active', ttlTier: '24h', canManage: true, canCreate: false } }),
      },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    await flushPromises()
    expect(w.find('[data-testid="gate-share-ttl"]').exists()).toBe(false)
    await w.get('[data-testid="gate-share-copy"]').trigger('click')
    await flushPromises()
    expect(mocks.regen).not.toHaveBeenCalled()
    expect(mocks.copyToClipboard).toHaveBeenCalledWith(url)
  })

  it('recalls URL from sessionStorage after in-memory miss (refresh)', async () => {
    const token = 'ef'.repeat(32)
    const url = `https://app.example/public/gate-approvals#t=${token}`
    rememberShareUrl('run-1', 'hg1', 1, url)
    forgetShareUrl('run-1', 'hg1', 1)
    sessionStorage.setItem('grasp.gateShareUrl.run-1:hg1:1', url)

    const w = mount(GateShareLinkPanel, {
      props: {
        open: true,
        target: item({ shareLink: { state: 'active', ttlTier: '24h', canManage: true, canCreate: false } }),
      },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    await flushPromises()
    await w.get('[data-testid="gate-share-copy"]').trigger('click')
    await flushPromises()
    expect(mocks.regen).not.toHaveBeenCalled()
    expect(mocks.copyToClipboard).toHaveBeenCalledWith(url)
  })

  it('disables copy when active URL cannot be recalled', async () => {
    sessionStorage.clear()
    forgetShareUrl('run-1', 'hg1', 1)

    const w = mount(GateShareLinkPanel, {
      props: {
        open: true,
        target: item({ shareLink: { state: 'active', ttlTier: '24h', canManage: true, canCreate: false } }),
      },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    await flushPromises()
    expect((w.get('[data-testid="gate-share-copy"]').element as HTMLButtonElement).disabled).toBe(true)
    expect(w.get('[data-testid="gate-share-copy-unavailable"]').text()).toMatch(/重新生成或撤销/)
    expect(mocks.regen).not.toHaveBeenCalled()
  })

  it('maps API error codes to locale text', async () => {
    mocks.create.mockRejectedValue(new Error('no_standard_action'))
    const w = mount(GateShareLinkPanel, {
      props: { open: true, target: item() },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    await w.get('[data-testid="gate-share-create"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="gate-share-error"]').text()).toContain('标准批准或驳回')
  })

  it('review kind calls reviews API and keeps primary actions at min-h-11', async () => {
    const token = 'ab'.repeat(32)
    const url = `https://app.example/public/gate-approvals#t=${token}`
    mocks.createReview.mockResolvedValue({ id: 'gsl-r1', url, ttlTier: '24h', expiresAt: '2026-08-10T00:00:00Z', state: 'active' })
    const w = mount(GateShareLinkPanel, {
      props: {
        open: true,
        kind: 'review',
        target: {
          runId: 'run-1',
          nodeId: 'research1',
          iteration: 1,
          shareLink: { state: 'none', canCreate: true },
          kind: 'review',
        },
      },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    const createBtn = w.get('[data-testid="gate-share-create"]')
    expect(createBtn.classes().join(' ')).toContain('min-h-11')
    await createBtn.trigger('click')
    await flushPromises()
    expect(mocks.createReview).toHaveBeenCalledWith('run-1', 'research1', '24h', 'full')
    expect(mocks.create).not.toHaveBeenCalled()
  })

  it('creates react_only preset and shows chip + regen inherit hint when active', async () => {
    const token = 'ro'.repeat(32)
    const url = `https://app.example/public/gate-approvals#t=${token}`
    mocks.create.mockResolvedValue({
      id: 'gsl-ro',
      url,
      ttlTier: '24h',
      permissionPreset: 'react_only',
      expiresAt: '2026-08-10T00:00:00Z',
      state: 'active',
    })
    const w = mount(GateShareLinkPanel, {
      props: { open: true, target: item() },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    const reactOnly = w
      .findAll('[data-testid="gate-share-permission"]')
      .find((b) => b.attributes('data-preset') === 'react_only')
    await reactOnly!.trigger('click')
    await w.get('[data-testid="gate-share-create"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith('run-1', 'hg1', '24h', 'react_only')
    expect(w.get('[data-testid="gate-share-preset-chip"]').text()).toContain('仅 ReAct')
    expect(w.get('[data-testid="gate-share-regen-inherit-hint"]').text()).toContain('继承')
    expect(w.get('[data-testid="gate-share-regen"]').text()).toContain('继承预设')
  })

  it('blocks copy and skips auto-copy for loopback share URLs', async () => {
    const token = 'lb'.repeat(32)
    const url = `http://localhost:8080/public/gate-approvals#t=${token}`
    mocks.create.mockResolvedValue({ id: 'gsl-lb', url, ttlTier: '24h', expiresAt: '2026-08-10T00:00:00Z', state: 'active' })

    const w = mount(GateShareLinkPanel, {
      props: { open: true, target: item() },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    await flushPromises()
    expect(w.get('[data-testid="gate-share-loopback-warning"]').text()).toMatch(/环回|外部不可达/)
    expect(w.find('[data-testid="gate-share-origin-hint"]').exists()).toBe(false)

    await w.get('[data-testid="gate-share-create"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalled()
    expect(mocks.copyToClipboard).not.toHaveBeenCalled()
    expect((w.get('[data-testid="gate-share-copy"]').element as HTMLButtonElement).disabled).toBe(true)
    expect(w.get('[data-testid="gate-share-loopback-copy-hint"]').text()).toMatch(/不可复制/)
    expect((w.get('[data-testid="gate-share-regen"]').element as HTMLButtonElement).disabled).toBe(false)
    expect((w.get('[data-testid="gate-share-revoke"]').element as HTMLButtonElement).disabled).toBe(false)
  })

  it('rewrites a loopback minted URL onto the page origin before copy', async () => {
    const happy = (window as unknown as { happyDOM?: { setURL: (u: string) => void } }).happyDOM
    const prev = window.location.href
    happy?.setURL('http://21.130.250.101:5173/gates')
    const token = 'pb'.repeat(32)
    const url = `http://localhost:8080/public/gate-approvals#t=${token}`
    const want = `http://21.130.250.101:5173/public/gate-approvals#t=${token}`
    mocks.create.mockResolvedValue({ id: 'gsl-pb', url, ttlTier: '24h', expiresAt: '2026-08-10T00:00:00Z', state: 'active' })

    const w = mount(GateShareLinkPanel, {
      props: { open: true, target: item() },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    await w.get('[data-testid="gate-share-create"]').trigger('click')
    await flushPromises()
    expect(mocks.copyToClipboard).toHaveBeenCalledWith(want)
    expect(w.find('[data-testid="gate-share-loopback-warning"]').exists()).toBe(false)
    happy?.setURL(prev)
    forgetShareUrl('run-1', 'hg1', 1)
  })

  it('shows origin-from-access hint and allows copy for non-loopback URLs', async () => {
    const token = 'pub'.repeat(32)
    const url = `https://approving.example.com/public/gate-approvals#t=${token}`
    rememberShareUrl('run-1', 'hg1', 1, url)

    const w = mount(GateShareLinkPanel, {
      props: {
        open: true,
        target: item({ shareLink: { state: 'active', ttlTier: '24h', canManage: true, canCreate: false } }),
      },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    await flushPromises()
    expect(w.get('[data-testid="gate-share-origin-hint"]').text()).toMatch(/当前访问|公开基址/)
    expect(w.find('[data-testid="gate-share-loopback-warning"]').exists()).toBe(false)
    await w.get('[data-testid="gate-share-copy"]').trigger('click')
    await flushPromises()
    expect(mocks.copyToClipboard).toHaveBeenCalledWith(url)
  })

  it('regen shows regenerated then autoCopied toasts (plan g2.3)', async () => {
    const token = 'rg'.repeat(32)
    const url = `https://approving.example.com/public/gate-approvals#t=${token}`
    const nextToken = 'nh'.repeat(32)
    const nextUrl = `https://approving.example.com/public/gate-approvals#t=${nextToken}`
    rememberShareUrl('run-1', 'hg1', 1, url)
    mocks.regen.mockResolvedValue({ id: 'gsl-rg', url: nextUrl, ttlTier: '24h', expiresAt: '2026-08-10T00:00:00Z', state: 'active' })

    const w = mount(GateShareLinkPanel, {
      props: {
        open: true,
        target: item({ shareLink: { state: 'active', ttlTier: '24h', canManage: true, canCreate: false } }),
      },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    await flushPromises()
    await w.get('[data-testid="gate-share-regen"]').trigger('click')
    await w.get('[data-testid="gate-share-confirm-ok"]').trigger('click')
    await flushPromises()
    expect(mocks.regen).toHaveBeenCalled()
    expect(mocks.copyToClipboard).toHaveBeenCalledWith(nextUrl)
    expect(mocks.toastSuccess.mock.calls.map((c) => c[0])).toEqual(['链接已重新生成', '已自动复制新链接'])
    const shown = (w.get('[data-testid="gate-share-url"]').element as HTMLTextAreaElement).value
    expect(shown).toContain('••••')
    expect(shown).not.toContain(nextToken)
  })

  it('skips auto-copy after regenerating a loopback URL but still toasts regenerated', async () => {
    const token = 'rg'.repeat(32)
    const url = `http://127.0.0.1:8080/public/gate-approvals#t=${token}`
    rememberShareUrl('run-1', 'hg1', 1, url)
    mocks.regen.mockResolvedValue({ id: 'gsl-rg', url, ttlTier: '24h', expiresAt: '2026-08-10T00:00:00Z', state: 'active' })

    const w = mount(GateShareLinkPanel, {
      props: {
        open: true,
        target: item({ shareLink: { state: 'active', ttlTier: '24h', canManage: true, canCreate: false } }),
      },
      global: { plugins: [i18n], stubs: { Teleport: true } },
    })
    await flushPromises()
    await w.get('[data-testid="gate-share-regen"]').trigger('click')
    await w.get('[data-testid="gate-share-confirm-ok"]').trigger('click')
    await flushPromises()
    expect(mocks.regen).toHaveBeenCalled()
    expect(mocks.copyToClipboard).not.toHaveBeenCalled()
    expect(mocks.toastSuccess).toHaveBeenCalledWith('链接已重新生成')
    expect((w.get('[data-testid="gate-share-copy"]').element as HTMLButtonElement).disabled).toBe(true)
  })
})
