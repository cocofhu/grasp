<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppSidebarNav from './AppSidebarNav.vue'
import BrandLogo from './BrandLogo.vue'
import ShellChromeControls from './ShellChromeControls.vue'
import Icon from '../ui/Icon.vue'
import { authApi } from '@/lib/api/api'
import { useAuth } from '@/lib/composables/useAuth'
import {
  focusDesktopNavControl,
  hideDesktopSidebar,
  sidebarHidden,
} from '@/lib/shared/sidebarHidden'

const { t } = useI18n()
const router = useRouter()
const { user, clearUser } = useAuth()

const initials = computed(() => {
  const name = user.value?.username || '?'
  return name.slice(0, 2).toUpperCase()
})

async function logout() {
  try {
    await authApi.logout()
  } catch {
    // ignore — cookie cleared server-side when possible
  }
  clearUser()
  await router.push('/login')
}

async function onHideNav() {
  hideDesktopSidebar()
  await focusDesktopNavControl('show')
}
</script>

<template>
  <aside
    id="app-desktop-sidebar"
    class="app-desktop-sidebar hidden h-full min-w-0 shrink-0 flex-col md:flex"
    :class="
      sidebarHidden
        ? 'w-0 border-r-0 overflow-hidden'
        : 'w-[232px] overflow-visible'
    "
    data-testid="app-desktop-sidebar"
    :data-floating="sidebarHidden ? 'false' : 'true'"
    :aria-hidden="sidebarHidden ? 'true' : undefined"
    :inert="sidebarHidden"
  >
    <div
      class="app-sidebar-card flex h-full w-[232px] min-w-[232px] flex-col bg-surface"
      data-testid="app-sidebar-card"
    >
      <div
        class="app-sidebar-brand-row flex h-14 items-center justify-between gap-2 pl-3.5 pr-2"
        data-testid="sidebar-brand-row"
      >
        <BrandLogo size="md" align="start" :show-tagline="false" use-custom-brand />
        <button
          type="button"
          class="flex h-11 w-11 shrink-0 items-center justify-center rounded-md text-txt3 hover:text-txt2"
          data-testid="desktop-nav-hide"
          :aria-label="t('shell.aria.hideNav')"
          :title="t('shell.aria.hideNav')"
          aria-expanded="true"
          aria-controls="app-desktop-sidebar"
          @click="onHideNav"
        >
          <Icon name="panel-left" :size="18" />
        </button>
      </div>

      <AppSidebarNav />

      <div class="mt-auto flex flex-col gap-2.5 border-t border-line p-3">
        <ShellChromeControls layout="sidebar" />
        <div class="flex items-center gap-2 px-0.5">
          <div
            class="force-radius-full flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-accent-dim text-[11px] font-semibold text-accent-2"
            data-testid="sidebar-user-avatar"
          >{{ initials }}</div>
          <div class="min-w-0 flex-1 truncate leading-tight">
            <div class="truncate text-[13px] font-medium text-txt" data-testid="sidebar-user-name">{{ user?.username || '—' }}</div>
          </div>
          <button
            type="button"
            class="shrink-0 border-0 bg-transparent px-2 py-1.5 text-xs text-txt3 transition hover:bg-err/10 hover:text-err"
            data-testid="sidebar-logout"
            @click="logout"
          >
            {{ t('shell.logout') }}
          </button>
        </div>
      </div>
    </div>
  </aside>
</template>
