<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import AnnotateBtn from './product/AnnotateBtn.vue'

export type ProposalItem = {
  id?: string
  title?: string
  summary?: string
  pros?: string[]
  cons?: string[]
  tradeoffs?: string
  effort?: string
  risk?: string
  recommended?: boolean
}
export type ProposalsDoc = {
  context?: string
  decision_drivers?: string[]
  proposals?: ProposalItem[]
}

const props = defineProps<{ doc: ProposalsDoc; resolvedId?: string | null; disabled?: boolean; readonly?: boolean }>()
const emit = defineEmits<{ (e: 'select', id: string): void }>()

const { t } = useI18n()
const proposals = computed(() => props.doc.proposals || [])
// Selection is only meaningful with ≥2 candidates; n<2 is read-only info
// (matches Approve “no pseudo-choice” and proposal_select single-candidate auto-adopt).
const needsChoice = computed(() => proposals.value.length >= 2)
const headerTitle = computed(() =>
  needsChoice.value ? t('pages.proposalSelect.title') : t('pages.proposalSelect.titleReadonly'),
)
const headerSubtitle = computed(() => {
  const n = proposals.value.length
  if (n >= 2) return t('pages.proposalSelect.subtitle', { n })
  if (n === 1) return t('pages.proposalSelect.subtitleSingle')
  return t('pages.proposalSelect.subtitleEmpty')
})

// One proposal per window; `current` is the visible index and `dir` drives the
// slide direction of the switch animation. Seed the view on the resolved choice
// (if any), else the recommended proposal, else the first.
const current = ref(0)
const dir = ref<'next' | 'prev'>('next')
watch(
  [proposals, () => props.resolvedId],
  () => {
    const list = proposals.value
    if (props.resolvedId) {
      const ri = list.findIndex((p, i) => pid(p, i) === props.resolvedId)
      if (ri >= 0) {
        current.value = ri
        return
      }
    }
    const rec = list.findIndex((p) => p.recommended)
    current.value = Math.min(Math.max(rec >= 0 ? rec : 0, 0), Math.max(list.length - 1, 0))
  },
  { immediate: true },
)

const cur = computed<ProposalItem | undefined>(() => proposals.value[current.value])

// Effective id: fall back to a positional id (p1, p2, …) when a proposal has no
// id. Older proposals.json artifacts were written without ids; without this the
// select button emits an empty id and the click does nothing / never resolves.
// The backend applies the same positional backfill, so the ids line up.
function pid(p: ProposalItem | undefined, i: number): string {
  const id = p?.id?.trim()
  return id ? id : `p${i + 1}`
}
const curId = computed(() => pid(cur.value, current.value))

function go(to: number) {
  const n = proposals.value.length
  if (to < 0 || to >= n || to === current.value) return
  dir.value = to > current.value ? 'next' : 'prev'
  current.value = to
}
function prev() {
  go(current.value - 1)
}
function next() {
  go(current.value + 1)
}

// effort / risk are normalized to low|medium|high by the backend; map to a
// localized label + severity color (low = good, high = risky).
const LEVEL: Record<string, { labelKey: string; cls: string }> = {
  low: { labelKey: 'pages.proposalSelect.level.low', cls: 'bg-ok/15 text-ok' },
  medium: { labelKey: 'pages.proposalSelect.level.medium', cls: 'bg-warn/15 text-warn' },
  high: { labelKey: 'pages.proposalSelect.level.high', cls: 'bg-err/15 text-err' },
}
function level(v?: string) {
  if (!v || !LEVEL[v]) return undefined
  return { label: t(LEVEL[v].labelKey), cls: LEVEL[v].cls }
}

function pick() {
  if (!cur.value || props.disabled || props.resolvedId || !needsChoice.value) return
  emit('select', curId.value)
}
</script>

<template>
  <div>
    <div class="mb-3 flex items-center gap-2">
      <div class="flex h-7 w-7 items-center justify-center rounded-md bg-accent/15 text-accent-2">
        <Icon name="gate" :size="15" />
      </div>
      <div class="min-w-0 flex-1">
        <div class="text-sm font-semibold text-txt">{{ headerTitle }}</div>
        <div class="text-[11px] text-txt3">{{ headerSubtitle }}</div>
      </div>
    </div>

    <!-- decision context (always shown above the carousel) -->
    <div v-if="doc.context" class="mb-3 rounded-lg border border-line bg-base/40 p-3">
      <div class="mb-1 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.proposalSelect.context') }}</div>
      <div class="text-[12px] leading-relaxed text-txt2">{{ doc.context }}</div>
      <div v-if="doc.decision_drivers?.length" class="mt-2 flex flex-wrap gap-1.5">
        <span
          v-for="(d, i) in doc.decision_drivers"
          :key="i"
          class="rounded-full bg-base px-2 py-0.5 text-[10px] text-txt3"
        >{{ d }}</span>
      </div>
    </div>

    <!-- carousel navigation: prev / dots / next. Sticks to the top of the
         scroll area so a long proposal can still be paged without scrolling
         back up; stays inert (non-bounded parents) in the readonly artifact view. -->
    <div v-if="proposals.length" class="sticky top-0 z-10 mb-2.5 flex items-center gap-2 border-b border-line bg-surface py-2 max-md:py-2.5">
      <button
        class="flex shrink-0 items-center justify-center rounded-md border border-line text-txt2 transition hover:bg-elevated hover:text-txt disabled:opacity-40 disabled:hover:bg-transparent max-md:h-11 max-md:w-11 h-7 w-7"
        :disabled="current === 0"
        :title="t('pages.proposalSelect.prev')"
        @click="prev"
      >
        <Icon name="chevron-right" :size="15" class="rotate-180" />
      </button>

      <div class="flex flex-1 items-center justify-center gap-1.5">
        <button
          v-for="(p, i) in proposals"
          :key="pid(p, i)"
          class="h-1.5 rounded-full transition-all"
          :class="[
            i === current ? 'w-5' : 'w-1.5',
            resolvedId === pid(p, i)
              ? 'bg-ok'
              : p.recommended
                ? i === current ? 'bg-accent' : 'bg-accent/40'
                : i === current ? 'bg-txt2' : 'bg-line',
          ]"
          :title="p.title"
          @click="go(i)"
        />
      </div>

      <span class="shrink-0 text-[11px] tabular-nums text-txt3">{{ current + 1 }} / {{ proposals.length }}</span>
      <button
        class="flex shrink-0 items-center justify-center rounded-md border border-line text-txt2 transition hover:bg-elevated hover:text-txt disabled:opacity-40 disabled:hover:bg-transparent max-md:h-11 max-md:w-11 h-7 w-7"
        :disabled="current >= proposals.length - 1"
        :title="t('pages.proposalSelect.next')"
        @click="next"
      >
        <Icon name="chevron-right" :size="15" />
      </button>
    </div>

    <!-- single proposal window (animated switch) -->
    <div class="relative overflow-hidden">
      <Transition :name="dir === 'next' ? 'slide-next' : 'slide-prev'" mode="out-in">
        <div
          v-if="cur"
          :key="curId"
          class="group rounded-lg border p-3.5"
          :class="[
            resolvedId === curId ? 'border-ok/60 bg-ok/5' : cur.recommended ? 'border-accent/50 bg-accent-dim/30' : 'border-line bg-base/40',
          ]"
        >
          <div class="flex flex-wrap items-center gap-2">
            <code class="shrink-0 rounded bg-base px-1.5 py-0.5 font-mono text-[10px] text-txt3">{{ curId }}</code>
            <span class="text-[13px] font-semibold text-txt">{{ cur.title }}</span>
            <AnnotateBtn :json-path="`proposals[${curId}]`" :label="cur.title || curId" />
            <span v-if="cur.recommended" class="inline-flex items-center gap-1 rounded-full bg-accent/15 px-2 py-0.5 text-[10px] font-medium text-accent-2">
              <Icon name="check" :size="11" />{{ t('pages.proposalSelect.recommended') }}
            </span>
            <span v-if="resolvedId === curId" class="inline-flex items-center gap-1 rounded-full bg-ok/15 px-2 py-0.5 text-[10px] font-medium text-ok">
              <Icon name="check" :size="11" />{{ t('pages.proposalSelect.selected') }}
            </span>
            <div class="ml-auto flex shrink-0 items-center gap-1.5">
              <span v-if="level(cur.effort)" class="rounded-md px-1.5 py-0.5 text-[10px] font-medium" :class="level(cur.effort)!.cls">{{ t('pages.proposalSelect.effort', { level: level(cur.effort)!.label }) }}</span>
              <span v-if="level(cur.risk)" class="rounded-md px-1.5 py-0.5 text-[10px] font-medium" :class="level(cur.risk)!.cls">{{ t('pages.proposalSelect.risk', { level: level(cur.risk)!.label }) }}</span>
            </div>
          </div>

          <div v-if="cur.summary" class="mt-1.5 text-[12px] leading-relaxed text-txt2">{{ cur.summary }}</div>

          <div v-if="cur.pros?.length || cur.cons?.length" class="mt-2.5 grid gap-x-4 gap-y-1 sm:grid-cols-2">
            <div v-if="cur.pros?.length" class="space-y-1">
              <div v-for="(pro, i) in cur.pros" :key="'pro' + i" class="flex items-start gap-1.5 text-[11px] leading-relaxed text-txt2">
                <Icon name="check" :size="13" class="mt-0.5 shrink-0 text-ok" /><span>{{ pro }}</span>
              </div>
            </div>
            <div v-if="cur.cons?.length" class="space-y-1">
              <div v-for="(con, i) in cur.cons" :key="'con' + i" class="flex items-start gap-1.5 text-[11px] leading-relaxed text-txt2">
                <span class="mt-0.5 shrink-0 text-warn">⚠</span><span>{{ con }}</span>
              </div>
            </div>
          </div>

          <div v-if="cur.tradeoffs" class="mt-2 rounded-md bg-base/60 px-2.5 py-1.5 text-[11px] leading-relaxed text-txt3">
            <span class="font-medium text-txt2">{{ t('pages.proposalSelect.tradeoffs') }}</span>{{ cur.tradeoffs }}
          </div>

          <div v-if="!resolvedId && !readonly && needsChoice" class="mt-3 flex justify-end max-md:justify-stretch">
            <button
              class="inline-flex items-center justify-center gap-1.5 rounded-md px-3.5 text-sm font-medium transition md:py-2 max-md:min-h-[44px] max-md:w-full"
              :class="cur.recommended ? 'bg-accent text-white hover:bg-accent-hover' : 'bg-ok/15 text-ok hover:bg-ok/25'"
            :disabled="disabled"
            @click="pick()"
            >
              <Icon name="check" :size="16" />
              {{ t('pages.proposalSelect.pickThis') }}
            </button>
          </div>
        </div>
      </Transition>
    </div>
  </div>
</template>

<style scoped>
.slide-next-enter-active,
.slide-next-leave-active,
.slide-prev-enter-active,
.slide-prev-leave-active {
  transition: opacity 0.22s ease, transform 0.22s ease;
}
.slide-next-enter-from {
  opacity: 0;
  transform: translateX(28px);
}
.slide-next-leave-to {
  opacity: 0;
  transform: translateX(-28px);
}
.slide-prev-enter-from {
  opacity: 0;
  transform: translateX(-28px);
}
.slide-prev-leave-to {
  opacity: 0;
  transform: translateX(28px);
}
</style>
