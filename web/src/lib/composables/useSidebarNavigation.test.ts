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
    ...['/settings', '/settings/credentials', '/board', '/workflows', '/requirements', '/assistant', '/dashboard', '/runs'].map((path) => ({ path, component: { render: () => null } })),
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
  it('resets navigation context when accounts change', async () => {
    const { router, navigation } = await setup()
    await router.push('/workflows')
    useAuth().setUser({ username: 'another-user', expiresAt: '2099-01-01T00:00:00Z' })
    await nextTick()
    expect(navigation.settingsChrome.value).toBe(false)
  })
})
