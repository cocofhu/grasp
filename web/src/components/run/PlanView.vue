<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import AnnotateBtn from './product/AnnotateBtn.vue'
import MermaidDiagram, { type PlanDiagram } from './MermaidDiagram.vue'
import type { Artifact } from '@/lib/shared/types'

export type PlanSub = { id?: string; title?: string; detail?: string; status?: string }
export type PlanGoal = { id?: string; title?: string; detail?: string; status?: string; subgoals?: PlanSub[] }
export type PlanField = {
  name?: string
  type?: string
  pk?: boolean
  nullable?: boolean
  fk?: string
  description?: string
}
export type PlanEntity = {
  name?: string
  fields?: PlanField[]
  attributes?: string[]
  description?: string
  relationships?: string[]
}
export type PlanInterfaceItem = {
  name?: string
  kind?: string
  direction?: string
  summary?: string
  detail?: string
  diagrams?: PlanDiagram[]
  diagram?: PlanDiagram
}
export type PlanComponentItem = {
  name?: string
  responsibility?: string
  dependencies?: string[]
  detail?: string
  diagrams?: PlanDiagram[]
  diagram?: PlanDiagram
}
export type PlanArchitecture = { summary?: string; diagrams?: PlanDiagram[]; diagram?: PlanDiagram }
export type PlanDataDesign = {
  summary?: string
  entities?: PlanEntity[]
  relationships?: string[]
  diagrams?: PlanDiagram[]
  diagram?: PlanDiagram
}
export type PlanInteraction = { summary?: string; diagrams?: PlanDiagram[]; diagram?: PlanDiagram }
export type PlanDoc = {
  title?: string
  architecture?: PlanArchitecture
  data_design?: PlanDataDesign
  interfaces?: PlanInterfaceItem[]
  components?: PlanComponentItem[]
  interaction?: PlanInteraction
  test_design?: string
  goals?: PlanGoal[]
}

const props = defineProps<{ doc: PlanDoc; accent?: string; artifacts?: Artifact[] }>()

const { t } = useI18n()
const hex = computed(() => props.accent || 'rgb(var(--c-hue-1))')
const hexSoft = computed(() => `color-mix(in srgb, ${hex.value} 13%, transparent)`)
const NA = computed(() => t('pages.plan.notApplicable'))

const progress = computed(() => {
  const goals = props.doc.goals || []
  const leaves: PlanSub[] = []
  for (const g of goals) {
    if (g.subgoals?.length) leaves.push(...g.subgoals)
    else leaves.push(g)
  }
  const done = leaves.filter((l) => l.status === 'done').length
  return { done, total: leaves.length, pct: leaves.length ? Math.round((done / leaves.length) * 100) : 0 }
})

const hasDesign = computed(
  () =>
    props.doc.architecture != null ||
    props.doc.data_design != null ||
    (props.doc.interfaces?.length ?? 0) > 0 ||
    (props.doc.components?.length ?? 0) > 0 ||
    props.doc.interaction != null ||
    !!(props.doc.test_design && props.doc.test_design.trim()),
)

function isNA(text?: string) {
  const s = (text || '').trim()
  return s === '不涉及' || s === 'N/A' || s === NA.value
}

function displaySummary(text?: string) {
  const s = (text || '').trim()
  return s || NA.value
}

const STATUS: Record<string, { labelKey: string; cls: string; dot: string }> = {
  done: { labelKey: 'pages.plan.status.done', cls: 'bg-ok/15 text-ok', dot: 'bg-ok' },
  in_progress: { labelKey: 'pages.plan.status.in_progress', cls: 'bg-info/15 text-info', dot: 'bg-info animate-pulseglow' },
  pending: { labelKey: 'pages.plan.status.pending', cls: 'bg-elevated text-txt3', dot: 'bg-line-strong' },
}
function fieldBadges(f: PlanField) {
  const tags: string[] = []
  if (f.pk) tags.push('PK')
  if (f.nullable) tags.push('nullable')
  if (f.fk) tags.push(`fk→${f.fk}`)
  return tags
}
function st(s?: string) {
  const key = s || 'pending'
  const meta = STATUS[key] || STATUS.pending
  return { label: t(meta.labelKey), cls: meta.cls, dot: meta.dot }
}

/** Collect diagrams[] + legacy singular diagram (dedupe by source). Evidence: g2.1 */
function collectSectionDiagrams(diagrams?: PlanDiagram[], diagram?: PlanDiagram): PlanDiagram[] {
  const out: PlanDiagram[] = []
  const seen = new Set<string>()
  const push = (d?: PlanDiagram) => {
    const src = (d?.source || '').trim()
    if (!d || !src || seen.has(src)) return
    seen.add(src)
    out.push(d)
  }
  for (const d of diagrams || []) push(d)
  push(diagram)
  return out
}

function kindLabel(kind?: string) {
  const k = (kind || '').trim().toLowerCase()
  if (k === 'activity' || k === 'flowchart' || k === 'sequence' || k === 'er') {
    return t(`pages.plan.diagramKinds.${k}`)
  }
  if (!k) return ''
  return t('pages.plan.diagramKinds.other')
}

function tabLabel(d: PlanDiagram, index: number) {
  const title = (d.title || '').trim()
  if (title) return title
  const kind = kindLabel(d.kind)
  if (kind) return kind
  return `${index + 1}`
}

const archDiagrams = computed(() => collectSectionDiagrams(props.doc.architecture?.diagrams, props.doc.architecture?.diagram))
const dataDiagrams = computed(() => collectSectionDiagrams(props.doc.data_design?.diagrams, props.doc.data_design?.diagram))
const ixDiagrams = computed(() => collectSectionDiagrams(props.doc.interaction?.diagrams, props.doc.interaction?.diagram))

/** Active tab index per section key. Reset when diagram set identity changes. */
const activeTab = reactive<Record<string, number>>({})

function ensureTab(key: string, count: number) {
  if (count <= 0) {
    delete activeTab[key]
    return
  }
  const cur = activeTab[key] ?? 0
  if (cur < 0 || cur >= count) activeTab[key] = 0
  else if (activeTab[key] == null) activeTab[key] = 0
}

watch(
  [archDiagrams, dataDiagrams, ixDiagrams],
  () => {
    ensureTab('architecture', archDiagrams.value.length)
    ensureTab('data_design', dataDiagrams.value.length)
    ensureTab('interaction', ixDiagrams.value.length)
  },
  { immediate: true },
)

function selectTab(key: string, index: number) {
  activeTab[key] = index
}

function currentDiagram(key: string, list: PlanDiagram[]): PlanDiagram | undefined {
  if (!list.length) return undefined
  const i = activeTab[key] ?? 0
  return list[Math.min(Math.max(i, 0), list.length - 1)]
}

function diagramJsonPath(section: string, list: PlanDiagram[], active: PlanDiagram | undefined): string {
  if (!active) return `${section}.diagram`
  const fromArr = (list === archDiagrams.value && section === 'architecture') ||
    (list === dataDiagrams.value && section === 'data_design') ||
    (list === ixDiagrams.value && section === 'interaction')
  // Prefer diagrams[i] when the active item lives in diagrams[]
  const diagramsField =
    section === 'architecture'
      ? props.doc.architecture?.diagrams
      : section === 'data_design'
        ? props.doc.data_design?.diagrams
        : props.doc.interaction?.diagrams
  const idx = (diagramsField || []).findIndex((d) => (d.source || '').trim() === (active.source || '').trim())
  if (idx >= 0) return `${section}.diagrams[${idx}]`
  void fromArr
  return `${section}.diagram`
}
</script>

<template>
  <div>
    <div class="mb-3 flex items-center gap-2">
      <div class="flex h-7 w-7 items-center justify-center rounded-md" :style="{ background: hexSoft, color: hex }">
        <Icon name="check" :size="15" />
      </div>
      <div class="group min-w-0 flex-1">
        <div class="flex min-w-0 items-center gap-1">
          <div class="truncate text-sm font-semibold text-txt">{{ doc.title || t('pages.plan.defaultTitle') }}</div>
          <AnnotateBtn v-if="doc.title" json-path="title" :label="doc.title" />
        </div>
      </div>
      <span class="shrink-0 rounded-full bg-base px-2 py-0.5 text-[11px] font-medium text-txt3">
        {{ progress.done }}/{{ progress.total }} · {{ progress.pct }}%
      </span>
    </div>

    <!-- overall progress bar -->
    <div class="mb-3 h-1.5 w-full overflow-hidden rounded-full bg-base">
      <div class="h-full rounded-full transition-all" :style="{ width: progress.pct + '%', background: hex }" />
    </div>

    <div v-if="hasDesign" class="mb-4 space-y-3" data-testid="plan-design">
      <div class="text-[11px] font-semibold uppercase tracking-wide text-txt3">{{ t('pages.plan.designTitle') }}</div>

      <section v-if="doc.architecture" class="rounded-lg border border-line bg-base/40 p-3" data-testid="plan-sec-architecture">
        <div class="group flex items-center gap-2">
          <div class="text-[13px] font-semibold text-txt">{{ t('pages.plan.sections.architecture') }}</div>
          <AnnotateBtn json-path="architecture" :label="t('pages.plan.sections.architecture')" />
        </div>
        <div
          class="mt-1 text-[12px] leading-relaxed"
          :class="isNA(doc.architecture.summary) ? 'text-warn' : 'text-txt2'"
          data-testid="plan-body-architecture"
        >
          {{ displaySummary(doc.architecture.summary) }}
        </div>
        <!-- g2.2/g2.3: ≥2 小 Tab; 1 张无 Tab; 0 张无图位 -->
        <div
          v-if="archDiagrams.length >= 2"
          class="mt-2.5 flex gap-1.5 overflow-x-auto"
          data-testid="plan-diagram-tabs"
          role="tablist"
        >
          <button
            v-for="(d, di) in archDiagrams"
            :key="(d.source || '') + di"
            type="button"
            role="tab"
            class="shrink-0 rounded-full px-2 py-0.5 text-[11px] leading-tight transition-colors"
            :class="(activeTab.architecture ?? 0) === di ? '' : 'bg-base text-txt3'"
            :style="
              (activeTab.architecture ?? 0) === di
                ? { background: hexSoft, color: hex }
                : undefined
            "
            :aria-selected="(activeTab.architecture ?? 0) === di"
            :data-testid="`plan-diagram-tab-${di}`"
            @click="selectTab('architecture', di)"
          >
            <span>{{ tabLabel(d, di) }}</span>
            <span v-if="d.scope" class="ml-1 text-[10px] opacity-70">{{ d.scope }}</span>
          </button>
        </div>
        <MermaidDiagram
          v-if="currentDiagram('architecture', archDiagrams)"
          :diagram="currentDiagram('architecture', archDiagrams)!"
          :json-path="diagramJsonPath('architecture', archDiagrams, currentDiagram('architecture', archDiagrams))"
          :artifacts="artifacts"
        />
      </section>

      <section v-if="doc.data_design" class="rounded-lg border border-line bg-base/40 p-3" data-testid="plan-sec-data">
        <div class="group flex items-center gap-2">
          <div class="text-[13px] font-semibold text-txt">{{ t('pages.plan.sections.dataDesign') }}</div>
          <AnnotateBtn json-path="data_design" :label="t('pages.plan.sections.dataDesign')" />
        </div>
        <div
          class="mt-1 text-[12px] leading-relaxed"
          :class="isNA(doc.data_design.summary) ? 'text-warn' : 'text-txt2'"
          data-testid="plan-body-data"
        >
          {{ displaySummary(doc.data_design.summary) }}
        </div>
        <ul v-if="doc.data_design.entities?.length" class="mt-2 space-y-2">
          <li v-for="(e, ei) in doc.data_design.entities" :key="e.name || ei" class="text-[12px] text-txt2">
            <div class="group flex flex-wrap items-center gap-2">
              <code class="font-mono text-[11px] text-txt3">{{ e.name }}</code>
              <span v-if="e.description"> — {{ e.description }}</span>
              <AnnotateBtn :json-path="`data_design.entities[${ei}]`" :label="e.name || `entity ${ei + 1}`" />
            </div>
            <div
              v-if="e.fields?.length"
              class="mt-1.5 overflow-x-auto"
              data-testid="plan-entity-fields"
            >
              <table class="w-full min-w-[28rem] border-collapse text-left">
                <thead>
                  <tr class="border-b border-line text-[10px] font-medium uppercase tracking-wide text-txt3">
                    <th class="py-1 pr-3 font-medium">{{ t('pages.plan.fieldTable.name') }}</th>
                    <th class="py-1 pr-3 font-medium">{{ t('pages.plan.fieldTable.type') }}</th>
                    <th class="py-1 pr-3 font-medium">{{ t('pages.plan.fieldTable.constraints') }}</th>
                    <th class="py-1 font-medium">{{ t('pages.plan.fieldTable.description') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="(f, fi) in e.fields"
                    :key="f.name || fi"
                    class="border-b border-line/50 align-top last:border-b-0"
                  >
                    <td class="py-1 pr-3">
                      <code class="font-mono text-[11px] text-txt">{{ f.name }}</code>
                    </td>
                    <td class="py-1 pr-3 text-[11px] text-txt3 break-words">{{ f.type }}</td>
                    <td class="py-1 pr-3">
                      <span class="inline-flex flex-wrap gap-1">
                        <span
                          v-for="tag in fieldBadges(f)"
                          :key="tag"
                          class="rounded bg-base px-1 py-0.5 text-[10px] font-medium text-txt3"
                          data-testid="plan-field-badge"
                        >{{ tag }}</span>
                      </span>
                    </td>
                    <td class="py-1 text-[11px] text-txt3 break-words">{{ f.description }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <ul v-if="e.attributes?.length" class="mt-1 space-y-0.5 border-l border-line pl-2" data-testid="plan-entity-attributes">
              <li v-for="(a, ai) in e.attributes" :key="ai" class="text-[11px] text-txt3">
                <span class="italic">legacy</span> {{ a }}
              </li>
            </ul>
            <ul v-if="e.relationships?.length" class="mt-1 space-y-0.5 border-l border-line pl-2" data-testid="plan-entity-relationships">
              <li v-for="(r, ri) in e.relationships" :key="ri" class="text-[11px] text-txt2">{{ r }}</li>
            </ul>
          </li>
        </ul>
        <ul
          v-if="doc.data_design.relationships?.length"
          class="mt-2 space-y-0.5"
          data-testid="plan-data-relationships"
        >
          <li
            v-for="(r, ri) in doc.data_design.relationships"
            :key="ri"
            class="text-[12px] text-txt2"
          >{{ r }}</li>
        </ul>
        <div
          v-if="dataDiagrams.length >= 2"
          class="mt-2.5 flex gap-1.5 overflow-x-auto"
          data-testid="plan-diagram-tabs"
          role="tablist"
        >
          <button
            v-for="(d, di) in dataDiagrams"
            :key="(d.source || '') + di"
            type="button"
            role="tab"
            class="shrink-0 rounded-full px-2 py-0.5 text-[11px] leading-tight transition-colors"
            :class="(activeTab.data_design ?? 0) === di ? '' : 'bg-base text-txt3'"
            :style="
              (activeTab.data_design ?? 0) === di
                ? { background: hexSoft, color: hex }
                : undefined
            "
            :aria-selected="(activeTab.data_design ?? 0) === di"
            :data-testid="`plan-diagram-tab-${di}`"
            @click="selectTab('data_design', di)"
          >
            <span>{{ tabLabel(d, di) }}</span>
            <span v-if="d.scope" class="ml-1 text-[10px] opacity-70">{{ d.scope }}</span>
          </button>
        </div>
        <MermaidDiagram
          v-if="currentDiagram('data_design', dataDiagrams)"
          :diagram="currentDiagram('data_design', dataDiagrams)!"
          :json-path="diagramJsonPath('data_design', dataDiagrams, currentDiagram('data_design', dataDiagrams))"
          :artifacts="artifacts"
        />
      </section>

      <section v-if="doc.interfaces?.length" class="rounded-lg border border-line bg-base/40 p-3" data-testid="plan-sec-interfaces">
        <div class="text-[13px] font-semibold text-txt">{{ t('pages.plan.sections.interfaces') }}</div>
        <ul class="mt-2 space-y-1.5">
          <li v-for="(it, ii) in doc.interfaces" :key="it.name || ii" class="text-[12px] text-txt2">
            <div class="group flex flex-wrap items-center gap-2">
              <code class="font-mono text-[11px] text-txt3">{{ it.name }}</code>
              <span
                v-if="it.summary"
                :class="isNA(it.name) || isNA(it.summary) ? 'text-warn' : 'text-txt2'"
                data-testid="plan-body-interface"
              >{{ it.summary }}</span>
              <AnnotateBtn :json-path="`interfaces[${ii}]`" :label="it.name || `iface ${ii + 1}`" />
            </div>
          </li>
        </ul>
      </section>

      <section v-if="doc.components?.length" class="rounded-lg border border-line bg-base/40 p-3" data-testid="plan-sec-components">
        <div class="text-[13px] font-semibold text-txt">{{ t('pages.plan.sections.components') }}</div>
        <ul class="mt-2 space-y-1.5">
          <li v-for="(c, ci) in doc.components" :key="c.name || ci" class="text-[12px] text-txt2">
            <div class="group flex flex-wrap items-center gap-2">
              <code class="font-mono text-[11px] text-txt3">{{ c.name }}</code>
              <span
                v-if="c.responsibility"
                :class="isNA(c.name) ? 'text-warn' : 'text-txt2'"
                data-testid="plan-body-component"
              >{{ c.responsibility }}</span>
              <AnnotateBtn :json-path="`components[${ci}]`" :label="c.name || `comp ${ci + 1}`" />
            </div>
          </li>
        </ul>
      </section>

      <section v-if="doc.interaction" class="rounded-lg border border-line bg-base/40 p-3" data-testid="plan-sec-interaction">
        <div class="group flex items-center gap-2">
          <div class="text-[13px] font-semibold text-txt">{{ t('pages.plan.sections.interaction') }}</div>
          <AnnotateBtn json-path="interaction" :label="t('pages.plan.sections.interaction')" />
        </div>
        <div
          class="mt-1 text-[12px] leading-relaxed"
          :class="isNA(doc.interaction.summary) ? 'text-warn' : 'text-txt2'"
          data-testid="plan-body-interaction"
        >
          {{ displaySummary(doc.interaction.summary) }}
        </div>
        <div
          v-if="ixDiagrams.length >= 2"
          class="mt-2.5 flex gap-1.5 overflow-x-auto"
          data-testid="plan-diagram-tabs"
          role="tablist"
        >
          <button
            v-for="(d, di) in ixDiagrams"
            :key="(d.source || '') + di"
            type="button"
            role="tab"
            class="shrink-0 rounded-full px-2 py-0.5 text-[11px] leading-tight transition-colors"
            :class="(activeTab.interaction ?? 0) === di ? '' : 'bg-base text-txt3'"
            :style="
              (activeTab.interaction ?? 0) === di
                ? { background: hexSoft, color: hex }
                : undefined
            "
            :aria-selected="(activeTab.interaction ?? 0) === di"
            :data-testid="`plan-diagram-tab-${di}`"
            @click="selectTab('interaction', di)"
          >
            <span>{{ tabLabel(d, di) }}</span>
            <span v-if="d.scope" class="ml-1 text-[10px] opacity-70">{{ d.scope }}</span>
          </button>
        </div>
        <MermaidDiagram
          v-if="currentDiagram('interaction', ixDiagrams)"
          :diagram="currentDiagram('interaction', ixDiagrams)!"
          :json-path="diagramJsonPath('interaction', ixDiagrams, currentDiagram('interaction', ixDiagrams))"
          :artifacts="artifacts"
        />
      </section>

      <section v-if="doc.test_design?.trim()" class="rounded-lg border border-line bg-base/40 p-3" data-testid="plan-sec-test">
        <div class="group flex items-center gap-2">
          <div class="text-[13px] font-semibold text-txt">{{ t('pages.plan.sections.testDesign') }}</div>
          <AnnotateBtn json-path="test_design" :label="t('pages.plan.sections.testDesign')" />
        </div>
        <div
          class="mt-1 text-[12px] leading-relaxed whitespace-pre-wrap"
          :class="isNA(doc.test_design) ? 'text-warn' : 'text-txt2'"
          data-testid="plan-body-test"
        >
          {{ displaySummary(doc.test_design) }}
        </div>
      </section>
    </div>

    <div v-if="hasDesign" class="mb-2 text-[11px] font-semibold uppercase tracking-wide text-txt3">
      {{ t('pages.plan.goalsTitle') }}
    </div>

    <div class="space-y-2.5">
      <div v-for="(g, gi) in doc.goals || []" :key="g.id || gi" class="rounded-lg border border-line bg-base/40 p-3">
        <div class="flex items-start gap-2.5">
          <span class="mt-1.5 h-2 w-2 shrink-0 rounded-full" :class="st(g.status).dot" />
          <div class="min-w-0 flex-1">
            <div class="group flex flex-wrap items-center gap-2">
              <code class="shrink-0 rounded bg-base px-1.5 py-0.5 font-mono text-[10px] text-txt3">{{ g.id }}</code>
              <span class="text-[13px] font-semibold text-txt">{{ g.title }}</span>
              <AnnotateBtn :json-path="`goals[${g.id || gi}]`" :label="g.title || `目标 ${gi + 1}`" />
              <span class="ml-auto shrink-0 rounded-md px-2 py-0.5 text-[10px] font-medium" :class="st(g.status).cls">{{ st(g.status).label }}</span>
            </div>
            <div v-if="g.detail" class="mt-1 text-[12px] leading-relaxed text-txt2" data-testid="plan-body-goal-detail">{{ g.detail }}</div>
          </div>
        </div>

        <div v-if="g.subgoals?.length" class="mt-2.5 space-y-2 border-l-2 border-line pl-3.5">
          <div v-for="(s, si) in g.subgoals" :key="s.id || si" class="flex items-start gap-2.5">
            <span class="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full" :class="st(s.status).dot" />
            <div class="min-w-0 flex-1">
              <div class="group flex flex-wrap items-center gap-2">
                <code class="shrink-0 rounded bg-base px-1.5 py-0.5 font-mono text-[10px] text-txt3">{{ s.id }}</code>
                <span class="text-[12px] text-txt2">{{ s.title }}</span>
                <AnnotateBtn :json-path="`goals[${g.id || gi}].subgoals[${s.id || si}]`" :label="s.title || `小目标 ${si + 1}`" />
                <span class="ml-auto shrink-0 rounded-md px-2 py-0.5 text-[10px] font-medium" :class="st(s.status).cls">{{ st(s.status).label }}</span>
              </div>
              <div v-if="s.detail" class="mt-1 text-[11px] leading-relaxed text-txt2" data-testid="plan-body-subgoal-detail">{{ s.detail }}</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
