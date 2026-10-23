<script setup lang="ts">
import { nextTick, onUpdated, ref, watch } from 'vue'
import RunBoardCard from './RunBoardCard.vue'
import type { Run } from '@/lib/shared/types'

const props = withDefaults(
  defineProps<{
    title: string
    hint?: string
    accent?: 'active' | 'running' | 'waiting' | 'done' | 'extra'
    items: Run[]
    emptyText: string
    truncatedHint?: string
    /** Server-reported total; used for badge when truncated. */
    total?: number
    /**
     * Unfiltered loaded count. Search hides rows without changing the header badge.
     * Falls back to items.length when omitted.
     */
    listedCount?: number
    loading?: boolean
    loadingText?: string
    /** Stretch to the parent height so the column body fills leftover space. */
    fill?: boolean
    /**
     * When true, column header is a focusable control that emits activate-header
     * (full board only; Dashboard leaves this off).
     */
    headerActivatable?: boolean
    /** Status key emitted with activate-header when headerActivatable. */
    status?: string
  }>(),
  { accent: 'extra', total: 0, loading: false, fill: false, headerActivatable: false },
)

const emit = defineEmits<{
  (e: 'select', run: Run): void
  (e: 'activate-header', status: string): void
}>()

const bodyEl = ref<HTMLElement | null>(null)
let savedScroll = 0
/** Scroll events caused by restoring a saved offset must not overwrite it. */
let ignoreScroll = 0

const accentClass: Record<string, string> = {
  active: 'bg-accent-2',
  running: 'bg-info',
  waiting: 'bg-warn',
  done: 'bg-ok',
  extra: 'bg-txt3',
}

function badgeLabel(): string {
  const n = props.listedCount ?? props.items.length
  const total = props.total || 0
  if (total > n && n > 0) return `${n}/${total}`
  if (total > n) return String(total)
  return String(n)
}

function onActivateHeader() {
  if (!props.headerActivatable) return
  emit('activate-header', props.status || '')
}

function scrollLimit(el: HTMLElement): number | null {
  const client = el.clientHeight
  if (client <= 0) return null
  return Math.max(0, el.scrollHeight - client)
}

/**
 * A shorter list, an empty search, or a hidden column makes the browser clamp
 * scrollTop and emit scroll. That clamped value is not a user gesture.
 */
function isClampedOffset(el: HTMLElement, top: number): boolean {
  if (top + 1 >= savedScroll) return false
  const limit = scrollLimit(el)
  return limit == null || savedScroll > limit + 1
}

function rememberScroll() {
  if (ignoreScroll > 0) return
  const el = bodyEl.value
  if (!el) return
  const top = el.scrollTop
  if (isClampedOffset(el, top)) return
  savedScroll = top
}

function restoreScroll() {
  const el = bodyEl.value
  if (!el || savedScroll <= 0) return
  const limit = scrollLimit(el)
  if (limit != null && savedScroll > limit + 1) return
  if (Math.abs(el.scrollTop - savedScroll) <= 1) return
  ignoreScroll += 1
  el.scrollTop = savedScroll
  queueMicrotask(() => {
    ignoreScroll = Math.max(0, ignoreScroll - 1)
  })
}

watch(
  () => props.items,
  async () => {
    await nextTick()
    restoreScroll()
  },
)
onUpdated(restoreScroll)
</script>

<template>
  <div
    class="flex min-w-0 flex-col overflow-hidden rounded-lg border border-line bg-surface"
    :class="fill ? 'h-full' : ''"
    data-testid="run-board-column"
  >
    <component
      :is="headerActivatable ? 'button' : 'div'"
      :type="headerActivatable ? 'button' : undefined"
      class="flex min-h-12 w-full shrink-0 items-center gap-2 border-b border-line/70 bg-elevated/35 px-3.5 py-3 text-left"
      :class="
        headerActivatable
          ? 'cursor-pointer transition hover:bg-elevated focus-visible:bg-elevated focus-visible:outline focus-visible:outline-1 focus-visible:outline-offset-[-1px] focus-visible:outline-accent'
          : ''
      "
      :aria-haspopup="headerActivatable ? 'dialog' : undefined"
      :data-testid="headerActivatable ? 'run-board-column-header' : undefined"
      @click="headerActivatable ? onActivateHeader() : undefined"
    >
      <span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="accentClass[accent] || accentClass.extra" aria-hidden="true" />
      <span class="text-[13px] font-semibold text-txt">{{ title }}</span>
      <span v-if="hint" class="text-[11px] text-txt3">{{ hint }}</span>
      <span
        class="ml-auto inline-flex h-5 min-w-[22px] items-center justify-center rounded-md bg-elevated px-1.5 text-[11px] font-medium text-txt3"
        data-testid="run-board-column-count"
      >
        {{ badgeLabel() }}
      </span>
    </component>
    <div
      ref="bodyEl"
      class="run-board-column-body scroll-area flex min-h-0 flex-col overflow-y-auto"
      :class="fill ? 'flex-1' : ''"
      data-testid="run-board-column-body"
      @scroll.passive="rememberScroll"
    >
      <template v-if="loading && !items.length">
        <div
          class="px-3 py-7 text-center text-xs text-txt3"
          data-testid="run-board-column-loading"
        >
          {{ loadingText || '…' }}
        </div>
      </template>
      <template v-else-if="items.length">
        <RunBoardCard v-for="r in items" :key="r.id" :run="r" @select="emit('select', $event)" />
      </template>
      <div
        v-else
        class="px-3 py-7 text-center text-xs text-txt3"
        data-testid="run-board-column-empty"
      >
        {{ emptyText }}
      </div>
      <p v-if="truncatedHint" class="px-3.5 text-[11px] text-txt3">{{ truncatedHint }}</p>
    </div>
    <div v-if="$slots.footer" class="run-board-column-foot shrink-0" data-testid="run-board-column-footer">
      <slot name="footer" />
    </div>
  </div>
</template>

<style scoped>
.run-board-column-body { overscroll-behavior: contain; }
.run-board-column-foot {
  display: flex;
  align-items: center;
  min-height: 40px;
  padding: 8px 16px;
  border-top: 1px solid rgb(var(--c-line) / .7);
  background: rgb(var(--c-elevated) / .35);
  color: rgb(var(--c-txt3));
  font-size: 12px;
}
@media (prefers-reduced-motion: reduce) {
  .run-board-column-body :deep(.run-board-card),
  .run-board-column-body :deep(.run-board-progress) {
    transition: none;
  }
}
</style>
