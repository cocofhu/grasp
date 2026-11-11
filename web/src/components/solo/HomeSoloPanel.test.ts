// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const store = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports -- vi.hoisted runs before ESM imports
  const { ref } = require('vue') as typeof import('vue')
  const now = new Date().toISOString()
  const sessions = [
    { id: 's-run', title: '进行中的会话', agentName: 'dev', status: 'stopped', busy: false, waiting: 0, createdAt: now, lastActiveAt: now },
    { id: 's-end', title: '已结束的会话', agentName: 'dev', status: 'closed', busy: false, waiting: 0, createdAt: now, lastActiveAt: now },
  ]
  return { loaded: ref(true), active: ref(sessions.slice(0, 1)), sessions: ref(sessions), maxSandboxes: ref(5), usedSandboxes: ref(0) }
})

vi.mock('@/lib/solo/useSoloSessions', async () => {
  const actual = await vi.importActual<typeof import('@/lib/solo/useSoloSessions')>('@/lib/solo/useSoloSessions')
  return { ...actual, useSoloSessions: () => store }
})

import HomeSoloPanel from './HomeSoloPanel.vue'

afterEach(() => { document.body.innerHTML = '' })

async function mountPanel() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/dashboard', component: { template: '<div />' } },
      { path: '/solo/:id', component: { template: '<div />' } },
    ],
  })
  await router.push('/dashboard')
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, pages: pages.pages } } })
  const wrapper = mount(HomeSoloPanel, { attachTo: document.body, global: { plugins: [router, i18n] } })
  await flushPromises()
  return { router, wrapper }
}

describe('HomeSoloPanel', () => {
  it('opens all sessions in a dialog on the home page and closes it when a session is picked', async () => {
    const { router, wrapper } = await mountPanel()
    expect(document.querySelector('[data-testid="solo-list"]')).toBeNull()

    await wrapper.find('[data-testid="home-solo-view-all"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/dashboard')
    expect(document.querySelector('[role="dialog"] [data-testid="solo-row-s-run"]')).not.toBeNull()

    ;(document.querySelector('[data-testid="solo-row-s-run"]') as HTMLElement).click()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/solo/s-run')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
  })
})
