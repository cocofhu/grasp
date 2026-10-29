<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import Icon from '@/components/ui/Icon.vue'
import { api } from '@/lib/api/api'
import type { Agent, ChannelConfig } from '@/lib/api/api'
import type { WorkspaceNotifyPolicy, WorkspaceVariable, Workflow, WorkflowDefaults } from '@/lib/shared/types'
const props = defineProps<{ workflow: Workflow; agents: Agent[] }>()
const { t } = useI18n()
const channels = ref<ChannelConfig[]>([])
const error = ref('')
const events = ['waiting_human', 'failed', 'completed']
const packs = ['pm-progress', 'pm-workflow-read', 'pm-workflow-write', 'pm-agent-fs', 'pm-prd-manager']
const defaults = computed(() => props.workflow.defaults)
const selectedEvents = computed(() => defaults.value?.notifyPolicy?.defaultEvents ?? ['waiting_human', 'failed'])
const selectedMcps = computed(() => defaults.value?.gateAuto?.enabledMcps ?? packs)
const variableTypes = computed(() => ['string', 'paragraph', 'number', 'bool', 'select'].map((value) => ({ value, label: t(`common.varTypes.${value}`) })))
const agentOptions = computed(() => {
  const options = props.agents.map((agent) => ({ value: agent.name, label: agent.name }))
  const selected = defaults.value?.gateAuto?.agentName
  if (selected && !options.some((option) => option.value === selected)) options.push({ value: selected, label: selected })
  return [{ value: '', label: t('pages.workspaceDefaults.selectAgent') }, ...options]
})
function toggleSelection(values: string[] | undefined, value: string): string[] {
  return values?.includes(value) ? values.filter((item) => item !== value) : [...(values || []), value]
}
function ensureDefaults(): WorkflowDefaults {
  const config = props.workflow.defaults ||= { inheritWorkspace: true, variables: [] }
  // Go serializes an empty inherited variable slice as null on migrated data.
  config.variables ||= []
  return config
}
function setInheritance(value: boolean) { ensureDefaults().inheritWorkspace = value }
function setSharedSandbox(value: boolean) {
  const config = ensureDefaults()
  config.sandbox = { ...config.sandbox, mode: value ? 'shared' : 'per_node' }
}
function prefersReducedMotion() {
  return typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
}
function isSwitchEvent(event: MouseEvent) {
  const target = event.target
  return target instanceof Element && !!target.closest('[role="switch"]')
}
function toggleShared(event: MouseEvent) {
  if (isSwitchEvent(event)) return
  setSharedSandbox(defaults.value?.sandbox?.mode !== 'shared')
}
function toggleInherit(event: MouseEvent) {
  if (isSwitchEvent(event)) return
  setInheritance(defaults.value?.inheritWorkspace === false)
}
function toggleNotify(event: MouseEvent) {
  if (isSwitchEvent(event)) return
  overrideNotify(!defaults.value?.notifyPolicy)
}
function toggleGate(event: MouseEvent) {
  if (isSwitchEvent(event)) return
  overrideGate(!defaults.value?.gateAuto)
}
const plusMotion = ref(0)
let variableSeq = 0
const variableKeys = new WeakMap<WorkspaceVariable, number>()
const entering = ref<Record<number, true>>({})
const rowMotion = ref<Record<number, 'out'>>({})
const pendingRemoval = new Set<WorkspaceVariable>()
const removalTimers = new Map<number, number>()
let alive = true
function keyOf(variable: WorkspaceVariable) {
  let key = variableKeys.get(variable)
  if (key == null) {
    key = ++variableSeq
    variableKeys.set(variable, key)
  }
  return key
}
function addVariable() {
  plusMotion.value += 1
  const config = ensureDefaults()
  config.variables.push({ name: '', type: 'string', value: '' })
  const variable = config.variables[config.variables.length - 1]
  if (!variable || prefersReducedMotion()) return
  const key = keyOf(variable)
  entering.value = { ...entering.value, [key]: true }
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      if (!alive || !entering.value[key]) return
      const next = { ...entering.value }
      delete next[key]
      entering.value = next
    })
  })
}
function commitRemoval(variable: WorkspaceVariable) {
  if (!pendingRemoval.has(variable)) return
  pendingRemoval.delete(variable)
  const key = variableKeys.get(variable)
  if (key != null) {
    const timer = removalTimers.get(key)
    if (timer != null) window.clearTimeout(timer)
    removalTimers.delete(key)
  }
  const list = props.workflow.defaults?.variables
  const index = list?.indexOf(variable) ?? -1
  if (index >= 0) list!.splice(index, 1)
  if (!alive || key == null || !rowMotion.value[key]) return
  const next = { ...rowMotion.value }
  delete next[key]
  rowMotion.value = next
}
function removeVariable(variable: WorkspaceVariable) {
  const list = props.workflow.defaults?.variables
  if (!list?.includes(variable) || pendingRemoval.has(variable)) return
  if (prefersReducedMotion()) {
    list.splice(list.indexOf(variable), 1)
    return
  }
  const key = keyOf(variable)
  pendingRemoval.add(variable)
  if (entering.value[key]) {
    const next = { ...entering.value }
    delete next[key]
    entering.value = next
  }
  rowMotion.value = { ...rowMotion.value, [key]: 'out' }
  removalTimers.set(key, window.setTimeout(() => commitRemoval(variable), 240))
}
function onVariableTransitionEnd(variable: WorkspaceVariable, event: TransitionEvent) {
  if (event.target !== event.currentTarget) return
  if (event.propertyName && event.propertyName !== 'grid-template-rows') return
  commitRemoval(variable)
}
const notifyFallback: WorkspaceNotifyPolicy = {
  enabled: true,
  defaultEvents: ['waiting_human', 'failed'],
  channelIds: [],
  waitingHumanTemplate: '',
  failedTemplate: '',
  completedTemplate: '',
}
const gateFallback: NonNullable<WorkflowDefaults['gateAuto']> = { enabled: false, agentName: '', variable: '', prompt: '', enabledMcps: [] }
const notifyForm = computed(() => defaults.value?.notifyPolicy ?? notifyFallback)
const gateForm = computed(() => defaults.value?.gateAuto ?? gateFallback)
function setNotifyField<K extends keyof WorkspaceNotifyPolicy>(key: K, value: WorkspaceNotifyPolicy[K]) {
  const policy = props.workflow.defaults?.notifyPolicy
  if (!policy) return
  policy[key] = value
}
function setGateField<K extends keyof NonNullable<WorkflowDefaults['gateAuto']>>(key: K, value: NonNullable<WorkflowDefaults['gateAuto']>[K]) {
  const gate = props.workflow.defaults?.gateAuto
  if (!gate) return
  gate[key] = value
}
function setVariableValue(variable: WorkspaceVariable, input: string) { variable.value = variable.type === 'number' ? (input === '' ? '' : Number(input)) : input }
function setVariableName(variable: WorkspaceVariable, name: string) {
  // The server can preserve a masked value only under its existing name.
  if (variable.name !== name && variable.value === '****') variable.value = ''
  variable.name = name
}
function setVariableSecret(variable: WorkspaceVariable, secret: boolean) {
  if (!secret && variable.value === '****') variable.value = ''
  variable.secret = secret
}
function setVariableType(variable: WorkspaceVariable, type: string) {
  variable.type = type
  // Changing metadata must not replace an unchanged secret with a coerced value.
  if (variable.value === '****') return
  if (type === 'bool') variable.value = variable.value === true || variable.value === 'true'
  else if (type === 'number') {
    const value = variable.value == null ? '' : String(variable.value).trim()
    variable.value = value === '' ? '' : Number.isFinite(Number(value)) ? Number(value) : 0
  } else variable.value = variable.value == null ? '' : String(variable.value)
}
function overrideNotify(value: boolean) {
  const config = ensureDefaults()
  if (value) config.notifyPolicy = { enabled: true, defaultEvents: ['waiting_human', 'failed'], channelIds: [] }
  else delete config.notifyPolicy
}
function overrideGate(value: boolean) {
  const config = ensureDefaults()
  if (value) config.gateAuto = { enabled: false, agentName: '', variable: '', prompt: '', enabledMcps: [] }
  else delete config.gateAuto
}
async function loadChannels() {
  try { channels.value = (await api.listChannels()).items || [] }
  catch (cause: any) { error.value = String(cause?.message || cause) }
}
onMounted(() => { void loadChannels() })
onBeforeUnmount(() => {
  alive = false
  for (const variable of [...pendingRemoval]) commitRemoval(variable)
})
</script>
<template>
  <div class="space-y-3 text-sm" data-testid="workflow-defaults-panel">
    <section class="defaults-section" data-testid="workflow-sandbox-settings">
      <div class="defaults-heading defaults-toggle" data-testid="workflow-shared-sandbox-row" @click="toggleShared">
        <span class="defaults-icon"><Icon name="server" :size="18" /></span>
        <div class="min-w-0 flex-1">
          <h3 class="font-semibold text-txt">{{ t('pages.workspaceDefaults.sharedSandbox') }}</h3>
          <p class="defaults-hint">{{ t('pages.workspaceDefaults.sharedSandboxHint') }}</p>
        </div>
        <AppSwitch
          :model-value="defaults?.sandbox?.mode === 'shared'"
          :aria-label="t('pages.workspaceDefaults.sharedSandbox')"
          data-testid="workflow-shared-sandbox-switch"
          @update:model-value="setSharedSandbox"
        />
      </div>
    </section>

    <section class="defaults-section" data-testid="workflow-variables-settings">
      <div class="defaults-heading">
        <span class="defaults-icon"><Icon name="variable" :size="18" /></span>
        <div class="min-w-0 flex-1">
          <h3 class="font-semibold text-txt">{{ t('pages.workspaceDefaults.variables') }}</h3>
          <p class="defaults-hint">{{ t('pages.workspaceDefaults.variablesHint') }}</p>
        </div>
        <AppButton variant="ghost" size="sm" :aria-label="t('pages.workspaceDefaults.addVariable')" :title="t('pages.workspaceDefaults.addVariable')" @click="addVariable">
          <Icon name="plus" :size="16" :motion-key="plusMotion" />
        </AppButton>
      </div>
      <div class="defaults-row defaults-toggle border-t border-line px-4 py-3" data-testid="workflow-inherit-row" @click="toggleInherit">
        <span>{{ t('pages.workspaceDefaults.inheritVariables') }}</span>
        <AppSwitch :model-value="defaults?.inheritWorkspace !== false" :aria-label="t('pages.workspaceDefaults.inheritVariables')" @update:model-value="setInheritance" />
      </div>
      <div
        v-for="variable in defaults?.variables || []"
        :key="keyOf(variable)"
        class="defaults-var"
        :class="{ 'is-entering': entering[keyOf(variable)], 'is-collapsing': rowMotion[keyOf(variable)] === 'out' }"
        data-variable-row
        @transitionend="onVariableTransitionEnd(variable, $event)"
      >
        <div class="defaults-var-clip">
        <div class="space-y-3 border-t border-line p-4">
        <div class="flex flex-wrap items-center gap-2">
          <input :value="variable.name" class="input min-w-24 flex-1 font-mono text-xs" :aria-label="t('pages.workspaceDefaults.variableName')" :placeholder="t('pages.workspaceDefaults.variableName')" @input="setVariableName(variable, ($event.target as HTMLInputElement).value)" />
          <AppSelect :model-value="variable.type" :options="variableTypes" searchable size="sm" class="w-28 shrink-0" :aria-label="t('pages.workspaceDefaults.variableType')" @update:model-value="setVariableType(variable, $event)" />
          <AppButton variant="ghost" size="sm" :aria-label="t('common.buttons.delete')" @click.stop="removeVariable(variable)"><Icon name="trash" :size="16" /></AppButton>
        </div>
        <div v-if="variable.type === 'bool'" class="defaults-row">
          <span>{{ t('pages.workspaceDefaults.defaultValue') }}</span>
          <AppSwitch :model-value="variable.value === true" :aria-label="t('pages.workspaceDefaults.defaultValue')" @update:model-value="variable.value = $event" />
        </div>
        <textarea v-else-if="variable.type === 'paragraph'" v-model="variable.value" class="input w-full text-xs" :aria-label="t('pages.workspaceDefaults.defaultValue')" :placeholder="t('pages.workspaceDefaults.defaultValue')" rows="3" />
        <input v-else :type="variable.secret ? 'password' : variable.type === 'number' ? 'number' : 'text'" :value="variable.value ?? ''" class="input w-full text-xs" :aria-label="t('pages.workspaceDefaults.defaultValue')" :placeholder="t('pages.workspaceDefaults.defaultValue')" @input="setVariableValue(variable, ($event.target as HTMLInputElement).value)" />
        <input v-model="variable.desc" class="input w-full text-xs" :aria-label="t('pages.workspaceDefaults.description')" :placeholder="t('pages.workspaceDefaults.description')" />
        <input v-if="variable.type === 'select'" v-model="variable.options" class="input w-full text-xs" :aria-label="t('pages.workspaceDefaults.options')" :placeholder="t('pages.workspaceDefaults.options')" />
        <div class="grid grid-cols-1 gap-x-6 gap-y-3 sm:grid-cols-2">
          <div v-for="field in ['ask', 'required', 'editable'] as const" :key="field" class="defaults-row">
            <span>{{ t(`pages.workspaceDefaults.${field}`) }}</span>
            <AppSwitch v-model="variable[field]" :aria-label="t(`pages.workspaceDefaults.${field}`)" />
          </div>
          <div class="defaults-row">
            <span>{{ t('pages.workspaceDefaults.secret') }}</span>
            <AppSwitch :model-value="variable.secret" :aria-label="t('pages.workspaceDefaults.secret')" @update:model-value="setVariableSecret(variable, $event)" />
          </div>
        </div>
        </div>
        </div>
      </div>
    </section>

    <section class="defaults-section" data-testid="workflow-notification-settings">
      <div class="defaults-heading defaults-toggle" data-testid="workflow-notify-row" @click="toggleNotify">
        <span class="defaults-icon"><Icon name="bell" :size="18" /></span>
        <div class="min-w-0 flex-1">
          <h3 class="font-semibold text-txt">{{ t('pages.workspaceDefaults.notifyTitle') }}</h3>
          <p class="defaults-hint">{{ t('pages.workspaceDefaults.notifyHint') }}</p>
        </div>
        <AppSwitch :model-value="!!defaults?.notifyPolicy" :aria-label="t('pages.workspaceDefaults.notifyOverride')" @update:model-value="overrideNotify" />
      </div>
      <div
        class="defaults-expansion"
        :class="{ 'is-open': !!defaults?.notifyPolicy }"
        data-testid="workflow-notify-expansion"
        :inert="defaults?.notifyPolicy ? undefined : true"
      >
        <div class="defaults-expansion-clip">
          <div class="space-y-4 border-t border-line p-4">
            <div class="defaults-row">
              <span>{{ t('pages.workspaceDefaults.notifyEnabled') }}</span>
              <AppSwitch :model-value="notifyForm.enabled !== false" :aria-label="t('pages.workspaceDefaults.notifyEnabled')" @update:model-value="setNotifyField('enabled', $event)" />
            </div>
            <div>
              <p class="defaults-label">{{ t('pages.workspaceDefaults.eventsLabel') }}</p>
              <div class="flex flex-wrap gap-2">
                <AppButton v-for="event in events" :key="event" variant="outline" size="sm" :aria-pressed="selectedEvents.includes(event)" :class="{ 'defaults-choice-selected': selectedEvents.includes(event) }" @click="setNotifyField('defaultEvents', toggleSelection(selectedEvents, event))">
                  {{ t(`pages.workspaceDefaults.events.${event}`) }}
                </AppButton>
              </div>
            </div>
            <div>
              <p class="defaults-label">{{ t('pages.workspaceDefaults.channelsLabel') }}</p>
              <p v-if="error" role="alert" class="text-xs text-err">{{ error }}</p>
              <div v-else-if="channels.length" class="flex flex-wrap gap-2">
                <AppButton v-for="channel in channels" :key="channel.id" variant="outline" size="sm" :aria-pressed="notifyForm.channelIds?.includes(channel.id) || false" :class="{ 'defaults-choice-selected': notifyForm.channelIds?.includes(channel.id) }" @click="setNotifyField('channelIds', toggleSelection(notifyForm.channelIds, channel.id))">
                  {{ channel.name || channel.id }}
                </AppButton>
              </div>
              <p v-else class="text-xs text-txt3">{{ t('pages.workspaceDefaults.noChannels') }}</p>
            </div>
            <label v-for="(field, index) in ['waitingHumanTemplate', 'failedTemplate', 'completedTemplate'] as const" :key="field" class="block text-xs text-txt2">
              {{ t(`pages.workspaceDefaults.events.${events[index]}`) }}
              <textarea :value="notifyForm[field] || ''" class="input mt-2 w-full text-xs" rows="2" :placeholder="t('pages.workspaceDefaults.templateHint')" @input="setNotifyField(field, ($event.target as HTMLTextAreaElement).value)" />
            </label>
          </div>
        </div>
      </div>
    </section>

    <section class="defaults-section" data-testid="workflow-gate-settings">
      <div class="defaults-heading defaults-toggle" data-testid="workflow-gate-row" @click="toggleGate">
        <span class="defaults-icon"><Icon name="check" :size="18" /></span>
        <div class="min-w-0 flex-1">
          <h3 class="font-semibold text-txt">{{ t('pages.workspaceDefaults.gateTitle') }}</h3>
          <p class="defaults-hint">{{ t('pages.workspaceDefaults.gateHint') }}</p>
        </div>
        <AppSwitch :model-value="!!defaults?.gateAuto" :aria-label="t('pages.workspaceDefaults.gateOverride')" @update:model-value="overrideGate" />
      </div>
      <div
        class="defaults-expansion"
        :class="{ 'is-open': !!defaults?.gateAuto }"
        data-testid="workflow-gate-expansion"
        :inert="defaults?.gateAuto ? undefined : true"
      >
        <div class="defaults-expansion-clip">
          <div class="space-y-4 border-t border-line p-4">
            <div class="defaults-row">
              <span>{{ t('pages.workspaceDefaults.gateEnabled') }}</span>
              <AppSwitch :model-value="!!gateForm.enabled" :aria-label="t('pages.workspaceDefaults.gateEnabled')" @update:model-value="setGateField('enabled', $event)" />
            </div>
            <div>
              <p class="defaults-label">{{ t('pages.workspaceDefaults.agent') }}</p>
              <AppSelect :model-value="gateForm.agentName" :options="agentOptions" searchable size="sm" :placeholder="t('pages.workspaceDefaults.selectAgent')" :aria-label="t('pages.workspaceDefaults.agent')" @update:model-value="setGateField('agentName', $event)" />
            </div>
            <label class="block text-xs text-txt2">{{ t('pages.workspaceDefaults.variableName') }}<input :value="gateForm.variable" class="input mt-2 w-full font-mono text-xs" @input="setGateField('variable', ($event.target as HTMLInputElement).value)" /></label>
            <label class="block text-xs text-txt2">{{ t('pages.workspaceDefaults.prompt') }}<textarea :value="gateForm.prompt" class="input mt-2 w-full text-xs" rows="3" @input="setGateField('prompt', ($event.target as HTMLTextAreaElement).value)" /></label>
            <div>
              <p class="defaults-label">{{ t('pages.workspaceDefaults.mcpPacks') }}</p>
              <div class="flex flex-wrap gap-2">
                <AppButton v-for="pack in packs" :key="pack" variant="outline" size="sm" :aria-pressed="selectedMcps.includes(pack)" :class="{ 'defaults-choice-selected': selectedMcps.includes(pack) }" @click="setGateField('enabledMcps', toggleSelection(selectedMcps, pack))">{{ pack }}</AppButton>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.defaults-section {
  @apply overflow-hidden rounded-xl border border-line;
}
.defaults-heading {
  @apply flex items-center gap-3 p-4;
}
.defaults-icon {
  @apply flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-accent/10 text-accent;
}
.defaults-hint {
  @apply mt-1 text-xs leading-5 text-txt3;
}
.defaults-row {
  @apply flex items-center justify-between gap-3 text-xs text-txt2;
}
.defaults-label {
  @apply mb-2 text-xs text-txt2;
}
.defaults-choice-selected {
  @apply border-accent/30 bg-accent/10 text-accent;
}
.defaults-toggle {
  cursor: pointer;
  transition: background-color var(--dur-press) var(--ease-out-expo);
}
.defaults-toggle:active {
  background-color: rgb(var(--c-elevated));
}
.defaults-expansion {
  display: grid;
  grid-template-rows: 0fr;
  opacity: 0;
  transition:
    grid-template-rows 180ms var(--ease-out-expo),
    opacity 180ms var(--ease-out-expo);
}
.defaults-expansion.is-open {
  grid-template-rows: 1fr;
  opacity: 1;
}
.defaults-expansion-clip,
.defaults-var-clip {
  min-height: 0;
  overflow: hidden;
}
.defaults-var {
  display: grid;
  grid-template-rows: 1fr;
  opacity: 1;
  transition:
    grid-template-rows 180ms var(--ease-out-expo),
    opacity 180ms var(--ease-out-expo);
}
.defaults-var.is-entering,
.defaults-var.is-collapsing {
  grid-template-rows: 0fr;
  opacity: 0;
}
@media (prefers-reduced-motion: reduce) {
  .defaults-toggle,
  .defaults-expansion,
  .defaults-var {
    transition: none;
    animation: none;
  }
}
</style>
