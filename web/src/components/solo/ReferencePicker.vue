<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import type { GitHubReferenceCandidate, MessageReference, SoloReferenceCandidate } from '@/lib/api/apiTypes'
import { useReferenceSearch, type GitHubStateFilter, type ReferenceScope, type ReferenceTab } from '@/lib/solo/useReferenceSearch'
import { referenceIcon, referenceTone } from '@/lib/solo/referenceDisplay'

const props = defineProps<{
  /** An open session, the launch composer's picks, a workflow, or a run node. */
  scope: ReferenceScope
  query: string
  /** Targets already picked in this message; hidden from the list. */
  taken: string[]
  full: boolean
  initialTab?: ReferenceTab
  /** Opens above the composer by default; the home composer opens below. */
  placement?: 'up' | 'down'
}>()
const emit = defineEmits<{ (e: 'pick', ref: MessageReference): void }>()
const { t } = useI18n()

const tab = ref<ReferenceTab>(props.initialTab || 'all')
const ghState = ref<GitHubStateFilter>('open')
/** -1 until the user moves: Enter without a highlight sends the text as typed. */
const sel = ref(-1)
const list = ref<HTMLElement | null>(null)
const content = ref<HTMLElement | null>(null)
const tabbar = ref<HTMLElement | null>(null)
const search = useReferenceSearch(computed(() => props.scope))
const TABS: ReferenceTab[] = ['all', 'solo', 'issue', 'pr']
const ALL_LIMIT = 5
/** Tallest the list grows before it scrolls. */
const MAX_LIST = 270
/** Rows that enter together fade in one after another, capped so a long list does not lag. */
const STAGGER_MS = 22
const STAGGER_MAX = 7

/** Under reduced motion nothing slides, fades or staggers. */
const still = typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches

type Option =
  | { type: 'item'; key: string; ref: MessageReference; icon: string; tone: string; title: string; subtitle: string }
  | { type: 'action'; key: string; label: string; run: () => void }
type Section = { key: string; title: string; options: Option[]; loading: boolean; first: boolean; note: string; error: boolean }
/** One keyed row of the list. Keys stay stable across replies so rows are reused, not rebuilt. */
type Row =
  | { type: 'heading'; key: string; title: string; loading: boolean }
  | { type: 'option'; key: string; option: Option; index: number }
  | { type: 'skeleton'; key: string }
  | { type: 'note'; key: string; text: string; error: boolean }

function soloOption(item: SoloReferenceCandidate): Option {
  const when = item.lastActiveAt ? new Date(item.lastActiveAt).toLocaleString() : ''
  const ref: MessageReference = { kind: 'solo', target: item.target, title: item.title }
  return {
    type: 'item', key: `solo:${item.target}`, icon: referenceIcon(ref), tone: referenceTone(ref), ref,
    title: item.title || t('pages.solo.untitled'),
    subtitle: [t(`pages.solo.reference.sessionStatus.${item.status}`, item.status), when].filter(Boolean).join(' · '),
  }
}

function githubOption(item: GitHubReferenceCandidate): Option {
  const ref: MessageReference = { kind: item.kind, target: item.target, title: item.title, state: item.state, url: item.url }
  return {
    type: 'item', key: `gh:${item.target}`, ref, icon: referenceIcon(ref), tone: referenceTone(ref),
    title: `#${item.number} ${item.title}`,
    subtitle: t('pages.solo.reference.githubSubtitle', { repo: item.repo, state: item.state, n: item.comments }),
  }
}

function laneNote(lane: { loading: boolean; settled: boolean; error: string; hint: string }, count: number) {
  if (lane.error) return lane.error
  if (lane.hint && !count) return lane.hint
  if (lane.settled && !count) return t('pages.solo.reference.noMatch')
  return ''
}

const sections = computed<Section[]>(() => {
  if (props.full) return []
  const taken = new Set(props.taken)
  const soloItems = search.solo.items.filter((item) => !taken.has(item.target)).map(soloOption)
  const ghItems = search.github.items.filter((item) => !taken.has(item.target))
  const issues = ghItems.filter((item) => item.kind === 'github_issue').map(githubOption)
  const prs = ghItems.filter((item) => item.kind === 'github_pr').map(githubOption)
  const out: Section[] = []
  const add = (key: ReferenceTab, title: string, options: Option[], lane: typeof search.solo | typeof search.github, more: boolean) => {
    if (lane.skipped) return
    const rows = tab.value === 'all' ? options.slice(0, ALL_LIMIT) : options.slice()
    if (tab.value === 'all' && (options.length > ALL_LIMIT || more)) {
      rows.push({ type: 'action', key: `more:${key}`, label: t('pages.solo.reference.more', { kind: title }), run: () => setTab(key) })
    } else if (tab.value !== 'all' && more) {
      rows.push({ type: 'action', key: `load:${key}`, label: lane.paging ? t('pages.solo.reference.loading') : t('pages.solo.reference.loadMore'),
        run: () => search.loadMore(key === 'solo' ? 'solo' : 'github', props.query, tab.value, ghState.value) })
    }
    const first = lane.loading && !lane.settled
    if (tab.value === 'all' && !rows.length && !first && !lane.error && !lane.hint) return
    out.push({ key, title, options: rows, loading: lane.loading, first, note: laneNote(lane, options.length), error: !!lane.error })
  }
  if (tab.value === 'all' || tab.value === 'solo') add('solo', t('pages.solo.reference.tabs.solo'), soloItems, search.solo, !!search.solo.nextPage)
  const ghMore = !!search.github.nextPage
  if (tab.value === 'all' || tab.value === 'issue') add('issue', t('pages.solo.reference.tabs.issue'), issues, search.github, tab.value === 'issue' && ghMore)
  if (tab.value === 'all' || tab.value === 'pr') add('pr', t('pages.solo.reference.tabs.pr'), prs, search.github, tab.value === 'pr' && ghMore)
  // In "all", one GitHub error or hint is enough; do not repeat it under PR.
  if (tab.value === 'all') {
    const issue = out.find((s) => s.key === 'issue')
    const pr = out.find((s) => s.key === 'pr')
    if (issue && pr && !pr.options.length && pr.note === issue.note) out.splice(out.indexOf(pr), 1)
  }
  return out
})

const options = computed(() => sections.value.flatMap((s) => s.options))
const refreshing = computed(() => sections.value.some((s) => s.loading && !s.first))

const rows = computed<Row[]>(() => {
  if (props.full) return [{ type: 'note', key: 'full', text: t('pages.solo.reference.full', { n: 5 }), error: false }]
  if (!sections.value.length) return [{ type: 'note', key: 'empty', text: t('pages.solo.reference.noMatch'), error: false }]
  const out: Row[] = []
  let index = 0
  for (const section of sections.value) {
    out.push({ type: 'heading', key: `h:${section.key}`, title: section.title, loading: section.loading })
    for (const option of section.options) out.push({ type: 'option', key: `o:${option.key}`, option, index: index++ })
    if (section.first && !section.options.length) out.push({ type: 'skeleton', key: `s:${section.key}` })
    else if (section.note && !section.options.length) out.push({ type: 'note', key: `n:${section.key}`, text: section.note, error: section.error })
    else if (section.error) out.push({ type: 'note', key: `e:${section.key}`, text: section.note, error: true })
  }
  return out
})

function refresh() {
  sel.value = -1
  search.search(props.query, tab.value, ghState.value)
}
// Stale rows stay up while a reply loads; when it lands the highlight follows
// its row (or clears), so Enter never picks a row the user did not choose.
watch(options, (next, prev) => {
  if (sel.value < 0 || !prev) return
  const key = prev[sel.value]?.key
  sel.value = key ? next.findIndex((o) => o.key === key) : -1
})
watch(() => [props.query, JSON.stringify(props.scope)] as const, refresh, { immediate: true })
watch([tab, ghState], refresh)

function setTab(next: ReferenceTab) {
  if (tab.value !== next) list.value?.scrollTo?.({ top: 0, behavior: still ? 'auto' : 'smooth' })
  tab.value = next
}

function optionEl(index: number) {
  return content.value?.querySelector<HTMLElement>(`[data-ref-index="${index}"]`) ?? null
}

/** Top of an option inside the list content; each row wrapper is its own offset parent. */
function topInContent(el: HTMLElement) {
  let top = 0
  for (let n: HTMLElement | null = el; n && n !== content.value; n = n.offsetParent as HTMLElement | null) top += n.offsetTop
  return top
}

/** Keyboard moves keep the highlighted row inside the scroll box. */
function ensureVisible() {
  void nextTick(() => {
    const box = list.value
    const el = optionEl(sel.value)
    if (!box || !el) return
    const pad = 6
    const top = topInContent(el)
    const bottom = top + el.offsetHeight
    const behavior = still ? 'auto' : 'smooth'
    if (top < box.scrollTop + pad) box.scrollTo?.({ top: Math.max(0, top - pad), behavior })
    else if (bottom > box.scrollTop + box.clientHeight - pad) box.scrollTo?.({ top: bottom - box.clientHeight + pad, behavior })
  })
}

/** Arrow keys move the highlight; the first move selects the first or last row. */
function move(delta: number) {
  const n = options.value.length
  if (!n) return
  sel.value = sel.value < 0 ? (delta > 0 ? 0 : n - 1) : (sel.value + delta + n) % n
  ensureVisible()
}

function choose(index: number) {
  const option = options.value[index]
  if (!option) return
  if (option.type === 'action') {
    option.run()
    return
  }
  emit('pick', option.ref)
}

/** Enter: picks only when a row is highlighted; otherwise the caller sends. */
function confirm(): boolean {
  if (sel.value < 0 || !options.value[sel.value]) return false
  choose(sel.value)
  return true
}

function cycleTab(delta: number) {
  const i = TABS.indexOf(tab.value)
  setTab(TABS[(i + delta + TABS.length) % TABS.length]!)
}

function tabCount(key: ReferenceTab) {
  if (key === 'all') return ''
  const lane = key === 'solo' ? search.solo : search.github
  if (lane.skipped) return ''
  const taken = new Set(props.taken)
  if (key === 'solo') return String(lane.items.filter((i) => !taken.has(i.target)).length) + (lane.nextPage ? '+' : '')
  const kind = key === 'issue' ? 'github_issue' : 'github_pr'
  if (tab.value === key && typeof search.github.total === 'number') return String(search.github.total)
  return String(search.github.items.filter((i) => i.kind === kind && !taken.has(i.target)).length) + (search.github.nextPage ? '+' : '')
}

// Sliding pieces: the highlight block and the tab ink move by transform, and
// jump without a transition the first time they appear.
const highlight = ref({ on: false, jump: true, y: 0, h: 0 })
const ink = ref({ jump: true, x: 0, w: 0 })
const listHeight = ref<{ px: number; jump: boolean } | null>(null)

function placeHighlight() {
  const el = sel.value >= 0 ? optionEl(sel.value) : null
  if (!el) {
    highlight.value = { ...highlight.value, on: false, jump: true }
    return
  }
  highlight.value = { on: true, jump: !highlight.value.on, y: topInContent(el), h: el.offsetHeight }
}

function placeInk() {
  const el = tabbar.value?.querySelector<HTMLElement>('[data-tab-on="true"]')
  if (!el) return
  ink.value = { jump: !ink.value.w, x: el.offsetLeft, w: el.offsetWidth }
}

function measureList() {
  const el = content.value
  if (!el) return
  listHeight.value = { px: Math.min(el.offsetHeight + 8, MAX_LIST), jump: !listHeight.value }
}

watch([sel, rows], () => void nextTick(placeHighlight), { flush: 'post' })
watch(tab, () => void nextTick(placeInk), { flush: 'post' })

let resize: ResizeObserver | null = null
onMounted(() => {
  placeInk()
  measureList()
  if (typeof ResizeObserver === 'undefined' || !content.value) return
  resize = new ResizeObserver(() => {
    measureList()
    placeHighlight()
  })
  resize.observe(content.value)
})
onBeforeUnmount(() => resize?.disconnect())

/** New rows fade in a beat after each other. */
let staggered = 0
let staggerReset = 0
function onBeforeEnter(el: Element) {
  const node = el as HTMLElement
  node.style.transitionDelay = `${Math.min(staggered++, STAGGER_MAX) * STAGGER_MS}ms`
  clearTimeout(staggerReset)
  staggerReset = window.setTimeout(() => (staggered = 0))
}
function onAfterEnter(el: Element) {
  ;(el as HTMLElement).style.transitionDelay = ''
}

defineExpose({ move, confirm, cycleTab, setTab })
</script>

<template>
  <div
    class="ref-picker"
    :class="{ 'ref-picker--down': placement === 'down', 'ref-picker--still': still }"
    role="listbox"
    :aria-label="t('pages.solo.reference.pickerLabel')"
    data-testid="solo-reference-picker"
    @mousedown.prevent
  >
    <div class="ref-picker__tabs">
      <div ref="tabbar" class="ref-picker__tabbar">
        <span
          class="ref-picker__ink"
          :class="{ 'ref-picker__ink--jump': ink.jump }"
          :style="{ width: `${ink.w}px`, transform: `translateX(${ink.x}px)` }"
          aria-hidden="true"
        />
        <button
          v-for="key in TABS"
          :key="key"
          type="button"
          class="ref-picker__tab"
          :class="{ 'ref-picker__tab--on': tab === key }"
          :data-tab-on="tab === key"
          :data-testid="`solo-reference-tab-${key}`"
          @click="setTab(key)"
        >
          {{ t(`pages.solo.reference.tabs.${key}`) }}
          <span v-if="tabCount(key)" class="ref-picker__count">{{ tabCount(key) }}</span>
        </button>
      </div>
      <div v-if="tab !== 'solo'" class="ref-picker__seg" role="radiogroup" :aria-label="t('pages.solo.reference.stateLabel')">
        <button
          v-for="s in (['open', 'all'] as const)"
          :key="s"
          type="button"
          role="radio"
          :aria-checked="ghState === s"
          :class="{ 'ref-picker__seg--on': ghState === s }"
          @click="ghState = s"
        >{{ t(`pages.solo.reference.state.${s}`) }}</button>
      </div>
    </div>
    <div
      ref="list"
      class="ref-picker__list scroll-area"
      :class="{ 'ref-picker__list--stale': refreshing, 'ref-picker__list--jump': listHeight?.jump }"
      :style="listHeight ? { height: `${listHeight.px}px` } : undefined"
      :aria-busy="sections.some((s) => s.loading)"
    >
      <div ref="content" class="ref-picker__content">
        <div
          class="ref-picker__hl"
          :class="{ 'ref-picker__hl--on': highlight.on, 'ref-picker__hl--jump': highlight.jump }"
          :style="{ height: `${highlight.h}px`, transform: `translateY(${highlight.y}px)` }"
          aria-hidden="true"
          data-testid="solo-reference-highlight"
        />
        <TransitionGroup name="ref-row" :css="!still" @before-enter="onBeforeEnter" @after-enter="onAfterEnter">
          <div v-for="row in rows" :key="row.key" class="ref-picker__row" :data-row="row.key">
            <h5 v-if="row.type === 'heading'" class="ref-picker__heading">
              {{ row.title }}
              <span class="ref-picker__spin" :class="{ 'ref-picker__spin--on': row.loading }"><Icon name="spinner" :size="11" /></span>
            </h5>
            <div
              v-else-if="row.type === 'option' && row.option.type === 'item'"
              class="ref-picker__opt"
              role="option"
              :aria-selected="row.index === sel"
              :data-ref-index="row.index"
              :data-testid="`solo-reference-option-${row.option.ref.target}`"
              @mousemove="sel = row.index"
              @click="choose(row.index)"
            >
              <span class="ref-picker__icon" :class="row.option.tone" :data-icon="row.option.icon"><Icon :name="row.option.icon" :size="15" /></span>
              <span class="min-w-0 flex-1">
                <span class="block truncate text-[12.5px] text-txt">{{ row.option.title }}</span>
                <span class="block truncate text-[11px] text-txt3">{{ row.option.subtitle }}</span>
              </span>
            </div>
            <div
              v-else-if="row.type === 'option' && row.option.type === 'action'"
              class="ref-picker__opt ref-picker__opt--action"
              role="option"
              :aria-selected="row.index === sel"
              :data-ref-index="row.index"
              @mousemove="sel = row.index"
              @click="choose(row.index)"
            >
              <span class="ref-picker__icon"><Icon :name="row.option.key.startsWith('load:') ? 'chevron-down' : 'arrow-right'" :size="14" /></span>
              <span class="text-[12px] text-accent">{{ row.option.label }}</span>
            </div>
            <div v-else-if="row.type === 'skeleton'" class="ref-picker__skeleton" aria-hidden="true"><i /><i /></div>
            <p v-else-if="row.type === 'note'" class="ref-picker__note" :class="{ 'text-err': row.error }">{{ row.text }}</p>
          </div>
        </TransitionGroup>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ref-picker {
  --ease-out: cubic-bezier(0.16, 1, 0.3, 1);
  position: absolute;
  left: 0;
  right: 0;
  bottom: calc(100% + 8px);
  z-index: 40;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid rgb(var(--c-line-strong));
  border-radius: 12px;
  background: rgb(var(--c-elevated));
  box-shadow: 0 12px 32px rgb(0 0 0 / 0.18);
  transform-origin: bottom center;
  animation: ref-picker-in 200ms var(--ease-out);
  --ref-picker-from: 4px;
}
.ref-picker--down {
  top: calc(100% + 8px);
  bottom: auto;
  transform-origin: top center;
  --ref-picker-from: -4px;
}
@keyframes ref-picker-in {
  from {
    opacity: 0;
    transform: translateY(var(--ref-picker-from)) scale(0.985);
  }
}
/* Parents wrap the picker in <Transition name="ref-pop"> for the closing fade. */
.ref-pop-leave-active {
  pointer-events: none;
  transition: opacity 140ms ease, transform 180ms cubic-bezier(0.2, 0.8, 0.2, 1);
}
.ref-pop-leave-to {
  opacity: 0;
  transform: translateY(var(--ref-picker-from)) scale(0.985);
}
.ref-picker__tabs {
  display: flex;
  align-items: center;
  gap: 8px;
  border-bottom: 1px solid rgb(var(--c-line));
  padding: 6px 8px;
}
.ref-picker__tabbar {
  position: relative;
  display: flex;
  min-width: 0;
  flex: 1;
  align-items: center;
  gap: 2px;
}
.ref-picker__ink {
  position: absolute;
  top: 0;
  bottom: 0;
  left: 0;
  border-radius: 7px;
  background: rgb(var(--c-accent-dim));
  box-shadow: inset 0 0 0 1px rgb(var(--c-accent) / 0.4);
  transition: transform 240ms var(--ease-out), width 240ms var(--ease-out);
}
.ref-picker__tab {
  position: relative;
  z-index: 1;
  display: inline-flex;
  height: 24px;
  align-items: center;
  gap: 4px;
  border-radius: 7px;
  padding: 0 9px;
  color: rgb(var(--c-txt3));
  font-size: 12px;
  transition: color 180ms ease;
}
.ref-picker__tab:hover {
  color: rgb(var(--c-txt));
}
.ref-picker__tab--on {
  color: rgb(var(--c-txt));
  font-weight: 600;
}
.ref-picker__count {
  color: rgb(var(--c-txt3));
  font-size: 10.5px;
  font-variant-numeric: tabular-nums;
  font-weight: 400;
}
.ref-picker__seg {
  display: inline-flex;
  border-radius: 7px;
  background: rgb(var(--c-base));
  box-shadow: inset 0 0 0 1px rgb(var(--c-line));
  padding: 2px;
}
.ref-picker__seg button {
  height: 20px;
  border-radius: 5px;
  padding: 0 9px;
  color: rgb(var(--c-txt3));
  font-size: 11px;
}
.ref-picker__seg .ref-picker__seg--on {
  background: rgb(var(--c-overlay));
  color: rgb(var(--c-txt));
}
.ref-picker__list {
  max-height: 278px;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: 2px 6px 6px;
  transition: height 240ms var(--ease-out);
}
.ref-picker__content {
  position: relative;
  transition: opacity 160ms ease;
}
/* Delay the dim so replies that land quickly never flicker. */
.ref-picker__list--stale .ref-picker__content {
  opacity: 0.6;
  transition-delay: 180ms;
}
.ref-picker__row {
  position: relative;
  z-index: 1;
}
.ref-row-move {
  transition: transform 240ms var(--ease-out);
}
.ref-row-enter-active {
  transition: opacity 240ms var(--ease-out), transform 240ms var(--ease-out);
}
.ref-row-enter-from {
  opacity: 0;
  transform: translateY(3px);
}
/* Leaving rows drop out of the flow where they stood, so the rest can move up. */
.ref-row-leave-active {
  position: absolute;
  left: 0;
  right: 0;
  pointer-events: none;
  transition: opacity 140ms ease;
}
.ref-row-leave-to {
  opacity: 0;
}
.ref-picker__hl {
  position: absolute;
  z-index: 0;
  top: 0;
  left: 0;
  right: 0;
  border-radius: 8px;
  background: rgb(var(--c-accent) / 0.08);
  opacity: 0;
  pointer-events: none;
  transition: transform 160ms var(--ease-out), height 160ms var(--ease-out), opacity 120ms ease;
}
.ref-picker__hl--on {
  opacity: 1;
}
.ref-picker__hl--jump,
.ref-picker__ink--jump,
.ref-picker__list--jump {
  transition-property: opacity;
}
.ref-picker__spin {
  display: inline-flex;
  margin-left: auto;
  opacity: 0;
  transition: opacity 160ms ease;
}
.ref-picker__spin :deep(svg) {
  animation: ref-picker-spin 0.9s linear infinite;
}
.ref-picker__spin--on {
  opacity: 1;
  transition-delay: 180ms;
}
@keyframes ref-picker-spin {
  to {
    transform: rotate(360deg);
  }
}
.ref-picker__skeleton {
  animation: ref-picker-fade 160ms ease 180ms backwards;
}
@keyframes ref-picker-fade {
  from {
    opacity: 0;
  }
}
.ref-picker__heading {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0;
  padding: 8px 6px 3px;
  color: rgb(var(--c-txt3));
  font-size: 11px;
  font-weight: 600;
}
.ref-picker__opt {
  display: flex;
  cursor: pointer;
  align-items: center;
  gap: 9px;
  border-radius: 8px;
  padding: 6px 7px;
}
.ref-picker__opt--action {
  padding-block: 4px;
}
.ref-picker__icon {
  display: grid;
  width: 26px;
  height: 26px;
  flex: none;
  place-items: center;
  border-radius: 7px;
  background: rgb(var(--c-overlay));
  color: rgb(var(--c-txt2));
}
.ref-picker__icon.ref-tone--open {
  color: rgb(var(--c-ok));
}
.ref-picker__icon.ref-tone--merged {
  color: rgb(var(--c-accent));
}
.ref-picker__icon.ref-tone--closed {
  color: rgb(var(--c-txt3));
}
.ref-picker__opt--action .ref-picker__icon {
  background: none;
  color: rgb(var(--c-accent));
}
.ref-picker__note {
  margin: 0;
  padding: 6px 8px;
  color: rgb(var(--c-txt3));
  font-size: 11.5px;
}
.ref-picker__skeleton {
  display: grid;
  gap: 6px;
  padding: 4px 7px 6px;
}
.ref-picker__skeleton i {
  display: block;
  height: 22px;
  border-radius: 6px;
  background: linear-gradient(90deg, rgb(var(--c-overlay)) 0%, rgb(var(--c-overlay) / 0.45) 50%, rgb(var(--c-overlay)) 100%) 0 0 / 200% 100%;
  animation: ref-picker-shimmer 1.2s linear infinite;
}
.ref-picker__skeleton i:last-child {
  width: 70%;
}
@keyframes ref-picker-shimmer {
  to {
    background-position: -200% 0;
  }
}
.ref-picker--still,
.ref-picker--still * {
  animation: none !important;
  transition: none !important;
}
@media (prefers-reduced-motion: reduce) {
  .ref-picker,
  .ref-picker * {
    animation: none !important;
    transition: none !important;
  }
}
</style>
