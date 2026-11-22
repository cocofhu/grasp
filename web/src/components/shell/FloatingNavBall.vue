<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import {
  focusDesktopNavControl,
  showDesktopSidebar,
  sidebarHidden,
} from '@/lib/shared/sidebarHidden'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { inlineMobileNavHosts } from '@/lib/shared/mobileNav'

const props = defineProps<{
  /** When true (mobile drawer open), ball stays hidden. */
  drawerOpen?: boolean
}>()

const emit = defineEmits<{
  (e: 'open-drawer'): void
}>()

const { t } = useI18n()
const { isMobile } = useBreakpoint()

const EXIT_MS = 200
const exiting = ref(false)
let exitTimer: ReturnType<typeof setTimeout> | null = null

function prefersReducedMotion(): boolean {
  return (
    typeof window !== 'undefined' &&
    !!window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches
  )
}

function clearExitTimer() {
  if (exitTimer != null) {
    clearTimeout(exitTimer)
    exitTimer = null
  }
}

const desktopVisible = computed(
  () => !isMobile.value && (sidebarHidden.value || exiting.value),
)

const mobileVisible = computed(
  () => isMobile.value && !props.drawerOpen && inlineMobileNavHosts.value === 0,
)

const visible = computed(() => desktopVisible.value || mobileVisible.value)

const wrapClass = computed(() => {
  if (!visible.value) {
    return 'pointer-events-none opacity-0 invisible translate-y-2.5 scale-[0.84]'
  }
  if (exiting.value) {
    return 'pointer-events-none opacity-0 visible translate-y-3 scale-[0.72]'
  }
  return 'pointer-events-auto opacity-100 visible translate-y-0 scale-100'
})

watch(sidebarHidden, (hidden) => {
  if (hidden) {
    clearExitTimer()
    exiting.value = false
  }
})

async function pinDesktop() {
  if (!sidebarHidden.value || exiting.value) return
  clearExitTimer()

  const reducedMotion = prefersReducedMotion()
  // Ball exit runs alongside the sidebar; it must never gate showDesktopSidebar.
  exiting.value = !reducedMotion
  showDesktopSidebar()

  if (!reducedMotion) {
    exitTimer = setTimeout(() => {
      exitTimer = null
      exiting.value = false
    }, EXIT_MS)
  }

  await focusDesktopNavControl('hide')
}

function onActivate() {
  if (isMobile.value) {
    emit('open-drawer')
    return
  }
  void pinDesktop()
}

onBeforeUnmount(() => {
  clearExitTimer()
})
</script>

<template>
  <div
    class="floating-nav-ball-wrap fixed bottom-[22px] left-[18px] z-50 transition-[opacity,transform,visibility] duration-[280ms] ease-[cubic-bezier(0.2,0.8,0.2,1)] motion-reduce:transition-none"
    :class="[
      wrapClass,
      exiting
        ? 'duration-[200ms] ease-[cubic-bezier(0.4,0,0.2,1)]'
        : '',
    ]"
    data-testid="floating-nav-ball-wrap"
    :data-exiting="exiting ? 'true' : 'false'"
    :aria-hidden="visible ? undefined : 'true'"
  >
    <button
      type="button"
      class="flex h-12 w-12 items-center justify-center rounded-full border border-line bg-surface text-txt shadow-card transition hover:-translate-y-px hover:shadow-lg focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent motion-reduce:transition-none"
      data-testid="floating-nav-ball"
      :aria-label="isMobile ? t('shell.aria.openNav') : t('shell.aria.showNav')"
      :title="isMobile ? t('shell.aria.openNav') : t('shell.aria.showNav')"
      :aria-expanded="isMobile ? (drawerOpen ? 'true' : 'false') : 'false'"
      :aria-controls="isMobile ? undefined : 'app-desktop-sidebar'"
      :tabindex="visible ? 0 : -1"
      @click="onActivate"
      @mouseenter.stop
    >
      <Icon name="menu" :size="20" />
    </button>
  </div>
</template>
