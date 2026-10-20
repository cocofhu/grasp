<script setup lang="ts">
import { computed, watch } from 'vue'
import { useRoute } from 'vue-router'
import AppShell from './components/shell/AppShell.vue'
import ToastHost from './components/ui/ToastHost.vue'
import AppSkeleton from './components/ui/AppSkeleton.vue'
import OnboardingWizard from '@/components/onboarding/OnboardingWizard.vue'
import { locale, updateDocumentTitle } from '@/lib/shared/locale'
import {
  closeFirstInstall,
  markFirstInstallCompleted,
  onboardingMode,
  onboardingOpen,
  probeFirstInstall,
} from '@/lib/pm/firstInstall'
import { useAuth } from '@/lib/composables/useAuth'
import { useDelayedBusy } from '@/lib/composables/useDelayedBusy'
import { routeViewTransition, useRoutePending } from '@/lib/shared/routePending'
import { useLoadingAnnouncer } from '@/lib/shared/loadingAnnouncer'

const route = useRoute()
const bareLayout = computed(() => !!route.meta.bare)
const auth = useAuth()
const routePending = useRoutePending()
const { liveMessage } = useLoadingAnnouncer()

const authBlocking = computed(() => !bareLayout.value && (!auth.ready.value || !auth.user.value))
const authDelayed = useDelayedBusy({ mode: 'initial' })

watch(
  authBlocking,
  (blocking) => {
    if (blocking) authDelayed.setBusy(true)
    else authDelayed.reset()
  },
  { immediate: true },
)

const showShellSkeleton = computed(
  () => !bareLayout.value && (authDelayed.showUi.value || routePending.showUi.value),
)

const canShowProtected = computed(
  () => !bareLayout.value && !!auth.ready.value && !!auth.user.value && !routePending.showUi.value,
)

watch(locale, () => {
  updateDocumentTitle(route.meta.titleKey as string | undefined)
})

watch(
  () => !bareLayout.value && !!auth.ready.value && !!auth.user.value,
  (signedIn) => {
    if (signedIn) void probeFirstInstall()
  },
  { immediate: true },
)
</script>

<template>
  <AppShell v-if="!bareLayout">
    <AppSkeleton v-if="showShellSkeleton" />
    <router-view v-else-if="canShowProtected" v-slot="{ Component }">
      <div class="route-view-wrap">
        <transition :name="routeViewTransition || undefined">
          <component :is="Component" :key="String(route.name || route.path)" />
        </transition>
      </div>
    </router-view>
  </AppShell>
  <router-view v-else />
  <OnboardingWizard
    v-if="!bareLayout"
    :open="onboardingOpen"
    :mode="onboardingMode"
    @close="closeFirstInstall"
    @completed="markFirstInstallCompleted"
  />
  <ToastHost />
  <div class="sr-only" role="status" aria-live="polite" aria-atomic="true" data-testid="loading-live">
    {{ liveMessage }}
  </div>
</template>
