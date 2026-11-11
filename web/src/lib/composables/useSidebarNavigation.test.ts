// @vitest-environment happy-dom
import { defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it } from 'vitest'
import { provideSidebarNavigation, useSidebarNavigation } from './useSidebarNavigation'
import { useAuth } from './useAuth'

const mounted: ReturnType<typeof mount>[] = []
afterEach(() => { mounted.splice(0).forEach((wrapper) => wrapper.unmount()) })
async function setup(path = '/settings') {
  useAuth().setUser({ username: 'navigation-user', expiresAt: '2099-01-01T00:00:00Z' })
  const router = createRouter({ history: createMemoryHistory(), routes: [
    ...['/settings', '/settings/credentials', '/settings/audit', '/board', '/workflows', '/requirements', '/assistant', '/dashboard', '/runs', '/stats'].map((path) => ({ path, component: { render: () => null } })),
    { path: '/workflows/:id/edit', component: { render: () => null }, meta: { full: true } },
  ] })
  await router.push(path)
  const showChild = ref(true)
  let navigation!: ReturnType<typeof provideSidebarNavigation>
  const Child = defineComponent({ setup() { const child = useSidebarNavigation(); return () => h('span', { 'data-mode': child.settingsChrome.value }) } })
  const Root = defineComponent({ setup() { navigation = provideSidebarNavigation(); return () => showChild.value ? h(Child) : null } })
  const wrapper = mount(Root, { global: { plugins: [router] } })
  mounted.push(wrapper)
  return { router, navigation, wrapper, showChild }
}

describe('sidebar navigation context', () => {
  it('keeps settings across shared tools, drawer remounts and full editor round trips', async () => {
    const { router, navigation, wrapper, showChild } = await setup()
    expect(navigation.settingsToggleTo.value).toBe('/dashboard')
    for (const path of ['/board', '/workflows', '/requirements', '/assistant', '/settings/credentials']) {
      await router.push(path)
      expect(navigation.settingsChrome.value).toBe(true)
    }
    showChild.value = false; await nextTick(); showChild.value = true; await nextTick()
    expect(wrapper.get('span').attributes('data-mode')).toBe('true')
    await router.push('/workflows/wf/edit')
    expect(navigation.settingsChrome.value).toBe(false)
    await router.push('/workflows')
    expect(navigation.settingsChrome.value).toBe(true)
    expect(navigation.settingsToggleTo.value).toBe('/dashboard')
  })
  it('returns from settings to the last workspace address including filters and hash', async () => {
    const workspacePath = '/runs?status=failed,cancelled#run-list'
    const { router, navigation } = await setup(workspacePath)
    expect(navigation.settingsToggleTo.value).toBe('/settings')
    navigation.requestMode(new MouseEvent('click'), navigation.settingsToggleTo.value, 'settings')
    await router.push(navigation.settingsToggleTo.value)
    expect(navigation.settingsChrome.value).toBe(true)
    expect(navigation.settingsToggleTo.value).toBe(workspacePath)
    await router.push('/settings/credentials')
    expect(navigation.settingsToggleTo.value).toBe(workspacePath)
    navigation.requestMode(new MouseEvent('click'), navigation.settingsToggleTo.value, 'workspace')
    await router.push(navigation.settingsToggleTo.value)
    expect(router.currentRoute.value.fullPath).toBe(workspacePath)
    expect(navigation.settingsChrome.value).toBe(false)
    expect(navigation.settingsToggleTo.value).toBe('/settings')
    await router.push('/stats?window=7d')
    await router.push('/settings')
    expect(navigation.settingsToggleTo.value).toBe('/stats?window=7d')
  })
  it('returns to the workspace even when a shared settings tool already has the same address', async () => {
    const { router, navigation } = await setup('/workflows')
    await router.push('/settings')
    await router.push('/workflows')
    expect(navigation.settingsChrome.value).toBe(true)
    expect(navigation.settingsToggleTo.value).toBe('/workflows')
    navigation.requestMode(new MouseEvent('click'), navigation.settingsToggleTo.value, 'workspace')
    await router.push(navigation.settingsToggleTo.value)
    expect(navigation.settingsChrome.value).toBe(false)
    expect(navigation.settingsToggleTo.value).toBe('/settings')
  })
  it('does not replace the workspace return address with a full editor opened from settings', async () => {
    const { router, navigation } = await setup('/runs?status=completed')
    await router.push('/settings')
    await router.push('/workflows/wf/edit')
    await router.push('/workflows')
    expect(navigation.settingsChrome.value).toBe(true)
    expect(navigation.settingsToggleTo.value).toBe('/runs?status=completed')
  })
  it('leaves direct workflow visits in workspace mode and explicitly returns home', async () => {
    const { router, navigation } = await setup('/workflows')
    expect(navigation.settingsChrome.value).toBe(false)
    await router.push('/settings')
    expect(navigation.settingsChrome.value).toBe(true)
    navigation.requestMode(new MouseEvent('click'), '/dashboard', 'workspace')
    await router.push('/dashboard')
    expect(navigation.settingsChrome.value).toBe(false)
  })
  it('honors same-address rail clicks but preserves mode on a blocked navigation or new-tab click', async () => {
    const { router, navigation } = await setup()
    await router.push('/workflows')
    navigation.requestMode(new MouseEvent('click', { ctrlKey: true }), '/workflows', 'workspace')
    await router.push('/workflows')
    expect(navigation.settingsChrome.value).toBe(true)
    const stop = router.beforeEach((to) => to.path === '/dashboard' ? false : undefined)
    navigation.requestMode(new MouseEvent('click'), '/dashboard', 'workspace')
    await router.push('/dashboard')
    expect(navigation.settingsChrome.value).toBe(true)
    stop()
    navigation.requestMode(new MouseEvent('click'), '/workflows', 'workspace')
    await router.push('/workflows')
    expect(navigation.settingsChrome.value).toBe(false)
  })
  it('retains the workspace return address when its navigation is blocked', async () => {
    const { router, navigation } = await setup('/runs?status=completed')
    await router.push('/settings')
    const stop = router.beforeEach((to) => to.path === '/runs' ? false : undefined)
    navigation.requestMode(new MouseEvent('click'), navigation.settingsToggleTo.value, 'workspace')
    await router.push(navigation.settingsToggleTo.value)
    expect(router.currentRoute.value.path).toBe('/settings')
    expect(navigation.settingsChrome.value).toBe(true)
    expect(navigation.settingsToggleTo.value).toBe('/runs?status=completed')
    stop()
    navigation.requestMode(new MouseEvent('click'), navigation.settingsToggleTo.value, 'workspace')
    await router.push(navigation.settingsToggleTo.value)
    expect(router.currentRoute.value.fullPath).toBe('/runs?status=completed')
    expect(navigation.settingsChrome.value).toBe(false)
  })
  it.each([
    { ctrlKey: true },
    { metaKey: true },
    { shiftKey: true },
    { altKey: true },
    { button: 1 },
  ])('does not toggle shared-route context for a modified click: %j', async (options) => {
    const { router, navigation } = await setup('/workflows')
    await router.push('/settings')
    await router.push('/workflows')
    navigation.requestMode(new MouseEvent('click', options), navigation.settingsToggleTo.value, 'workspace')
    await router.push('/workflows')
    expect(navigation.settingsChrome.value).toBe(true)
    expect(navigation.settingsToggleTo.value).toBe('/workflows')
  })
  it('keeps /stats in workspace mode on a direct visit, a later jump, and an account switch', async () => {
    const { router, navigation } = await setup('/stats')
    expect(navigation.settingsChrome.value).toBe(false)
    await router.push('/settings')
    expect(navigation.settingsChrome.value).toBe(true)
    await router.push('/stats?window=7d')
    expect(navigation.settingsChrome.value).toBe(false)
    navigation.requestMode(new MouseEvent('click'), '/settings', 'settings')
    await router.push('/settings/audit')
    expect(navigation.settingsChrome.value).toBe(true)
    navigation.requestMode(new MouseEvent('click'), '/stats', 'workspace')
    await router.push('/stats')
    expect(navigation.settingsChrome.value).toBe(false)
    useAuth().setUser({ username: 'stats-user', expiresAt: '2099-01-01T00:00:00Z' })
    await nextTick()
    expect(navigation.settingsChrome.value).toBe(false)
  })
  it('resets navigation context when accounts change', async () => {
    const { router, navigation } = await setup()
    await router.push('/workflows')
    useAuth().setUser({ username: 'another-user', expiresAt: '2099-01-01T00:00:00Z' })
    await nextTick()
    expect(navigation.settingsChrome.value).toBe(false)
  })
  it('clears the remembered workspace address when accounts change in settings', async () => {
    const { router, navigation } = await setup('/runs?status=completed')
    await router.push('/settings')
    expect(navigation.settingsToggleTo.value).toBe('/runs?status=completed')
    useAuth().setUser({ username: 'another-user', expiresAt: '2099-01-01T00:00:00Z' })
    await nextTick()
    expect(navigation.settingsChrome.value).toBe(true)
    expect(navigation.settingsToggleTo.value).toBe('/dashboard')
  })
})
