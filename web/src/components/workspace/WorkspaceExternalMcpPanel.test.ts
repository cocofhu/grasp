// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import zhPages from '@/locales/zh-CN/pages.json'
import zhCommon from '@/locales/zh-CN/common.json'
import WorkspaceExternalMcpPanel from './WorkspaceExternalMcpPanel.vue'

const apiMocks = vi.hoisted(() => ({
  getExternalMcp: vi.fn(),
  updateExternalMcp: vi.fn(),
  listMcpKeys: vi.fn(),
  createMcpKey: vi.fn(),
  revokeMcpKey: vi.fn(),
}))

const mocks = vi.hoisted(() => ({
  copyToClipboard: vi.fn(async (_text: string) => true),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}))

vi.mock('@/lib/api/api', () => ({
  api: apiMocks,
}))

vi.mock('@/lib/shared/copyToClipboard', () => ({
  copyToClipboard: (text: string) => mocks.copyToClipboard(text),
}))

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: mocks.toastSuccess, error: mocks.toastError }),
}))

function mountPanel() {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { pages: zhPages.pages, common: zhCommon.common } },
  })
  return mount(WorkspaceExternalMcpPanel, {
    attachTo: document.body,
    global: { plugins: [i18n], stubs: { Teleport: true } },
  })
}

describe('WorkspaceExternalMcpPanel (plan g3.1/g3.2/g3.3)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.copyToClipboard.mockReset()
    mocks.copyToClipboard.mockResolvedValue(true)
    apiMocks.getExternalMcp.mockResolvedValue({
      enabled: false,
      enabledPacks: [],
      mcpBaseUrl: 'http://api.example.com/mcp/workspace',
    })
    apiMocks.listMcpKeys.mockResolvedValue([])
    apiMocks.updateExternalMcp.mockResolvedValue({
      enabled: true,
      enabledPacks: ['pm-progress'],
      mcpBaseUrl: 'http://api.example.com/mcp/workspace',
    })
    apiMocks.createMcpKey.mockResolvedValue({
      id: 'k1',
      name: 'cursor',
      key: 'cf_proj_abcd',
      key_prefix: 'cf_proj_••••abcd',
      created_at: '2026-01-01T00:00:00Z',
    })
  })

  it('loads settings and saves enabled packs (g3.1)', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    expect(apiMocks.getExternalMcp).toHaveBeenCalledWith()
    expect(wrapper.find('[data-testid="external-mcp-enabled"]').exists()).toBe(true)

    await wrapper.get('[data-testid="external-mcp-save"]').trigger('click')
    await flushPromises()
    expect(apiMocks.updateExternalMcp).toHaveBeenCalled()
  })

  it('opens create-key modal (g3.2)', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('[data-testid="external-mcp-create-key"]').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('新建工作区 MCP 密钥')
  })

  it('interpolates {base} in urlPackSuffix (g3.2 review v3)', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('http://api.example.com/mcp/workspace/pm-progress')
    expect(text).not.toContain('{base}')
  })
})

describe('WorkspaceExternalMcpPanel copy (plan g1/g2)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.copyToClipboard.mockReset()
    mocks.copyToClipboard.mockResolvedValue(true)
    apiMocks.getExternalMcp.mockResolvedValue({
      enabled: true,
      enabledPacks: ['pm-progress'],
      mcpBaseUrl: 'http://api.example.com/mcp/workspace',
    })
    apiMocks.listMcpKeys.mockResolvedValue([])
    apiMocks.createMcpKey.mockResolvedValue({
      id: 'k1',
      name: 'cursor',
      key: 'cf_proj_abcd',
      key_prefix: 'cf_proj_••••abcd',
      created_at: '2026-01-01T00:00:00Z',
    })
  })

  it('copies MCP base URL via copyToClipboard (g1.1 / g1.2)', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('[data-testid="external-mcp-copy-url"]').trigger('click')
    await flushPromises()
    expect(mocks.copyToClipboard).toHaveBeenCalledWith('http://api.example.com/mcp/workspace')
    expect(mocks.toastSuccess).toHaveBeenCalledWith('已复制到剪贴板')
    expect(mocks.toastError).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="external-mcp-copy-url"]').text()).toContain('已复制')
  })

  it('copies MCP JSON example via copyToClipboard (g1.1 / g1.2)', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('[data-testid="external-mcp-copy-example"]').trigger('click')
    await flushPromises()
    const call = mocks.copyToClipboard.mock.calls[0]?.[0] as string
    expect(call).toContain('http://api.example.com/mcp/workspace')
    expect(call).toContain('mcpServers')
    expect(mocks.toastSuccess).toHaveBeenCalledWith('已复制到剪贴板')
    expect(mocks.toastError).not.toHaveBeenCalled()
  })

  it('copies new key plain text via copyToClipboard (g1.1 / g1.2)', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('[data-testid="external-mcp-create-key"]').trigger('click')
    await flushPromises()
    const nameInput = wrapper.get('[data-testid="external-mcp-key-name"]')
    await nameInput.setValue('cursor')
    const createBtn = wrapper.findAll('button').find((b) => b.text().includes('创建密钥') && !b.attributes('data-testid'))
    expect(createBtn).toBeTruthy()
    await createBtn!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('cf_proj_abcd')
    const keyCode = wrapper.findAll('code').find((c) => c.text().includes('cf_proj_abcd'))
    expect(keyCode).toBeTruthy()
    const row = keyCode!.element.parentElement
    const copyBtn = row?.querySelector('button')
    expect(copyBtn?.textContent).toContain('复制')
    await copyBtn!.dispatchEvent(new Event('click'))
    await flushPromises()
    expect(mocks.copyToClipboard).toHaveBeenCalledWith('cf_proj_abcd')
    expect(mocks.toastSuccess).toHaveBeenCalledWith('已复制到剪贴板')
  })

  it('shows copyFailed toast when copyToClipboard returns false (g2.1 / g2.2)', async () => {
    mocks.copyToClipboard.mockResolvedValue(false)
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('[data-testid="external-mcp-copy-url"]').trigger('click')
    await flushPromises()
    expect(mocks.toastError).toHaveBeenCalledWith('复制失败')
    expect(mocks.toastSuccess).not.toHaveBeenCalled()
  })

  it('does not call navigator.clipboard.writeText directly (g2.2)', () => {
    const panelSrc = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), 'WorkspaceExternalMcpPanel.vue'),
      'utf8',
    )
    expect(panelSrc).not.toMatch(/navigator\.clipboard\.writeText/)
    expect(panelSrc).toMatch(/copyToClipboard/)
  })
})
