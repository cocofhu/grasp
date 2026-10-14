// @vitest-environment happy-dom
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type { SandboxView } from '@/lib/api/api'

const extendSandboxLease = vi.fn()
vi.mock('@/lib/api/api', () => ({ api: { extendSandboxLease: (...a: unknown[]) => extendSandboxLease(...a) } }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => ({ success: vi.fn(), error: vi.fn() }) }))

import SandboxRetention from './SandboxRetention.vue'
import { formatRemaining } from './retention'

const i18n = createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false, messages: { en: {} } })

function sandbox(over: Partial<SandboxView> = {}): SandboxView {
  return {
    id: 7, name: 'sb', profile: 'impl', purpose: 'run', status: 'running', runId: 'run-1',
    createdAt: '2026-10-15T00:00:00Z', updatedAt: '2026-10-15T00:00:00Z',
    containerStatus: 'running', busy: false, connected: false, hasCodeServer: true, hasAcp: true,
    destroyAt: new Date(Date.now() + 90 * 60_000).toISOString(),
    leases: [{ kind: 'debug', until: new Date(Date.now() + 30 * 60_000).toISOString(), createdAt: '2026-10-15T00:00:00Z' }, { kind: 'preview', createdAt: '2026-10-15T00:00:00Z' }],
    ...over,
  }
}

describe('formatRemaining', () => {
  it('formats hours, minutes and seconds', () => {
    expect(formatRemaining(0, 'soon')).toBe('soon')
    expect(formatRemaining(45_000, 'soon')).toBe('45s')
    expect(formatRemaining(12 * 60_000 + 5_000, 'soon')).toBe('12m 5s')
    expect(formatRemaining(65 * 60_000, 'soon')).toBe('1h 05m')
  })
})

describe('SandboxRetention', () => {
  beforeEach(() => extendSandboxLease.mockReset())

  it('renders only for retained run sandboxes', () => {
    expect(mount(SandboxRetention, { props: { sandbox: sandbox({ leases: [] }) }, global: { plugins: [i18n] } }).find('[data-testid="sandbox-retention"]').exists()).toBe(false)
    expect(mount(SandboxRetention, { props: { sandbox: sandbox({ purpose: 'test' }) }, global: { plugins: [i18n] } }).find('[data-testid="sandbox-retention"]').exists()).toBe(false)
    const w = mount(SandboxRetention, { props: { sandbox: sandbox() }, global: { plugins: [i18n] } })
    expect(w.findAll('[data-testid="sandbox-retention-lease"]')).toHaveLength(2)
    expect(w.get('[data-testid="sandbox-retention-remaining"]').text()).toMatch(/^1h (29|30)m$/)
  })

  it('extends and emits the refreshed view', async () => {
    const fresh = sandbox({ leases: [{ kind: 'manual', createdAt: '2026-10-15T00:00:00Z' }] })
    extendSandboxLease.mockResolvedValue(fresh)
    const w = mount(SandboxRetention, { props: { sandbox: sandbox() }, global: { plugins: [i18n] } })
    await w.get('[data-testid="sandbox-retention-extend-run"]').trigger('click')
    await flushPromises()
    expect(extendSandboxLease).toHaveBeenCalledWith(7, { until: 'run_end' })
    expect(w.emitted('updated')?.[0]?.[0]).toBe(fresh)
    await w.get('[data-testid="sandbox-retention-extend-30"]').trigger('click')
    expect(extendSandboxLease).toHaveBeenLastCalledWith(7, { minutes: 30 })
  })
})
