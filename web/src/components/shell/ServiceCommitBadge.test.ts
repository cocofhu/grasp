// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { serviceCommit } from '@/lib/composables/useServiceCommit'
import ServiceCommitBadge from './ServiceCommitBadge.vue'

const route = { name: 'dashboard' as string }

vi.mock('vue-router', () => ({
  useRoute: () => route,
}))

function mountBadge() {
  return mount(ServiceCommitBadge)
}

describe('ServiceCommitBadge', () => {
  beforeEach(() => {
    route.name = 'dashboard'
    serviceCommit.value = ''
  })

  afterEach(() => {
    serviceCommit.value = ''
  })

  it('shows a 7-char SHA on dashboard with text-txt3 and pointer-events-none', () => {
    serviceCommit.value = 'b01bb39'
    const wrapper = mountBadge()
    const el = wrapper.get('[data-testid="service-commit-badge"]')
    expect(el.text()).toBe('b01bb39')
    expect(el.text()).toHaveLength(7)
    expect(el.classes()).toEqual(
      expect.arrayContaining([
        'text-txt3',
        'pointer-events-none',
        'font-mono',
        'z-10',
        'right-[14px]',
        'bottom-[calc(10px+env(safe-area-inset-bottom,0px))]',
      ]),
    )
    expect(wrapper.text()).not.toMatch(/unknown|N\/A|—|服务程序|commit:/i)
    wrapper.unmount()
  })

  it('hides when the short SHA is empty', () => {
    serviceCommit.value = ''
    const wrapper = mountBadge()
    expect(wrapper.find('[data-testid="service-commit-badge"]').exists()).toBe(false)
    expect(wrapper.text()).toBe('')
    wrapper.unmount()
  })

  it('does not render on non-dashboard routes', () => {
    serviceCommit.value = 'b01bb39'
    for (const name of ['workflows', 'runs', 'login', 'public-gate-approval', 'settings']) {
      route.name = name
      const wrapper = mountBadge()
      expect(wrapper.find('[data-testid="service-commit-badge"]').exists()).toBe(false)
      wrapper.unmount()
    }
  })
})

describe('AppShell hosts the badge outside the scroll area', () => {
  it('is a sibling of scroll-area inside relative main (g2.2 / g2.3)', async () => {
    const { readFileSync } = await import('node:fs')
    const { dirname, join } = await import('node:path')
    const { fileURLToPath } = await import('node:url')
    const dir = dirname(fileURLToPath(import.meta.url))
    const shell = readFileSync(join(dir, 'AppShell.vue'), 'utf8')
    expect(shell).toMatch(/<main\s+class="relative/)
    expect(shell).toMatch(/class="scroll-area safe-area-bottom/)
    expect(shell).toMatch(/<ServiceCommitBadge\s*\/>/)
    const badgeIdx = shell.indexOf('<ServiceCommitBadge')
    const scrollIdx = shell.indexOf('class="scroll-area')
    const mainEnd = shell.indexOf('</main>')
    expect(scrollIdx).toBeGreaterThan(-1)
    expect(badgeIdx).toBeGreaterThan(scrollIdx)
    expect(badgeIdx).toBeLessThan(mainEnd)
    expect(shell).not.toMatch(/VITE_GIT_COMMIT/)
    expect(shell).not.toMatch(/unknown|N\/A/)
  })
})

describe('ServiceCommitBadge is unused on bare layouts', () => {
  it('App.vue keeps login/public routes outside AppShell', async () => {
    const { readFileSync } = await import('node:fs')
    const { dirname, join } = await import('node:path')
    const { fileURLToPath } = await import('node:url')
    const dir = dirname(fileURLToPath(import.meta.url))
    const app = readFileSync(join(dir, '../../App.vue'), 'utf8')
    expect(app).toMatch(/<AppShell v-if="!bareLayout">/)
    expect(app).toMatch(/<router-view v-else \/>/)
    expect(app).toMatch(/<ToastHost \/>/)
  })
})
