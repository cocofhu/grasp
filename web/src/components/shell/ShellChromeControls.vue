<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import LangSelect from '../ui/LangSelect.vue'
import StatusMetrics from './StatusMetrics.vue'
import { theme, toggleTheme } from '@/lib/shared/theme'
import { isDraining } from '@/lib/composables/useShutdownState'
import { requestNotificationsPageReset } from '@/lib/composables/useNotificationsPageEntry'
import { locale, setLocale, type AppLocale } from '@/lib/shared/locale'
import { relTime } from '@/lib/shared/format'
import { useRunTerminalNotifications } from '@/lib/run/useRunTerminalNotifications'

/** Avoid stopPolling when a sibling chrome instance (sidebar + drawer) remains mounted. */
let chromePollMounts = 0

const props = withDefaults(
  defineProps<{
    /** bar = horizontal (e2e harness); sidebar = compact stack in nav chrome */
    layout?: 'bar' | 'sidebar'
  }>(),
  { layout: 'sidebar' },
)

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const panelOpen = ref(false)
const bellWrapEl = ref<HTMLElement | null>(null)
const panelStyle = ref<Record<string, string>>({})

const {
  previewItems,
  remainingCount,
  unreadCount,
  hasUnreadFailed,
  badgeLabel,
  markAllRead,
  startPolling,
  stopPolling,
} = useRunTerminalNotifications()

const themeTitle = computed(() =>
  t(theme.value === 'dark' ? 'shell.theme.toLight' : 'shell.theme.toDark'),
)

const metricsVariant = computed(() => (props.layout === 'sidebar' ? 'compact' : 'auto'))

watch(
  () => route.path,
  () => {
    panelOpen.value = false
  },
)

function onLocaleSelect(v: AppLocale) {
  void setLocale(v)
}

function placePanel() {
  const root = bellWrapEl.value
  if (!root) return
  const rect = root.getBoundingClientRect()
  const width = Math.min(380, window.innerWidth - 16)
  let left = rect.right - width
  if (left < 8) left = 8
  if (left + width > window.innerWidth - 8) left = window.innerWidth - width - 8
  const spaceBelow = window.innerHeight - rect.bottom - 12
  const preferBelow = spaceBelow >= 220 || spaceBelow >= rect.top
  if (preferBelow) {
    panelStyle.value = {
      position: 'fixed',
      top: `${Math.round(rect.bottom + 6)}px`,
      left: `${Math.round(left)}px`,
      width: `${width}px`,
    }
  } else {
    panelStyle.value = {
      position: 'fixed',
      bottom: `${Math.round(window.innerHeight - rect.top + 6)}px`,
      left: `${Math.round(left)}px`,
      width: `${width}px`,
    }
  }
}

async function togglePanel() {
  panelOpen.value = !panelOpen.value
  if (panelOpen.value) {
    await nextTick()
    placePanel()
  }
}

function closePanel() {
  panelOpen.value = false
}

function onDocClick(ev: MouseEvent) {
  if (!panelOpen.value) return
  const root = bellWrapEl.value
  const panel = document.querySelector('[data-testid="run-notifications-panel"]')
  const t = ev.target
  if (!(t instanceof Node)) return
  if (root?.contains(t) || panel?.contains(t)) return
  closePanel()
}

function onDocKeydown(ev: KeyboardEvent) {
  if (ev.key === 'Escape' && panelOpen.value) {
    closePanel()
  }
}

function onResize() {
  if (panelOpen.value) placePanel()
}

function statusLabel(status: string) {
  return status === 'failed'
    ? t('common.status.failed')
    : t('common.status.completed')
}

function itemTitle(item: {
  title: string
  titleNeutral?: boolean
  workflowName: string
  runId: string
  status: 'completed' | 'failed'
}) {
  if (item.titleNeutral) {
    return t('shell.runNotifications.neutralTitle', {
      name: item.workflowName || item.runId,
      status: statusLabel(item.status),
    })
  }
  return item.title
}

function itemContext(item: { workflowName: string; runId: string; title: string }) {
  const wf = item.workflowName || item.title
  return wf ? `${wf} · ${item.runId}` : item.runId
}

async function onItemClick() {
  closePanel()
  requestNotificationsPageReset()
  await router.push({ path: '/notifications' })
}

function onMarkAllRead() {
  markAllRead()
}

function viewAll() {
  closePanel()
  requestNotificationsPageReset()
  void router.push({ path: '/notifications' })
}

onMounted(() => {
  chromePollMounts += 1
  if (chromePollMounts === 1) startPolling()
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onDocKeydown)
  window.addEventListener('resize', onResize)
})

onBeforeUnmount(() => {
  chromePollMounts = Math.max(0, chromePollMounts - 1)
  if (chromePollMounts === 0) stopPolling()
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onDocKeydown)
  window.removeEventListener('resize', onResize)
})

defineExpose({
  panelOpen,
  togglePanel,
})
</script>

<template>
  <div
    class="shell-chrome-controls"
    data-testid="shell-chrome-controls"
    :data-layout="layout"
    :class="
      layout === 'bar'
        ? 'flex items-center gap-3'
        : 'flex flex-col gap-2.5'
    "
  >
    <StatusMetrics :variant="metricsVariant" />

    <div
      :class="
        layout === 'bar'
          ? 'flex items-center gap-3'
          : 'flex items-center gap-0.5'
      "
    >
      <span
        v-if="isDraining()"
        class="rounded-md inline-flex items-center gap-1.5 border border-warn/45 bg-warn/10 px-2.5 py-1 text-xs font-medium text-warn"
        :title="t('shell.shutdown.drainingTitle')"
      >
        <span class="inline-flex h-1.5 w-1.5 animate-pulse rounded-full bg-warn" />
        {{ t('shell.shutdown.draining') }}
      </span>
      <LangSelect
        :model-value="locale"
        :variant="layout === 'sidebar' ? 'ghost' : 'default'"
        @update:model-value="onLocaleSelect"
      />
      <span v-if="layout === 'sidebar'" class="min-w-0 flex-1" aria-hidden="true" />
      <button
        type="button"
        class="shell-theme-toggle flex items-center justify-center text-txt2 hover:bg-elevated hover:text-txt"
        :class="layout === 'sidebar' ? 'h-8 w-8' : 'h-9 w-9'"
        :title="themeTitle"
        data-testid="shell-theme-toggle"
        data-motion="overlay-pop"
        @click="toggleTheme"
      >
        <span class="shell-theme-icon-stack" aria-hidden="true">
          <span
            class="shell-theme-icon shell-theme-icon-moon"
            :class="{ 'is-active': theme !== 'dark' }"
            data-testid="shell-theme-icon-moon"
          >
            <Icon name="moon" :size="layout === 'sidebar' ? 16 : 18" />
          </span>
          <span
            class="shell-theme-icon shell-theme-icon-sun"
            :class="{ 'is-active': theme === 'dark' }"
            data-testid="shell-theme-icon-sun"
          >
            <Icon name="sun" :size="layout === 'sidebar' ? 16 : 18" />
          </span>
        </span>
      </button>
      <div ref="bellWrapEl" class="relative">
        <button
          type="button"
          class="relative flex items-center justify-center text-txt2 hover:bg-elevated hover:text-txt"
          :class="[
            layout === 'sidebar' ? 'h-8 w-8' : 'h-9 w-9',
            panelOpen ? 'bg-elevated text-txt' : '',
          ]"
          data-testid="run-notifications-bell"
          :aria-label="t('shell.runNotifications.title')"
          aria-haspopup="true"
          :aria-expanded="panelOpen ? 'true' : 'false'"
          @click.stop="togglePanel"
        >
          <Icon name="bell" :size="layout === 'sidebar' ? 16 : 18" />
          <span
            v-if="badgeLabel"
            class="absolute inline-flex items-center justify-center font-bold leading-none text-white"
            :class="[
              layout === 'sidebar'
                ? 'force-radius-full right-[3px] top-[3px] h-3.5 min-w-3.5 rounded-full px-[3px] text-[9px] shadow-[0_0_0_1.5px_rgb(var(--c-surface))]'
                : 'right-0.5 top-0.5 h-4 min-w-4 px-1 text-[10px]',
              hasUnreadFailed ? 'bg-err' : 'bg-accent',
            ]"
            data-testid="run-notifications-badge"
          >{{ badgeLabel }}</span>
        </button>
      </div>
    </div>

    <Teleport to="body">
      <div
        v-if="panelOpen"
        class="rounded-lg z-[60] flex max-h-[min(420px,70vh)] flex-col border border-line-strong bg-elevated shadow-card"
        role="menu"
        :aria-label="t('shell.runNotifications.title')"
        data-testid="run-notifications-panel"
        :style="panelStyle"
        @click.stop
      >
        <div class="flex items-center justify-between gap-2 border-b border-line px-3.5 py-3">
          <h3 class="m-0 text-[13px] font-semibold text-txt">{{ t('shell.runNotifications.title') }}</h3>
          <div class="flex items-center gap-2">
            <span class="text-xs text-txt3">
              {{ t('shell.runNotifications.unreadCount', { n: unreadCount }) }}
            </span>
            <button
              v-if="unreadCount > 0"
              type="button"
              class="rounded-md border border-line bg-transparent px-2 py-0.5 text-[11px] text-txt2 hover:border-accent hover:text-accent"
              data-testid="run-notifications-mark-all"
              @click="onMarkAllRead"
            >
              {{ t('shell.runNotifications.markAllRead') }}
            </button>
          </div>
        </div>

        <div class="min-h-0 flex-1 overflow-auto">
          <div
            v-if="!previewItems.length"
            class="px-5 py-9 text-center"
            data-testid="run-notifications-empty"
          >
            <p class="m-0 text-[13px] text-txt2">{{ t('shell.runNotifications.empty') }}</p>
            <p class="mt-1.5 text-xs text-txt3">{{ t('shell.runNotifications.emptyHint') }}</p>
            <p class="mt-1.5 text-xs text-txt3">{{ t('shell.runNotifications.emptyRunsHint') }}</p>
          </div>
          <button
            v-for="item in previewItems"
            :key="item.runId"
            type="button"
            class="relative block w-full border-b border-line px-3.5 py-3 text-left hover:bg-overlay"
            :class="item.unread ? 'bg-accent/5' : ''"
            data-testid="run-notifications-item"
            :data-run-id="item.runId"
            :data-status="item.status"
            :data-unread="item.unread ? 'true' : 'false'"
            :data-before-baseline="item.beforeBaseline ? 'true' : 'false'"
            @click="onItemClick()"
          >
            <span
              v-if="item.unread"
              class="absolute bottom-0 left-0 top-0 w-0.5 bg-accent"
              aria-hidden="true"
            />
            <div class="mb-1 flex items-center gap-2">
              <span
                class="rounded-md shrink-0 border px-1.5 py-0.5 text-[10px] font-semibold"
                :class="
                  item.status === 'failed'
                    ? 'border-err/45 text-err'
                    : 'border-ok/40 text-ok'
                "
              >{{ statusLabel(item.status) }}</span>
              <span class="truncate text-[13px] font-medium text-txt">{{ itemTitle(item) }}</span>
            </div>
            <div class="truncate text-xs text-txt3">
              {{ itemContext(item) }}
              <template v-if="item.status === 'completed'">
                · {{ t('shell.runNotifications.clickForOutput') }}
              </template>
              <template v-else>
                · {{ t('shell.runNotifications.clickForDetail') }}
              </template>
              · {{ relTime(item.finishedApprox || item.startedAt) }}
              <template v-if="item.beforeBaseline">
                ·
                <span data-testid="run-notifications-before-baseline">{{
                  t('shell.runNotifications.beforeBaseline')
                }}</span>
              </template>
            </div>
          </button>
        </div>

        <div class="flex flex-col gap-2 border-t border-line px-3.5 py-2.5">
          <div
            v-if="remainingCount > 0"
            class="text-xs text-txt3"
            data-testid="run-notifications-more"
          >
            {{ t('shell.runNotifications.moreHint', { n: remainingCount }) }}
          </div>
          <button
            type="button"
            class="rounded-md w-full border border-line bg-transparent px-3 py-2 text-[13px] text-accent-2 hover:border-accent hover:bg-accent-dim hover:text-accent"
            data-testid="run-notifications-view-all"
            @click="viewAll"
          >
            {{ t('shell.runNotifications.viewAll') }}
          </button>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
/*
 * plan g1.1 — bar and sidebar share this stack; no layout-specific motion.
 * plan g1.2 — same duration, easing, and amplitude as .overlay-pop
 *   (opacity + translateY(-4px) + scale(0.98), var(--dur-overlay) / var(--ease-out-expo)).
 *   Replaces the longer rotate cross-fade.
 * plan g1.3 — prefers-reduced-motion paints the active icon immediately.
 */
.shell-theme-icon-stack {
  position: relative;
  display: inline-flex;
  width: 1.125rem;
  height: 1.125rem;
  align-items: center;
  justify-content: center;
  view-transition-name: match-element;
  view-transition-class: theme-icon;
}

.shell-theme-icon {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  opacity: 0;
  transform: translateY(-4px) scale(0.98);
  transition:
    opacity var(--dur-overlay) var(--ease-out-expo),
    transform var(--dur-overlay) var(--ease-out-expo);
  pointer-events: none;
}

.shell-theme-icon.is-active {
  opacity: 1;
  transform: translateY(0) scale(1);
}

@media (prefers-reduced-motion: reduce) {
  .shell-theme-icon {
    transition: none;
    transform: translateY(0) scale(1);
  }

  .shell-theme-icon:not(.is-active) {
    opacity: 0;
  }
}
</style>
