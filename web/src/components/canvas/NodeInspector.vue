<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Icon from '../ui/Icon.vue'
import AppButton from '../ui/AppButton.vue'
import AppSwitch from '../ui/AppSwitch.vue'
import OutputSourcesEditor from './OutputSourcesEditor.vue'
import AgentPicker from './inspector/AgentPicker.vue'
import GoalEditor, { type TemplateToken } from './inspector/GoalEditor.vue'
import VariablesField from './inspector/VariablesField.vue'
import { syncHumanGateFormDefaults } from '@/data/nodeRegistry'
import { useNodeDefs } from '@/lib/run/useNodeDefs'
import { buildOutputSourceOptions } from '@/lib/run/outputSourceOptions'
import { summarizeCapabilities, validateCapabilities, capabilityIssueKey } from '@/lib/workflow/agentCapabilities'
import type { FieldSchema, WFEdge, WFNode } from '@/lib/shared/types'
import { agentHueVar, agentInitial } from './composables/agentAvatar'
import { findAgent, schemaLabel, type CanvasAgent } from './composables/outlets'
import { newCaseId } from './composables/graphOps'
import { NODE_ICONS } from './composables/paletteItems'

const props = defineProps<{
  node: WFNode
  allNodes: WFNode[]
  edges: WFEdge[]
  agents: CanvasAgent[]
  agentsLoaded?: boolean
  focusGoalTick?: number
}>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'delete'): void }>()

const { t } = useI18n()
const tr = (key: string, named?: Record<string, unknown>) => (named ? t(key, named) : t(key))
const { NODE_DEFS } = useNodeDefs()

const def = computed(() => NODE_DEFS.value[props.node.type])
watch(
  () => props.node,
  (n) => {
    if (!n.config) n.config = {}
  },
  { immediate: true },
)
const cfg = computed<Record<string, any>>(() => props.node.config)
const isAgent = computed(() => props.node.type === 'agent')
const isCollab = computed(() => props.node.type === 'human_gate')

watch(
  () => (props.node.type === 'human_gate' ? String(props.node.config?.body_template ?? '') : null),
  () => {
    if (props.node.type === 'human_gate') syncHumanGateFormDefaults(props.node.config)
  },
)

// ── Agent ──
const agentName = computed(() => String(cfg.value.agent_profile ?? '').trim())
const agent = computed(() => findAgent(props.agents, agentName.value) ?? null)
const caps = computed(() => agent.value?.capabilities ?? props.node.caps ?? null)
const summary = computed(() => summarizeCapabilities(caps.value))
const capsIssue = computed(() => {
  if (!caps.value) return null
  const issue = validateCapabilities(caps.value)
  return issue ? t(capabilityIssueKey(issue), { value: issue.value ?? '' }) : null
})
const agentMissing = computed(() => !!agentName.value && !!props.agentsLoaded && !agent.value)
const studioLink = computed(() => ({
  path: '/agents',
  query: agentName.value ? { agent: agentName.value, studioTab: 'capabilities' } : {},
}))

function setAgent(name: string) {
  cfg.value.agent_profile = name
  if (props.node.caps) delete props.node.caps
  const label = props.node.label
  const prevDefault = !label || label === props.node.type || label === def.value?.label || label === agentName.value
  if (prevDefault && name) props.node.label = name
}

// ── Template tokens for the goal ──
const upstreamIds = computed(() => {
  const preds: Record<string, string[]> = {}
  for (const e of props.edges) (preds[e.target] ||= []).push(e.source)
  const seen = new Set<string>()
  const stack = [...(preds[props.node.id] || [])]
  while (stack.length) {
    const id = stack.pop()!
    if (seen.has(id)) continue
    seen.add(id)
    stack.push(...(preds[id] || []))
  }
  return seen
})

const globalVars = computed(() => {
  const input = props.allNodes.find((n) => n.type === 'input')
  const names: { name: string; type: string }[] = []
  const seen = new Set<string>()
  const add = (name: unknown, type = 'string') => {
    const n = String(name ?? '').trim()
    if (!n || seen.has(n)) return
    seen.add(n)
    names.push({ name: n, type })
  }
  for (const v of (input?.config?.variables as any[]) || []) add(v?.name, v?.type)
  for (const n of props.allNodes) {
    if (n.type === 'human_gate') {
      add(n.config?.output_var || 'action')
      for (const f of (n.config?.form as any[]) || []) add(f?.key, 'paragraph')
    }
    if (n.type === 'set_var') for (const a of (n.config?.assignments as any[]) || []) add(a?.var)
  }
  return names
})

const tokens = computed<TemplateToken[]>(() => {
  const out: TemplateToken[] = globalVars.value.map((v) => ({ token: `{{vars.${v.name}}}`, label: v.name, group: 'vars' }))
  for (const n of props.allNodes) {
    if (!upstreamIds.value.has(n.id)) continue
    for (const o of NODE_DEFS.value[n.type]?.outputs || []) {
      out.push({ token: `{{nodes.${n.id}.outputs.${o.key}}}`, label: `${n.label || n.id} · ${o.desc || o.key}`, group: 'upstream' })
    }
  }
  return out
})

// ── Generic fields ──
const fields = computed<FieldSchema[]>(() =>
  (def.value?.fields || []).filter((f) => !isAgent.value || !['agent_profile', 'prompt', 'timeout'].includes(f.key)),
)

function fieldOptions(f: FieldSchema) {
  if (f.key !== 'body_template') return f.options || []
  const opts = buildOutputSourceOptions(props.allNodes, props.edges, props.node.id, t)
  const cur = String(cfg.value.body_template || '')
  if (cur && !opts.some((o) => o.value === cur)) opts.unshift({ value: cur, label: t('common.gateBodyLabels.custom', { value: cur }) })
  return [
    { value: '', label: opts.length ? t('pages.workflowEditor.inspector.selectBody') : t('pages.workflowEditor.inspector.connectUpstreamForBody') },
    ...opts,
  ]
}

function list(key: string): any[] {
  if (!Array.isArray(cfg.value[key])) cfg.value[key] = []
  return cfg.value[key]
}

function renameOutlet(oldId: string, next: string) {
  for (const e of props.edges) if (e.source === props.node.id && e.sourceHandle === oldId) e.sourceHandle = next
}

function setRowId(row: { id?: string }, value: string) {
  const old = String(row.id ?? '')
  const v = value.trim().replace(/\s+/g, '_')
  row.id = v
  if (old && v && old !== v) renameOutlet(old, v)
}

function addCase() {
  const cases = list('cases')
  cases.push({ id: newCaseId(cases), when: '' })
}

function addAction() {
  const actions = list('actions')
  let i = actions.length + 1
  while (actions.some((a: any) => a.id === `action${i}`)) i++
  actions.push({ id: `action${i}`, label: t('pages.workflowEditor.inspector.newAction') })
}

function switchOn(f: FieldSchema): boolean {
  return !!cfg.value[f.key]
}

const timeout = computed({
  get: () => (cfg.value.timeout === undefined || cfg.value.timeout === null ? '' : String(cfg.value.timeout)),
  set: (v: string | number) => {
    const s = String(v ?? '').trim()
    const n = Number(s)
    if (!s || !Number.isFinite(n) || n <= 0) delete cfg.value.timeout
    else cfg.value.timeout = Math.round(n)
  },
})

const hue = computed(() => {
  const v = agentHueVar(agentName.value || props.node.id)
  return { background: `rgb(var(${v}) / 0.16)`, color: `rgb(var(${v}))` }
})
</script>

<template>
  <aside
    class="flex h-full w-[360px] max-w-full flex-col border-l border-line bg-surface shadow-drawer"
    role="complementary"
    :aria-label="t('canvas.aria.inspector')"
    data-testid="node-inspector"
  >
    <header class="flex items-center gap-2.5 border-b border-line px-4 py-3">
      <span v-if="isAgent && agentName" class="cnode-avatar h-8 w-8 shrink-0 text-[13px]" :style="hue" aria-hidden="true">{{ agentInitial(agentName) }}</span>
      <span v-else class="cnode-icon h-8 w-8 shrink-0" :style="isCollab ? { color: 'rgb(var(--c-warn))' } : undefined" aria-hidden="true">
        <Icon :name="NODE_ICONS[node.type] || 'alert'" :size="16" />
      </span>
      <div class="min-w-0 flex-1">
        <input
          v-model="node.label"
          class="w-full truncate rounded bg-transparent px-1 -ml-1 text-[14px] font-semibold text-txt outline-none hover:bg-elevated focus:bg-elevated"
          :aria-label="t('pages.workflowEditor.inspector.nodeName')"
          data-testid="inspector-name"
        />
        <div class="truncate text-[11px] text-txt3">{{ def?.label || node.type }} · <span class="font-mono">{{ node.id }}</span></div>
      </div>
      <button
        type="button"
        class="cchrome-btn hover:!text-err"
        :aria-label="t('pages.workflowEditor.inspector.deleteNode')"
        :title="t('pages.workflowEditor.inspector.deleteNode')"
        data-testid="inspector-delete"
        @click="emit('delete')"
      >
        <Icon name="trash" :size="14" />
      </button>
      <button type="button" class="cchrome-btn" :aria-label="t('canvas.inspector.close')" data-testid="inspector-close" @click="emit('close')">
        <Icon name="close" :size="15" />
      </button>
    </header>

    <div class="scroll-area min-h-0 flex-1 space-y-3 overflow-y-auto p-3">
      <template v-if="isAgent">
        <section class="insp-card">
          <h3 class="insp-title">{{ t('canvas.inspector.sections.agent') }}</h3>
          <AgentPicker
            :model-value="agentName"
            :agents="agents"
            :loading="!agentsLoaded"
            :invalid="!agentName || agentMissing"
            @update:model-value="setAgent"
          />
          <p v-if="agentMissing" class="insp-warn" data-testid="inspector-agent-missing">{{ t('canvas.node.agentMissing') }}</p>
          <p v-else-if="!agentName" class="mt-1.5 text-[11px] text-txt3">{{ t('canvas.node.noAgent') }}</p>
        </section>

        <section class="insp-card">
          <h3 class="insp-title">{{ t('canvas.inspector.sections.goal') }}</h3>
          <GoalEditor
            v-model="cfg.prompt"
            :tokens="tokens"
            :placeholder="t('canvas.inspector.goalPlaceholder')"
            :focus-tick="focusGoalTick"
          />
          <p class="mt-1.5 text-[11px] text-txt3">{{ t('canvas.inspector.goalHint') }}</p>
        </section>

        <section class="insp-card">
          <h3 class="insp-title">{{ t('canvas.inspector.sections.timeout') }}</h3>
          <div class="flex items-center gap-2">
            <input v-model="timeout" type="number" min="1" class="input flex-1" :placeholder="t('canvas.inspector.timeoutPlaceholder')" data-testid="inspector-timeout" />
            <span class="chip">{{ t('common.minutes') }}</span>
          </div>
        </section>

        <section class="insp-card" data-testid="inspector-capabilities">
          <div class="mb-2 flex items-center justify-between">
            <h3 class="insp-title !mb-0">{{ t('canvas.inspector.sections.capabilities') }}</h3>
            <RouterLink v-if="agentName" :to="studioLink" class="inline-flex items-center gap-1 text-[11px] text-accent-2 hover:underline" data-testid="inspector-studio-link">
              {{ t('canvas.inspector.editInStudio') }}<Icon name="chevron-right" :size="11" />
            </RouterLink>
          </div>
          <p v-if="!agentName" class="text-[12px] text-txt3">{{ t('canvas.inspector.none') }}</p>
          <p v-else-if="!summary" class="insp-warn !mt-0">{{ t('canvas.inspector.noCapsBody') }}</p>
          <dl v-else class="insp-caps" data-testid="inspector-caps-list">
            <div class="insp-row">
              <dt>{{ t('canvas.inspector.interaction') }}</dt>
              <dd>
                <span class="insp-chip is-main">{{ t(`nodes.capabilities.interaction.${summary.interaction}`) }}</span>
                <span v-if="summary.review" class="insp-chip">{{ t('canvas.caps.review') }}</span>
                <span v-if="summary.gated" class="insp-chip">{{ t('nodes.capabilities.gated') }}</span>
              </dd>
            </div>
            <div class="insp-row">
              <dt>{{ t('canvas.inspector.tools') }}</dt>
              <dd>
                <span v-for="tool in summary.tools" :key="tool" class="insp-chip">{{ t(`nodes.capabilities.tools.${tool}.label`) }}</span>
                <span v-if="!summary.tools.length" class="insp-none">{{ t('canvas.inspector.none') }}</span>
              </dd>
            </div>
            <div class="insp-row">
              <dt>{{ t('canvas.inspector.reads') }}</dt>
              <dd>
                <span v-if="summary.readsAll" class="insp-chip">{{ t('nodes.capabilities.readsAll') }}</span>
                <span v-for="r in summary.reads" :key="r" class="insp-chip font-mono">{{ r }}</span>
                <span v-if="!summary.readsAll && !summary.reads.length" class="insp-none">{{ t('canvas.inspector.none') }}</span>
              </dd>
            </div>
            <div class="insp-row">
              <dt>{{ t('canvas.inspector.writes') }}</dt>
              <dd>
                <span
                  v-for="w in summary.writes"
                  :key="w.name"
                  class="insp-chip"
                  :title="`${schemaLabel(w.name, tr)} · ${w.required ? t('nodes.capabilities.required') : t('nodes.capabilities.optional')}`"
                >
                  <span class="insp-dot" :class="w.required ? 'is-required' : ''" />{{ schemaLabel(w.name, tr) }}
                </span>
                <span v-if="!summary.writes.length" class="insp-none">{{ t('canvas.inspector.none') }}</span>
              </dd>
            </div>
          </dl>
          <p v-if="summary && summary.writes.length" class="insp-legend">
            <span class="insp-dot is-required" />{{ t('nodes.capabilities.required') }}
            <span class="insp-dot ml-2" />{{ t('nodes.capabilities.optional') }}
          </p>
          <p v-if="capsIssue" class="insp-warn">{{ capsIssue }}</p>
        </section>
      </template>

      <section v-if="fields.length" class="insp-card">
        <h3 class="insp-title">{{ t('canvas.inspector.sections.settings') }}</h3>
        <div v-for="f in fields" :key="f.key" class="mb-3 last:mb-0" :data-testid="`field-${f.key}`">
          <label v-if="f.type !== 'switch'" class="label">
            {{ f.label }}<span v-if="f.optional" class="ml-1 text-txt3">({{ t('common.optional') }})</span>
          </label>

          <input v-if="f.type === 'text'" v-model="cfg[f.key]" class="input" :placeholder="f.placeholder" />
          <input v-else-if="f.type === 'number'" v-model.number="cfg[f.key]" type="number" class="input" :placeholder="f.placeholder" />
          <div v-else-if="f.type === 'duration'" class="flex items-center gap-2">
            <input v-model.number="cfg[f.key]" type="number" min="1" class="input flex-1" :placeholder="t('canvas.inspector.timeoutPlaceholder')" />
            <span class="chip">{{ t('common.minutes') }}</span>
          </div>
          <select v-else-if="f.type === 'select'" v-model="cfg[f.key]" class="input">
            <option v-for="o in fieldOptions(f)" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
          <textarea v-else-if="f.type === 'textarea'" v-model="cfg[f.key]" class="input min-h-[80px] text-[12px]" :placeholder="f.placeholder" />

          <div v-else-if="f.type === 'switch'" class="flex items-center justify-between gap-2">
            <span class="label !mb-0">{{ f.label }}</span>
            <AppSwitch :model-value="switchOn(f)" :aria-label="f.label || f.key" :data-testid="'node-switch-' + f.key" @update:model-value="cfg[f.key] = $event" />
          </div>

          <OutputSourcesEditor
            v-else-if="f.type === 'output_sources'"
            :node="node"
            :all-nodes="allNodes"
            :edges="edges"
          />

          <VariablesField v-else-if="f.type === 'variables'" :config="cfg" />

          <div v-else-if="f.type === 'assignments'" class="space-y-2">
            <div v-for="(a, i) in list('assignments')" :key="i" class="flex items-center gap-2">
              <input v-model="a.var" class="input w-28 font-mono text-[12px]" :placeholder="t('pages.workflowEditor.inspector.assignments.selectVar')" list="insp-vars" />
              <span class="text-txt3">=</span>
              <input v-model="a.expr" class="input flex-1 font-mono text-[12px]" :placeholder="t('pages.workflowEditor.inspector.assignments.exprPlaceholder')" />
              <button type="button" class="text-txt3 hover:text-err" :aria-label="t('common.buttons.delete')" @click="list('assignments').splice(i, 1)"><Icon name="close" :size="14" /></button>
            </div>
            <datalist id="insp-vars">
              <option v-for="g in globalVars" :key="g.name" :value="g.name" />
            </datalist>
            <AppButton size="sm" variant="subtle" icon="plus" @click="list('assignments').push({ var: '', expr: '' })">{{ t('pages.workflowEditor.inspector.assignments.add') }}</AppButton>
          </div>

          <div v-else-if="f.type === 'cases'" class="space-y-2" data-testid="field-cases-list">
            <div v-for="(c, i) in list('cases')" :key="i" class="rounded-md border border-line bg-surface p-2">
              <div class="mb-1.5 flex items-center gap-2">
                <span class="rounded bg-accent-dim px-1.5 py-0.5 font-mono text-[10px] font-semibold text-accent-2">{{ i === 0 ? 'IF' : 'ELSE IF' }}</span>
                <input
                  :value="c.id"
                  class="input h-7 w-28 font-mono text-[11.5px]"
                  :placeholder="t('canvas.inspector.cases.idPlaceholder')"
                  :aria-label="t('canvas.inspector.cases.idPlaceholder')"
                  @change="setRowId(c, ($event.target as HTMLInputElement).value)"
                />
                <button type="button" class="ml-auto text-txt3 hover:text-err" :aria-label="t('pages.workflowEditor.inspector.cases.deleteBranch')" @click="list('cases').splice(i, 1)"><Icon name="close" :size="13" /></button>
              </div>
              <input v-model="c.when" class="input font-mono text-[12px]" :placeholder="t('canvas.inspector.cases.whenPlaceholder')" />
            </div>
            <div class="flex items-center gap-2 rounded-md border border-dashed border-line px-2 py-1.5 text-[11px] text-txt3">
              <span class="rounded bg-warn/15 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-warn">ELSE</span>
              {{ t('canvas.inspector.cases.elseNote') }}
            </div>
            <AppButton size="sm" variant="subtle" icon="plus" @click="addCase">{{ t('canvas.inspector.cases.add') }}</AppButton>
          </div>

          <div v-else-if="f.type === 'actions'" class="space-y-2" data-testid="field-actions-list">
            <div v-for="(a, i) in list('actions')" :key="i" class="rounded-md border border-line bg-surface p-2">
              <div class="flex items-center gap-2">
                <input
                  :value="a.id"
                  class="input w-24 font-mono text-[12px]"
                  :placeholder="t('pages.workflowEditor.inspector.actions.idPlaceholder')"
                  @change="setRowId(a, ($event.target as HTMLInputElement).value)"
                />
                <input v-model="a.label" class="input flex-1" :placeholder="t('pages.workflowEditor.inspector.actions.labelPlaceholder')" />
                <button type="button" class="text-txt3 hover:text-err" :aria-label="t('common.buttons.delete')" @click="list('actions').splice(i, 1)"><Icon name="close" :size="14" /></button>
              </div>
              <div class="mt-1.5 flex items-center gap-2">
                <button type="button" class="chip" :class="a.requireForm ? 'border-accent/50 text-accent-2' : ''" @click="a.requireForm = !a.requireForm">
                  {{ t('pages.workflowEditor.inspector.actions.requireForm') }}
                </button>
                <span class="text-[10px] text-txt3">{{ t('pages.workflowEditor.inspector.actions.requireFormHint') }}</span>
              </div>
            </div>
            <AppButton size="sm" variant="subtle" icon="plus" @click="addAction">{{ t('canvas.inspector.actions.add') }}</AppButton>
            <p class="text-[10.5px] leading-4 text-txt3">{{ t('canvas.inspector.actions.note') }}</p>
          </div>

          <div v-else-if="f.type === 'form'" class="space-y-2">
            <div v-for="(ff, i) in list('form')" :key="i" class="flex items-center gap-2">
              <input v-model="ff.key" class="input w-24 font-mono text-[12px]" :placeholder="t('pages.workflowEditor.inspector.form.keyPlaceholder')" />
              <input v-model="ff.label" class="input flex-1" :placeholder="t('pages.workflowEditor.inspector.form.labelPlaceholder')" />
              <button type="button" class="chip" :class="ff.required ? 'border-accent/50 text-accent-2' : ''" @click="ff.required = !ff.required">{{ t('common.required') }}</button>
              <button type="button" class="text-txt3 hover:text-err" :aria-label="t('common.buttons.delete')" @click="list('form').splice(i, 1)"><Icon name="close" :size="14" /></button>
            </div>
            <AppButton size="sm" variant="subtle" icon="plus" @click="list('form').push({ key: 'field', label: t('pages.workflowEditor.inspector.defaultFieldLabel'), required: false })">
              {{ t('pages.workflowEditor.inspector.form.add') }}
            </AppButton>
          </div>

          <p v-if="f.help" class="mt-1 text-[11px] leading-4 text-txt3">{{ f.help }}</p>
        </div>
      </section>

      <section v-if="def?.outputs?.length" class="insp-card">
        <h3 class="insp-title">{{ t('canvas.inspector.sections.outputs') }}</h3>
        <div v-for="o in def.outputs" :key="o.key" class="mb-1 flex items-start gap-2 last:mb-0">
          <code class="shrink-0 rounded bg-base px-1.5 py-0.5 font-mono text-[11px] text-accent-2">{{ o.key }}</code>
          <span class="text-[11px] leading-5 text-txt3">{{ o.desc }}</span>
        </div>
      </section>
    </div>
  </aside>
</template>

<style scoped>
.insp-card {
  border: 1px solid rgb(var(--c-line));
  border-radius: 10px;
  background: rgb(var(--c-base) / 0.45);
  padding: 12px;
}
.insp-title {
  margin-bottom: 8px;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.02em;
  color: rgb(var(--c-txt2));
}
.insp-warn {
  margin-top: 8px;
  border-radius: 8px;
  border: 1px solid rgb(var(--c-warn) / 0.35);
  background: rgb(var(--c-warn) / 0.08);
  padding: 6px 10px;
  font-size: 12px;
  line-height: 1.45;
  color: rgb(var(--c-warn));
}
.insp-caps {
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr);
  column-gap: 12px;
  row-gap: 8px;
  font-size: 12px;
}
.insp-row {
  display: contents;
}
.insp-row dt {
  align-self: start;
  color: rgb(var(--c-txt3));
  line-height: 22px;
  white-space: nowrap;
}
.insp-row dd {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  gap: 4px;
  color: rgb(var(--c-txt));
}
.insp-chip {
  display: inline-flex;
  max-width: 100%;
  align-items: center;
  gap: 5px;
  height: 22px;
  padding: 0 8px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 6px;
  background: rgb(var(--c-surface));
  font-size: 11.5px;
  line-height: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.insp-chip.is-main {
  border-color: rgb(var(--c-accent) / 0.35);
  background: rgb(var(--c-accent-dim));
  color: rgb(var(--c-accent-2));
}
.insp-none {
  line-height: 22px;
  color: rgb(var(--c-txt3));
}
.insp-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  flex: none;
  border-radius: 9999px;
  border: 1px solid rgb(var(--c-txt3));
}
.insp-dot.is-required {
  border-color: rgb(var(--c-accent-2));
  background: rgb(var(--c-accent-2));
}
.insp-legend {
  margin-top: 10px;
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  color: rgb(var(--c-txt3));
}
</style>
