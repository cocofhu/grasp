<script setup lang="ts">
import { RouterLink, useRoute } from 'vue-router'
import { computed, ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import { sidebarNavGroups } from '@/data/sidebarNav'
import {
  isSettingsChrome,
  settingsItemActive,
  settingsItemKey,
  settingsNavItems,
  type SettingsNavItem,
} from '@/data/settingsNav'
import { usePendingGates } from '@/lib/inbox/usePendingGates'
import { useRunTerminalNotifications } from '@/lib/run/useRunTerminalNotifications'
import { useWorkflowFavorites } from '@/lib/run/useWorkflowFavorites'
import { useWorkflowRunLaunch } from '@/lib/run/useWorkflowRunLaunch'
import { useToast } from '@/lib/composables/useToast'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'

defineProps<{ drawer?: boolean }>()
const emit = defineEmits<{ (e: 'navigate'): void }>()

const { t } = useI18n()
const route = useRoute()
const toast = useToast()

// Shared singleton source so approving a gate elsewhere updates the badge immediately.
const { count: gateCount, peek, refresh } = usePendingGates()
// Same unreadCount singleton as shell chrome bell — keep the settings notification badge in sync.
const { unreadCount } = useRunTerminalNotifications()
const { displayItems, hydrateDisplay, unfavorite, getFavoriteWorkflow, reorderFavorites } = useWorkflowFavorites()
const { openLaunch } = useWorkflowRunLaunch()
const { isMobile } = useBreakpoint()

const DRAG_THRESHOLD_PX = 4
const quickList = ref<HTMLElement>()
const suppressQuickItemClick = ref(false)
const dragState = ref<{
  workflowId: string
  from: number
  placeholder: number
  x: number
  y: number
  offsetX: number
  offsetY: number
  width: number
}>()
const dragItems = computed(() =>
  dragState.value
    ? displayItems.value.filter((item) => item.workflowId !== dragState.value?.workflowId)
    : displayItems.value,
)

let timer: number | undefined
function badgeFor(to: string): number {
  if (to === '/gates') return gateCount.value
  if (to === '/notifications') return unreadCount.value
  return 0
}
function pollRefresh() {
  return peek({ source: 'sidebar-poll' })
}
onMounted(() => {
  refresh({ source: 'mount' })
  void hydrateDisplay()
  timer = window.setInterval(pollRefresh, 15000)
  chromeHasMounted.value = true
})
onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})
// Refresh promptly when navigating (e.g. right after approving a gate).
watch(
  () => route.path,
  () => {
    refresh({ source: 'navigate' })
    void hydrateDisplay()
  },
)

function isActive(to: string) {
  return route.path === to || route.path.startsWith(to + '/')
}

const settingsChrome = computed(() => isSettingsChrome(route.path, route.meta.full === true))

function prefersReducedMotion(): boolean {
  return (
    typeof window !== 'undefined' &&
    !!window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches
  )
}

/** First paint (direct /agents etc.) must not play the push animation. */
const chromeHasMounted = ref(false)
const chromeSlideName = ref('chrome-none')

watch(settingsChrome, (now) => {
  if (!chromeHasMounted.value || prefersReducedMotion()) {
    chromeSlideName.value = 'chrome-none'
    return
  }
  chromeSlideName.value = now ? 'chrome-slide-left' : 'chrome-slide-right'
})

const chromeUseCss = computed(() => chromeSlideName.value !== 'chrome-none')

function isSettingsItemActive(item: SettingsNavItem) {
  return settingsItemActive(item, route.path, route.query)
}

function settingsLinkTo(item: SettingsNavItem) {
  if (item.query) return { path: item.to, query: item.query }
  return item.to
}

function onNavigate() {
  emit('navigate')
}

function onUnfavorite(workflowId: string, name: string, ev: Event) {
  ev.stopPropagation()
  ev.preventDefault()
  if (suppressQuickItemClick.value) return
  unfavorite(workflowId, { name })
}

async function onLaunch(workflowId: string) {
  if (suppressQuickItemClick.value) return
  try {
    const wf = await getFavoriteWorkflow(workflowId)
    if (!wf) return
    openLaunch(wf)
    onNavigate()
  } catch {
    toast.error(t('common.toast.favoriteLaunchFailed'))
  }
}

function updatePlaceholder(clientY: number) {
  if (!dragState.value || !quickList.value) return
  const rows = Array.from(quickList.value.querySelectorAll<HTMLElement>('[data-sortable-row]'))
  const slot = rows.findIndex((row) => clientY < row.getBoundingClientRect().top + row.offsetHeight / 2)
  dragState.value.placeholder = slot === -1 ? rows.length : slot
}

function onHandlePointerDown(workflowId: string, event: PointerEvent) {
  if (isMobile.value || event.button !== 0) return
  const source = (event.currentTarget as HTMLElement).closest<HTMLElement>('[data-sortable-row]')
  const from = displayItems.value.findIndex((item) => item.workflowId === workflowId)
  if (!source || from < 0) return

  event.preventDefault()
  const handle = event.currentTarget as HTMLElement
  const startX = event.clientX
  const startY = event.clientY
  let activated = false
  let offsetX = 0
  let offsetY = 0

  try {
    handle.setPointerCapture(event.pointerId)
  } catch {
    // Pointer capture is a progressive enhancement for the drag session.
  }

  const activate = (moveEvent: PointerEvent) => {
    const rect = source.getBoundingClientRect()
    offsetX = moveEvent.clientX - rect.left
    offsetY = moveEvent.clientY - rect.top
    activated = true
    suppressQuickItemClick.value = true
    dragState.value = {
      workflowId,
      from,
      placeholder: from,
      x: moveEvent.clientX,
      y: moveEvent.clientY,
      offsetX,
      offsetY,
      width: rect.width,
    }
    document.body.classList.add('quick-pipeline-dragging')
    updatePlaceholder(moveEvent.clientY)
  }

  const onMove = (moveEvent: PointerEvent) => {
    if (!activated) {
      const dx = moveEvent.clientX - startX
      const dy = moveEvent.clientY - startY
      if (Math.hypot(dx, dy) < DRAG_THRESHOLD_PX) return
      activate(moveEvent)
    }
    if (!dragState.value) return
    dragState.value.x = moveEvent.clientX
    dragState.value.y = moveEvent.clientY
    updatePlaceholder(moveEvent.clientY)
    moveEvent.preventDefault()
  }

  const finish = (cancelled: boolean) => {
    handle.removeEventListener('pointermove', onMove)
    handle.removeEventListener('pointerup', onUp)
    handle.removeEventListener('pointercancel', onCancel)
    window.removeEventListener('pointermove', onMove)
    window.removeEventListener('pointerup', onUp)
    window.removeEventListener('pointercancel', onCancel)
    try {
      handle.releasePointerCapture(event.pointerId)
    } catch {
      // Capture may already be released by the browser.
    }
    if (!activated || !dragState.value) return

    const { from: dragFrom, placeholder } = dragState.value
    dragState.value = undefined
    document.body.classList.remove('quick-pipeline-dragging')
    if (!cancelled) reorderFavorites(dragFrom, placeholder)
    window.setTimeout(() => {
      suppressQuickItemClick.value = false
    }, 0)
  }
  const onUp = () => finish(false)
  const onCancel = () => finish(true)

  handle.addEventListener('pointermove', onMove)
  handle.addEventListener('pointerup', onUp)
  handle.addEventListener('pointercancel', onCancel)
  // Once activation removes the source row from the rendered list, some browsers
  // release element-level pointer capture. Window listeners keep the session intact.
  window.addEventListener('pointermove', onMove)
  window.addEventListener('pointerup', onUp)
  window.addEventListener('pointercancel', onCancel)
}

const primaryGroup = sidebarNavGroups[0]
const settingsItems = settingsNavItems
</script>

<template>
  <nav
    class="scroll-area flex-1 overflow-y-auto px-3 py-2"
    data-testid="app-sidebar-nav"
    :data-nav-mode="settingsChrome ? 'settings' : 'workspace'"
  >
    <div class="nav-chrome-viewport">
    <Transition :name="chromeSlideName" :css="chromeUseCss">
    <!-- Settings chrome: back to home + category list (plan g2.2) -->
    <div v-if="settingsChrome" key="settings" class="nav-chrome-pane mb-3" data-testid="nav-settings-chrome">
      <RouterLink
        v-hover-ink
        to="/dashboard"
        class="nav-item mb-2 text-txt3"
        data-testid="nav-back-home"
        @click="onNavigate"
      >
        <Icon name="arrow-left" :size="16" />
        <span class="flex-1 text-xs">{{ t('nav.backHome') }}</span>
      </RouterLink>
      <template v-for="(item, index) in settingsItems" :key="settingsItemKey(item, index)">
        <div
          v-if="item.groupKey"
          class="px-3 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-wider text-txt3"
        >
          {{ t(item.groupKey) }}
        </div>
        <RouterLink
          v-hover-ink
          :to="settingsLinkTo(item)"
          class="nav-item mb-0.5"
          :class="{ active: isSettingsItemActive(item) }"
          :data-testid="item.query?.integrations ? 'nav-settings-integrations' : undefined"
          @click="onNavigate"
        >
          <Icon :name="item.icon" :size="17" />
          <span class="flex-1">{{ t(item.labelKey) }}</span>
          <span
            v-if="badgeFor(item.to)"
            class="force-radius-full flex h-5 min-w-5 items-center justify-center rounded-full bg-accent px-1.5 text-[11px] font-semibold text-white"
            :data-testid="item.to === '/notifications' ? 'nav-notifications-badge' : undefined"
          >{{ badgeFor(item.to) }}</span>
        </RouterLink>
      </template>
    </div>

    <!-- Workspace primary + quick pipelines slide together (plan g2.2) -->
    <div v-else-if="primaryGroup" key="workspace" class="nav-chrome-pane">
    <div class="mb-3" data-testid="nav-workspace-chrome">
      <RouterLink
        v-for="item in primaryGroup.items"
        :key="item.to"
        v-hover-ink
        :to="item.to"
        class="nav-item mb-0.5"
        :class="{ active: isActive(item.to) }"
        @click="onNavigate"
      >
        <Icon :name="item.icon" :size="17" />
        <span class="flex-1">{{ t(item.labelKey) }}</span>
        <span
          v-if="badgeFor(item.to)"
          class="force-radius-full flex h-5 min-w-5 items-center justify-center rounded-full bg-accent px-1.5 text-[11px] font-semibold text-white"
          :data-testid="item.to === '/gates' ? 'nav-gates-badge' : undefined"
        >{{ badgeFor(item.to) }}</span>
      </RouterLink>
    </div>

    <!-- Quick pipelines: workspace only, and only when favorites exist (plan g1.2) -->
    <div v-if="displayItems.length" class="mb-3" data-testid="nav-quick-pipelines">
      <div class="px-3 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        {{ t('nav.quickPipelines') }}
      </div>
      <div ref="quickList" class="quick-pipelines-list">
        <template v-for="(item, index) in dragItems" :key="item.workflowId">
          <div
            v-if="dragState && dragState.placeholder === index"
            class="quick-pipeline-placeholder"
            data-testid="nav-quick-pipeline-placeholder"
          />
          <div
            class="mb-0.5 grid w-full grid-cols-[28px_1fr_28px] items-center gap-0.5 px-2 py-1.5 text-left text-txt2 transition hover:bg-elevated hover:text-txt"
            data-sortable-row
            data-testid="nav-quick-pipeline-item"
            role="button"
            tabindex="0"
            @click="onLaunch(item.workflowId)"
            @keydown.enter.prevent="onLaunch(item.workflowId)"
            @keydown.space.prevent="onLaunch(item.workflowId)"
          >
            <button
              v-if="!isMobile"
              type="button"
              class="quick-pipeline-handle flex h-7 w-7 items-center justify-center text-txt3 hover:bg-overlay hover:text-txt"
              data-testid="nav-quick-pipeline-drag-handle"
              aria-label="拖动调整顺序"
              title="拖动调整顺序"
              @pointerdown="onHandlePointerDown(item.workflowId, $event)"
            ><span aria-hidden="true">⠿</span></button>
            <div v-else aria-hidden="true" />
            <div class="min-w-0">
              <div class="truncate text-[13px] text-txt" :title="item.name">{{ item.name }}</div>
              <div class="mt-0.5 flex min-w-0 items-center gap-1.5">
                <span class="truncate text-[11px] text-txt3" :title="item.projectName">{{ item.projectName }}</span>
                <span
                  v-if="item.status === 'draft'"
                  class="rounded-md shrink-0 border border-warn/35 bg-warn/10 px-1.5 py-px text-[10px] text-warn"
                >{{ t('common.status.draft') }}</span>
              </div>
            </div>
            <button
              type="button"
              class="flex h-7 w-7 items-center justify-center text-warn hover:text-warn"
              data-testid="nav-quick-pipeline-unfavorite"
              :aria-label="t('common.buttons.unfavorite')"
              :title="t('common.buttons.unfavorite')"
              @click="onUnfavorite(item.workflowId, item.name, $event)"
            >
              <Icon name="star-filled" :size="14" />
            </button>
          </div>
        </template>
        <div
          v-if="dragState && dragState.placeholder === dragItems.length"
          class="quick-pipeline-placeholder"
          data-testid="nav-quick-pipeline-placeholder"
        />
      </div>
    </div>
    </div>
    </Transition>
    </div>

  </nav>
  <div
    v-if="dragState"
    class="quick-pipeline-drag-float grid grid-cols-[28px_1fr_28px] items-center gap-0.5 px-2 py-1.5"
    :style="{
      width: `${dragState.width}px`,
      left: `${dragState.x - dragState.offsetX}px`,
      top: `${dragState.y - dragState.offsetY}px`,
    }"
    aria-hidden="true"
  >
    <span class="flex h-7 w-7 items-center justify-center text-txt">⠿</span>
    <div class="min-w-0">
      <div class="truncate text-[13px] text-txt">{{ displayItems.find((item) => item.workflowId === dragState?.workflowId)?.name }}</div>
      <div class="truncate text-[11px] text-txt3">{{ displayItems.find((item) => item.workflowId === dragState?.workflowId)?.projectName }}</div>
    </div>
    <span class="flex h-7 w-7 items-center justify-center text-warn"><Icon name="star-filled" :size="14" /></span>
  </div>
</template>

<style scoped>
.nav-chrome-viewport {
  position: relative;
  overflow: hidden;
}

.nav-chrome-pane {
  width: 100%;
}

.chrome-slide-left-enter-active,
.chrome-slide-left-leave-active,
.chrome-slide-right-enter-active,
.chrome-slide-right-leave-active {
  transition:
    transform 320ms cubic-bezier(0.22, 1, 0.36, 1),
    opacity 320ms ease;
}

.chrome-slide-left-leave-active,
.chrome-slide-right-leave-active {
  position: absolute;
  inset-inline: 0;
  top: 0;
}

.chrome-slide-left-enter-from {
  transform: translateX(100%);
  opacity: 0.4;
}

.chrome-slide-left-leave-to {
  transform: translateX(-100%);
  opacity: 0.4;
}

.chrome-slide-right-enter-from {
  transform: translateX(-100%);
  opacity: 0.4;
}

.chrome-slide-right-leave-to {
  transform: translateX(100%);
  opacity: 0.4;
}

@media (prefers-reduced-motion: reduce) {
  .chrome-slide-left-enter-active,
  .chrome-slide-left-leave-active,
  .chrome-slide-right-enter-active,
  .chrome-slide-right-leave-active {
    transition: none;
  }
}

.quick-pipeline-handle {
  cursor: grab;
  touch-action: none;
}

.quick-pipeline-handle:active {
  cursor: grabbing;
}

.quick-pipeline-placeholder {
  height: 46px;
  margin-bottom: 2px;
  border: 1px dashed rgb(var(--c-accent) / 70%);
  background: rgb(var(--c-accent) / 12%);
}

.quick-pipeline-drag-float {
  position: fixed;
  z-index: 9999;
  pointer-events: none;
  border: 1px solid rgb(var(--c-accent));
  background: rgb(var(--c-elevated));
  box-shadow: 0 12px 28px rgb(0 0 0 / 55%), 0 0 0 1px rgb(var(--c-accent) / 35%);
  opacity: 0.96;
  transform: scale(1.02);
}

:global(body.quick-pipeline-dragging),
:global(body.quick-pipeline-dragging *) {
  cursor: grabbing !important;
  user-select: none;
}
</style>
