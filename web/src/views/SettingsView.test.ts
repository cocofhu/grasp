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

const apiMocks = vi.hoisted(() => ({
  getSettings: vi.fn(),
  updateSettings: vi.fn(),
  listSandboxes: vi.fn(),
  dashboard: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getSettings: apiMocks.getSettings,
      updateSettings: apiMocks.updateSettings,
      listSandboxes: apiMocks.listSandboxes,
      dashboard: apiMocks.dashboard,
    },
  }
})

vi.mock('@/lib/composables/useAuth', async () => {
  const { ref } = await import('vue')
  return {
    useAuth: () => ({ user: ref({ username: 'admin', isAdmin: true }) }),
  }
})

import SettingsView from './SettingsView.vue'

const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'SettingsView.vue'), 'utf8')

const SETTINGS = {
  brand: { product_name: 'Acme Flow', home_subtitle: 'Clarify together' },
  items: [
    { key: 'max_concurrent_runs', value: 4, min: 1, source: 'ui', locked: false },
    { key: 'run_sandbox_ttl_minutes', value: 30, min: 1, source: 'ui', locked: false },
    { key: 'test_sandbox_ttl_minutes', value: 15, min: 1, source: 'ui', locked: false },
    { key: 'max_test_sandboxes', value: 3, min: 1, source: 'ui', locked: false },
    { key: 'sandbox_memory_mb', value: 8192, min: 1024, source: 'config', locked: false },
    { key: 'agent_idle_timeout_minutes', value: 20, min: 2, max: 120, source: 'config', locked: false },
  ],
}

function mountSettings() {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
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

describe('SettingsView loading source lock', () => {
  it('first load uses grouped form skeleton, not EmptyState loadingTitle', () => {
    expect(src).toMatch(/data-testid="settings-form-skeleton"/)
    expect(src).not.toMatch(/EmptyState/)
    expect(src).not.toMatch(/pages\.settings\.loadingTitle/)
    expect(src).toMatch(/admin-list-thin-bar bg-accent/)
    expect(src).toMatch(/opacity-\[0\.55\]/)
    expect(src).toMatch(/flex-col items-stretch gap-3 md:flex-row md:items-end md:justify-between/)
    expect(src).toMatch(/min-h-11 w-full md:min-h-0 md:w-auto/)
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
    await w.get('[data-testid="brand-product-name"]').setValue('New Name')
    await w.get('[data-testid="brand-home-subtitle"]').setValue('New subtitle')
    const saveButton = w.findAll('button').find((button) => button.text().includes('保存'))
    await saveButton!.trigger('click')
    await flushPromises()
    expect(apiMocks.updateSettings).toHaveBeenCalledWith(expect.objectContaining({
      brand_product_name: 'New Name',
      brand_home_subtitle: 'New subtitle',
      max_concurrent_runs: 4,
    }))
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
    expect(w.text()).toContain('max_concurrent_runs')
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
    expect(router.currentRoute.value.query.integrations).toBe('1')
    w.unmount()
  })
})
