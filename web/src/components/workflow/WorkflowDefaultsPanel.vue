<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import { api } from '@/lib/api/api'
import type { Agent, ChannelConfig } from '@/lib/api/api'
import type { SandboxMode, WorkspaceVariable, Workflow, WorkflowDefaults } from '@/lib/shared/types'
const props = defineProps<{ workflow: Workflow; agents: Agent[] }>()
const { t } = useI18n()
const channels = ref<ChannelConfig[]>([])
const error = ref('')
// Platform knob shared_run_sandbox_enabled; missing/0/unreadable keeps the section hidden.
const sharedSandboxEnabled = ref(false)
const sandboxModes: SandboxMode[] = ['per_node', 'shared']
const events = ['waiting_human', 'failed', 'completed']
const packs = ['pm-progress', 'pm-workflow-read', 'pm-workflow-write', 'pm-agent-fs', 'pm-prd-manager']
const defaults = computed(() => props.workflow.defaults)
function ensureDefaults(): WorkflowDefaults {
  const config = props.workflow.defaults ||= { inheritWorkspace: true, variables: [] }
  // Go serializes an empty inherited variable slice as null on migrated data.
  config.variables ||= []
  return config
}
function setInheritance(value: boolean) { ensureDefaults().inheritWorkspace = value }
function addVariable() { ensureDefaults().variables.push({ name: '', type: 'string', value: '' }) }
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
function setSandboxMode(mode: SandboxMode) {
  if (mode === 'shared') ensureDefaults().sandbox = { mode }
  // per_node is the server default; dropping the field keeps old payloads unchanged.
  else if (props.workflow.defaults) delete props.workflow.defaults.sandbox
}
async function loadSharedSandboxKnob() {
  try { sharedSandboxEnabled.value = (await api.getSettings()).items?.some((item) => item.key === 'shared_run_sandbox_enabled' && item.value === 1) ?? false }
  catch { sharedSandboxEnabled.value = false }
}
async function loadChannels() {
  try { channels.value = (await api.listChannels()).items || [] }
  catch (cause: any) { error.value = String(cause?.message || cause) }
}
onMounted(() => { void loadChannels(); void loadSharedSandboxKnob() })
</script>
<template>
  <div class="space-y-6 text-sm" data-testid="workflow-defaults-panel">
    <p class="text-xs leading-5 text-txt3">{{ t('pages.workspaceDefaults.hint') }}</p>
    <section>
      <h3 class="font-semibold text-txt">{{ t('pages.workspaceDefaults.variables') }}</h3>
      <label class="mt-2 flex items-center gap-2 text-xs text-txt2"><input type="checkbox" :checked="defaults?.inheritWorkspace !== false" @change="setInheritance(($event.target as HTMLInputElement).checked)" />{{ t('pages.workspaceDefaults.inheritVariables') }}</label>
      <div v-for="(variable, index) in defaults?.variables || []" :key="index" class="mt-3 rounded border border-line p-3">
        <div class="flex flex-wrap gap-2">
          <input :value="variable.name" class="input min-w-32 flex-1 font-mono text-xs" :aria-label="t('pages.workspaceDefaults.variableName')" :placeholder="t('pages.workspaceDefaults.variableName')" @input="setVariableName(variable, ($event.target as HTMLInputElement).value)" />
          <select :value="variable.type" class="input text-xs" :aria-label="t('pages.workspaceDefaults.variableType')" @change="setVariableType(variable, ($event.target as HTMLSelectElement).value)"><option v-for="kind in ['string', 'paragraph', 'number', 'bool', 'select']" :key="kind" :value="kind">{{ kind }}</option></select>
          <AppButton variant="ghost" size="sm" @click="defaults?.variables.splice(index, 1)">{{ t('common.buttons.delete') }}</AppButton>
        </div>
        <label v-if="variable.type === 'bool'" class="mt-2 flex items-center gap-2 text-xs"><input v-model="variable.value" type="checkbox" />{{ t('pages.workspaceDefaults.defaultValue') }}</label>
        <textarea v-else-if="variable.type === 'paragraph'" v-model="variable.value" class="input mt-2 w-full text-xs" :aria-label="t('pages.workspaceDefaults.defaultValue')" />
        <input v-else :type="variable.secret ? 'password' : variable.type === 'number' ? 'number' : 'text'" :value="variable.value ?? ''" class="input mt-2 w-full text-xs" :aria-label="t('pages.workspaceDefaults.defaultValue')" :placeholder="t('pages.workspaceDefaults.defaultValue')" @input="setVariableValue(variable, ($event.target as HTMLInputElement).value)" />
        <input v-model="variable.desc" class="input mt-2 w-full text-xs" :aria-label="t('pages.workspaceDefaults.description')" :placeholder="t('pages.workspaceDefaults.description')" />
        <input v-if="variable.type === 'select'" v-model="variable.options" class="input mt-2 w-full text-xs" :aria-label="t('pages.workspaceDefaults.options')" :placeholder="t('pages.workspaceDefaults.options')" />
        <div class="mt-2 flex flex-wrap gap-4 text-xs text-txt2"><label><input v-model="variable.ask" type="checkbox" /> {{ t('pages.workspaceDefaults.ask') }}</label><label><input v-model="variable.required" type="checkbox" /> {{ t('pages.workspaceDefaults.required') }}</label><label><input v-model="variable.editable" type="checkbox" /> {{ t('pages.workspaceDefaults.editable') }}</label><label><input :checked="variable.secret" type="checkbox" @change="setVariableSecret(variable, ($event.target as HTMLInputElement).checked)" /> {{ t('pages.workspaceDefaults.secret') }}</label></div>
      </div>
      <AppButton class="mt-3" variant="outline" size="sm" @click="addVariable">{{ t('pages.workspaceDefaults.addVariable') }}</AppButton>
    </section>
    <section v-if="sharedSandboxEnabled" class="border-t border-line pt-4" data-testid="workflow-sandbox-mode">
      <h3 class="font-semibold text-txt">{{ t('pages.workspaceDefaults.sandboxMode.title') }}</h3>
      <div class="mt-2 flex flex-wrap gap-4 text-xs text-txt2" role="radiogroup" :aria-label="t('pages.workspaceDefaults.sandboxMode.title')">
        <label v-for="mode in sandboxModes" :key="mode" class="flex items-center gap-2"><input type="radio" name="workflow-sandbox-mode" :value="mode" :checked="(defaults?.sandbox?.mode || 'per_node') === mode" @change="setSandboxMode(mode)" />{{ t(`pages.workspaceDefaults.sandboxMode.${mode}`) }}</label>
      </div>
      <p class="mt-2 text-xs leading-5 text-txt3">{{ t('pages.workspaceDefaults.sandboxMode.hint') }}</p>
    </section>
    <section class="border-t border-line pt-4">
      <label class="flex items-center gap-2 font-semibold text-txt"><input type="checkbox" :checked="!!defaults?.notifyPolicy" @change="overrideNotify(($event.target as HTMLInputElement).checked)" />{{ t('pages.workspaceDefaults.notifyOverride') }}</label>
      <div v-if="defaults?.notifyPolicy" class="mt-3 space-y-3 text-xs text-txt2">
        <label class="flex items-center gap-2"><input v-model="defaults.notifyPolicy.enabled" type="checkbox" />{{ t('pages.workspaceDefaults.notifyEnabled') }}</label>
        <div class="flex flex-wrap gap-4"><label v-for="event in events" :key="event"><input v-model="defaults.notifyPolicy.defaultEvents" type="checkbox" :value="event" /> {{ t(`pages.workspaceDefaults.events.${event}`) }}</label></div>
        <p v-if="error" role="alert" class="text-err">{{ error }}</p>
        <div v-for="channel in channels" :key="channel.id"><label><input v-model="defaults.notifyPolicy.channelIds" type="checkbox" :value="channel.id" /> {{ channel.name || channel.id }}</label></div>
        <label v-for="(field, index) in ['waitingHumanTemplate', 'failedTemplate', 'completedTemplate'] as const" :key="field" class="block">{{ t(`pages.workspaceDefaults.events.${events[index]}`) }}<textarea v-model="defaults.notifyPolicy[field]" class="input mt-1 w-full text-xs" rows="3" :placeholder="t('pages.workspaceDefaults.templateHint')" /></label>
      </div>
    </section>
    <section class="border-t border-line pt-4">
      <label class="flex items-center gap-2 font-semibold text-txt"><input type="checkbox" :checked="!!defaults?.gateAuto" @change="overrideGate(($event.target as HTMLInputElement).checked)" />{{ t('pages.workspaceDefaults.gateOverride') }}</label>
      <div v-if="defaults?.gateAuto" class="mt-3 space-y-3 text-xs text-txt2">
        <label class="flex items-center gap-2"><input v-model="defaults.gateAuto.enabled" type="checkbox" />{{ t('pages.workspaceDefaults.gateEnabled') }}</label>
        <label class="block">{{ t('pages.workspaceDefaults.agent') }}<select v-model="defaults.gateAuto.agentName" class="input mt-1 w-full text-xs"><option value="">{{ t('pages.workspaceDefaults.selectAgent') }}</option><option v-for="agent in agents" :key="agent.name" :value="agent.name">{{ agent.name }}</option></select></label>
        <label class="block">{{ t('pages.workspaceDefaults.variableName') }}<input v-model="defaults.gateAuto.variable" class="input mt-1 w-full font-mono text-xs" /></label>
        <label class="block">{{ t('pages.workspaceDefaults.prompt') }}<textarea v-model="defaults.gateAuto.prompt" class="input mt-1 w-full text-xs" rows="3" /></label>
        <label v-for="pack in packs" :key="pack" class="mr-3 inline-flex items-center gap-1"><input v-model="defaults.gateAuto.enabledMcps" type="checkbox" :value="pack" /> {{ pack }}</label>
      </div>
    </section>
  </div>
</template>
