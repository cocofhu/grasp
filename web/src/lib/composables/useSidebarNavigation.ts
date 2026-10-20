import { computed, inject, onScopeDispose, provide, ref, watch, type ComputedRef, type InjectionKey } from 'vue'
import { isNavigationFailure, NavigationFailureType, useRoute, useRouter, type RouteLocationRaw } from 'vue-router'
import { isSettingsChrome, settingsNavItems } from '@/data/settingsNav'
import { useAuth } from './useAuth'

type NavigationMode = 'workspace' | 'settings'
interface SidebarNavigation {
  settingsChrome: ComputedRef<boolean>
  requestMode: (event: MouseEvent | undefined, to: RouteLocationRaw, mode: NavigationMode) => void
}
const sidebarNavigationKey: InjectionKey<SidebarNavigation> = Symbol('sidebar-navigation')

/** One mode for the shell: closing/recreating the mobile drawer does not reset it. */
export function provideSidebarNavigation(): SidebarNavigation {
  const route = useRoute()
  const router = useRouter()
  const { user } = useAuth()
  const mode = ref<NavigationMode>(isSettingsChrome(route.path) ? 'settings' : 'workspace')
  let requested: { path: string; mode: NavigationMode } | undefined
  const settingsChrome = computed(() => mode.value === 'settings' && route.meta.full !== true)

  const removeHook = router.afterEach((to, _from, failure) => {
    const intent = requested
    if (intent?.path === to.fullPath) requested = undefined
    if (failure && !isNavigationFailure(failure, NavigationFailureType.duplicated)) return
    if (intent?.path === to.fullPath) {
      mode.value = intent.mode
    } else if (!failure) {
      requested = undefined
      if (isSettingsChrome(to.path)) mode.value = 'settings'
      else if (to.meta.full !== true && !settingsNavItems.some((item) => to.path === item.to || to.path.startsWith(`${item.to}/`))) mode.value = 'workspace'
    }
  })
  onScopeDispose(removeHook)
  watch(() => user.value?.username, () => {
    requested = undefined
    mode.value = user.value && isSettingsChrome(route.path) ? 'settings' : 'workspace'
  })

  const navigation: SidebarNavigation = {
    settingsChrome,
    requestMode(event, to, targetMode) {
      // New-tab clicks keep the current tab's navigation context.
      if (!event || event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
      if (event.currentTarget instanceof HTMLAnchorElement && event.currentTarget.target === '_blank') return
      requested = { path: router.resolve(to).fullPath, mode: targetMode }
    },
  }
  provide(sidebarNavigationKey, navigation)
  return navigation
}

export function useSidebarNavigation(): SidebarNavigation {
  const route = useRoute()
  return inject(sidebarNavigationKey, {
    settingsChrome: computed(() => isSettingsChrome(route.path, route.meta.full === true)),
    requestMode: () => undefined,
  })
}
