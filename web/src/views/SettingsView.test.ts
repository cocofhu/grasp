// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'

const authMocks = vi.hoisted(() => ({ isAdmin: true }))

const apiMocks = vi.hoisted(() => ({
  getSettings: vi.fn(),
  updateSettings: vi.fn(),
  updateWorkspace: vi.fn(),
  listSandboxes: vi.fn(),
  dashboard: vi.fn(),
}))

const workspaceState = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports -- vi.hoisted runs before ESM imports
  const { ref } = require('vue') as typeof import('vue')
  const workspace = ref<any>({ id: 'ws-1', name: 'Studio', description: 'Our studio', unknownModelDisplayName: '' })
  return {
    workspace,
    loading: ref(false),
    error: ref(''),
    ensureLoaded: vi.fn(async () => workspace.value),
    reload: vi.fn(async () => workspace.value),
  }
})

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getSettings: apiMocks.getSettings,
      updateSettings: apiMocks.updateSettings,
      updateWorkspace: apiMocks.updateWorkspace,
      listSandboxes: apiMocks.listSandboxes,
      dashboard: apiMocks.dashboard,
    },
  }
})

vi.mock('@/lib/composables/useAuth', async () => {
  const { ref } = await import('vue')
  return {
    useAuth: () => ({ user: ref({ username: 'admin', isAdmin: authMocks.isAdmin }) }),
  }
})

vi.mock('@/lib/composables/useWorkspaceContext', () => ({
  useWorkspaceContext: () => workspaceState,
}))

import SettingsView from './SettingsView.vue'

const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'SettingsView.vue'), 'utf8')

const SETTINGS = {
  brand: { product_name: 'Acme Flow', home_subtitle: 'Clarify together' },
  items: [
    { key: 'max_concurrent_runs', value: 4, min: 1, source: 'db', locked: false },
    { key: 'run_sandbox_ttl_minutes', value: 30, min: 1, source: 'db', locked: false },
    { key: 'test_sandbox_ttl_minutes', value: 15, min: 1, source: 'db', locked: false },
    { key: 'max_test_sandboxes', value: 3, min: 1, source: 'db', locked: false },
    { key: 'sandbox_memory_mb', value: 8192, min: 1024, source: 'config', locked: false },
    { key: 'agent_idle_timeout_minutes', value: 20, min: 2, max: 120, source: 'config', locked: false },
  ],
}

function mountSettings(locale: 'zh-CN' | 'en' = 'zh-CN') {
  const i18n = createI18n({
    legacy: false,
    locale,
    messages: {
      'zh-CN': { ...common, ...pages },
      en: { ...enCommon, ...enPages },
    },
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/settings', component: SettingsView }],
  })
  void router.push('/settings')
  return mount(SettingsView, {
    global: {
      plugins: [i18n, router],
      stubs: {
        Icon: true,
        AppButton: { template: '<button type="button" v-bind="$attrs"><slot /></button>' },
        IntegrationsPanel: {
          template: '<div data-testid="integrations-panel-stub" />',
        },
      },
    },
  })
}

beforeEach(() => {
  authMocks.isAdmin = true
  workspaceState.workspace.value = { id: 'ws-1', name: 'Studio', description: 'Our studio', unknownModelDisplayName: '' }
  workspaceState.loading.value = false
  workspaceState.error.value = ''
  apiMocks.updateWorkspace.mockImplementation(async (patch: Record<string, unknown>) => ({ ...workspaceState.workspace.value, ...patch }))
})

describe('SettingsView loading source lock', () => {
  it('first load uses grouped form skeleton, not EmptyState loadingTitle', () => {
    expect(src).toMatch(/data-testid="settings-form-skeleton"/)
    expect(src).not.toMatch(/EmptyState/)
    expect(src).not.toMatch(/pages\.settings\.loadingTitle/)
    expect(src).toMatch(/admin-list-thin-bar bg-accent/)
    expect(src).toMatch(/opacity-\[0\.55\]/)
    expect(src).toMatch(/class="settings-page"/)
    expect(src).not.toMatch(/overflow-y-auto/)
    expect(src).toMatch(/data-testid="settings-save-actions"/)
  })
})

describe('SettingsView brand copy', () => {
  it('hydrates and saves both brand fields with scheduling values', async () => {
    apiMocks.getSettings.mockResolvedValue(SETTINGS)
    apiMocks.listSandboxes.mockResolvedValue([])
    apiMocks.dashboard.mockResolvedValue({ running: 0 })
    apiMocks.updateSettings.mockResolvedValue({
      ...SETTINGS,
      brand: { product_name: 'New Name', home_subtitle: 'New subtitle' },
    })
    const w = mountSettings()
    await flushPromises()
    expect(w.get('[data-testid="brand-product-name"]').element).toHaveProperty('value', 'Acme Flow')
    expect(w.get('[data-testid="brand-home-subtitle"]').element).toHaveProperty('value', 'Clarify together')
    expect(w.get('[data-testid="brand-home-subtitle"]').attributes('placeholder')).toBe('我们应该做些什么？')
    await w.get('[data-testid="brand-product-name"]').setValue('New Name')
    await w.get('[data-testid="brand-home-subtitle"]').setValue('New subtitle')
    const saveButton = w.findAll('button').find((button) => button.text().includes('保存'))
    await saveButton!.trigger('click')
    await flushPromises()
    expect(apiMocks.updateWorkspace).not.toHaveBeenCalled()
    expect(apiMocks.updateSettings).toHaveBeenCalledWith({
      brand_product_name: 'New Name',
      brand_home_subtitle: 'New subtitle',
    })
    w.unmount()
  })
})

describe('SettingsView number input polish (Demo 优化后)', () => {
  it('skeleton keeps w-[88px] number placeholder width', () => {
    expect(src).toMatch(/data-testid="settings-form-skeleton"/)
    expect(src).toMatch(/h-9 w-\[88px\] bg-elevated animate-pulse/)
  })

  it('number rows: label/for, chip unit, no empty w-9, disabled visual, scoped spinner hide', () => {
    // g1.1 — no empty unit placeholder
    expect(src).not.toMatch(/<span v-else class="w-9"\s*\/>/)
    // g1.2 — TTL unit via .chip + common.minutes (NodeInspector-aligned)
    expect(src).toMatch(/class="chip">\{\{\s*settingUnit\(key\)\s*\}\}<\/span>/)
    expect(src).toMatch(/run_sandbox_ttl_minutes: 'common\.minutes'/)
    expect(src).not.toMatch(/common\.format\.minutes/)
    // g1.3 / f7 — width + right align preserved
    expect(src).toMatch(/settings-number-input w-\[88px\] text-right/)
    // g2.1 — locked/saving disabled visual
    expect(src).toMatch(/disabled:cursor-not-allowed disabled:opacity-55/)
    // g2.2 — scoped spinner hide (not global.css)
    expect(src).toMatch(/\.settings-number-input\[type='number'\]::-webkit-inner-spin-button/)
    expect(src).toMatch(/appearance:\s*textfield/)
    // g3.1 — label/for + stable id
    expect(src).toMatch(/:for="`setting-\$\{key\}`"/)
    expect(src).toMatch(/:id="`setting-\$\{key\}`"/)
  })

  it('mounted: unit rows show chip 分钟; no-unit rows have only input on the right', async () => {
    apiMocks.getSettings.mockResolvedValue(SETTINGS)
    apiMocks.listSandboxes.mockResolvedValue([])
    apiMocks.dashboard.mockResolvedValue({ running: 0 })
    const w = mountSettings()
    await flushPromises()

    const runTtl = w.find('#setting-run_sandbox_ttl_minutes')
    expect(runTtl.exists()).toBe(true)
    expect(runTtl.classes()).toContain('settings-number-input')
    const runCtrl = runTtl.element.parentElement!
    expect(runCtrl.querySelector('.chip')?.textContent).toContain('分钟')
    expect(runCtrl.querySelectorAll('span.w-9').length).toBe(0)

    const mcr = w.find('#setting-max_concurrent_runs')
    expect(mcr.exists()).toBe(true)
    const mcrCtrl = mcr.element.parentElement!
    expect(mcrCtrl.querySelector('.chip')).toBeNull()
    expect(mcrCtrl.querySelectorAll('span.w-9').length).toBe(0)

    // label association: clicking title focuses input when enabled
    const label = w.find('label[for="setting-max_concurrent_runs"]')
    expect(label.exists()).toBe(true)
    expect(label.text()).toContain('最大并发运行数')

    w.unmount()
  })
})

describe('SettingsView sandbox memory', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.listSandboxes.mockResolvedValue([])
    apiMocks.dashboard.mockResolvedValue({ running: 0 })
  })

  it('renders the memory limit with a MiB chip and saves the edited value', async () => {
    apiMocks.getSettings.mockResolvedValue(SETTINGS)
    apiMocks.updateSettings.mockResolvedValue(SETTINGS)
    const w = mountSettings()
    await flushPromises()

    expect(w.text()).toContain('沙箱资源')
    const input = w.find('#setting-sandbox_memory_mb')
    expect(input.exists()).toBe(true)
    expect(input.element).toHaveProperty('value', '8192')
    expect(input.attributes('min')).toBe('1024')
    expect(input.element.parentElement!.querySelector('.chip')?.textContent).toContain('MiB')
    expect(w.find('label[for="setting-sandbox_memory_mb"]').text()).toContain('沙箱内存上限')

    await input.setValue('12288')
    const saveButton = w.findAll('button').find((button) => button.text().includes('保存'))
    await saveButton!.trigger('click')
    await flushPromises()
    expect(apiMocks.updateSettings).toHaveBeenCalledWith(expect.objectContaining({
      sandbox_memory_mb: 12288,
    }))
    w.unmount()
  })

  it('renders the agent no-activity limit with its range and saves it', async () => {
    apiMocks.getSettings.mockResolvedValue(SETTINGS)
    apiMocks.updateSettings.mockResolvedValue(SETTINGS)
    const w = mountSettings()
    await flushPromises()

    const input = w.find('#setting-agent_idle_timeout_minutes')
    expect(input.element).toHaveProperty('value', '20')
    expect(input.attributes('min')).toBe('2')
    expect(input.attributes('max')).toBe('120')
    expect(w.find('label[for="setting-agent_idle_timeout_minutes"]').text()).toContain('Agent 无动作时限')

    await input.setValue('45')
    const saveButton = w.findAll('button').find((button) => button.text().includes('保存'))
    await saveButton!.trigger('click')
    await flushPromises()
    expect(apiMocks.updateSettings).toHaveBeenCalledWith(expect.objectContaining({
      agent_idle_timeout_minutes: 45,
    }))
    w.unmount()
  })
})

describe('SettingsView first skeleton vs reset keep form', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.listSandboxes.mockResolvedValue([])
    apiMocks.dashboard.mockResolvedValue({ running: 0 })
  })

  it('shows form skeleton before settings arrive', async () => {
    let release!: (v: unknown) => void
    apiMocks.getSettings.mockReturnValue(new Promise((resolve) => { release = resolve }))
    const w = mountSettings()
    await flushPromises()
    expect(w.find('[data-testid="settings-form-skeleton"]').exists()).toBe(true)
    release!(SETTINGS)
    await flushPromises()
    expect(w.find('[data-testid="settings-form-skeleton"]').exists()).toBe(false)
    expect(w.text()).toContain('最大并发运行数')
    w.unmount()
  })

  it('reset keeps form fields on screen with thin progress', async () => {
    apiMocks.getSettings.mockResolvedValue(SETTINGS)
    const w = mountSettings()
    await flushPromises()
    expect(w.find('input').exists()).toBe(true)
    let release!: (v: unknown) => void
    apiMocks.getSettings.mockReturnValue(new Promise((resolve) => { release = resolve }))
    const resetBtn = w.findAll('button').find((b) => b.text().includes('重置'))
    expect(resetBtn).toBeTruthy()
    await resetBtn!.trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="settings-thin-progress"]').exists()).toBe(true)
    expect(w.find('input').exists()).toBe(true)
    expect(w.find('[data-testid="settings-form-skeleton"]').exists()).toBe(false)
    release!(SETTINGS)
    await flushPromises()
    w.unmount()
  })
})

describe('SettingsView general page has no entry cards (plan g2.1 / f1 / f2)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.listSandboxes.mockResolvedValue([])
    apiMocks.dashboard.mockResolvedValue({ running: 0 })
  })

  it('source has no platform-rules or integrations entry cards; keeps inline panel', () => {
    expect(src).not.toMatch(/data-testid="settings-integrations-card"/)
    expect(src).not.toMatch(/data-testid="settings-integrations-open"/)
    expect(src).not.toMatch(/platformRulesCard/)
    expect(src).not.toMatch(/integrationsCard/)
    expect(src).not.toMatch(/openIntegrations/)
    expect(src).toMatch(/IntegrationsPanel v-if="showIntegrations"/)
    expect(src).not.toMatch(/IntegrationsModal/)
    expect(src).not.toMatch(/router\.replace/)
    expect(src).toMatch(/query\.integrations/)
  })

  it('does not render entry cards while settings still loading (g2.1)', async () => {
    apiMocks.getSettings.mockReturnValue(new Promise(() => {}))
    const w = mountSettings()
    await flushPromises()
    expect(w.find('[data-testid="settings-integrations-card"]').exists()).toBe(false)
    expect(w.find('[data-testid="settings-integrations-open"]').exists()).toBe(false)
    expect(w.text()).not.toContain('查看集成')
    expect(w.text()).not.toContain('查看规则')
    expect(w.text()).not.toContain('管理规则')
    w.unmount()
  })

  it('does not render entry cards on 403 (g2.1 / edge 403)', async () => {
    apiMocks.getSettings.mockRejectedValue(Object.assign(new Error('forbidden'), { status: 403 }))
    const w = mountSettings()
    await flushPromises()
    expect(w.find('[data-testid="settings-denied"]').exists()).toBe(true)
    expect(w.find('[data-testid="settings-integrations-card"]').exists()).toBe(false)
    expect(w.find('[data-testid="integrations-panel-stub"]').exists()).toBe(false)
    expect(w.text()).not.toContain('查看集成')
    w.unmount()
  })

  it('renders integrations inline from ?integrations=1 and keeps query (f4 / s4)', async () => {
    apiMocks.getSettings.mockResolvedValue(SETTINGS)
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/settings', component: SettingsView }],
    })
    await router.push('/settings?integrations=1')
    const w = mount(SettingsView, {
      global: {
        plugins: [i18n, router],
        stubs: {
          Icon: true,
          AppButton: { template: '<button type="button" v-bind="$attrs"><slot /></button>' },
          IntegrationsPanel: {
            template: '<div data-testid="integrations-panel-stub" />',
          },
        },
      },
    })
    await flushPromises()
    expect(w.find('[data-testid="integrations-panel-stub"]').exists()).toBe(true)
    expect(w.find('[data-testid="settings-integrations-card"]').exists()).toBe(false)
    expect(w.find('[data-testid="settings-form-skeleton"]').exists()).toBe(false)
    expect(w.find('[data-testid="workspace-info-section"]').exists()).toBe(false)
    expect(w.find('[data-testid="settings-save-actions"]').exists()).toBe(false)
    expect(w.find('#workspace-meta-name').exists()).toBe(false)
    expect(router.currentRoute.value.query.integrations).toBe('1')
    w.unmount()
  })
})

describe('SettingsView grouped form behavior', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.getSettings.mockResolvedValue(SETTINGS)
    apiMocks.updateSettings.mockResolvedValue(SETTINGS)
    apiMocks.listSandboxes.mockResolvedValue([])
    apiMocks.dashboard.mockResolvedValue({ running: 0 })
  })

  it('keeps environment-locked values read-only and omits them from saves', async () => {
    const locked = {
      ...SETTINGS,
      items: SETTINGS.items.map((item) => item.key === 'max_concurrent_runs' ? { ...item, source: 'env', locked: true } : item),
    }
    apiMocks.getSettings.mockResolvedValue(locked)
    apiMocks.updateSettings.mockResolvedValue(locked)
    const w = mountSettings()
    await flushPromises()
    expect(w.get('#setting-max_concurrent_runs').attributes('disabled')).toBeDefined()
    expect(w.text()).toContain('环境变量锁定')
    await w.get('[data-testid="brand-product-name"]').setValue('Renamed')
    await w.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(apiMocks.updateSettings).toHaveBeenCalledWith(expect.objectContaining({ brand_product_name: 'Renamed' }))
    expect(apiMocks.updateSettings.mock.calls[0]![0]).not.toHaveProperty('max_concurrent_runs')
    w.unmount()
  })

  it('leaves brand fields read-only for non-admins and saves only allowed numeric values', async () => {
    authMocks.isAdmin = false
    const w = mountSettings()
    await flushPromises()
    expect(w.get('[data-testid="brand-product-name"]').attributes('disabled')).toBeDefined()
    expect(w.get('[data-testid="brand-home-subtitle"]').attributes('disabled')).toBeDefined()
    expect(w.text()).toContain('仅管理员可修改品牌文案')
    await w.get('#setting-max_concurrent_runs').setValue(5)
    await w.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(apiMocks.updateSettings).toHaveBeenCalledWith(expect.objectContaining({ max_concurrent_runs: 5 }))
    expect(apiMocks.updateSettings.mock.calls[0]![0]).not.toHaveProperty('brand_product_name')
    expect(apiMocks.updateSettings.mock.calls[0]![0]).not.toHaveProperty('brand_home_subtitle')
    w.unmount()
  })

  it('keeps all detailed resource and retention rules available', async () => {
    apiMocks.getSettings.mockResolvedValue({
      ...SETTINGS,
      items: [...SETTINGS.items, { key: 'run_sandbox_max_retention_hours', value: 12, min: 1, source: 'config', locked: false }],
    })
    const w = mountSettings()
    await flushPromises()
    expect(w.findAll('details').length).toBe(3)
    expect(w.get('label[for="setting-sandbox_memory_mb"]').text()).toContain('沙箱内存上限')
    expect(w.findAll('details').map((details) => details.text()).join(' ')).toContain('保留不跨重启')
    expect(w.findAll('details').map((details) => details.text()).join(' ')).toContain('OOM')
    expect(w.findAll('details').map((details) => details.text()).join(' ')).toContain('不会被误杀')
    w.unmount()
  })

  it('rejects an invalid test sandbox limit and keeps the edit after a save failure', async () => {
    const w = mountSettings()
    await flushPromises()
    await w.get('#setting-max_test_sandboxes').setValue(0)
    const save = w.findAll('button').find((button) => button.text() === '保存')!
    await save.trigger('click')
    expect(apiMocks.updateSettings).not.toHaveBeenCalled()
    expect(w.get('[role="alert"]').text()).toContain('不能小于 1')
    await w.get('#setting-max_test_sandboxes').setValue(6)
    apiMocks.updateSettings.mockRejectedValueOnce(new Error('Save unavailable'))
    await save.trigger('click')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toContain('Save unavailable')
    expect(w.get('#setting-max_test_sandboxes').element).toHaveProperty('value', '6')
    w.unmount()
  })

  it('retries a failed initial settings request', async () => {
    apiMocks.getSettings.mockRejectedValueOnce(new Error('Service unavailable'))
    const w = mountSettings()
    await flushPromises()
    expect(w.find('[data-testid="settings-failed"]').exists()).toBe(true)
    await w.get('[data-testid="settings-retry"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="settings-failed"]').exists()).toBe(false)
    expect(w.get('#setting-max_concurrent_runs').element).toHaveProperty('value', '4')
    w.unmount()
  })

  it('polls capacity every five seconds, retains the last values on failure, and stops on unmount', async () => {
    vi.useFakeTimers()
    const w = mountSettings()
    try {
      apiMocks.listSandboxes.mockResolvedValue([{ id: 'test-1', purpose: 'test', status: 'running' }, { id: 'ttl-1', purpose: 'run', status: 'running' }])
      apiMocks.dashboard.mockResolvedValue({ running: 4 })
      await flushPromises()
      await vi.advanceTimersByTimeAsync(5000)
      await flushPromises()
      const usage = w.get('[data-testid="settings-capacity-usage"]')
      expect(usage.text()).toContain('5个')
      expect(usage.text()).toContain('1/3')
      expect(usage.text()).toContain('4/4')
      expect(usage.find('.bg-err').exists()).toBe(true)
      apiMocks.listSandboxes.mockRejectedValueOnce(new Error('Usage unavailable'))
      await vi.advanceTimersByTimeAsync(5000)
      await flushPromises()
      expect(usage.text()).toContain('Usage unavailable')
      expect(usage.text()).toContain('5个')
      w.unmount()
      const callCount = apiMocks.listSandboxes.mock.calls.length
      await vi.advanceTimersByTimeAsync(10000)
      expect(apiMocks.listSandboxes.mock.calls.length).toBe(callCount)
    } finally {
      w.unmount()
      vi.useRealTimers()
    }
  })
})

describe('SettingsView workspace information on the general page', () => {
  function saveButton(w: ReturnType<typeof mountSettings>) {
    return w.findAll('button').find((button) => button.text() === '保存')!
  }

  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.getSettings.mockResolvedValue(SETTINGS)
    apiMocks.updateSettings.mockResolvedValue(SETTINGS)
    apiMocks.listSandboxes.mockResolvedValue([])
    apiMocks.dashboard.mockResolvedValue({ running: 0 })
    apiMocks.updateWorkspace.mockImplementation(async (patch: Record<string, unknown>) => ({ ...workspaceState.workspace.value, ...patch }))
  })

  it('places workspace information above brand copy and keeps a single footer', async () => {
    const w = mountSettings()
    await flushPromises()
    const titles = w.findAll('h2').map((heading) => heading.text())
    expect(titles[0]).toBe('工作区信息')
    expect(titles[1]).toBe('品牌文案')
    expect(w.text()).toContain('管理工作区信息、品牌文案、运行并发与沙箱资源。')
    expect(w.text()).toContain('使用页底保存')
    expect(w.get('#workspace-meta-name').element).toHaveProperty('value', 'Studio')
    expect(w.get('[data-testid="workspace-info-section"]').findAll('button').map((button) => button.text())).toEqual(['清空显示名'])
    expect(w.get('[data-testid="settings-save-actions"]').text()).toContain('重置')
    expect(w.get('[data-testid="settings-save-actions"]').text()).toContain('保存')
    w.unmount()
  })

  it('submits only the workspace when its fields change', async () => {
    const w = mountSettings()
    await flushPromises()
    await w.get('#workspace-meta-name').setValue('New studio')
    await w.get('#workspace-meta-desc').setValue('A shared workspace')
    await saveButton(w).trigger('click')
    await flushPromises()
    expect(apiMocks.updateWorkspace).toHaveBeenCalledWith({ name: 'New studio', description: 'A shared workspace', unknownModelDisplayName: '' })
    expect(apiMocks.updateSettings).not.toHaveBeenCalled()
    expect(workspaceState.workspace.value.name).toBe('New studio')
    expect(w.get('[data-testid="settings-save-actions"]').text()).toContain('已保存')
    expect(saveButton(w).attributes('disabled')).toBeDefined()
    w.unmount()
  })

  it('submits only changed platform values when workspace information is untouched', async () => {
    const w = mountSettings()
    await flushPromises()
    await w.get('#setting-max_concurrent_runs').setValue('8')
    await saveButton(w).trigger('click')
    await flushPromises()
    expect(apiMocks.updateWorkspace).not.toHaveBeenCalled()
    expect(apiMocks.updateSettings).toHaveBeenCalledWith({ max_concurrent_runs: 8 })
    w.unmount()
  })

  it('submits both sides from one click and omits locked values and brand for non-admins', async () => {
    authMocks.isAdmin = false
    const locked = {
      ...SETTINGS,
      items: SETTINGS.items.map((item) => item.key === 'sandbox_memory_mb' ? { ...item, locked: true, source: 'env' } : item),
    }
    apiMocks.getSettings.mockResolvedValue(locked)
    const w = mountSettings()
    await flushPromises()
    await w.get('#workspace-meta-name').setValue('Renamed')
    await w.get('#setting-max_concurrent_runs').setValue('9')
    await w.get('#setting-sandbox_memory_mb').setValue('2048')
    await saveButton(w).trigger('click')
    await flushPromises()
    expect(apiMocks.updateWorkspace).toHaveBeenCalledWith(expect.objectContaining({ name: 'Renamed' }))
    expect(apiMocks.updateSettings).toHaveBeenCalledWith({ max_concurrent_runs: 9 })
    expect(apiMocks.updateSettings.mock.calls[0]![0]).not.toHaveProperty('brand_product_name')
    expect(apiMocks.updateSettings.mock.calls[0]![0]).not.toHaveProperty('sandbox_memory_mb')
    w.unmount()
  })

  it('blocks both requests when the name, display name, or test sandbox limit is invalid', async () => {
    const w = mountSettings()
    await flushPromises()
    await w.get('#workspace-meta-name').setValue('   ')
    await w.get('#setting-max_concurrent_runs').setValue('8')
    await saveButton(w).trigger('click')
    expect(apiMocks.updateWorkspace).not.toHaveBeenCalled()
    expect(apiMocks.updateSettings).not.toHaveBeenCalled()
    expect(w.get('[data-testid="workspace-meta-name-error"]').text()).toContain('工作区名称不能为空')

    await w.get('#workspace-meta-name').setValue('Studio')
    await w.get('[data-testid="workspace-meta-unknown-display"]').setValue('x'.repeat(65))
    await saveButton(w).trigger('click')
    expect(apiMocks.updateWorkspace).not.toHaveBeenCalled()
    expect(apiMocks.updateSettings).not.toHaveBeenCalled()
    expect(w.get('[data-testid="workspace-meta-unknown-display-error"]').text()).toContain('64')

    await w.get('[data-testid="workspace-meta-unknown-display"]').setValue('gpt-5')
    await w.get('#setting-max_test_sandboxes').setValue('0')
    await saveButton(w).trigger('click')
    expect(apiMocks.updateWorkspace).not.toHaveBeenCalled()
    expect(apiMocks.updateSettings).not.toHaveBeenCalled()
    expect(w.get('[role="alert"]').text()).toContain('不能小于 1')
    w.unmount()
  })

  it('keeps the failed side editable and does not roll back the saved side', async () => {
    apiMocks.updateSettings.mockRejectedValueOnce(new Error('platform down'))
    const w = mountSettings()
    await flushPromises()
    await w.get('#workspace-meta-name').setValue('Kept')
    await w.get('#setting-max_concurrent_runs').setValue('8')
    await saveButton(w).trigger('click')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toBe('平台参数保存失败：platform down')
    expect(w.get('[role="alert"]').text()).not.toContain('工作区信息保存失败')
    expect(workspaceState.workspace.value.name).toBe('Kept')
    expect(w.get('#setting-max_concurrent_runs').element).toHaveProperty('value', '8')
    expect(saveButton(w).attributes('disabled')).toBeUndefined()

    apiMocks.updateSettings.mockResolvedValueOnce({
      ...SETTINGS,
      items: SETTINGS.items.map((item) => item.key === 'max_concurrent_runs' ? { ...item, value: 8 } : item),
    })
    await saveButton(w).trigger('click')
    await flushPromises()
    expect(apiMocks.updateWorkspace).toHaveBeenCalledTimes(1)
    expect(apiMocks.updateSettings).toHaveBeenCalledTimes(2)
    expect(saveButton(w).attributes('disabled')).toBeDefined()
    w.unmount()
  })

  it('retains workspace edits when only that side fails', async () => {
    apiMocks.updateWorkspace.mockRejectedValueOnce(new Error('workspace down'))
    apiMocks.updateSettings.mockResolvedValueOnce({
      ...SETTINGS,
      items: SETTINGS.items.map((item) => item.key === 'max_concurrent_runs' ? { ...item, value: 8 } : item),
    })
    const w = mountSettings()
    await flushPromises()
    await w.get('#workspace-meta-name').setValue('Still editing')
    await w.get('#setting-max_concurrent_runs').setValue('8')
    await saveButton(w).trigger('click')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toBe('工作区信息保存失败：workspace down')
    expect(w.get('[role="alert"]').text()).not.toContain('平台参数保存失败')
    expect(w.get('#workspace-meta-name').element).toHaveProperty('value', 'Still editing')
    expect(w.get('#setting-max_concurrent_runs').element).toHaveProperty('value', '8')
    expect(workspaceState.workspace.value.name).toBe('Studio')
    expect(saveButton(w).attributes('disabled')).toBeUndefined()
    await saveButton(w).trigger('click')
    await flushPromises()
    expect(apiMocks.updateWorkspace).toHaveBeenCalledTimes(2)
    expect(apiMocks.updateSettings).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('hides the saved status after clearing a display name that was just saved', async () => {
    const w = mountSettings()
    await flushPromises()
    await w.get('[data-testid="workspace-meta-unknown-display"]').setValue('gpt-5')
    await saveButton(w).trigger('click')
    await flushPromises()
    expect(apiMocks.updateWorkspace).toHaveBeenCalledTimes(1)
    expect(w.get('[data-testid="settings-save-actions"]').text()).toContain('已保存')

    await w.get('[data-testid="workspace-meta-unknown-display-clear"]').trigger('click')
    expect(apiMocks.updateWorkspace).toHaveBeenCalledTimes(1)
    expect(w.get('[data-testid="workspace-meta-unknown-display"]').element).toHaveProperty('value', '')
    expect(w.get('[data-testid="settings-save-actions"]').text()).not.toContain('已保存')
    expect(saveButton(w).attributes('disabled')).toBeUndefined()
    w.unmount()
  })

  it('joins an English failure reason with an ASCII colon', async () => {
    apiMocks.updateSettings.mockRejectedValueOnce(new Error('platform down'))
    const w = mountSettings('en')
    await flushPromises()
    await w.get('#workspace-meta-name').setValue('Kept')
    await w.get('#setting-max_concurrent_runs').setValue('8')
    await w.findAll('button').find((button) => button.text() === 'Save')!.trigger('click')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toBe('Platform settings failed to save: platform down')
    expect(w.get('[role="alert"]').text()).not.toContain('：')
    w.unmount()
  })

  it('clears the display name locally and saves it with the footer', async () => {
    workspaceState.workspace.value = { ...workspaceState.workspace.value, unknownModelDisplayName: 'gpt-5' }
    const w = mountSettings()
    await flushPromises()
    await w.get('[data-testid="workspace-meta-unknown-display-clear"]').trigger('click')
    expect(apiMocks.updateWorkspace).not.toHaveBeenCalled()
    expect(w.get('[data-testid="workspace-meta-unknown-display"]').element).toHaveProperty('value', '')
    expect(w.get('[data-testid="settings-save-actions"]').text()).not.toContain('已保存')
    expect(saveButton(w).attributes('disabled')).toBeUndefined()
    await saveButton(w).trigger('click')
    await flushPromises()
    expect(apiMocks.updateWorkspace).toHaveBeenCalledWith(expect.objectContaining({ unknownModelDisplayName: '' }))
    expect(apiMocks.updateSettings).not.toHaveBeenCalled()
    w.unmount()
  })

  it('resets both drafts and ignores a save that finishes after leaving the page', async () => {
    const w = mountSettings()
    await flushPromises()
    await w.get('#workspace-meta-name').setValue('Temporary')
    await w.get('#setting-max_concurrent_runs').setValue('8')
    await w.findAll('button').find((button) => button.text() === '重置')!.trigger('click')
    expect(w.get('#workspace-meta-name').element).toHaveProperty('value', 'Studio')
    expect(w.get('#setting-max_concurrent_runs').element).toHaveProperty('value', '4')

    let resolveSave!: (value: unknown) => void
    apiMocks.updateWorkspace.mockReturnValueOnce(new Promise((resolve) => { resolveSave = resolve }))
    await w.get('#workspace-meta-name').setValue('Late')
    await saveButton(w).trigger('click')
    w.unmount()
    resolveSave({ id: 'ws-1', name: 'Late', description: 'Our studio', unknownModelDisplayName: '' })
    await flushPromises()
    expect(workspaceState.workspace.value.name).toBe('Studio')
  })

  it('does not show a saveable workspace form when workspace context failed', async () => {
    workspaceState.workspace.value = null
    workspaceState.error.value = 'workspace unavailable'
    const w = mountSettings()
    await flushPromises()
    expect(w.find('[data-testid="settings-failed"]').exists()).toBe(true)
    expect(w.find('#workspace-meta-name').exists()).toBe(false)
    expect(w.find('[data-testid="settings-save-actions"]').exists()).toBe(false)
    w.unmount()
  })
})

describe('SettingsView execution mode', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.listSandboxes.mockResolvedValue([])
    apiMocks.dashboard.mockResolvedValue({ running: 0 })
  })

  it('shows the sandbox pill from the image and keeps memory settings', async () => {
    apiMocks.getSettings.mockResolvedValue({ ...SETTINGS, execution_mode: 'sandbox' })
    const w = mountSettings()
    await flushPromises()
    expect(w.get('[data-testid="execution-mode-value"]').text()).toBe('沙箱')
    expect(w.get('[data-testid="execution-mode"]').text()).toContain('来自当前镜像')
    expect(w.find('#setting-sandbox_memory_mb').exists()).toBe(true)
    expect(w.find('[data-testid="execution-mode"] input, [data-testid="execution-mode"] [role="switch"]').exists()).toBe(false)
    expect(w.find('[data-testid="execution-mode-banner"]').exists()).toBe(false)
    w.unmount()
  })

  it('shows a read-only worktree mode and does not save it', async () => {
    const settings = { ...SETTINGS, execution_mode: 'worktree' as const }
    apiMocks.getSettings.mockResolvedValue(settings)
    apiMocks.updateSettings.mockResolvedValue(settings)
    const w = mountSettings()
    await flushPromises()
    expect(w.get('[data-testid="execution-mode-value"]').text()).toBe('工作树')
    expect(w.get('[data-testid="execution-mode"]').text()).toContain('没有另一层容器边界')
    expect(w.get('[data-testid="execution-mode"]').text()).toContain('来自当前镜像')
    expect(w.find('#setting-sandbox_memory_mb').exists()).toBe(false)
    expect(w.find('#setting-run_sandbox_ttl_minutes').exists()).toBe(false)
    expect(w.find('#setting-max_test_sandboxes').exists()).toBe(false)
    expect(w.get('[data-testid="execution-mode-banner"]').text()).toContain('没有 dockerd、sshd 和 code-server')
    expect(w.get('[data-testid="execution-mode-banner"]').text()).toContain('没有模式开关')
    expect(w.find('[data-testid="execution-mode"] input, [data-testid="execution-mode"] [role="switch"]').exists()).toBe(false)

    await w.get('#setting-max_concurrent_runs').setValue('6')
    await w.findAll('button').find((button) => button.text().includes('保存'))!.trigger('click')
    await flushPromises()
    const patch = apiMocks.updateSettings.mock.calls[0]![0]
    expect(patch).toMatchObject({ max_concurrent_runs: 6 })
    expect(patch).not.toHaveProperty('execution_mode')
    expect(w.get('[data-testid="execution-mode-value"]').text()).toBe('工作树')
    w.unmount()
  })

  it('does not put an execution-mode switch on home, new session, or start run', () => {
    const dir = dirname(fileURLToPath(import.meta.url))
    for (const rel of [
      '../components/dashboard/HomeModeSwitch.vue',
      '../components/solo/HomeSoloPanel.vue',
      '../components/workflow/RunLaunchModal.vue',
      'SoloView.vue',
    ]) {
      const src = readFileSync(join(dir, rel), 'utf8')
      expect(src, rel).not.toContain('execution_mode')
      expect(src, rel).not.toContain('执行模式')
    }
  })
})
