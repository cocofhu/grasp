// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'

const store = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports -- vi.hoisted runs before ESM imports
  const { ref } = require('vue') as typeof import('vue')
  const now = new Date().toISOString()
  const sessions = [
    { id: 's-run', title: '进行中的会话', agentName: 'dev', status: 'stopped', busy: false, waiting: 0, createdAt: now, lastActiveAt: now },
    { id: 's-end', title: '已结束的会话', agentName: 'dev', status: 'closed', busy: false, waiting: 0, createdAt: now, lastActiveAt: now },
  ]
  return {
    loaded: ref(true),
    active: ref(sessions.slice(0, 1)),
    sessions: ref(sessions),
    maxSandboxes: ref(5),
    refresh: vi.fn(async () => {}),
  }
})

vi.mock('@/lib/solo/useSoloSessions', async () => {
  const actual = await vi.importActual<typeof import('@/lib/solo/useSoloSessions')>('@/lib/solo/useSoloSessions')
  return { ...actual, useSoloSessions: () => store }
})
vi.mock('@/components/solo/SoloSessionPane.vue', () => ({ default: { template: '<div data-testid="pane" />' } }))
vi.mock('@/components/solo/SoloCreateModal.vue', () => ({ default: { template: '<div />' } }))

import SoloView from './SoloView.vue'

async function mountAt(path: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/solo', component: SoloView },
      { path: '/solo/:id', component: SoloView },
      { path: '/dashboard', component: { template: '<div />' } },
    ],
  })
  await router.push(path)
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { pages: pages.pages } } })
  const wrapper = mount(SoloView, { global: { plugins: [router, i18n] } })
  await flushPromises()
  return { router, wrapper }
}

describe('SoloView', () => {
  it('lists sessions on /solo instead of leaving a blank page', async () => {
    const { router, wrapper } = await mountAt('/solo')
    expect(router.currentRoute.value.path).toBe('/solo')
    expect(wrapper.find('[data-testid="solo-row-s-run"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="solo-row-s-end"]').exists()).toBe(false)

    await wrapper.find('[data-testid="solo-filter-all"]').trigger('click')
    expect(wrapper.find('[data-testid="solo-row-s-end"]').exists()).toBe(true)
  })

  it('opens the conversation for /solo/:id', async () => {
    const { wrapper } = await mountAt('/solo/s-run')
    expect(wrapper.find('[data-testid="pane"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="solo-list"]').exists()).toBe(false)
  })
})
