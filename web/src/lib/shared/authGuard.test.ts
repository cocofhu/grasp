// @vitest-environment happy-dom
import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { getAuthState, useAuth } from '../composables/useAuth'

const me = vi.fn()

vi.mock('@/lib/api/api', () => ({
  authApi: {
    me: (...args: unknown[]) => me(...args),
  },
}))

import { installAuthGuard, isFramed } from './authGuard'

function resetAuth() {
  const s = getAuthState()
  s.user = null
  s.ready = false
}

describe('installAuthGuard', () => {
  beforeEach(() => {
    me.mockReset()
    resetAuth()
  })

  it('does not block navigation while /me is in flight', async () => {
    me.mockReturnValue(new Promise(() => {}))
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/login', name: 'login', component: { template: '<div />' }, meta: { public: true, bare: true } },
        { path: '/dashboard', name: 'dashboard', component: { template: '<div />' } },
      ],
    })
    installAuthGuard(router)
    await router.push('/dashboard')
    expect(router.currentRoute.value.path).toBe('/dashboard')
    expect(getAuthState().ready).toBe(false)
    expect(me).toHaveBeenCalled()
  })

  it('redirects to login with redirect query when /me fails', async () => {
    me.mockRejectedValue(new Error('unauthorized'))
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/login', name: 'login', component: defineComponent({ template: '<div />' }), meta: { public: true, bare: true } },
        { path: '/runs', name: 'runs', component: defineComponent({ template: '<div />' }) },
      ],
    })
    installAuthGuard(router)
    await router.push('/runs')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/runs')
    expect(getAuthState().ready).toBe(true)
  })

  it('marks auth ready when cached user exists', async () => {
    useAuth().setUser({ username: 'u', expiresAt: 't' })
    me.mockRejectedValue(new Error('should not call'))
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/dashboard', name: 'dashboard', component: { template: '<div />' } }],
    })
    installAuthGuard(router)
    await router.push('/dashboard')
    expect(me).not.toHaveBeenCalled()
    expect(getAuthState().ready).toBe(true)
    expect(router.currentRoute.value.path).toBe('/dashboard')
  })

  it('login stays non-blocking and uses string authRedirectPath', async () => {
    me.mockReturnValue(new Promise(() => {}))
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/login', name: 'login', component: { template: '<div />' }, meta: { public: true, bare: true } },
        { path: '/', name: 'home', component: { template: '<div />' } },
      ],
    })
    installAuthGuard(router)
    await router.push('/login')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(getAuthState().ready).toBe(false)
  })
})

describe('isFramed', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    document.head.querySelector('meta[name="grasp-workspace-framing"]')?.remove()
  })

  it('limits a framed document to preview routes unless the server allows workspace framing', () => {
    expect(isFramed()).toBe(false)
    vi.spyOn(window, 'top', 'get').mockReturnValue({} as Window)
    expect(isFramed()).toBe(true)
    const meta = document.createElement('meta')
    meta.name = 'grasp-workspace-framing'
    meta.content = 'allowed'
    document.head.appendChild(meta)
    expect(isFramed()).toBe(false)
  })

  it('blocks workspace navigation only in a restricted frame', async () => {
    vi.spyOn(window, 'top', 'get').mockReturnValue({} as Window)
    me.mockResolvedValue({ username: 'u', expires_at: '2099-01-01T00:00:00Z', is_admin: false })
    const routes = [
      { path: '/dashboard', name: 'dashboard', component: defineComponent({ template: '<div />' }) },
      { path: '/embed/chat', name: 'embed', component: defineComponent({ template: '<div />' }), meta: { public: true, bare: true, previewFrame: true } },
    ]
    const restricted = createRouter({ history: createMemoryHistory(), routes })
    installAuthGuard(restricted)
    await restricted.push('/dashboard')
    expect(restricted.currentRoute.value.path).toBe('/')
    await restricted.push('/embed/chat')
    expect(restricted.currentRoute.value.path).toBe('/embed/chat')

    const meta = document.createElement('meta')
    meta.name = 'grasp-workspace-framing'
    meta.content = 'allowed'
    document.head.appendChild(meta)
    const allowed = createRouter({ history: createMemoryHistory(), routes })
    installAuthGuard(allowed)
    await allowed.push('/dashboard')
    expect(allowed.currentRoute.value.path).toBe('/dashboard')
  })
})
