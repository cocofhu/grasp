<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import AppButton from '../ui/AppButton.vue'
import AppSwitch from '../ui/AppSwitch.vue'
import { nodeColorHex, syncHumanGateFormDefaults } from '@/data/nodeRegistry'
import { useNodeDefs } from '@/lib/run/useNodeDefs'
import { buildOutputSourceOptions } from '@/lib/run/outputSourceOptions'
import OutputSourcesEditor from './OutputSourcesEditor.vue'
import { api } from '@/lib/api/api'
import { renderMarkdown } from '@/lib/shared/markdown'
import { getAgentProfile } from '@/lib/run/workflowIO'
import type { WFNode, WFEdge, FieldSchema } from '@/lib/shared/types'

const { t } = useI18n()
const { NODE_DEFS } = useNodeDefs()

const props = defineProps<{
  node: WFNode
  allNodes: WFNode[]
  edges: WFEdge[]
  /** Workflow home project; required for same-project Agent filtering. */
  projectId?: string
  outputMigration?: boolean
}>()
const emit = defineEmits<{ (e: 'delete'): void }>()

const def = computed(() => NODE_DEFS.value[props.node.type])
const hex = computed(() => nodeColorHex(props.node.type))

watch(
  () =>
    props.node.type === 'human_gate'
      ? String(props.node.config?.body_template ?? '')
      : null,
  () => {
    if (props.node.type !== 'human_gate') return
    syncHumanGateFormDefaults(props.node.config)
  },
)

const tab = ref<'config' | 'help'>('config')
const helpHtml = computed(() => (def.value?.help ? renderMarkdown(def.value.help) : ''))

type AgentRow = { name: string; projectId?: string }
const agents = ref<AgentRow[]>([])

/** Classify a configured agent_profile against the workflow project. */
function classifyAgentProfile(name: string): { ok: boolean; label: string } {
  const cur = String(name || '').trim()
  if (!cur) return { ok: true, label: '' }
  const a = agents.value.find((x) => x.name === cur)
  if (!a) return { ok: false, label: t('pages.workflowEditor.inspector.staleReasons.deleted') }
  const home = String(a.projectId || '').trim()
  if (!home) return { ok: false, label: t('pages.workflowEditor.inspector.staleReasons.unbound') }
  const pid = String(props.projectId || '').trim()
  // No workflow projectId must not widen to global selectable — treat as foreign.
  if (!pid || home !== pid) {
    return { ok: false, label: t('pages.workflowEditor.inspector.staleReasons.foreign') }
  }
  return { ok: true, label: '' }
}

const agentProfileStale = computed(() => {
  const hasField = def.value?.fields?.some((f) => f.key === 'agent_profile')
  if (!hasField) return null
  const cur = getAgentProfile(props.node.config)
  if (!cur) return null
  const st = classifyAgentProfile(cur)
  if (st.ok) return null
  return { name: cur, reason: st.label }
})

onMounted(async () => {
  try {
    agents.value = ((await api.listAgents()) || []).map((a) => ({
      name: a.name,
      projectId: a.projectId,
    }))
  } catch {
    agents.value = []
  }
  // Empty agent_profile must remain allowed — do not auto-fill the first agent.
})

const gateBodyOptions = computed(() =>
  buildOutputSourceOptions(props.allNodes, props.edges, props.node.id, t),
)

function fieldOptions(f: FieldSchema): { value: string; label: string }[] {
  if (f.key === 'agent_profile') {
    const pid = String(props.projectId || '').trim()
    // Only same-project, non-empty projectId agents are selectable.
    const same = pid
      ? agents.value.filter((a) => String(a.projectId || '').trim() === pid)
      : []
    const opts: { value: string; label: string }[] = [
      { value: '', label: t('pages.workflowEditor.inspector.emptyAgent') },
    ]
    for (const a of same) {
      opts.push({ value: a.name, label: a.name })
    }
    const cur = getAgentProfile(props.node.config)
    if (cur && !same.some((a) => a.name === cur)) {
      const st = classifyAgentProfile(cur)
      opts.push({
        value: cur,
        label: st.ok ? cur : `${cur} · ${st.label}`,
      })
    }
    if (opts.length === 1 && !same.length) {
      // Keep empty option; hint via noAgents when nothing selectable.
      opts[0] = { value: '', label: t('pages.workflowEditor.inspector.noAgents') }
    }
    return opts
  }
  if (f.key === 'body_template') {
    const opts = [...gateBodyOptions.value]
    const cur = (props.node.config?.body_template || '').toString()
    if (cur && !opts.some((o) => o.value === cur)) {
      opts.unshift({ value: cur, label: t('common.gateBodyLabels.custom', { value: cur }) })
    }
    return [
      { value: '', label: opts.length ? t('pages.workflowEditor.inspector.selectBody') : t('pages.workflowEditor.inspector.connectUpstreamForBody') },
      ...opts,
    ]
  }
  return f.options || []
}

const globalVars = computed<{ name: string; type: string; value: any }[]>(() => {
  const input = props.allNodes.find((n) => n.type === 'input')
  const out: { name: string; type: string; value: any }[] = [...((input?.config?.variables as any[]) || [])]
  const seen = new Set(out.map((v) => v.name))
  const add = (name: string, type = 'string') => {
    const n = String(name || '').trim()
    if (!n || seen.has(n)) return
    seen.add(n)
    out.push({ name: n, type, value: '' })
  }
  for (const n of props.allNodes) {
    if (n.type === 'human_gate') {
      add(String(n.config?.output_var || 'action'))
      for (const f of (n.config?.form as any[]) || []) add(String(f?.key || ''), 'paragraph')
    }
    if (n.type === 'test' || n.type === 'review') {
      add(String(n.config?.reason_var || 'reason'))
    }
  }
  return out
})

// repoNames collects repo names from every `repos`-typed workflow variable so a
// `repo_select` field (e.g. submit_mr's target repo) can offer them as options
// instead of a free-text repo name. Accepts either the decoded array or a JSON
// string value.
const repoNames = computed<string[]>(() => {
  const names: string[] = []
  const seen = new Set<string>()
  for (const v of globalVars.value) {
    if ((v as any).type !== 'repos') continue
    let arr: any = (v as any).value
    if (typeof arr === 'string') {
      try {
        arr = JSON.parse(arr)
      } catch {
        arr = []
      }
    }
    if (!Array.isArray(arr)) continue
    for (const r of arr) {
      const nm = String(r?.name || '').trim()
      if (nm && !seen.has(nm)) {
        seen.add(nm)
        names.push(nm)
      }
    }
  }
  return names
})

// Combobox for repo_select: free text + dropdown shortcuts (literal names,
// {{vars.repos}} list mode, empty = legacy default). No multi-select UI.
const REPOS_LIST_TOKEN = '{{vars.repos}}'
const repoComboOpen = ref(false)
const repoComboRoot = ref<HTMLElement | null>(null)

// Function ref: repo_select sits inside v-for; a string ref would become an
// array and break outside-click detection (el.contains).
function bindRepoComboRoot(el: unknown) {
  repoComboRoot.value = el instanceof HTMLElement ? el : null
}

function setRepoSelect(key: string, value: string) {
  props.node.config[key] = value
  repoComboOpen.value = false
}

function onRepoComboDocClick(ev: MouseEvent) {
  if (!repoComboOpen.value) return
  const el = repoComboRoot.value
  if (el && !el.contains(ev.target as Node)) {
    repoComboOpen.value = false
  }
}

onMounted(() => {
  document.addEventListener('mousedown', onRepoComboDocClick)
})
onUnmounted(() => {
  document.removeEventListener('mousedown', onRepoComboDocClick)
})

const condPrompt = computed<{ when_var: string; text: string }>(() => {
  const cfg = props.node.config as Record<string, any>
  if (!cfg.conditional_prompt || typeof cfg.conditional_prompt !== 'object') {
    cfg.conditional_prompt = { when_var: '', text: '' }
  }
  return cfg.conditional_prompt
})

const upstreamIds = computed<Set<string>>(() => {
  const preds: Record<string, string[]> = {}
  for (const e of props.edges) (preds[e.target] ||= []).push(e.source)
  const seen = new Set<string>()
  const stack = [...(preds[props.node.id] || [])]
  while (stack.length) {
    const id = stack.pop()!
    if (seen.has(id)) continue
    seen.add(id)
    for (const p of preds[id] || []) stack.push(p)
  }
  return seen
})

type VarItem = { token: string; label: string }
const varGroups = computed<{ title: string; items: VarItem[] }[]>(() => {
  const groups: { title: string; items: VarItem[] }[] = []
  if (globalVars.value.length) {
    groups.push({
      title: t('pages.workflowEditor.inspector.varGroups.global'),
      items: globalVars.value.filter((v) => v.name).map((v) => ({ token: `{{vars.${v.name}}}`, label: v.name })),
    })
  }
  const upstream: VarItem[] = []
  for (const n of props.allNodes) {
    if (!upstreamIds.value.has(n.id)) continue
    for (const o of NODE_DEFS.value[n.type].outputs) {
      upstream.push({ token: `{{nodes.${n.id}.outputs.${o.key}}}`, label: `${n.label}·${o.desc || o.key}` })
    }
  }
  if (upstream.length) groups.push({ title: t('pages.workflowEditor.inspector.varGroups.upstream'), items: upstream })
  return groups
})

const hasVars = computed(() => varGroups.value.some((g) => g.items.length))

function insertVar(field: string, v: string) {
  props.node.config[field] = (props.node.config[field] || '') + (props.node.config[field] ? ' ' : '') + v
}

function addAction() {
  if (!props.node.config.actions) props.node.config.actions = []
  props.node.config.actions.push({
    id: 'action' + (props.node.config.actions.length + 1),
    label: t('pages.workflowEditor.inspector.newAction'),
  })
}
function addElseIf() {
  if (!props.node.config.cases) props.node.config.cases = []
  const cases = props.node.config.cases
  const elseIdx = cases.findIndex((c: any) => c.when === 'default')
  const row = { when: '', goto: '' }
  if (elseIdx >= 0) cases.splice(elseIdx, 0, row)
  else cases.push(row)
}
function caseLabel(c: any, i: number) {
  if (c.when === 'default') return 'ELSE'
  return i === 0 ? 'IF' : 'ELSE IF'
}
function insertCond(c: any, snippet: string) {
  c.when = (c.when ? c.when + ' && ' : '') + snippet
}
const CONDITION_SNIPPETS = [
  'exists("changes_summary.md")',
  'nodes.review.outputs.action == "approve"',
  'vars.attempt < 3',
]
const VAR_SYNTAX = '{' + '{vars.名称}' + '}'
function addFormField() {
  if (!props.node.config.form) props.node.config.form = []
  props.node.config.form.push({ key: 'field', label: t('pages.workflowEditor.inspector.defaultFieldLabel'), required: false })
}
const VAR_TYPES = computed(() => [
  { value: 'string', label: t('common.varTypes.string') },
  { value: 'paragraph', label: t('common.varTypes.paragraph') },
  { value: 'number', label: t('common.varTypes.number') },
  { value: 'bool', label: t('common.varTypes.bool') },
  { value: 'select', label: t('common.varTypes.select') },
  { value: 'repos', label: t('common.varTypes.repos') },
])
function addVariable() {
  if (!props.node.config.variables) props.node.config.variables = []
  props.node.config.variables.push({ name: '', type: 'string', value: '', ask: false, desc: '', required: false, editable: true })
}
// asRepos coerces a `repos`-typed variable's value into an editable array of
// {name,url,branch}. Accepts a prior JSON string or undefined.
function asRepos(v: any): any[] {
  if (!Array.isArray(v.value)) {
    if (typeof v.value === 'string' && v.value.trim()) {
      try {
        const p = JSON.parse(v.value)
        v.value = Array.isArray(p) ? p : []
      } catch {
        v.value = []
      }
    } else {
      v.value = []
    }
  }
  return v.value
}
function addRepo(v: any) {
  asRepos(v).push({ name: '', url: '', branch: '' })
}
function removeRepo(v: any, i: number) {
  asRepos(v).splice(i, 1)
}
function addAssignment() {
  if (!props.node.config.assignments) props.node.config.assignments = []
  props.node.config.assignments.push({ var: '', expr: '' })
}

/** Preview injection and Live default on when omitted by legacy graphs. */
function switchOn(f: FieldSchema): boolean {
  const v = props.node.config?.[f.key]
  if ((f.key === 'auto_inject' || f.key === 'live_variants') && (v === undefined || v === null || v === '')) {
    return true
  }
  return !!v
}

function setSwitch(key: string, on: boolean) {
  if (!props.node.config) props.node.config = {}
  props.node.config[key] = on
}
</script>

<template>
  <div class="flex h-full flex-col">
    <div class="flex items-center gap-2.5 border-b border-line px-4 py-3">
      <div class="flex h-9 w-9 items-center justify-center rounded-md" :style="{ background: hex + '22', color: hex }">
        <Icon :name="def?.icon || 'agent'" :size="18" />
      </div>
      <div class="min-w-0 flex-1">
        <div class="text-sm font-semibold text-txt">{{ def?.label || node.type }} {{ t('pages.workflowEditor.inspector.nodeSuffix') }}</div>
        <div class="truncate text-[11px] text-txt3">{{ def?.desc }}</div>
      </div>
      <button class="flex h-8 w-8 items-center justify-center rounded-md text-txt3 hover:bg-err/10 hover:text-err" :title="t('pages.workflowEditor.inspector.deleteNode')" @click="emit('delete')">
        <Icon name="close" :size="16" />
      </button>
    </div>

    <div v-if="def?.help" class="flex items-center gap-1 border-b border-line px-3 pt-2">
      <button
        class="rounded-t-md px-3 py-1.5 text-[12px] font-medium transition"
        :class="tab === 'config' ? 'border-b-2 border-accent text-txt' : 'text-txt3 hover:text-txt2'"
        @click="tab = 'config'"
      >{{ t('pages.workflowEditor.inspector.tabConfig') }}</button>
      <button
        class="rounded-t-md px-3 py-1.5 text-[12px] font-medium transition"
        :class="tab === 'help' ? 'border-b-2 border-accent text-txt' : 'text-txt3 hover:text-txt2'"
        @click="tab = 'help'"
      >{{ t('pages.workflowEditor.inspector.tabHelp') }}</button>
    </div>

    <div v-show="!def.help || tab === 'config'" class="scroll-area flex-1 overflow-y-auto p-4">
      <div class="mb-4">
        <label class="label">{{ t('pages.workflowEditor.inspector.nodeName') }}</label>
        <input v-model="node.label" class="input" />
      </div>

      <div v-for="f in def.fields" :key="f.key" class="mb-4">
        <label class="label">
          {{ f.label }}
          <span v-if="f.optional" class="ml-1 text-txt3">({{ t('common.optional') }})</span>
        </label>

        <input v-if="f.type === 'text'" v-model="node.config[f.key]" class="input" :placeholder="f.placeholder" />

        <template v-else-if="f.type === 'repo_select'">
          <div :ref="bindRepoComboRoot" class="relative">
            <div class="flex gap-1">
              <input
                v-model="node.config[f.key]"
                class="input flex-1 font-mono text-[12px]"
                :placeholder="f.placeholder || t('pages.workflowEditor.inspector.repoSelect.placeholder')"
                autocomplete="off"
                @focus="repoComboOpen = true"
              />
              <button
                type="button"
                class="chip shrink-0 px-2 hover:border-accent/50"
                :aria-expanded="repoComboOpen"
                :title="t('pages.workflowEditor.inspector.repoSelect.toggle')"
                @click="repoComboOpen = !repoComboOpen"
              >
                <Icon name="chevron-down" :size="14" />
              </button>
            </div>
            <div
              v-if="repoComboOpen"
              class="card scroll-area absolute left-0 right-0 z-20 mt-1 max-h-64 overflow-y-auto"
              role="listbox"
            >
              <button
                type="button"
                class="block w-full px-3 py-2 text-left text-[12px] text-accent-2 hover:bg-base"
                role="option"
                @click="setRepoSelect(f.key, REPOS_LIST_TOKEN)"
              >
                <span class="font-mono">{{ REPOS_LIST_TOKEN }}</span>
                <span class="ml-2 text-[10px] text-txt3">{{ t('pages.workflowEditor.inspector.repoSelect.reposListHint') }}</span>
              </button>
              <button
                type="button"
                class="block w-full px-3 py-2 text-left text-[12px] text-txt2 hover:bg-base"
                role="option"
                @click="setRepoSelect(f.key, '')"
              >
                {{ t('pages.workflowEditor.inspector.repoSelect.any') }}
              </button>
              <div v-if="repoNames.length" class="border-t border-line px-3 py-1 text-[10px] text-txt3">
                {{ t('pages.workflowEditor.inspector.repoSelect.literalGroup') }}
              </div>
              <button
                v-for="rn in repoNames"
                :key="rn"
                type="button"
                class="block w-full px-3 py-1.5 text-left font-mono text-[12px] text-txt hover:bg-base"
                role="option"
                @click="setRepoSelect(f.key, rn)"
              >{{ rn }}</button>
              <p v-if="!repoNames.length" class="px-3 py-2 text-[11px] leading-4 text-txt3">
                {{ t('pages.workflowEditor.inspector.repoSelect.empty') }}
              </p>
            </div>
          </div>
          <p class="mt-1 text-[11px] leading-4 text-txt3">{{ t('pages.workflowEditor.inspector.repoSelect.hint') }}</p>
        </template>

        <input v-else-if="f.type === 'number'" v-model.number="node.config[f.key]" type="number" class="input" :placeholder="f.placeholder" />

        <div v-else-if="f.type === 'duration'" class="flex items-center gap-2">
          <input v-model.number="node.config[f.key]" type="number" class="input flex-1" placeholder="30" />
          <span class="chip">{{ t('common.minutes') }}</span>
        </div>

        <template v-else-if="f.type === 'select'">
          <select v-model="node.config[f.key]" class="input">
            <option v-for="o in fieldOptions(f)" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
          <div
            v-if="f.key === 'agent_profile' && agentProfileStale"
            class="rounded-lg mt-2 border border-warn/40 bg-warn/10 px-2.5 py-2 text-[12px] leading-5 text-warn"
            data-testid="skill-profile-stale-banner"
          >
            <strong class="font-semibold">{{ t('pages.workflowEditor.inspector.staleBannerTitle') }}</strong>
            —
            {{ t('pages.workflowEditor.inspector.staleBannerBody', { name: agentProfileStale.name, reason: agentProfileStale.reason }) }}
          </div>
          <p
            v-else-if="f.key === 'agent_profile'"
            class="mt-1 text-[11px] leading-4 text-txt3"
          >{{ t('pages.workflowEditor.inspector.agentProfileHint') }}</p>
        </template>

        <OutputSourcesEditor
          v-else-if="f.type === 'output_sources'"
          :node="node"
          :all-nodes="allNodes"
          :edges="edges"
          :show-migration="outputMigration && node.type === 'output'"
        />

        <div
          v-else-if="f.type === 'switch'"
          class="flex items-center gap-2"
        >
          <AppSwitch
            :model-value="switchOn(f)"
            :aria-label="f.label || f.key"
            :data-testid="'node-switch-' + f.key"
            @update:model-value="setSwitch(f.key, $event)"
          />
          <span class="text-xs text-txt2">{{ switchOn(f) ? t('common.switchOn') : t('common.switchOff') }}</span>
        </div>

        <template v-else-if="f.type === 'textarea' || f.type === 'prompt'">
          <textarea v-model="node.config[f.key]" class="input min-h-[96px] font-mono text-[12px] leading-relaxed" :placeholder="f.placeholder" />
          <div v-if="f.type === 'prompt'" class="mt-2 space-y-1.5">
            <div v-if="!hasVars" class="text-[10px] text-txt3">{{ t('pages.workflowEditor.inspector.promptVarHint') }}</div>
            <div v-for="g in varGroups" :key="g.title" class="flex flex-wrap items-center gap-1.5">
              <span class="w-12 shrink-0 text-[10px] text-txt3">{{ g.title }}</span>
              <button
                v-for="it in g.items"
                :key="it.token"
                class="rounded border border-line bg-base px-1.5 py-0.5 text-[10px] text-accent-2 hover:border-accent/50"
                :title="it.token"
                @click="insertVar(f.key, it.token)"
              >{{ it.label }}</button>
            </div>
          </div>
        </template>

        <div v-else-if="f.type === 'actions'" class="space-y-2">
          <div v-for="(a, i) in node.config.actions || []" :key="i" class="rounded-md border border-line bg-base/40 p-2">
            <div class="flex items-center gap-2">
              <input v-model="a.id" class="input w-24 font-mono text-[12px]" :placeholder="t('pages.workflowEditor.inspector.actions.idPlaceholder')" />
              <input v-model="a.label" class="input flex-1" :placeholder="t('pages.workflowEditor.inspector.actions.labelPlaceholder')" />
              <button class="text-txt3 hover:text-err" @click="node.config.actions.splice(i, 1)"><Icon name="close" :size="14" /></button>
            </div>
            <div class="mt-1.5 flex items-center gap-2">
              <span class="shrink-0 text-[11px] text-txt3">{{ t('pages.workflowEditor.inspector.actions.goto') }}</span>
              <select v-model="a.goto" class="input flex-1 text-[12px]">
                <option value="">{{ t('pages.workflowEditor.inspector.actions.gotoDefault') }}</option>
                <option v-for="n in allNodes" :key="n.id" :value="n.id">{{ n.label }} ({{ n.id }})</option>
              </select>
            </div>
            <div class="mt-1.5 flex items-center gap-2">
              <button
                class="chip"
                :class="a.requireForm ? 'border-accent/50 text-accent-2' : ''"
                @click="a.requireForm = !a.requireForm"
              >{{ t('pages.workflowEditor.inspector.actions.requireForm') }}</button>
              <span class="text-[10px] text-txt3">{{ t('pages.workflowEditor.inspector.actions.requireFormHint') }}</span>
            </div>
          </div>
          <AppButton size="sm" variant="subtle" icon="plus" @click="addAction">{{ t('pages.workflowEditor.inspector.actions.add') }}</AppButton>
          <p class="text-[10px] leading-4 text-txt3" v-html="t('pages.workflowEditor.inspector.actions.help')" />
        </div>

        <div v-else-if="f.type === 'conditional'" class="space-y-2">
          <div class="flex items-center gap-2">
            <span class="shrink-0 text-[11px] text-txt3">{{ t('pages.workflowEditor.inspector.conditional.whenVar') }}</span>
            <select v-model="condPrompt.when_var" class="input flex-1 text-[12px]">
              <option value="">{{ t('pages.workflowEditor.inspector.conditional.noInject') }}</option>
              <option v-for="g in globalVars" :key="g.name" :value="g.name">{{ g.name }}</option>
            </select>
            <span class="shrink-0 text-[11px] text-txt3">{{ t('pages.workflowEditor.inspector.conditional.whenPresent') }}</span>
          </div>
          <textarea v-model="condPrompt.text" class="input min-h-[60px] text-[12px]" :placeholder="t('pages.workflowEditor.inspector.conditional.textPlaceholder')" />
          <p class="text-[10px] leading-4 text-txt3">{{ t('pages.workflowEditor.inspector.conditional.help') }}</p>
        </div>

        <div v-else-if="f.type === 'form'" class="space-y-2">
          <div v-for="(ff, i) in node.config.form || []" :key="i" class="flex items-center gap-2">
            <input v-model="ff.key" class="input w-24 font-mono text-[12px]" :placeholder="t('pages.workflowEditor.inspector.form.keyPlaceholder')" />
            <input v-model="ff.label" class="input flex-1" :placeholder="t('pages.workflowEditor.inspector.form.labelPlaceholder')" />
            <button class="chip" :class="ff.required ? 'border-accent/50 text-accent-2' : ''" @click="ff.required = !ff.required">{{ t('common.required') }}</button>
            <button class="text-txt3 hover:text-err" @click="node.config.form.splice(i, 1)"><Icon name="close" :size="14" /></button>
          </div>
          <AppButton size="sm" variant="subtle" icon="plus" @click="addFormField">{{ t('pages.workflowEditor.inspector.form.add') }}</AppButton>
        </div>

        <div v-else-if="f.type === 'cases'" class="space-y-2.5">
          <div v-for="(c, i) in node.config.cases || []" :key="i" class="rounded-md border border-line bg-base/40 p-2.5">
            <div class="mb-1.5 flex items-center gap-2">
              <span
                class="rounded px-1.5 py-0.5 font-mono text-[10px] font-semibold"
                :class="c.when === 'default' ? 'bg-warn/15 text-warn' : i === 0 ? 'bg-accent-dim text-accent-2' : 'bg-elevated text-txt2'"
              >{{ caseLabel(c, i) }}</span>
              <span class="text-[10px] text-txt3">{{ t('pages.workflowEditor.inspector.cases.priority', { n: i + 1 }) }}</span>
              <button v-if="c.when !== 'default'" class="ml-auto text-txt3 hover:text-err" :title="t('pages.workflowEditor.inspector.cases.deleteBranch')" @click="node.config.cases.splice(i, 1)"><Icon name="close" :size="13" /></button>
            </div>
            <input
              v-if="c.when !== 'default'"
              v-model="c.when"
              class="input mb-1.5 font-mono text-[12px]"
              :placeholder="t('pages.workflowEditor.inspector.cases.whenPlaceholder')"
            />
            <div v-else class="mb-1.5 text-[11px] text-txt3">{{ t('pages.workflowEditor.inspector.cases.elseFallback') }}</div>
            <div v-if="c.when !== 'default'" class="mb-2 flex flex-wrap gap-1">
              <span class="text-[10px] text-txt3">{{ t('pages.workflowEditor.inspector.cases.insert') }}</span>
              <button
                v-for="s in CONDITION_SNIPPETS"
                :key="s"
                class="rounded border border-line bg-base px-1.5 py-0.5 font-mono text-[10px] text-info hover:border-info/50"
                @click="insertCond(c, s)"
              >{{ s.split('(')[0].split(' ')[0] }}</button>
            </div>
            <div class="flex items-center gap-2">
              <Icon name="chevron-right" :size="14" class="shrink-0 text-txt3" />
              <span class="shrink-0 text-[11px] text-txt3">{{ t('pages.workflowEditor.inspector.cases.goto') }}</span>
              <select v-model="c.goto" class="input flex-1 text-[12px]">
                <option value="" disabled>{{ t('pages.workflowEditor.inspector.cases.selectTarget') }}</option>
                <option v-for="n in allNodes" :key="n.id" :value="n.id">{{ n.label }} ({{ n.id }})</option>
              </select>
            </div>
          </div>
          <AppButton size="sm" variant="subtle" icon="plus" @click="addElseIf">{{ t('pages.workflowEditor.inspector.cases.addElseIf') }}</AppButton>
          <p class="text-[10px] leading-4 text-txt3" v-html="t('pages.workflowEditor.inspector.cases.help')" />
        </div>

        <div v-else-if="f.type === 'variables'" class="space-y-2">
          <div v-for="(v, i) in node.config.variables || []" :key="i" class="rounded-md border border-line bg-base/40 p-2.5 space-y-1.5">
            <div class="flex items-center gap-2">
              <input v-model="v.name" class="input w-32 font-mono text-[12px]" :placeholder="t('pages.workflowEditor.inspector.variables.namePlaceholder')" />
              <select v-model="v.type" class="input flex-1 text-[12px]">
                <option v-for="vt in VAR_TYPES" :key="vt.value" :value="vt.value">{{ vt.label }}</option>
              </select>
              <button class="text-txt3 hover:text-err" @click="node.config.variables.splice(i, 1)"><Icon name="close" :size="14" /></button>
            </div>
            <input v-if="v.ask" v-model="v.desc" class="input text-[12px]" :placeholder="t('pages.workflowEditor.inspector.variables.descPlaceholder')" />
            <input v-if="v.type === 'select'" v-model="v.options" class="input text-[12px]" :placeholder="t('pages.workflowEditor.inspector.variables.optionsPlaceholder')" />
            <select v-else-if="v.type === 'bool'" v-model="v.value" class="input text-[12px]">
              <option :value="true">true</option>
              <option :value="false">false</option>
            </select>
            <input v-else-if="v.type === 'number'" v-model.number="v.value" type="number" class="input text-[12px]" :placeholder="v.ask ? t('pages.workflowEditor.inspector.variables.defaultOptional') : t('pages.workflowEditor.inspector.variables.initialValue')" />
            <div v-else-if="v.type === 'repos'" class="space-y-2">
              <div v-for="(r, ri) in asRepos(v)" :key="ri" class="rounded-md border border-line bg-base/40 p-2 space-y-1.5">
                <div class="flex items-center gap-1.5">
                  <span class="flex items-center gap-1 text-[11px] font-medium text-txt2"><Icon name="git" :size="12" />{{ t('pages.workflowEditor.inspector.repos.itemLabel', { n: ri + 1 }) }}</span>
                  <button class="ml-auto shrink-0 text-txt3 hover:text-err" :title="t('pages.workflowEditor.inspector.repos.remove')" @click="removeRepo(v, ri)"><Icon name="close" :size="14" /></button>
                </div>
                <input v-model="r.url" class="input w-full font-mono text-[12px]" :placeholder="t('pages.workflowEditor.inspector.repos.urlPlaceholder')" />
                <div class="flex items-center gap-1.5">
                  <input v-model="r.name" class="input flex-1 font-mono text-[12px]" :placeholder="t('pages.workflowEditor.inspector.repos.namePlaceholder')" />
                  <input v-model="r.branch" class="input flex-1 font-mono text-[12px]" :placeholder="t('pages.workflowEditor.inspector.repos.branchPlaceholder')" />
                </div>
              </div>
              <AppButton size="sm" variant="subtle" icon="plus" @click="addRepo(v)">{{ t('pages.workflowEditor.inspector.repos.add') }}</AppButton>
            </div>
            <textarea v-else v-model="v.value" class="input min-h-[40px] text-[12px]" :placeholder="v.ask ? t('pages.workflowEditor.inspector.variables.defaultOptional') : t('pages.workflowEditor.inspector.variables.initialValue')" />
            <div class="flex flex-wrap items-center gap-1.5">
              <button class="chip whitespace-nowrap" :class="v.ask ? 'border-accent/50 text-accent-2' : 'text-txt3'" @click="v.ask = !v.ask" :title="t('pages.workflowEditor.inspector.variables.askTitle')">
                <Icon name="play" :size="12" />{{ t('pages.workflowEditor.inspector.variables.askAtLaunch') }}
              </button>
              <template v-if="v.ask">
                <button class="chip whitespace-nowrap" :class="v.required ? 'border-accent/50 text-accent-2' : 'text-txt3'" @click="v.required = !v.required">
                  <Icon name="check" :size="12" />{{ t('common.required') }}
                </button>
                <button class="chip whitespace-nowrap" :class="v.editable !== false ? 'border-accent/50 text-accent-2' : 'text-txt3'" @click="v.editable = v.editable === false" :title="v.editable !== false ? t('pages.workflowEditor.inspector.variables.editableTitle') : t('pages.workflowEditor.inspector.variables.lockedTitle')">
                  <Icon :name="v.editable !== false ? 'edit' : 'gate'" :size="12" />{{ v.editable !== false ? t('pages.workflowEditor.inspector.variables.editable') : t('pages.workflowEditor.inspector.variables.locked') }}
                </button>
              </template>
            </div>
          </div>
          <AppButton size="sm" variant="subtle" icon="plus" @click="addVariable">{{ t('pages.workflowEditor.inspector.variables.add') }}</AppButton>
          <p class="text-[10px] leading-4 text-txt3" v-html="t('pages.workflowEditor.inspector.variables.help', { syntax: VAR_SYNTAX })" />
        </div>

        <div v-else-if="f.type === 'assignments'" class="space-y-2">
          <div v-for="(a, i) in node.config.assignments || []" :key="i" class="flex items-center gap-2">
            <select v-model="a.var" class="input w-32 text-[12px]">
              <option value="" disabled>{{ t('pages.workflowEditor.inspector.assignments.selectVar') }}</option>
              <option v-for="g in globalVars" :key="g.name" :value="g.name">{{ g.name }} ({{ g.type }})</option>
            </select>
            <span class="text-txt3">=</span>
            <input v-model="a.expr" class="input flex-1 font-mono text-[12px]" :placeholder="t('pages.workflowEditor.inspector.assignments.exprPlaceholder')" />
            <button class="text-txt3 hover:text-err" @click="node.config.assignments.splice(i, 1)"><Icon name="close" :size="14" /></button>
          </div>
          <AppButton size="sm" variant="subtle" icon="plus" @click="addAssignment">{{ t('pages.workflowEditor.inspector.assignments.add') }}</AppButton>
          <p v-if="!globalVars.length" class="text-[10px] leading-4 text-warn">{{ t('pages.workflowEditor.inspector.assignments.noVars') }}</p>
        </div>

        <p v-if="f.help" class="mt-1 text-[11px] leading-4 text-txt3">{{ f.help }}</p>
      </div>

      <div class="mt-6 border-t border-line pt-4">
        <div class="mb-2 text-[10px] font-semibold uppercase tracking-wider text-txt3">{{ t('pages.workflowEditor.inspector.outputs') }}</div>
        <div v-for="o in def.outputs" :key="o.key" class="mb-1.5 flex items-start gap-2">
          <code class="shrink-0 rounded bg-base px-1.5 py-0.5 font-mono text-[11px] text-accent-2">{{ o.key }}</code>
          <span class="text-[11px] leading-5 text-txt3">{{ o.desc }}</span>
        </div>
      </div>
    </div>

    <div v-if="def.help && tab === 'help'" class="scroll-area flex-1 overflow-y-auto p-4">
      <div class="prose-help text-[13px] leading-6 text-txt2" v-html="helpHtml" />
    </div>
  </div>
</template>

<style scoped>
.prose-help :deep(h3) {
  margin: 0 0 0.5rem;
  font-size: 14px;
  font-weight: 600;
  color: rgb(var(--c-txt));
}
.prose-help :deep(p) {
  margin: 0.5rem 0;
}
.prose-help :deep(ul) {
  margin: 0.5rem 0;
  padding-left: 1.1rem;
  list-style: disc;
}
.prose-help :deep(li) {
  margin: 0.2rem 0;
}
.prose-help :deep(code) {
  border-radius: 4px;
  background: rgb(var(--c-base));
  padding: 1px 5px;
  font-size: 12px;
  font-family: ui-monospace, monospace;
  color: rgb(var(--c-accent-2));
}
.prose-help :deep(strong) {
  color: rgb(var(--c-txt));
  font-weight: 600;
}
</style>
