<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppModal from '@/components/ui/AppModal.vue'
import Icon from '@/components/ui/Icon.vue'
import AnnotateBtn from './AnnotateBtn.vue'
import { useTestScreenshotLoad, type TestScreenshotInput } from '@/lib/composables/useTestScreenshotLoad'
import type { Artifact } from '@/lib/shared/types'

export type TestCase = { id?: string; name?: string; status?: string; detail?: string }
export type TestDefect = { id?: string; title?: string; severity?: string; detail?: string; status?: string }
export type TestScreenshot = TestScreenshotInput
export type TestResultDoc = {
  summary?: string
  cases?: TestCase[]
  defects?: TestDefect[]
  variances?: string
  assessment?: string
  screenshots?: TestScreenshot[]
  passed?: number
  failed?: number
  skipped?: number
}

const props = defineProps<{
  doc: TestResultDoc
  accent?: string
  runId?: string
  artifacts?: Artifact[]
  /** Live run status; omitted/empty ⇒ treat as terminal (allow final error). */
  runStatus?: string
}>()

const { t } = useI18n()

const accent = computed(() => props.accent || 'var(--accent, #6366f1)')
const shots = computed(() => props.doc.screenshots || [])
const artifactsRef = computed(() => props.artifacts || [])
const runStatusRef = computed(() => props.runStatus)
const { states: shotStates, successIndices } = useTestScreenshotLoad(shots, artifactsRef, runStatusRef)

function shotKey(s: TestScreenshot, i: number): string {
  return s.artifact?.trim() || `shot-${i}`
}

function shotCaption(s: TestScreenshot, i: number): string {
  return s.caption || t('pages.product.testResult.screenshotAlt', { n: i + 1 })
}

const galleryIndices = computed(() => successIndices())
const lightboxGalleryPos = ref<number | null>(null)

const lightboxShotIndex = computed(() => {
  if (lightboxGalleryPos.value == null) return null
  return galleryIndices.value[lightboxGalleryPos.value] ?? null
})

const lightboxState = computed(() => {
  const idx = lightboxShotIndex.value
  return idx != null ? shotStates.value[idx] ?? null : null
})

function openLightbox(i: number) {
  const st = shotStates.value[i]
  if (st?.status !== 'success') return
  const pos = galleryIndices.value.indexOf(i)
  if (pos >= 0) lightboxGalleryPos.value = pos
}

function closeLightbox() {
  lightboxGalleryPos.value = null
}

function step(delta: number) {
  const n = galleryIndices.value.length
  if (lightboxGalleryPos.value == null || n === 0) return
  lightboxGalleryPos.value = (lightboxGalleryPos.value + delta + n) % n
}

function lightboxSrc(): string {
  const st = lightboxState.value
  if (st?.status !== 'success') return ''
  return st.src
}

function onKey(e: KeyboardEvent) {
  if (lightboxGalleryPos.value == null) return
  if (e.key === 'ArrowLeft') step(-1)
  else if (e.key === 'ArrowRight') step(1)
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))

const total = computed(() => (props.doc.passed || 0) + (props.doc.failed || 0) + (props.doc.skipped || 0))
const passPct = computed(() => (total.value ? Math.round(((props.doc.passed || 0) / total.value) * 100) : 0))
const allPassed = computed(() => total.value > 0 && (props.doc.failed || 0) === 0)

const REPO_PREFIX = /^\[([^\]]+)\]\s*(.*)$/

type CaseGroup = { repo: string | null; cases: TestCase[] }

function parseCaseRepo(name?: string): { repo: string | null; displayName: string } {
  if (!name) return { repo: null, displayName: '' }
  const m = name.match(REPO_PREFIX)
  if (!m) return { repo: null, displayName: name }
  const display = m[2].trim() || name
  return { repo: m[1], displayName: display }
}

const caseGroups = computed((): CaseGroup[] => {
  // truthy non-array (e.g. paginated object) must not enter for…of
  const cases = Array.isArray(props.doc.cases) ? props.doc.cases : []
  if (!cases.length) return []
  const hasRepoPrefix = cases.some(c => REPO_PREFIX.test(c.name || ''))
  if (!hasRepoPrefix) return [{ repo: null, cases }]
  const map = new Map<string, TestCase[]>()
  const order: string[] = []
  for (const c of cases) {
    const { repo, displayName } = parseCaseRepo(c.name)
    const key = repo || ''
    if (!map.has(key)) {
      map.set(key, [])
      order.push(key)
    }
    map.get(key)!.push({ ...c, name: displayName })
  }
  return order.map(key => ({ repo: key || null, cases: map.get(key)! }))
})

function groupStats(cases: TestCase[]) {
  let passed = 0
  let failed = 0
  let skipped = 0
  for (const c of cases) {
    const s = (c.status || 'failed').toLowerCase()
    if (s === 'passed' || s === 'pass' || s === 'ok') passed++
    else if (s === 'skipped' || s === 'skip') skipped++
    else failed++
  }
  return { passed, failed, skipped, total: passed + failed + skipped }
}

const CASE: Record<string, { icon: string; cls: string }> = {
  passed: { icon: '✓', cls: 'text-ok' },
  failed: { icon: '✕', cls: 'text-err' },
  skipped: { icon: '⏭', cls: 'text-txt3' },
}
function cs(s?: string) {
  return CASE[s || 'failed'] || CASE.failed
}
const SEV: Record<string, string> = {
  critical: 'bg-err/20 text-err',
  high: 'bg-err/15 text-err',
  medium: 'bg-warn/15 text-warn',
  low: 'bg-info/15 text-info',
}
</script>

<template>
  <div class="space-y-4">
    <div v-if="doc.summary" class="group flex items-start gap-1 rounded-lg border border-line bg-base/40 p-3 text-[12px] leading-relaxed text-txt2">
      <span class="min-w-0 flex-1" data-json-path="summary" data-label="概述">{{ doc.summary }}</span>
      <AnnotateBtn json-path="summary" label="概述" />
    </div>

    <section v-if="total">
      <div class="mb-2 flex items-center gap-2">
        <span
          class="rounded-md px-2 py-0.5 text-[11px] font-semibold"
          :class="allPassed ? 'bg-ok/15 text-ok' : 'bg-err/15 text-err'"
        >{{ allPassed ? t('pages.product.testResult.allPassed') : t('pages.product.testResult.failedCount', { n: doc.failed || 0 }) }}</span>
        <span class="text-[11px] text-txt3">{{ t('pages.product.testResult.passRate', { pct: passPct, total }) }}</span>
      </div>
      <div class="mb-2 flex h-1.5 w-full overflow-hidden rounded-full bg-base">
        <div class="h-full bg-ok" :style="{ width: ((doc.passed || 0) / total) * 100 + '%' }" />
        <div class="h-full bg-err" :style="{ width: ((doc.failed || 0) / total) * 100 + '%' }" />
        <div class="h-full bg-line-strong" :style="{ width: ((doc.skipped || 0) / total) * 100 + '%' }" />
      </div>
      <div class="flex gap-2">
        <div class="flex-1 rounded-lg border border-line bg-base/40 p-2 text-center">
          <div class="text-[15px] font-semibold text-ok">{{ doc.passed || 0 }}</div>
          <div class="text-[10px] text-txt3">{{ t('pages.product.testResult.passed') }}</div>
        </div>
        <div class="flex-1 rounded-lg border border-line bg-base/40 p-2 text-center">
          <div class="text-[15px] font-semibold text-err">{{ doc.failed || 0 }}</div>
          <div class="text-[10px] text-txt3">{{ t('pages.product.testResult.failed') }}</div>
        </div>
        <div class="flex-1 rounded-lg border border-line bg-base/40 p-2 text-center">
          <div class="text-[15px] font-semibold text-txt3">{{ doc.skipped || 0 }}</div>
          <div class="text-[10px] text-txt3">{{ t('pages.product.testResult.skipped') }}</div>
        </div>
      </div>
    </section>

    <section v-if="caseGroups.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.product.testResult.cases') }}</div>
      <div class="space-y-3">
        <div v-for="(g, gi) in caseGroups" :key="g.repo || `flat-${gi}`">
          <div v-if="g.repo" class="mb-1 flex items-center gap-2 text-[11px]">
            <span class="rounded bg-accent/10 px-1.5 py-0.5 font-mono text-accent">{{ g.repo }}</span>
            <span class="text-txt3">
              {{ t('pages.product.testResult.repoStats', groupStats(g.cases)) }}
            </span>
          </div>
          <div class="space-y-1">
            <div v-for="(c, i) in g.cases" :key="c.id || `${gi}-${i}`" class="group flex items-start gap-2 rounded-md bg-base/40 px-2 py-1.5 text-[11px]">
              <span class="mt-0.5 shrink-0 font-bold" :class="cs(c.status).cls">{{ cs(c.status).icon }}</span>
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-1 text-txt2">
                  <span
                    class="min-w-0 flex-1"
                    :data-json-path="`cases[${c.id || `${gi}-${i}`}]`"
                    :data-label="c.name || `用例 ${i + 1}`"
                  >{{ c.name }}</span>
                  <AnnotateBtn :json-path="`cases[${c.id || `${gi}-${i}`}]`" :label="c.name || `用例 ${i + 1}`" />
                </div>
                <div
                  v-if="c.detail"
                  class="mt-0.5 text-[10px] leading-4 text-txt3"
                  :data-json-path="`cases[${c.id || `${gi}-${i}`}].detail`"
                  :data-label="`${c.name || i} 详情`"
                >{{ c.detail }}</div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <section v-if="doc.defects?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-err">{{ t('pages.product.testResult.defects', { n: doc.defects.length }) }}</div>
      <div class="space-y-2">
        <div v-for="(d, i) in doc.defects" :key="d.id || i" class="group rounded-lg border border-line bg-base/40 p-2.5">
          <div class="flex flex-wrap items-center gap-2">
            <span v-if="d.severity" class="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium uppercase" :class="SEV[d.severity] || 'bg-base text-txt3'">{{ d.severity }}</span>
            <span
              class="text-[12px] font-medium text-txt"
              :data-json-path="`defects[${d.id || i}]`"
              :data-label="d.title || `缺陷 ${i + 1}`"
            >{{ d.title }}</span>
            <AnnotateBtn :json-path="`defects[${d.id || i}]`" :label="d.title || `缺陷 ${i + 1}`" />
            <span v-if="d.status" class="ml-auto shrink-0 text-[10px] text-txt3">{{ d.status }}</span>
          </div>
          <div
            v-if="d.detail"
            class="mt-1 text-[11px] leading-relaxed text-txt3"
            :data-json-path="`defects[${d.id || i}].detail`"
            :data-label="`${d.id || i} 详情`"
          >{{ d.detail }}</div>
        </div>
      </div>
    </section>

    <section v-if="doc.variances" class="group rounded-lg border border-warn/30 bg-warn/5 p-3">
      <div class="mb-1 flex items-center gap-1 text-[10px] font-semibold uppercase tracking-wider text-warn">
        {{ t('pages.product.testResult.variances') }}
        <AnnotateBtn json-path="variances" :label="t('pages.product.testResult.variances')" />
      </div>
      <div
        class="text-[12px] leading-relaxed text-txt2"
        data-json-path="variances"
        :data-label="t('pages.product.testResult.variances')"
      >{{ doc.variances }}</div>
    </section>

    <section v-if="doc.assessment" class="group rounded-lg border border-line bg-base/40 p-3">
      <div class="mb-1 flex items-center gap-1 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        {{ t('pages.product.testResult.assessment') }}
        <AnnotateBtn json-path="assessment" :label="t('pages.product.testResult.assessment')" />
      </div>
      <div
        class="text-[12px] leading-relaxed text-txt2"
        data-json-path="assessment"
        :data-label="t('pages.product.testResult.assessment')"
      >{{ doc.assessment }}</div>
    </section>

    <section v-if="shots.length">
      <div class="mb-2 flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        <Icon name="monitor" :size="12" :style="{ color: accent }" />
        {{ t('pages.product.testResult.screenshots', { n: shots.length }) }}
      </div>
      <div class="grid grid-cols-2 gap-3 sm:grid-cols-3">
        <figure v-for="(s, i) in shots" :key="shotKey(s, i)" class="flex min-w-0 flex-col gap-1.5">
          <!-- loading -->
          <div
            v-if="shotStates[i]?.status === 'loading'"
            class="relative aspect-[4/3] w-full cursor-wait overflow-hidden rounded-lg border border-line bg-base"
          >
            <div class="shot-shimmer absolute inset-0" />
            <div class="absolute inset-0 flex flex-col items-center justify-center gap-1.5 text-[10px] text-txt3">
              <div class="h-5 w-5 animate-spin rounded-full border-2 border-line border-t-accent" />
              <span>{{ t('pages.product.testResult.screenshotLoading', { artifact: shotStates[i].artifact }) }}</span>
            </div>
            <span class="absolute left-1.5 top-1.5 rounded bg-black/55 px-1.5 py-0.5 text-[10px] font-semibold leading-none text-white backdrop-blur-sm">{{ i + 1 }}</span>
          </div>

          <!-- success -->
          <button
            v-else-if="shotStates[i]?.status === 'success'"
            type="button"
            class="group relative aspect-[4/3] w-full overflow-hidden rounded-lg border border-line bg-base shadow-sm transition duration-200 hover:-translate-y-0.5 hover:shadow-md"
            :style="{ borderColor: lightboxShotIndex === i ? accent : undefined }"
            :title="shotCaption(s, i)"
            @click="openLightbox(i)"
          >
            <img
              :src="shotStates[i].src"
              :alt="shotCaption(s, i)"
              loading="lazy"
              class="h-full w-full object-cover transition duration-300 group-hover:scale-[1.05]"
            />
            <span class="pointer-events-none absolute inset-0 flex items-center justify-center bg-black/0 opacity-0 transition duration-200 group-hover:bg-black/25 group-hover:opacity-100">
              <span class="flex h-8 w-8 items-center justify-center rounded-full bg-white/90 text-gray-800 shadow-sm">
                <Icon name="expand" :size="15" />
              </span>
            </span>
            <span class="absolute left-1.5 top-1.5 rounded bg-black/55 px-1.5 py-0.5 text-[10px] font-semibold leading-none text-white backdrop-blur-sm">{{ i + 1 }}</span>
          </button>

          <!-- error -->
          <div
            v-else
            class="relative aspect-[4/3] w-full overflow-hidden rounded-lg border border-dashed border-err/30 bg-err/[0.06]"
          >
            <div class="flex h-full flex-col items-center justify-center gap-2 p-3 text-center">
              <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="text-err">
                <circle cx="12" cy="12" r="10" /><path d="M12 8v4M12 16h.01" />
              </svg>
              <div class="text-[11px] font-semibold text-err">{{ t('pages.product.testResult.screenshotError') }}</div>
              <code v-if="shotStates[i]?.status === 'error'" class="max-w-full break-all rounded border border-line bg-base px-1.5 py-0.5 font-mono text-[9px] text-txt3">{{ shotStates[i].artifact }}</code>
            </div>
            <span class="absolute left-1.5 top-1.5 rounded bg-black/55 px-1.5 py-0.5 text-[10px] font-semibold leading-none text-white backdrop-blur-sm">{{ i + 1 }}</span>
          </div>

          <figcaption v-if="s.caption" class="truncate px-0.5 text-[10px] leading-4 text-txt3" :title="s.caption">{{ s.caption }}</figcaption>
        </figure>
      </div>
    </section>

    <AppModal
      :open="lightboxGalleryPos !== null"
      :title="t('pages.product.testResult.screenshotOf', { i: (lightboxGalleryPos ?? 0) + 1, n: galleryIndices.length })"
      :width="1040"
      @close="closeLightbox"
    >
      <div v-if="lightboxState" class="flex flex-col items-center gap-3">
        <div class="relative flex w-full items-center justify-center">
          <button
            v-if="galleryIndices.length > 1"
            type="button"
            class="absolute left-0 flex h-9 w-9 items-center justify-center rounded-full border border-line bg-surface/90 text-txt2 shadow-sm transition hover:bg-surface hover:text-txt"
            :title="t('pages.product.testResult.prev')"
            @click="step(-1)"
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 6l-6 6 6 6" /></svg>
          </button>
          <img
            :src="lightboxSrc()"
            :alt="lightboxState.caption || 'screenshot'"
            class="max-h-[74vh] max-w-full rounded-lg border border-line object-contain shadow-md"
          />
          <button
            v-if="galleryIndices.length > 1"
            type="button"
            class="absolute right-0 flex h-9 w-9 items-center justify-center rounded-full border border-line bg-surface/90 text-txt2 shadow-sm transition hover:bg-surface hover:text-txt"
            :title="t('pages.product.testResult.next')"
            @click="step(1)"
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 6l6 6-6 6" /></svg>
          </button>
        </div>
        <div v-if="lightboxState.caption" class="max-w-2xl text-center text-[12px] leading-relaxed text-txt2">{{ lightboxState.caption }}</div>
        <div v-if="galleryIndices.length > 1" class="flex flex-wrap justify-center gap-1.5">
          <button
            v-for="(idx, gi) in galleryIndices"
            :key="idx"
            type="button"
            class="h-11 w-14 overflow-hidden rounded-md border transition"
            :class="lightboxGalleryPos === gi ? 'opacity-100' : 'border-line opacity-55 hover:opacity-90'"
            :style="{ borderColor: lightboxGalleryPos === gi ? accent : undefined }"
            @click="lightboxGalleryPos = gi"
          >
            <img
              v-if="shotStates[idx]?.status === 'success'"
              :src="shotStates[idx].src"
              :alt="shotCaption(shots[idx], idx)"
              class="h-full w-full object-cover"
            />
          </button>
        </div>
      </div>
    </AppModal>
  </div>
</template>

<style scoped>
.shot-shimmer {
  background: linear-gradient(90deg, rgb(var(--c-elevated, 28 28 33)) 25%, rgb(var(--c-overlay, 38 38 44)) 50%, rgb(var(--c-elevated, 28 28 33)) 75%);
  background-size: 200% 100%;
  animation: shimmer 1.4s infinite;
}
@keyframes shimmer {
  0% { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}
</style>
