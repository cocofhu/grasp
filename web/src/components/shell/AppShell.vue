<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppSidebar from './AppSidebar.vue'
import AppSidebarNav from './AppSidebarNav.vue'
import BrandLogo from './BrandLogo.vue'
import FloatingNavBall from './FloatingNavBall.vue'
import ShellChromeControls from './ShellChromeControls.vue'
import Icon from '../ui/Icon.vue'
import RunLaunchModal from '@/components/workflow/RunLaunchModal.vue'
import {
  drainToast,
  formatGrace,
  isDraining,
  isOffline,
  shutdownState,
  startShutdownPolling,
  stopShutdownPolling,
} from '@/lib/composables/useShutdownState'
import { useAuth } from '@/lib/composables/useAuth'
import { useRefreshChrome } from '@/lib/shared/refreshChrome'
import { useRoutePending } from '@/lib/shared/routePending'
import { useWorkflowRunLaunch } from '@/lib/run/useWorkflowRunLaunch'
import ServiceCommitBadge from './ServiceCommitBadge.vue'
import { sidebarHidden } from '@/lib/shared/sidebarHidden'
import { loadBrandSettings } from '@/lib/composables/useBrandSettings'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const full = computed(() => route.meta.full === true)
const draining = computed(() => isDraining())
const offline = computed(() => isOffline())
const auth = useAuth()
const refresh = useRefreshChrome()
const routePending = useRoutePending()
const launch = useWorkflowRunLaunch()
const {
  open: launchOpen,
  target: launchTarget,
  runFields: launchFields,
  runInputs: launchInputs,
  runImages: launchImages,
  draftRestored: launchDraftRestored,
  closeLaunch,
  saveRunDraftClick: saveShellRunDraft,
  onStarted: onShellRunStarted,
} = launch

const showRefreshBar = computed(() => refresh.showTopBar.value)
const dimContent = computed(() => refresh.dimContent.value)
const mainAriaBusy = computed(() => {
  if (!auth.ready.value || !auth.user.value) return true
  if (routePending.pending.value || routePending.showUi.value) return true
  return refresh.ariaBusy.value
})

const drawerOpen = ref(false)

watch(
  () => auth.user.value,
  (user) => {
    if (user) void loadBrandSettings()
  },
  { immediate: true },
)

watch(
  () => route.path,
  () => {
    drawerOpen.value = false
  },
)

function closeDrawer() {
  drawerOpen.value = false
}

function openDrawer() {
  drawerOpen.value = true
}

function onShellViewRun(runId: string) {
  void router.push('/runs/' + runId)
}

onMounted(() => startShutdownPolling(4000))
onUnmounted(() => stopShutdownPolling())
</script>

<template>
  <div
    class="app-shell-dotgrid relative flex h-screen w-screen overflow-hidden text-txt"
    :data-testid="full ? 'app-shell-full' : 'app-shell-workspace'"
  >
    <div
      class="hidden h-full min-h-0 shrink-0 md:flex"
      :class="!sidebarHidden ? 'py-[14px] pl-[14px]' : ''"
      data-testid="app-shell-sidebar-slot"
    >
      <AppSidebar />
    </div>

    <div class="flex min-w-0 flex-1 flex-col">
      <div
        v-if="draining && !full"
        class="flex shrink-0 items-center gap-3 border-b border-warn/35 bg-warn/10 px-6 py-2 text-sm text-warn"
        data-testid="shell-draining-banner"
      >
        <span class="inline-flex h-2 w-2 animate-pulse rounded-full bg-warn" />
        <strong class="font-semibold text-txt">{{ t('common.shutdown.shuttingDown') }}</strong>
        <span class="text-txt2">{{ shutdownState.message || t('common.shutdown.notAcceptingRequests') }}</span>
        <span class="ml-auto font-mono text-xs text-txt2">
          {{ t('common.shutdown.graceRemaining', { time: formatGrace(shutdownState.graceRemainingSeconds) }) }}
        </span>
      </div>

      <!-- No AppTopbar: chrome lives in sidebar / drawer; floating ball restores nav -->
      <div
        v-if="!full && showRefreshBar"
        class="app-refresh-track"
        data-testid="app-refresh-bar"
        aria-hidden="true"
      >
        <div class="app-refresh-bar" />
      </div>
      <main
        class="relative min-h-0 flex-1 overflow-hidden"
        :aria-busy="mainAriaBusy ? 'true' : 'false'"
      >
        <span class="sr-only" aria-live="polite">{{
          sidebarHidden ? t('shell.aria.navHidden') : t('shell.aria.navShown')
        }}</span>
        <div
          v-if="full && showRefreshBar"
          class="app-refresh-track"
          data-testid="app-refresh-bar"
          aria-hidden="true"
        >
          <div class="app-refresh-bar" />
        </div>
        <div
          v-if="full"
          class="h-full"
          data-testid="app-full-main"
          :class="{ 'app-refresh-dim': dimContent }"
        >
          <slot />
        </div>
        <div v-else class="scroll-area safe-area-bottom h-full min-h-0 overflow-y-auto">
          <div
            class="flex h-full min-h-0 flex-col px-4 pb-4 pt-2 md:px-6 md:pb-6 md:pt-3"
            :class="{ 'app-refresh-dim': dimContent }"
          >
            <slot />
          </div>
        </div>

        <ServiceCommitBadge />

        <div
          v-if="offline"
          class="absolute inset-0 z-40 flex items-center justify-center bg-base/75 backdrop-blur-sm"
        >
          <div class="rounded-lg border border-line bg-surface px-8 py-6 text-center shadow-card">
            <Icon name="alert" :size="28" class="mx-auto mb-3 text-txt3" />
            <h4 class="text-base font-semibold">{{ t('common.shutdown.offlineTitle') }}</h4>
            <p class="mt-2 text-sm text-txt3">{{ t('common.shutdown.offlineDesc') }}</p>
          </div>
        </div>
      </main>
    </div>

    <FloatingNavBall :drawer-open="drawerOpen" @open-drawer="openDrawer" />

    <div
      v-if="drainToast.visible"
      class="rounded-lg pointer-events-none fixed bottom-6 right-6 z-50 max-w-sm border border-err/40 bg-elevated px-4 py-3 text-sm text-txt2 shadow-card"
    >
      <strong class="mb-1 block font-semibold text-err">{{ t('common.shutdown.actionUnavailable') }}</strong>
      {{ drainToast.text }}
    </div>

    <!-- Mobile drawer overlay -->
    <Teleport to="body">
      <Transition name="drawer-fade">
        <div
          v-if="drawerOpen"
          class="fixed inset-0 z-40 bg-black/50 md:hidden"
          @click="closeDrawer"
        />
      </Transition>
      <Transition name="drawer-slide">
        <aside
          v-if="drawerOpen"
          class="app-sidebar-card fixed bottom-3.5 left-3.5 top-3.5 z-50 flex w-[min(280px,calc(85vw-14px))] flex-col bg-surface md:hidden"
          data-testid="mobile-nav-drawer"
        >
          <div class="safe-area-top flex h-14 items-center justify-between gap-2 px-4">
            <BrandLogo use-custom-brand />
            <button
              class="flex h-11 w-11 items-center justify-center rounded-md text-txt2 hover:bg-elevated hover:text-txt"
              :aria-label="t('shell.aria.closeNav')"
              data-testid="mobile-nav-close"
              @click="closeDrawer"
            >
              <Icon name="close" :size="18" />
            </button>
          </div>
          <AppSidebarNav @navigate="closeDrawer" />
          <div class="mt-auto border-t border-line p-3">
            <ShellChromeControls layout="sidebar" />
          </div>
        </aside>
      </Transition>
    </Teleport>

    <!-- Shell singleton: sidebar quick-launch opens the same RunLaunchModal as list「运行」 -->
    <RunLaunchModal
      v-if="launchTarget"
      :open="launchOpen"
      :workflow-id="launchTarget.id"
      :project-id="launchTarget.projectId"
      :workflow-name="launchTarget.name"
      :fields="launchFields"
      :run-inputs="launchInputs"
      :run-images="launchImages"
      :draft-restored="launchDraftRestored"
      @close="closeLaunch()"
      @view-run="onShellViewRun"
      @save-draft="saveShellRunDraft()"
      @started="onShellRunStarted()"
    />
  </div>
</template>

<style scoped>
.drawer-fade-enter-active,
.drawer-fade-leave-active {
  transition: opacity var(--dur-overlay) var(--ease-out-expo);
}
.drawer-fade-enter-from,
.drawer-fade-leave-to {
  opacity: 0;
}
.drawer-slide-enter-active,
.drawer-slide-leave-active {
  transition: transform var(--dur-overlay) var(--ease-out-expo);
}
.drawer-slide-enter-from,
.drawer-slide-leave-to {
  transform: translateX(-100%);
}
</style>
