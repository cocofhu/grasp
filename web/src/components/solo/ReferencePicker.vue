<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import type { GitHubReferenceCandidate, MessageReference, SoloReferenceCandidate } from '@/lib/api/apiTypes'
import { useReferenceSearch, type GitHubStateFilter, type ReferenceScope, type ReferenceTab } from '@/lib/solo/useReferenceSearch'
import { referenceIcon } from '@/lib/solo/referenceDisplay'

const props = defineProps<{
  /** An open session, or the launch composer's agent and projects. */
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
const search = useReferenceSearch(computed(() => props.scope))
const TABS: ReferenceTab[] = ['all', 'solo', 'issue', 'pr']
const ALL_LIMIT = 5

type Option =
  | { type: 'item'; key: string; ref: MessageReference; icon: string; tone: string; title: string; subtitle: string }
  | { type: 'action'; key: string; label: string; run: () => void }
type Section = { key: string; title: string; options: Option[]; loading: boolean; note: string; error: boolean }

function soloOption(item: SoloReferenceCandidate): Option {
  const when = item.lastActiveAt ? new Date(item.lastActiveAt).toLocaleString() : ''
  return {
    type: 'item', key: `solo:${item.target}`, icon: 'chat', tone: '',
    ref: { kind: 'solo', target: item.target, title: item.title },
    title: item.title || t('pages.solo.untitled'),
    subtitle: [t(`pages.solo.reference.sessionStatus.${item.status}`, item.status), when].filter(Boolean).join(' · '),
  }
}

function githubOption(item: GitHubReferenceCandidate): Option {
  const ref: MessageReference = { kind: item.kind, target: item.target, title: item.title, state: item.state, url: item.url }
  return {
    type: 'item', key: `gh:${item.target}`, ref, icon: referenceIcon(ref), tone: `ref-tone--${item.state}`,
    title: `#${item.number} ${item.title}`,
    subtitle: t('pages.solo.reference.githubSubtitle', { repo: item.repo, state: item.state, n: item.comments }),
  }
}

function laneNote(lane: { loading: boolean; error: string; hint: string; skipped: boolean }, count: number) {
  if (lane.error) return lane.error
  if (lane.hint && !count) return lane.hint
  if (!lane.loading && !count) return t('pages.solo.reference.noMatch')
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
    if (tab.value === 'all' && !rows.length && !lane.loading && !lane.error && !lane.hint) return
    out.push({ key, title, options: rows, loading: lane.loading, note: laneNote(lane, options.length), error: !!lane.error })
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
const empty = computed(() => !props.full && !sections.value.length)

function refresh() {
  sel.value = -1
  search.search(props.query, tab.value, ghState.value)
}
watch(() => [props.query, JSON.stringify(props.scope)] as const, refresh, { immediate: true })
watch([tab, ghState], refresh)

function setTab(next: ReferenceTab) {
  tab.value = next
}

function scrollToSelected() {
  void nextTick(() => list.value?.querySelector<HTMLElement>('[data-ref-selected="true"]')?.scrollIntoView({ block: 'nearest' }))
}

/** Arrow keys move the highlight; the first move selects the first or last row. */
function move(delta: number) {
  const n = options.value.length
  if (!n) return
  sel.value = sel.value < 0 ? (delta > 0 ? 0 : n - 1) : (sel.value + delta + n) % n
  scrollToSelected()
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

function optionIndex(option: Option) {
  return options.value.indexOf(option)
}

defineExpose({ move, confirm, cycleTab, setTab })
</script>

<template>
  <div class="ref-picker" :class="{ 'ref-picker--down': placement === 'down' }" role="listbox" :aria-label="t('pages.solo.reference.pickerLabel')" data-testid="solo-reference-picker" @mousedown.prevent>
    <div class="ref-picker__tabs">
      <div class="flex min-w-0 flex-1 items-center gap-0.5">
        <button
          v-for="key in TABS"
          :key="key"
          type="button"
          class="ref-picker__tab"
          :class="{ 'ref-picker__tab--on': tab === key }"
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
    <div ref="list" class="ref-picker__list scroll-area">
      <p v-if="full" class="ref-picker__note">{{ t('pages.solo.reference.full', { n: 5 }) }}</p>
      <p v-else-if="empty" class="ref-picker__note">{{ t('pages.solo.reference.noMatch') }}</p>
      <template v-for="section in sections" :key="section.key">
        <h5 class="ref-picker__heading">
          {{ section.title }}
          <Icon v-if="section.loading" name="spinner" :size="11" class="ml-auto" />
        </h5>
        <template v-for="option in section.options" :key="option.key">
          <div
            v-if="option.type === 'item'"
            class="ref-picker__opt"
            role="option"
            :aria-selected="optionIndex(option) === sel"
            :data-ref-selected="optionIndex(option) === sel"
            :data-testid="`solo-reference-option-${option.ref.target}`"
            @mousemove="sel = optionIndex(option)"
            @click="choose(optionIndex(option))"
          >
            <span class="ref-picker__icon" :class="option.tone"><Icon :name="option.icon" :size="16" /></span>
            <span class="min-w-0 flex-1">
              <span class="block truncate text-[12.5px] text-txt">{{ option.title }}</span>
              <span class="block truncate text-[11px] text-txt3">{{ option.subtitle }}</span>
            </span>
          </div>
          <div
            v-else
            class="ref-picker__opt ref-picker__opt--action"
            role="option"
            :aria-selected="optionIndex(option) === sel"
            :data-ref-selected="optionIndex(option) === sel"
            @mousemove="sel = optionIndex(option)"
            @click="choose(optionIndex(option))"
          >
            <span class="ref-picker__icon"><Icon :name="option.key.startsWith('load:') ? 'chevron-down' : 'arrow-right'" :size="14" /></span>
            <span class="text-[12px] text-accent">{{ option.label }}</span>
          </div>
        </template>
        <div v-if="section.loading && !section.options.length" class="ref-picker__skeleton" aria-hidden="true"><i /><i /></div>
        <p v-else-if="section.note && !section.options.length" class="ref-picker__note" :class="{ 'text-err': section.error }">{{ section.note }}</p>
        <p v-else-if="section.error" class="ref-picker__note text-err">{{ section.note }}</p>
      </template>
    </div>
  </div>
</template>

<style scoped>
.ref-picker {
  position: absolute;
  left: 0;
  right: 0;
  bottom: calc(100% + 8px);
  z-index: 40;
  display: flex;
  max-height: 340px;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid rgb(var(--c-line-strong));
  border-radius: 12px;
  background: rgb(var(--c-elevated));
  box-shadow: 0 12px 32px rgb(0 0 0 / 0.18);
}
.ref-picker--down {
  top: calc(100% + 8px);
  bottom: auto;
}
.ref-picker__tabs {
  display: flex;
  align-items: center;
  gap: 8px;
  border-bottom: 1px solid rgb(var(--c-line));
  padding: 6px 8px;
}
.ref-picker__tab {
  display: inline-flex;
  height: 24px;
  align-items: center;
  gap: 4px;
  border-radius: 6px;
  padding: 0 9px;
  color: rgb(var(--c-txt2));
  font-size: 12px;
}
.ref-picker__tab:hover {
  background: rgb(var(--c-overlay));
}
.ref-picker__tab--on {
  background: rgb(var(--c-accent-dim));
  color: rgb(var(--c-txt));
  box-shadow: inset 0 0 0 1px rgb(var(--c-accent) / 0.4);
}
.ref-picker__count {
  color: rgb(var(--c-txt3));
  font-size: 10.5px;
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
  min-height: 0;
  flex: 1;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: 2px 6px 6px;
}
.ref-picker__heading {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 6px 4px 2px;
  color: rgb(var(--c-txt3));
  font-size: 11px;
  font-weight: 500;
}
.ref-picker__opt {
  display: flex;
  cursor: pointer;
  align-items: center;
  gap: 9px;
  border-radius: 8px;
  padding: 6px 8px;
}
.ref-picker__opt--action {
  padding-block: 4px;
}
.ref-picker__opt[aria-selected='true'] {
  background: rgb(var(--c-accent) / 0.08);
}
.ref-picker__icon {
  display: grid;
  width: 24px;
  height: 24px;
  flex: none;
  place-items: center;
  color: rgb(var(--c-txt2));
}
.ref-picker__note {
  padding: 6px 8px;
  color: rgb(var(--c-txt3));
  font-size: 11.5px;
}
.ref-picker__skeleton {
  display: grid;
  gap: 8px;
  padding: 6px 8px;
}
.ref-picker__skeleton i {
  display: block;
  height: 22px;
  border-radius: 6px;
  background: rgb(var(--c-overlay));
}
.ref-tone--open {
  color: rgb(var(--c-ok));
}
.ref-tone--merged,
.ref-tone--closed {
  color: rgb(var(--c-accent));
}
</style>
