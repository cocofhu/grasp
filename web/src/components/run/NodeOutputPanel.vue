<script setup lang="ts">
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import StatusPill from '../ui/StatusPill.vue'
import { renderMarkdown } from '@/lib/shared/markdown'
import { fmtDuration } from '@/lib/shared/format'
import { api } from '@/lib/api/api'
import { nodeColor } from '@/data/nodeRegistry'
import { useNodeDefs } from '@/lib/run/useNodeDefs'
import { resolveNodeDisplayLabelFromNode } from '@/lib/run/resolveNodeDisplayLabel'
import CompositeVarBlock from '@/components/ui/CompositeVarBlock.vue'
import PlanView, { type PlanDoc } from './PlanView.vue'
import StructuredArtifactView from './StructuredArtifactView.vue'
import AppPreviewPanel from './AppPreviewPanel.vue'
import OutputResultCards from './OutputResultCards.vue'
import type { NodeRun, WFNode, Run, OutputCard } from '@/lib/shared/types'
import { isClarifyNode as isClarifyAgent, isPreviewNode } from '@/lib/shared/clarifyInteractive'
import { declaredProducts, hasTool, writesSchema } from '@/lib/workflow/agentCapabilities'
import { getAgentProfile } from '@/lib/run/workflowIO'

const props = defineProps<{ node: WFNode; nodeRun: NodeRun; run: Run }>()

const { t } = useI18n()
const { NODE_DEFS } = useNodeDefs()

const def = computed(() => NODE_DEFS.value[props.node.type])
const displayLabel = computed(() =>
  resolveNodeDisplayLabelFromNode(props.node, t, def.value?.label),
)
const hex = computed(() => nodeColor(props.node.type))
const hexSoft = computed(() => nodeColor(props.node.type, 0.13))

// Live-ticking elapsed time. The backend leaves durationSec at 0 while a node is
// still running, so a raw read would freeze at 00:00. Instead derive it from
// startedAt on a 1s tick until the node finishes (then durationSec is authoritative).
const nowMs = ref(Date.now())
let clock: number | undefined
const isNodeActive = computed(() => props.nodeRun.status === 'running' || props.nodeRun.status === 'waiting_human')
onMounted(() => {
  clock = window.setInterval(() => {
    if (isNodeActive.value) nowMs.value = Date.now()
  }, 1000)
})
onUnmounted(() => {
  if (clock) window.clearInterval(clock)
})
const displayDuration = computed<number | null>(() => {
  if (isNodeActive.value && props.nodeRun.startedAt) {
    const start = Date.parse(props.nodeRun.startedAt)
    if (!isNaN(start)) return Math.max(0, Math.floor((nowMs.value - start) / 1000))
  }
  return props.nodeRun.durationSec != null ? props.nodeRun.durationSec : null
})
const showGit = computed(() => !!props.node.config?.detect_push && !!props.run.git?.pushed)
const nodeArtifacts = computed(() => props.run.artifacts.filter((a) => a.nodeId === props.node.id))

const outputs = computed<Record<string, any>>(() => props.nodeRun.outputs || {})
const hasRun = computed(() => props.nodeRun.status !== 'pending')

const outputCards = computed<OutputCard[]>(() => {
  if (props.node.type !== 'output') return []
  const raw = outputs.value.outputCards
  return Array.isArray(raw) ? (raw as OutputCard[]) : []
})
const isOutputNode = computed(() => props.node.type === 'output')
const showOutputMd = computed(() => !!props.nodeRun.outputMd && !isOutputNode.value)

// input: declared global variables paired with the value actually used this run.
const inputItems = computed(() => {
  const decl = (props.node.config?.variables || []) as { name?: string; desc?: string }[]
  return decl
    .filter((v) => v.name)
    .map((v) => ({ key: v.name as string, label: v.desc || (v.name as string), value: outputs.value[v.name as string] }))
})

// agent
const agentProfile = computed(() => getAgentProfile(props.node.config) || undefined)
const narration = computed(() => outputs.value.narration_summary as string | undefined)

const caps = computed(() => (props.node.type === 'agent' ? props.node.caps : undefined))
const clarifyAgent = computed(() => isClarifyAgent(props.node))
const previewAgent = computed(() => isPreviewNode(props.node) && !clarifyAgent.value)

// plan writers / plan progress trackers: render the structured two-level plan.json
// artifact instead of leaving it as an opaque blob. Agents granted
// update_plan_status update statuses in the same file, so we surface it there too.
// plan.json is a reserved run-scoped artifact name. The run/artifact list DTOs
// omit `content` (server marks it json:"-"), so the content is fetched by id and
// re-fetched whenever the plan artifact changes (size/id) — e.g. as an Agent
// marks progress mid-run.
const tracksPlan = computed(() => hasTool(caps.value, 'update_plan_status'))
const planArtifact = computed(() => {
  if (!writesSchema(caps.value, 'plan') && !tracksPlan.value) return null
  return (
    props.run.artifacts.find(
      (x) => x.name === 'plan.json' && (tracksPlan.value || x.nodeId === props.node.id),
    ) || null
  )
})
const planDoc = ref<PlanDoc | null>(null)
async function loadPlan() {
  const raw = props.nodeRun.outputs?.plan_json
  if (typeof raw === 'string' && raw.trim()) {
    try {
      const doc = JSON.parse(raw) as PlanDoc
      planDoc.value = Array.isArray(doc.goals) ? doc : null
    } catch {
      planDoc.value = null
    }
    return
  }
  const a = planArtifact.value
  if (!a) {
    planDoc.value = null
    return
  }
  try {
    const full = await api.artifactContent(a.id)
    const doc = JSON.parse(full.content || '{}') as PlanDoc
    planDoc.value = Array.isArray(doc.goals) ? doc : null
  } catch {
    planDoc.value = null
  }
}
watch(
  () => {
    const snap = props.nodeRun.outputs?.plan_json || ''
    const a = planArtifact.value
    return `${props.nodeRun.iteration ?? 0}:${snap}:${a ? `${a.id}:${a.sizeBytes}` : ''}`
  },
  () => loadPlan(),
  { immediate: true },
)

const clarifiedArtifact = computed(() => {
  if (!writesSchema(caps.value, 'clarified_requirement')) return null
  return (
    props.run.artifacts.find(
      (x) => x.name === 'clarified_requirement.json' && x.nodeId === props.node.id,
    ) || null
  )
})
const clarifiedDoc = ref<Record<string, any> | null>(null)
async function loadClarifiedRequirement() {
  const raw = props.nodeRun.outputs?.clarified_requirement_json
  if (typeof raw === 'string' && raw.trim()) {
    try {
      const doc = JSON.parse(raw) as Record<string, any>
      clarifiedDoc.value = doc && typeof doc === 'object' && !Array.isArray(doc) ? doc : null
    } catch {
      clarifiedDoc.value = null
    }
    return
  }
  if (raw && typeof raw === 'object' && !Array.isArray(raw)) {
    clarifiedDoc.value = raw as Record<string, any>
    return
  }
  const a = clarifiedArtifact.value
  if (!a) {
    clarifiedDoc.value = null
    return
  }
  try {
    const full = await api.artifactContent(a.id)
    const doc = JSON.parse(full.content || '{}') as Record<string, any>
    clarifiedDoc.value = doc && typeof doc === 'object' && !Array.isArray(doc) ? doc : null
  } catch {
    clarifiedDoc.value = null
  }
}
watch(
  () => {
    const snap = props.nodeRun.outputs?.clarified_requirement_json || ''
    const a = clarifiedArtifact.value
    return `${props.nodeRun.iteration ?? 0}:${JSON.stringify(snap)}:${a ? `${a.id}:${a.sizeBytes}` : ''}`
  },
  () => loadClarifiedRequirement(),
  { immediate: true },
)

// structured products: render the first declared product's markdown. plan /
// clarified_requirement / page have dedicated views above.
const DEDICATED_SCHEMAS = new Set(['plan', 'clarified_requirement', 'page'])
const structuredMd = computed<string | null>(() => {
  const keys = declaredProducts(caps.value)
    .filter((p) => !DEDICATED_SCHEMAS.has(p.name))
    .map((p) => p.outputKey)
  for (const key of keys) {
    const v = outputs.value[key]
    if (typeof v === 'string' && v.trim()) return v
  }
  return null
})

// clarify: resolve THIS node's dialogue. A run can hold several clarify Agents, so
// read the per-node map (clarifyByNode) and only fall back to the single active
// `clarify` for this node — otherwise a non-active clarify node would mirror the
// currently-paused node's state.
const clarifyForNode = computed(
  () =>
    props.run.clarifyByNode?.[props.node.id] ||
    (props.run.clarify?.nodeId === props.node.id ? props.run.clarify : null),
)
const isClarifyNode = computed(() => !!clarifyForNode.value)
const clarifyRounds = computed(() => (clarifyForNode.value?.turns || []).filter((t) => t.role === 'human').length)
const clarified = computed(() => outputs.value.clarified_requirement as string | undefined)

// human_gate: resolve the chosen action id back to its button label + form values.
// Gated Agents report the pass / fail outlet they took.
const gateAction = computed(() => {
  const id = outputs.value.action
  if (!id) return null
  if (props.node.type === 'agent') {
    const outlet = String(id)
    return { id: outlet, label: outlet === 'pass' || outlet === 'fail' ? t(`nodes.outlets.${outlet}`) : outlet }
  }
  const a = ((props.node.config?.actions || []) as { id: string; label: string }[]).find((x) => x.id === id)
  return { id: String(id), label: a?.label || String(id) }
})
const gateForm = computed<[string, any][]>(() => {
  const f = outputs.value.form
  return f && typeof f === 'object' && !Array.isArray(f) ? Object.entries(f) : []
})

// branch: matched is a 0-based case index (-1 = none); goto is the target node id.
const branchMatched = computed<number | undefined>(() =>
  typeof outputs.value.matched === 'number' ? outputs.value.matched : undefined,
)
const branchGoto = computed(() => outputs.value.goto as string | undefined)

// set_var
const setVars = computed<[string, any][]>(() => {
  const v = outputs.value.vars
  return v && typeof v === 'object' && !Array.isArray(v) ? Object.entries(v) : []
})

// code changes: VCS-neutral report from the sandbox (agent/react nodes).
type ChangedFile = { path: string; status: string; added?: number; deleted?: number }
const codeChanges = computed(() => {
  const o = outputs.value
  const files = Array.isArray(o.changed_files) ? (o.changed_files as ChangedFile[]) : []
  if (!o.branch && !o.commit_sha && !o.diff_stat && !files.length) return null
  return {
    branch: o.branch as string | undefined,
    baseBranch: o.base_branch as string | undefined,
    newBranch: !!o.new_branch,
    commitSha: o.commit_sha as string | undefined,
    diffStat: o.diff_stat as string | undefined,
    pushed: o.pushed === true,
    hasPushInfo: 'pushed' in o,
    unpushed: typeof o.unpushed_count === 'number' ? (o.unpushed_count as number) : 0,
    files,
  }
})

function fileStatusClass(s: string): string {
  switch (s) {
    case 'added':
      return 'bg-ok/10 text-ok'
    case 'deleted':
      return 'bg-err/10 text-err'
    case 'untracked':
      return 'bg-info/10 text-info'
    default:
      return 'bg-base text-txt3'
  }
}
</script>

<template>
  <!-- Padding on inner wrapper (not the scroll root) so OutputResultCards sticky top-0
       sits flush with the panel chrome instead of below p-4. Empty state stays inside. -->
  <div class="scroll-area h-full min-w-0 w-full max-w-full overflow-x-clip overflow-y-auto" data-testid="node-output-scroll">
    <div class="p-4">
    <div class="mb-3 flex min-w-0 items-center gap-2.5">
      <div class="flex h-8 w-8 items-center justify-center rounded-md" :style="{ background: hexSoft, color: hex }">
        <Icon :name="def.icon" :size="16" />
      </div>
      <div class="min-w-0 flex-1">
        <div class="text-sm font-semibold text-txt [overflow-wrap:anywhere]">{{ displayLabel }}</div>
        <div class="text-[11px] text-txt3 [overflow-wrap:anywhere]">{{ def.label }} · {{ node.id }}</div>
      </div>
      <StatusPill :status="nodeRun.status" size="sm" />
    </div>

    <div class="mb-3 flex flex-wrap gap-2 text-[11px] text-txt3">
      <span v-if="displayDuration != null" class="chip"><Icon name="clock" :size="11" /> {{ fmtDuration(displayDuration) }}</span>
    </div>

    <!-- ============ type-specific panel ============ -->

    <!-- input: launch parameters used this run -->
    <div v-if="node.type === 'input'" class="card mb-3 p-3">
      <div class="mb-2 flex items-center gap-1.5 text-xs font-semibold text-txt2"><Icon name="input" :size="13" :style="{ color: hex }" /> {{ t('pages.nodeOutput.inputParams') }}</div>
      <div v-if="inputItems.length" class="space-y-1.5">
        <div v-for="it in inputItems" :key="it.key" class="text-[12px]">
          <div class="text-txt3">{{ it.label }} <code class="ml-1 font-mono text-[10px] text-txt3">{{ it.key }}</code></div>
          <div class="mt-0.5 rounded bg-base px-2 py-1 font-mono text-[12px] text-txt">
            <CompositeVarBlock :value="it.value" locale-bool pre-wrap size="md" />
          </div>
        </div>
      </div>
      <div v-else class="text-[12px] text-txt3">{{ t('pages.nodeOutput.noInputFields') }}</div>
    </div>

    <!-- auto Agent: which agent ran + summary -->
    <div v-else-if="node.type === 'agent' && !clarifyAgent" class="card mb-3 p-3">
      <div class="mb-2 flex items-center gap-1.5 text-xs font-semibold text-txt2"><Icon name="robot" :size="13" :style="{ color: hex }" /> {{ t('pages.nodeOutput.agentInfo') }}</div>
      <div class="space-y-1.5 text-[12px]">
        <div class="flex min-w-0 max-w-full items-center gap-2"><span class="w-20 shrink-0 text-txt3">Agent</span><code class="min-w-0 max-w-full flex-1 overflow-x-auto whitespace-nowrap rounded bg-base px-1.5 py-0.5 font-mono text-accent-2">{{ agentProfile || '—' }}</code></div>
        <div v-if="outputs.artifact_id" class="flex min-w-0 max-w-full items-center gap-2"><span class="w-20 shrink-0 text-txt3">{{ t('pages.nodeOutput.artifactId') }}</span><code class="min-w-0 max-w-full flex-1 overflow-x-auto whitespace-nowrap rounded bg-base px-1.5 py-0.5 font-mono text-accent-2">{{ outputs.artifact_id }}</code></div>
      </div>
      <div v-if="narration" class="mt-2 border-t border-line pt-2">
        <div class="mb-1 text-[10px] uppercase tracking-wider text-txt3">{{ t('pages.nodeOutput.workSummary') }}</div>
        <div class="text-[12px] leading-relaxed text-txt2 [overflow-wrap:anywhere]">{{ narration }}</div>
      </div>
      <div v-if="previewAgent" class="mt-3 border-t border-line pt-2">
        <div class="mb-2 flex items-center gap-1.5 text-xs font-semibold text-txt2">
          <Icon name="dashboard" :size="13" :style="{ color: hex }" /> {{ t('pages.nodeOutput.appPreview') }}
        </div>
        <AppPreviewPanel :run-id="run.id" :node-id="node.id" compact />
      </div>
      <div v-if="gateAction" class="mt-3 border-t border-line pt-2 text-[12px]">
        <span class="text-txt3">{{ t('pages.nodeOutput.chosenAction') }}</span>
        <span class="ml-2 rounded-md bg-ok/15 px-2 py-0.5 text-[11px] font-medium text-ok">{{ gateAction.label }}</span>
      </div>
    </div>

    <!-- clarify Agent: clarification dialogue summary -->
    <div v-else-if="clarifyAgent" class="card mb-3 p-3">
      <div class="mb-2 flex items-center gap-1.5 text-xs font-semibold text-txt2"><Icon name="chat" :size="13" :style="{ color: hex }" /> {{ t('pages.nodeOutput.reactInteraction') }}</div>
      <div class="space-y-1.5 text-[12px]">
        <div class="flex min-w-0 max-w-full items-center gap-2"><span class="w-20 shrink-0 text-txt3">Agent</span><code class="min-w-0 max-w-full flex-1 overflow-x-auto whitespace-nowrap rounded bg-base px-1.5 py-0.5 font-mono text-accent-2">{{ agentProfile || '—' }}</code></div>
        <div v-if="isClarifyNode" class="flex items-center gap-2"><span class="w-20 shrink-0 text-txt3">{{ t('pages.nodeOutput.humanRounds') }}</span><span class="text-txt">{{ t('pages.nodeOutput.rounds', { n: clarifyRounds }) }}</span></div>
        <div class="flex items-center gap-2">
          <span class="w-20 shrink-0 text-txt3">{{ t('pages.nodeOutput.status') }}</span>
          <span class="text-txt">{{ clarifyForNode?.done ? t('pages.nodeOutput.completed') : isClarifyNode ? t('pages.nodeOutput.inProgress') : '—' }}</span>
        </div>
      </div>
      <div v-if="clarified" class="mt-2 border-t border-line pt-2">
        <div class="mb-1 text-[10px] uppercase tracking-wider text-txt3">{{ t('pages.nodeOutput.conclusion') }}</div>
        <div class="md text-[12px] leading-relaxed text-txt2" v-html="renderMarkdown(clarified)" />
      </div>
      <p v-if="isClarifyNode && !clarifyForNode?.done" class="mt-2 text-[11px] text-txt3">{{ t('pages.nodeOutput.clarifyTabHint') }}</p>
    </div>

    <!-- human_gate: the decision made -->
    <div v-else-if="node.type === 'human_gate'" class="card mb-3 p-3">
      <div class="mb-2 flex items-center gap-1.5 text-xs font-semibold text-txt2"><Icon name="gate" :size="13" :style="{ color: hex }" /> {{ t('pages.nodeOutput.gateResult') }}</div>
      <div v-if="gateAction" class="space-y-1.5 text-[12px]">
        <div class="flex items-center gap-2">
          <span class="w-20 shrink-0 text-txt3">{{ t('pages.nodeOutput.chosenAction') }}</span>
          <span class="rounded-md bg-ok/15 px-2 py-0.5 text-[11px] font-medium text-ok">{{ gateAction.label }}</span>
        </div>
        <div v-if="outputs.reviewer_id" class="flex items-center gap-2"><span class="w-20 shrink-0 text-txt3">{{ t('pages.nodeOutput.reviewer') }}</span><span class="text-txt">{{ outputs.reviewer_id }}</span></div>
        <template v-if="gateForm.length">
          <div class="border-t border-line pt-1.5 text-[10px] uppercase tracking-wider text-txt3">{{ t('pages.nodeOutput.form') }}</div>
          <div v-for="[k, v] in gateForm" :key="k" class="flex items-start gap-2">
            <span class="w-20 shrink-0 text-txt3">{{ k }}</span>
            <div class="min-w-0 flex-1 text-[12px]">
              <CompositeVarBlock :value="v" locale-bool pre-wrap />
            </div>
          </div>
        </template>
      </div>
      <div v-else class="text-[12px] text-txt3">{{ hasRun ? t('pages.nodeOutput.waitingApproval') : t('pages.nodeOutput.notYetInApproval') }}</div>
    </div>

    <!-- branch: routing decision -->
    <div v-else-if="node.type === 'branch'" class="card mb-3 p-3">
      <div class="mb-2 flex items-center gap-1.5 text-xs font-semibold text-txt2"><Icon name="branch" :size="13" :style="{ color: hex }" /> {{ t('pages.nodeOutput.routeResult') }}</div>
      <div v-if="branchMatched !== undefined" class="space-y-1.5 text-[12px]">
        <div class="flex items-center gap-2">
          <span class="w-20 shrink-0 text-txt3">{{ t('pages.nodeOutput.matchedBranch') }}</span>
          <span class="text-txt">{{ branchMatched < 0 ? t('pages.nodeOutput.noBranchMatch') : t('pages.nodeOutput.branchIf', { n: branchMatched + 1 }) }}</span>
        </div>
        <div v-if="branchGoto" class="flex min-w-0 max-w-full items-center gap-2"><span class="w-20 shrink-0 text-txt3">{{ t('pages.nodeOutput.goto') }}</span><code class="min-w-0 max-w-full flex-1 overflow-x-auto whitespace-nowrap rounded bg-base px-1.5 py-0.5 font-mono text-accent-2">{{ branchGoto }}</code></div>
      </div>
      <div v-else class="text-[12px] text-txt3">{{ t('pages.nodeOutput.notEvaluated') }}</div>
    </div>

    <!-- set_var: resulting variable snapshot -->
    <div v-else-if="node.type === 'set_var'" class="card mb-3 p-3">
      <div class="mb-2 flex items-center gap-1.5 text-xs font-semibold text-txt2"><Icon name="edit" :size="13" :style="{ color: hex }" /> {{ t('pages.nodeOutput.varAssignment') }}</div>
      <div v-if="setVars.length" class="space-y-1">
        <div v-for="[k, v] in setVars" :key="k" class="flex items-start gap-2 text-[12px]">
          <code class="shrink-0 rounded bg-base px-1.5 py-0.5 font-mono text-accent-2">{{ k }}</code>
          <span class="shrink-0 text-txt3">=</span>
          <div class="min-w-0 flex-1 text-[12px]">
            <CompositeVarBlock :value="v" locale-bool pre-wrap />
          </div>
        </div>
      </div>
      <div v-else class="text-[12px] text-txt3">{{ t('pages.nodeOutput.noVarSnapshot') }}</div>
    </div>

    <!-- plan.json: structured two-level plan visualization (plan/implement/approve nodes) -->
    <div v-if="planDoc" class="card mb-3 p-3">
      <PlanView :doc="planDoc" :accent="hex" :artifacts="run.artifacts" />
    </div>

    <div v-if="clarifiedDoc" class="card mb-3 p-3" data-testid="node-output-clarified-requirement">
      <StructuredArtifactView
        name="clarified_requirement.json"
        :doc="clarifiedDoc"
        :accent="hex"
        :run-id="run.id"
        :artifacts="run.artifacts"
      />
    </div>

    <!-- structured framework-card product: rendered markdown of the reserved
         JSON (research / test / review / …). -->
    <div v-if="structuredMd" class="card mb-3 p-3">
      <div class="mb-2 flex items-center gap-1.5 text-xs font-semibold text-txt2"><Icon :name="def.icon" :size="13" :style="{ color: hex }" /> {{ def.label }}{{ t('pages.nodeOutput.resultSuffix') }}</div>
      <div class="md text-[12px] leading-relaxed text-txt2" v-html="renderMarkdown(structuredMd)" />
    </div>

    <!-- code changes: VCS-neutral report from the sandbox (branch/commit/diff) -->
    <div v-if="codeChanges" class="card mb-3 p-3">
      <div class="mb-2 flex items-center gap-1.5 text-xs font-semibold text-txt2"><Icon name="branch" :size="13" :style="{ color: hex }" /> {{ t('pages.nodeOutput.codeChanges') }}</div>
      <div class="space-y-1.5 text-[12px]">
        <div class="flex flex-wrap items-center gap-2">
          <span class="w-16 shrink-0 text-txt3">{{ t('pages.nodeOutput.branch') }}</span>
          <code class="min-w-0 max-w-full overflow-x-auto whitespace-nowrap rounded bg-base px-1.5 py-0.5 font-mono text-accent-2">{{ codeChanges.branch || '—' }}</code>
          <span v-if="codeChanges.newBranch" class="rounded-md bg-ok/15 px-1.5 py-0.5 text-[10px] font-medium text-ok">{{ t('pages.nodeOutput.new') }}</span>
          <span v-if="codeChanges.baseBranch" class="min-w-0 max-w-full overflow-x-auto whitespace-nowrap text-[11px] text-txt3">← {{ codeChanges.baseBranch }}</span>
        </div>
        <div v-if="codeChanges.commitSha" class="flex items-center gap-2"><span class="w-16 shrink-0 text-txt3">{{ t('pages.nodeOutput.commit') }}</span><code class="rounded bg-base px-1.5 py-0.5 font-mono text-accent-2">{{ codeChanges.commitSha.slice(0, 10) }}</code></div>
        <div v-if="codeChanges.hasPushInfo" class="flex items-center gap-2">
          <span class="w-16 shrink-0 text-txt3">{{ t('pages.nodeOutput.push') }}</span>
          <span v-if="codeChanges.pushed" class="rounded-md bg-ok/15 px-1.5 py-0.5 text-[10px] font-medium text-ok">{{ t('pages.nodeOutput.pushedRemote') }}</span>
          <span v-else class="rounded-md bg-warn/15 px-1.5 py-0.5 text-[10px] font-medium text-warn">{{ t('pages.nodeOutput.notPushed') }}{{ codeChanges.unpushed ? t('pages.nodeOutput.aheadCommits', { n: codeChanges.unpushed }) : '' }}</span>
        </div>
        <div v-if="codeChanges.diffStat" class="flex min-w-0 max-w-full items-center gap-2"><span class="w-16 shrink-0 text-txt3">{{ t('pages.nodeOutput.changes') }}</span><span class="min-w-0 text-txt [overflow-wrap:anywhere]">{{ codeChanges.diffStat }}</span></div>
      </div>
      <div v-if="codeChanges.files.length" class="mt-2 border-t border-line pt-2">
        <div class="mb-1 text-[10px] uppercase tracking-wider text-txt3">{{ t('pages.nodeOutput.changedFiles', { n: codeChanges.files.length }) }}</div>
        <div class="scroll-area max-h-40 space-y-0.5 overflow-y-auto">
          <div v-for="f in codeChanges.files" :key="f.path" class="flex items-center gap-2 text-[11px]">
            <code class="w-16 shrink-0 rounded px-1 py-0.5 text-center font-mono text-[10px]" :class="fileStatusClass(f.status)">{{ f.status }}</code>
            <code class="flex-1 truncate font-mono text-txt2">{{ f.path }}</code>
            <span v-if="f.added || f.deleted" class="shrink-0 text-[10px]"><span class="text-ok">+{{ f.added || 0 }}</span> <span class="text-err">-{{ f.deleted || 0 }}</span></span>
          </div>
        </div>
      </div>
    </div>

    <!-- Git detection (sandbox agent with detect_push) -->
    <div v-if="showGit" class="card mb-3 p-3">
      <div class="mb-2 flex items-center gap-1.5 text-xs font-semibold text-txt2"><Icon name="branch" :size="13" /> {{ t('pages.nodeOutput.gitDetection') }}</div>
      <div class="space-y-1.5 text-[12px]">
        <div class="flex min-w-0 max-w-full items-center gap-2"><span class="w-16 shrink-0 text-txt3">{{ t('pages.nodeOutput.branch') }}</span><code class="min-w-0 max-w-full flex-1 overflow-x-auto whitespace-nowrap rounded bg-base px-1.5 py-0.5 font-mono text-accent-2">{{ run.git!.branch }}</code></div>
        <div class="flex min-w-0 max-w-full items-center gap-2"><span class="w-16 shrink-0 text-txt3">SHA</span><code class="min-w-0 max-w-full flex-1 overflow-x-auto whitespace-nowrap rounded bg-base px-1.5 py-0.5 font-mono text-accent-2">{{ run.git!.pushedSha }}</code></div>
        <div class="flex min-w-0 max-w-full items-center gap-2"><span class="w-16 shrink-0 text-txt3">MR</span><a class="min-w-0 max-w-full flex-1 overflow-x-auto whitespace-nowrap text-info hover:underline" :href="run.git!.mrUrl">{{ run.git!.mrUrl }}</a></div>
      </div>
    </div>

    <!-- artifacts written via artifact-store MCP -->
    <div v-if="nodeArtifacts.length" class="card mb-3 p-3">
      <div class="mb-2 flex items-center gap-1.5 text-xs font-semibold text-txt2"><Icon name="artifact" :size="13" class="text-n-artifact" /> {{ t('pages.nodeOutput.artifactsMcp') }}</div>
      <div class="space-y-1.5">
        <div v-for="a in nodeArtifacts" :key="a.id" class="flex min-w-0 max-w-full items-center gap-2 text-[12px]">
          <code class="rounded bg-ok/10 px-1 py-0.5 font-mono text-[10px] text-ok">write</code>
          <code class="min-w-0 max-w-full flex-1 overflow-x-auto whitespace-nowrap font-mono text-accent-2">{{ a.name }}</code>
          <span class="text-txt3">· {{ a.sizeBytes }} B</span>
        </div>
      </div>
    </div>

    <!-- output node: multi-card structured display -->
    <div v-if="isOutputNode && (hasRun || run.status === 'completed')" class="mb-3">
      <OutputResultCards v-if="outputCards.length" :cards="outputCards" :run="run" />
      <div
        v-else
        class="px-1 py-8 text-center text-[12px] text-txt3"
        data-testid="node-output-empty"
      >
        <strong class="mb-1.5 block text-[13px] text-txt">{{ t('pages.nodeOutput.noPreviewCardsTitle') }}</strong>
        <p class="mb-1">{{ t('pages.nodeOutput.noPreviewCardsBody') }}</p>
        <p>{{ t('pages.nodeOutput.noPreviewCardsHint') }}</p>
      </div>
    </div>

    <div v-if="showOutputMd" class="card p-4">
      <div class="mb-2 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.nodeOutput.output') }}</div>
      <div class="md" v-html="renderMarkdown(nodeRun.outputMd || '')" />
    </div>
    <div
      v-else-if="!hasRun && !(isOutputNode && run.status === 'completed')"
      class="px-1 py-8 text-center text-[12px] text-txt3"
    >{{ t('pages.nodeOutput.nodeNotRun') }}</div>
    <div v-else-if="!isOutputNode && node.type !== 'input' && node.type !== 'branch' && node.type !== 'set_var'" class="px-1 py-8 text-center text-[12px] text-txt3">{{ t('pages.nodeOutput.nodeNoOutput') }}</div>
    </div>
  </div>
</template>
