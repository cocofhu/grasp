<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppSidebarNav from './AppSidebarNav.vue'
import BrandCharacterMark from './BrandCharacterMark.vue'
import BrandLogo from './BrandLogo.vue'
import ShellChromeControls from './ShellChromeControls.vue'
import Icon from '../ui/Icon.vue'
import SidebarAccountMenu from './SidebarAccountMenu.vue'
import { authApi } from '@/lib/api/api'
import { useAuth } from '@/lib/composables/useAuth'
import { clearAllEmbedSessions } from '@/lib/inbox/embedChat'
import { isBoardSectionPath, isHomeSectionPath, sidebarNavGroups } from '@/data/sidebarNav'
import { useSidebarNavigation } from '@/lib/composables/useSidebarNavigation'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import {
  focusDesktopNavControl,
  hideDesktopSidebar,
  sidebarHidden,
} from '@/lib/shared/sidebarHidden'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const { user, clearUser } = useAuth()
const { isMobile } = useBreakpoint()
const { settingsChrome, settingsToggleTo, requestMode } = useSidebarNavigation()
const railItems = sidebarNavGroups[0]?.items.filter((item) => item.to !== '/settings') ?? []

function isRailActive(to: string) {
  if (settingsChrome.value) return false
  if (to === '/dashboard' && isHomeSectionPath(route.path)) return true
  if (to === '/board') return isBoardSectionPath(route.path)
  return route.path === to || route.path.startsWith(to + '/')
}

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
  clearAllEmbedSessions()
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
    class="app-desktop-sidebar hidden h-full min-w-0 shrink-0 md:flex"
    :class="[
      sidebarHidden ? 'w-0 border-r-0 overflow-hidden' : 'w-[360px] overflow-visible',
      { 'is-settings': settingsChrome },
    ]"
    data-testid="app-desktop-sidebar"
    :data-nav-mode="settingsChrome ? 'settings' : 'workspace'"
    :data-floating="sidebarHidden ? 'false' : 'true'"
    :aria-hidden="sidebarHidden ? 'true' : undefined"
    :inert="sidebarHidden"
  >
    <nav class="sidebar-app-rail" :aria-label="t('nav.workspace')" data-testid="sidebar-app-rail">
      <RouterLink
        v-for="item in railItems"
        :key="item.to"
        :to="item.to"
        @click.capture="requestMode($event, item.to, 'workspace')"
        class="sidebar-app-rail__item"
        :class="{ 'is-active': isRailActive(item.to) }"
        :aria-label="t(item.labelKey)"
        :title="t(item.labelKey)"
        :aria-current="isRailActive(item.to) ? 'page' : undefined"
      >
        <Icon :name="item.to === '/dashboard' ? 'home-start' : item.icon" :size="20" />
      </RouterLink>
      <div class="sidebar-app-rail__bottom">
        <RouterLink
          :to="settingsToggleTo"
          @click.capture="requestMode($event, settingsToggleTo, settingsChrome ? 'workspace' : 'settings')"
          class="sidebar-app-rail__item"
          data-testid="sidebar-settings-toggle"
          :class="{ 'is-active': settingsChrome }"
          :aria-label="t(settingsChrome ? 'nav.backWorkspace' : 'nav.settings')"
          :title="t(settingsChrome ? 'nav.backWorkspace' : 'nav.settings')"
          :aria-current="settingsChrome ? 'page' : undefined"
        >
          <Icon name="settings" :size="20" />
        </RouterLink>
        <SidebarAccountMenu
          :username="user?.username || '—'"
          :initials="initials"
          :visible="!sidebarHidden && !isMobile"
          @logout="logout"
        />
      </div>
    </nav>
    <div
      class="app-sidebar-card flex h-full w-[304px] min-w-[304px] flex-col bg-surface"
      data-testid="app-sidebar-card"
    >
      <div
        class="app-sidebar-brand-row flex h-16 shrink-0 items-center justify-between gap-2 pl-3.5 pr-2"
        data-testid="sidebar-brand-row"
      >
        <h2 v-if="settingsChrome" class="sidebar-settings-title" data-testid="sidebar-settings-title">{{ t('nav.settings') }}</h2>
        <div v-else class="flex min-w-0 items-center gap-2" data-testid="sidebar-brand-lockup">
          <BrandCharacterMark :size="28" />
          <BrandLogo size="md" align="start" :show-tagline="false" use-custom-brand />
        </div>
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

      <AppSidebarNav rail :workspace-lists="!isMobile" />

      <div class="sidebar-footer mt-auto flex flex-col gap-1 border-t border-line p-3">
        <ShellChromeControls layout="sidebar" />
      </div>
    </div>
  </aside>
</template>

<style scoped>
.app-desktop-sidebar {
  --sidebar-rail-bg: light-dark(#f6f6f6, rgb(var(--c-base)));
  --sidebar-panel-bg: light-dark(#fff, rgb(var(--c-surface)));
  border-radius: 16px;
  background: var(--sidebar-rail-bg);
}
.sidebar-app-rail {
  display: flex;
  width: 56px;
  min-width: 56px;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding: 10px 0 8px;
}
.sidebar-app-rail__item {
  display: flex;
  width: 44px;
  height: 44px;
  flex: none;
  align-items: center;
  justify-content: center;
  border-radius: 12px;
  color: color-mix(in srgb, rgb(var(--c-txt2)) 75%, rgb(var(--c-txt3)));
  transition: background-color 160ms ease, color 160ms ease;
}
.sidebar-app-rail__item:hover {
  color: rgb(var(--c-txt));
  background: rgb(var(--c-txt) / .045);
}
.sidebar-app-rail__item.is-active {
  color: rgb(var(--c-txt));
  background: light-dark(#eaeaea, rgb(var(--c-txt) / .065));
}
.sidebar-app-rail__bottom {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: auto;
  padding-top: 12px;
}
.sidebar-app-rail a:focus-visible {
  outline: 2px solid rgb(var(--c-accent) / .55);
  outline-offset: -2px;
}
.app-desktop-sidebar .app-sidebar-card {
  background: var(--sidebar-panel-bg);
  box-shadow: 0 0 0 1px rgb(var(--c-line) / .6), 0 2px 8px rgb(0 0 0 / .025);
}
.sidebar-settings-title {
  padding-left: 4px;
  color: rgb(var(--c-txt));
  font-size: 18px;
  font-weight: 600;
  letter-spacing: -.02em;
}
.is-settings .sidebar-footer {
  background: transparent;
}
.app-sidebar-brand-row {
  padding-left: 18px;
  padding-right: 8px;
}
.app-sidebar-brand-row [data-testid="desktop-nav-hide"] {
  border-radius: 10px;
  color: rgb(var(--c-txt3));
}
.app-sidebar-brand-row [data-testid="desktop-nav-hide"]:hover {
  background: rgb(var(--c-txt) / .04);
  color: rgb(var(--c-txt2));
}
.app-sidebar-brand-row :deep(.brand-logo__name) {
  background: none;
  -webkit-text-fill-color: currentColor;
  color: rgb(var(--c-txt));
  animation: none;
  font-weight: 600;
  font-size: 18px;
  letter-spacing: -.02em;
}
.sidebar-footer {
  padding: 10px 12px 12px;
  gap: 4px;
  border-color: rgb(var(--c-line) / .6);
  background: linear-gradient(180deg, transparent, rgb(var(--c-surface) / .55));
}
.sidebar-footer :deep(.shell-chrome-controls) { gap: 4px; }
.sidebar-footer :deep([data-testid="status-metrics-compact"]) {
  min-height: 24px;
  height: 24px;
  border-radius: 7px;
  background: rgb(var(--c-txt) / .025);
}
.sidebar-footer :deep([data-testid="lang-select-trigger"]),
.sidebar-footer :deep([data-testid="shell-theme-toggle"]),
.sidebar-footer :deep([data-testid="run-notifications-bell"]) {
  border-radius: 8px;
  color: rgb(var(--c-txt2));
}
/* Resting secondary text above must not cover the theme button's hover
   and keyboard-focus body color. Language and the bell keep their own rules. */
.sidebar-footer :deep([data-testid="shell-theme-toggle"]:is(:hover, :focus-visible)) {
  color: rgb(var(--c-txt));
}
@media (prefers-reduced-motion: reduce) {
  .sidebar-app-rail__item { transition: none; }
}
</style>
