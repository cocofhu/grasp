<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AgentGitGuide from '@/components/agent/AgentGitGuide.vue'
import WizardApiKeyStepPanel from '@/components/agent/WizardApiKeyStepPanel.vue'
import WizardAcpStartPathPanel from '@/components/agent/WizardAcpStartPathPanel.vue'
import { api, type TeamBootstrapSession } from '@/lib/api/api'
import {
  TEAM_ENGINEER_COUNT,
  TEAM_WIZARD_STEPS,
  applyTeamAcpBackend,
  applyTeamStartPath,
  assembleTeamBootstrapPayload,
  artifactStorePreset,
  freshTeamDraft,
  syncDerivedNames,
  teamHasAuth,
  validateTeamBasics,
  type TeamWizardDraft,
  type WizardBackendId,
  type StartPath,
} from '@/lib/agent/agentTeamWizard'
import { authGuideFor, defaultSettingsPlaceholder } from '@/lib/agent/backendAuthGuide'
import type { GitCredentialType } from '@/lib/agent/gitCredentialAnalysis'
import { getRegionPolicy, setRegion } from '@/lib/shared/regionPolicy'
import {
  kvToRec,
  parseCustomConfigJson,
  stripAuthKeysFromEnv,
  type WizardAuthMode,
} from '@/lib/agent/agentCreateWizard'
import {
  applyOpenCodeFields,
  openCodeCustomBaseRequired,
  openCodeFieldsFromEnv,
  openCodeModelRequired,
  type OpenCodeProviderId,
} from '@/lib/agent/openCodeProvider'
import { useInheritedGitEnv } from '@/lib/agent/useInheritedGitEnv'

const props = defineProps<{
  open: boolean
  existingNames: string[]
  /** When set, Git step can inherit project shared Agent env for hide/infer. */
  projectId?: string
}>()

const emit = defineEmits<{
  close: []
  started: [session: TeamBootstrapSession]
}>()

const { t } = useI18n()
const { inheritedEnv } = useInheritedGitEnv(() => props.projectId)
const draft = ref<TeamWizardDraft>(freshTeamDraft())
const fieldError = ref('')
const submitError = ref('')
const submitting = ref(false)
const apiKeyInput = ref('')
const customConfigError = ref(false)
const openCodeBaseError = ref(false)
const openCodeModelError = ref(false)
const customConfigDraft = ref('')
const stepAnimKey = ref(0)
const bgExpanded = ref(true)

const currentStep = computed(() => TEAM_WIZARD_STEPS[draft.value.step])
const progressPct = computed(() => ((draft.value.step + 1) / TEAM_WIZARD_STEPS.length) * 100)
const regionPolicy = computed(() => getRegionPolicy(draft.value.acpBackend))
const currentRegion = computed(() => {
  const policy = regionPolicy.value
  if (!policy) return ''
  return draft.value.env.find((e) => e.k.trim() === policy.regionEnvKey)?.v || ''
})
const authGuide = computed(() => authGuideFor(draft.value.acpBackend, currentRegion.value))
const primaryAuthKey = computed(() => authGuide.value.keys[0]?.key || '')
const primaryAuthAlt = computed(() => authGuide.value.keys[0]?.alt || '')
const previewLine = computed(() => {
  const root = draft.value.rootGroupName || '—'
  const pipe = draft.value.workflowGroupName || '工作流(GitHub)'
  const pm = draft.value.pmName || '—'
  return t('pages.agentStudio.teamWizard.previewLine', {
    root,
    pipe,
    pm,
    n: TEAM_ENGINEER_COUNT,
  })
})

watch(
  () => props.open,
  (open) => {
    if (!open) return
    draft.value = freshTeamDraft()
    fieldError.value = ''
    submitError.value = ''
    submitting.value = false
    apiKeyInput.value = ''
    customConfigError.value = false
    customConfigDraft.value = ''
    stepAnimKey.value++
    nextTick(() => document.getElementById('team-wiz-project')?.focus())
  },
)

function close() {
  if (submitting.value) return
  emit('close')
}

function upsertEnv(key: string, value: string) {
  const row = draft.value.env.find((e) => e.k === key)
  if (row) row.v = value
  else draft.value.env.push({ k: key, v: value })
}

function onProjectInput() {
  syncDerivedNames(draft.value)
}

function selectAcp(id: WizardBackendId) {
  if (id === draft.value.acpBackend) return
  applyTeamAcpBackend(draft.value, id)
  syncApiKeyInput()
}

function selectStartPath(path: StartPath) {
  if (path === draft.value.startPath) return
  applyTeamStartPath(draft.value, path)
  syncApiKeyInput()
}

function selectRegion(region: string) {
  const env = Object.fromEntries(
    draft.value.env.filter((i) => i.k.trim()).map((i) => [i.k.trim(), i.v]),
  )
  draft.value.env = Object.entries(setRegion(env, draft.value.acpBackend, region)).map(([k, v]) => ({
    k,
    v,
  }))
}

function syncApiKeyInput() {
  const key = primaryAuthKey.value
  apiKeyInput.value = draft.value.env.find((e) => e.k === key)?.v ?? ''
}

function clearAuthKeys() {
  draft.value.env = stripAuthKeysFromEnv(draft.value.env, draft.value.acpBackend)
  apiKeyInput.value = ''
}

function setAuthMode(mode: WizardAuthMode) {
  if (mode === draft.value.authMode) return
  customConfigError.value = false
  if (mode === 'apiKey') {
    customConfigDraft.value = draft.value.customConfigContent
    draft.value.customConfigContent = ''
    draft.value.authMode = mode
    syncApiKeyInput()
    return
  }
  clearAuthKeys()
  draft.value.authMode = mode
  draft.value.customConfigContent =
    customConfigDraft.value || defaultSettingsPlaceholder(draft.value.acpBackend)
}

function onCustomConfigInput(value: string) {
  draft.value.customConfigContent = value
  customConfigError.value = false
}

function patchOpenCode(fields: Parameters<typeof applyOpenCodeFields>[1]) {
  draft.value.env = Object.entries(applyOpenCodeFields(kvToRec(draft.value.env), fields)).map(
    ([k, v]) => ({ k, v }),
  )
  openCodeBaseError.value = false
  openCodeModelError.value = false
}

function onOpenCodeProvider(value: OpenCodeProviderId) {
  patchOpenCode({ provider: value })
}

function onOpenCodeBaseURL(value: string) {
  patchOpenCode({ baseURL: value })
}

function onOpenCodeModel(value: string) {
  patchOpenCode({ model: value })
}

function onOpenCodeVision(value: boolean) {
  patchOpenCode({ vision: value })
}

function onApiKeyInput(value: string) {
  apiKeyInput.value = value
  const key = primaryAuthKey.value
  if (!key) return
  if (value.trim()) upsertEnv(key, value)
  else {
    const idx = draft.value.env.findIndex((e) => e.k === key)
    if (idx >= 0) draft.value.env.splice(idx, 1)
  }
}

function onGitCredentialType(value: GitCredentialType) {
  draft.value.gitCredentialType = value
}

function goPrev() {
  if (draft.value.step === 0 || submitting.value) return
  draft.value.step--
  stepAnimKey.value++
  if (currentStep.value.id === 'apiKey') syncApiKeyInput()
}

function goSkip() {
  const step = currentStep.value
  if (!step.skip || submitting.value) return
  draft.value.skipped[step.id] = true
  if (step.id === 'apiKey') {
    clearAuthKeys()
    draft.value.customConfigContent = ''
    customConfigDraft.value = ''
    customConfigError.value = false
  }
  draft.value.step++
  stepAnimKey.value++
  if (currentStep.value.id === 'apiKey') syncApiKeyInput()
}

function validateApiKeyStep(): boolean {
  if (draft.value.acpBackend === 'opencode' && draft.value.authMode === 'apiKey') {
    const fields = openCodeFieldsFromEnv(kvToRec(draft.value.env))
    if (openCodeModelRequired(fields.model)) {
      openCodeModelError.value = true
      return false
    }
    if (openCodeCustomBaseRequired(fields.provider, fields.baseURL)) {
      openCodeBaseError.value = true
      return false
    }
  }
  if (draft.value.authMode !== 'customConfig') return true
  const parsed = parseCustomConfigJson(draft.value.customConfigContent)
  if (!parsed.ok) {
    customConfigError.value = true
    return false
  }
  customConfigError.value = false
  return true
}

function goNext() {
  if (submitting.value) return
  if (currentStep.value.id === 'team') {
    const err = validateTeamBasics(draft.value, props.existingNames)
    if (err) {
      fieldError.value = t(`pages.agentStudio.teamWizard.errors.${err}`)
      return
    }
    fieldError.value = ''
  }
  if (currentStep.value.id === 'apiKey' && !validateApiKeyStep()) return
  if (currentStep.value.id === 'review') {
    void submit()
    return
  }
  delete draft.value.skipped[currentStep.value.id]
  draft.value.step++
  stepAnimKey.value++
  if (currentStep.value.id === 'apiKey') syncApiKeyInput()
}

async function submit() {
  const err = validateTeamBasics(draft.value, props.existingNames)
  if (err) {
    draft.value.step = 0
    fieldError.value = t(`pages.agentStudio.teamWizard.errors.${err}`)
    stepAnimKey.value++
    return
  }
  submitting.value = true
  submitError.value = ''
  try {
    const payload = assembleTeamBootstrapPayload(draft.value)
    const session = await api.bootstrapAgentTeam(payload)
    emit('started', session)
    emit('close')
  } catch (e: any) {
    submitError.value = e?.message || String(e)
  } finally {
    submitting.value = false
  }
}

function addMcp() {
  draft.value.mcp.push({
    name: '',
    transport: 'url',
    url: '',
    headers: [{ k: '', v: '' }],
    command: '',
    args: '',
    env: [],
  })
}

function restoreArtifact() {
  if (draft.value.mcp.some((m) => m.name.trim() === 'artifact-store')) return
  draft.value.mcp.unshift(artifactStorePreset())
}

function addNamedMcp(name: string) {
  if (draft.value.mcp.some((m) => m.name.trim() === name)) return
  draft.value.mcp.push({
    name,
    transport: 'url',
    url: '',
    headers: [],
    command: '',
    args: '',
    env: [],
  })
}

function removeMcp(i: number) {
  draft.value.mcp.splice(i, 1)
}

const mcpNames = computed(() =>
  draft.value.mcp
    .map((m) => m.name.trim())
    .filter(Boolean)
    .join(', ') || t('pages.agentStudio.teamWizard.review.none'),
)
const envSummary = computed(() => {
  const rows = draft.value.env.filter((e) => e.k.trim())
  if (!rows.length) return t('pages.agentStudio.teamWizard.review.none')
  return rows.map((e) => e.k).join(', ')
})
const hasArtifact = computed(() => draft.value.mcp.some((m) => m.name.trim() === 'artifact-store'))
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="wiz-root fixed inset-0 z-50 flex items-center justify-center p-4">
      <div class="absolute inset-0 bg-black/70" @click="close" />
      <div
        class="rounded-xl wiz-modal relative z-10 flex w-full flex-col overflow-hidden border border-line bg-surface shadow-card"
        style="width: min(980px, 100%); height: min(700px, 94vh); border-radius: 16px"
        role="dialog"
        aria-modal="true"
      >
        <div class="wiz-head relative flex h-16 shrink-0 items-center gap-3.5 border-b border-line px-5">
          <div class="rounded-md hero-mark grid h-9 w-9 shrink-0 place-items-center border border-accent/55 text-accent-2">
            <Icon name="user" :size="20" />
          </div>
          <div class="min-w-0 flex-1">
            <h2 class="m-0 text-[16px] font-semibold tracking-tight text-txt">
              {{ t('pages.agentStudio.teamWizard.title') }}
            </h2>
            <span class="mt-0.5 block text-[12px] font-normal text-txt3">
              {{ t('pages.agentStudio.teamWizard.headSub') }}
            </span>
          </div>
          <button
            type="button"
            class="grid h-8 w-8 shrink-0 place-items-center text-txt3 hover:bg-elevated hover:text-txt"
            :aria-label="t('pages.agentStudio.dialogs.cancel')"
            :disabled="submitting"
            @click="close"
          >
            <Icon name="close" :size="18" />
          </button>
          <div class="wiz-progress absolute inset-x-0 bottom-0 h-[3px] overflow-hidden bg-elevated">
            <span :style="{ width: progressPct + '%' }" />
          </div>
        </div>

        <div class="flex min-h-0 flex-1">
          <aside class="wiz-rail scroll-area hidden w-[208px] shrink-0 overflow-y-auto border-r border-line bg-elevated px-3 pb-4 pt-5 md:block">
            <div class="mb-4 flex items-center gap-2 px-1.5 text-[10px] font-semibold uppercase tracking-[0.08em] text-txt3">
              <span class="pulse-dot h-1.5 w-1.5 shrink-0 bg-accent" />
              {{ t('pages.agentStudio.wizard.railCap') }}
            </div>
            <div
              v-for="(s, i) in TEAM_WIZARD_STEPS"
              :key="s.id"
              class="rail-item"
              :class="{ done: i < draft.step, cur: i === draft.step }"
            >
              <div class="track">
                <div class="node" aria-hidden="true"><i /></div>
                <div class="connector" />
              </div>
              <div class="lbl">
                <strong>{{ t(s.labelKey) }}</strong>
              </div>
            </div>
          </aside>

          <div class="flex min-w-0 flex-1 flex-col">
            <div class="border-b border-line px-5 py-2 text-[12px] text-txt3 md:hidden">
              {{ draft.step + 1 }} / {{ TEAM_WIZARD_STEPS.length }} · {{ t(currentStep.labelKey) }}
            </div>
            <div class="scroll-area min-h-0 flex-1 overflow-y-auto px-6 py-6 md:px-8 md:py-7">
              <div :key="stepAnimKey" class="step-pane">
                <div class="sec-head mb-1">
                  <h3 class="m-0 text-[18px] font-semibold text-txt">{{ t(currentStep.labelKey) }}</h3>
                </div>

                <template v-if="currentStep.id === 'team'">
                  <p class="sec-meta">{{ t('pages.agentStudio.teamWizard.team.meta') }}</p>
                  <div class="mb-4 grid gap-3 md:grid-cols-2">
                    <label class="block">
                      <span class="mb-1.5 block text-[12px] font-medium text-txt2">
                        {{ t('pages.agentStudio.teamWizard.team.projectName') }}
                        <span class="text-err">*</span>
                      </span>
                      <input
                        id="team-wiz-project"
                        v-model="draft.projectName"
                        class="rounded-md w-full border border-line bg-base px-3 py-2 text-[13px] text-txt outline-none focus:border-accent"
                        @input="onProjectInput"
                      />
                    </label>
                    <label class="block">
                      <span class="mb-1.5 block text-[12px] font-medium text-txt2">
                        {{ t('pages.agentStudio.teamWizard.team.prefix') }}
                        <span class="text-err">*</span>
                      </span>
                      <input
                        v-model="draft.prefix"
                        class="rounded-md w-full border border-line bg-base px-3 py-2 text-[13px] text-txt outline-none focus:border-accent"
                        @input="draft.prefixTouched = true; syncDerivedNames(draft)"
                      />
                    </label>
                  </div>
                  <div class="mb-4 grid gap-3 md:grid-cols-2">
                    <label class="block">
                      <span class="mb-1.5 block text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.teamWizard.team.rootGroup') }}</span>
                      <input
                        v-model="draft.rootGroupName"
                        class="rounded-md w-full border border-line bg-base px-3 py-2 text-[13px] text-txt outline-none focus:border-accent"
                        @input="draft.rootTouched = true"
                      />
                    </label>
                    <label class="block">
                      <span class="mb-1.5 block text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.teamWizard.team.workflowGroup') }}</span>
                      <input
                        v-model="draft.workflowGroupName"
                        class="rounded-md w-full border border-line bg-base px-3 py-2 text-[13px] text-txt outline-none focus:border-accent"
                        @input="draft.workflowGroupTouched = true"
                      />
                      <p class="mt-1 text-[11px] text-txt3">{{ t('pages.agentStudio.teamWizard.team.workflowGroupHint') }}</p>
                    </label>
                  </div>
                  <label class="mb-4 block">
                    <span class="mb-1.5 block text-[12px] font-medium text-txt2">
                      {{ t('pages.agentStudio.teamWizard.team.pmName') }}
                      <span class="text-err">*</span>
                    </span>
                    <input
                      v-model="draft.pmName"
                      class="rounded-md w-full border border-line bg-base px-3 py-2 text-[13px] text-txt outline-none focus:border-accent"
                      @input="draft.pmTouched = true"
                    />
                  </label>
                  <label class="block">
                    <span class="mb-1.5 block text-[12px] font-medium text-txt2">
                      {{ t('pages.agentStudio.teamWizard.team.background') }}
                      <span class="text-err">*</span>
                    </span>
                    <textarea
                      v-model="draft.background"
                      rows="6"
                      class="rounded-md w-full resize-y border border-line bg-base px-3 py-2 text-[13px] leading-6 text-txt outline-none focus:border-accent"
                      :placeholder="t('pages.agentStudio.teamWizard.team.backgroundPlaceholder')"
                    />
                    <p class="mt-1.5 text-[11px] text-txt3">{{ t('pages.agentStudio.teamWizard.team.backgroundHint') }}</p>
                  </label>
                  <div class="rounded-lg mt-4 border border-accent/35 bg-accent-dim/40 px-3 py-2.5 text-[12px] text-accent-2">
                    {{ previewLine }}
                  </div>
                  <p v-if="fieldError" class="mt-3 text-[12px] text-err">{{ fieldError }}</p>
                </template>

                <template v-else-if="currentStep.id === 'acp'">
                  <p class="sec-meta">{{ t('pages.agentStudio.teamWizard.acp.meta') }}</p>
                  <WizardAcpStartPathPanel
                    :start-path="draft.startPath"
                    :acp-backend="draft.acpBackend"
                    :config-root="draft.configRoot"
                    :region-policy="regionPolicy"
                    :current-region="currentRegion"
                    test-id-prefix="team-wizard"
                    @select-start-path="selectStartPath"
                    @select-backend="selectAcp"
                    @select-region="selectRegion"
                  />
                </template>

                <template v-else-if="currentStep.id === 'apiKey'">
                  <WizardApiKeyStepPanel
                    :acp-backend="draft.acpBackend"
                    :config-root="draft.configRoot"
                    :auth-mode="draft.authMode"
                    :api-key-input="apiKeyInput"
                    :custom-config-content="draft.customConfigContent"
                    :custom-config-error="customConfigError"
                    :auth-guide="authGuide"
                    :primary-auth-key="primaryAuthKey"
                    :primary-auth-alt="primaryAuthAlt"
                    :env="kvToRec(draft.env)"
                    :open-code-base-error="openCodeBaseError"
                    :open-code-model-error="openCodeModelError"
                    @update:auth-mode="setAuthMode"
                    @update:api-key-input="onApiKeyInput"
                    @update:custom-config-content="onCustomConfigInput"
                    @update:open-code-provider="onOpenCodeProvider"
                    @update:open-code-base-url="onOpenCodeBaseURL"
                    @update:open-code-model="onOpenCodeModel"
                    @update:open-code-vision="onOpenCodeVision"
                  />
                </template>

                <template v-else-if="currentStep.id === 'git'">
                  <p class="sec-meta">{{ t('pages.agentStudio.teamWizard.git.meta') }}</p>
                  <label class="mb-4 block">
                    <span class="mb-1.5 block text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.teamWizard.git.url') }}</span>
                    <input
                      v-model="draft.gitUrl"
                      class="rounded-md w-full border border-line bg-base px-3 py-2 text-[13px] text-txt outline-none focus:border-accent"
                      placeholder="https://github.com/org/repo.git"
                    />
                  </label>
                  <AgentGitGuide
                    :env="draft.env"
                    :inherited-env="inheritedEnv"
                    :allow-token-recommend="false"
                    :upsert-env="(k, v) => upsertEnv(k, v)"
                    :credential-type="draft.gitCredentialType"
                    @update:credential-type="onGitCredentialType"
                  />
                </template>

                <template v-else-if="currentStep.id === 'mcp'">
                  <p class="sec-meta">{{ t('pages.agentStudio.teamWizard.mcp.meta') }}</p>
                  <div
                    v-for="(m, i) in draft.mcp"
                    :key="i"
                    class="rounded-lg mb-3 border border-line bg-elevated p-3"
                  >
                    <div class="mb-2 flex items-center justify-between gap-2">
                      <span class="text-[12px] font-semibold text-txt">MCP #{{ i + 1 }}</span>
                      <button type="button" class="text-[11px] text-txt3 hover:text-err" @click="removeMcp(i)">
                        {{ t('pages.agentStudio.dialogs.delete') }}
                      </button>
                    </div>
                    <div class="mb-2 grid gap-2 md:grid-cols-2">
                      <input
                        v-model="m.name"
                        class="rounded-md border border-line bg-base px-2 py-1.5 text-[12px] text-txt outline-none focus:border-accent"
                        :placeholder="t('pages.agentStudio.teamWizard.mcp.namePh')"
                      />
                      <select
                        v-model="m.transport"
                        class="rounded-md border border-line bg-base px-2 py-1.5 text-[12px] text-txt"
                      >
                        <option value="url">HTTP (url)</option>
                        <option value="command">stdio</option>
                      </select>
                    </div>
                    <template v-if="m.transport === 'url'">
                      <input
                        v-model="m.url"
                        class="rounded-md mb-2 w-full border border-line bg-base px-2 py-1.5 font-mono text-[11px] text-txt outline-none focus:border-accent"
                        placeholder="${GRASP_ARTIFACT_URL}"
                      />
                      <div class="mb-1 flex items-center justify-between text-[11px] text-txt2">
                        <span>{{ t('pages.agentStudio.teamWizard.mcp.headers') }}</span>
                        <button type="button" class="text-accent-2" @click="m.headers.push({ k: '', v: '' })">
                          + {{ t('pages.agentStudio.teamWizard.mcp.addHeader') }}
                        </button>
                      </div>
                      <div v-for="(h, hi) in m.headers" :key="hi" class="mb-1 grid grid-cols-[1fr_1.4fr_auto] gap-1">
                        <input v-model="h.k" class="rounded-md border border-line bg-base px-2 py-1 text-[11px]" placeholder="Header" />
                        <input v-model="h.v" class="rounded-md border border-line bg-base px-2 py-1 font-mono text-[11px]" placeholder="Value" />
                        <button type="button" class="px-2 text-[11px] text-txt3" @click="m.headers.splice(hi, 1)">×</button>
                      </div>
                    </template>
                    <template v-else>
                      <input v-model="m.command" class="rounded-md mb-2 w-full border border-line bg-base px-2 py-1.5 text-[12px]" placeholder="command" />
                      <textarea v-model="m.args" rows="2" class="rounded-md w-full border border-line bg-base px-2 py-1.5 font-mono text-[11px]" placeholder="args (one per line)" />
                    </template>
                  </div>
                  <div class="flex flex-wrap gap-2">
                    <AppButton size="sm" variant="outline" icon="plus" @click="addMcp">{{ t('pages.agentStudio.teamWizard.mcp.add') }}</AppButton>
                    <AppButton size="sm" variant="ghost" @click="restoreArtifact">{{ t('pages.agentStudio.teamWizard.mcp.restoreArtifact') }}</AppButton>
                    <AppButton size="sm" variant="ghost" @click="addNamedMcp('memory-store')">+ memory-store</AppButton>
                    <AppButton size="sm" variant="ghost" @click="addNamedMcp('context-store')">+ context-store</AppButton>
                  </div>
                  <p class="mt-3 text-[11px] text-txt3">{{ t('pages.agentStudio.teamWizard.mcp.runVars') }}</p>
                </template>

                <template v-else-if="currentStep.id === 'env'">
                  <p class="sec-meta">{{ t('pages.agentStudio.teamWizard.env.meta') }}</p>
                  <div
                    v-for="(row, i) in draft.env"
                    :key="i"
                    class="mb-2 grid grid-cols-[1fr_1.4fr_auto] gap-2"
                  >
                    <input v-model="row.k" class="rounded-md border border-line bg-base px-2 py-1.5 font-mono text-[12px]" placeholder="KEY" />
                    <input v-model="row.v" class="rounded-md border border-line bg-base px-2 py-1.5 font-mono text-[12px]" placeholder="value" />
                    <button type="button" class="text-[11px] text-txt3" @click="draft.env.splice(i, 1)">{{ t('pages.agentStudio.dialogs.delete') }}</button>
                  </div>
                  <AppButton size="sm" variant="outline" icon="plus" @click="draft.env.push({ k: '', v: '' })">
                    {{ t('pages.agentStudio.env.add') }}
                  </AppButton>
                </template>

                <template v-else-if="currentStep.id === 'review'">
                  <p class="sec-meta">{{ t('pages.agentStudio.teamWizard.review.meta') }}</p>
                  <div class="rounded-lg border border-line bg-elevated px-4 py-3 text-[13px] leading-7 text-txt2">
                    <div>{{ t('pages.agentStudio.teamWizard.review.project') }}：<strong class="text-txt">{{ draft.projectName }}</strong></div>
                    <div>{{ t('pages.agentStudio.teamWizard.review.root') }}：<strong class="text-txt">{{ draft.rootGroupName }}</strong></div>
                    <div>{{ t('pages.agentStudio.teamWizard.review.workflowGroup') }}：<strong class="text-txt">{{ draft.workflowGroupName }}</strong></div>
                    <div>PM：<strong class="text-txt">{{ draft.pmName }}</strong></div>
                    <div>ACP：<strong class="text-txt">{{ draft.acpBackend }}</strong></div>
                    <div>API Key：<strong class="text-txt">{{ teamHasAuth(draft) ? (draft.authMode === 'customConfig' ? t('pages.agentStudio.wizard.review.customConfigWritten') : t('pages.agentStudio.teamWizard.review.set')) : t('pages.agentStudio.teamWizard.review.skip') }}</strong></div>
                    <div>Git：<strong class="text-txt">{{ draft.gitUrl || t('pages.agentStudio.teamWizard.review.none') }}</strong></div>
                    <div>MCP：<strong class="text-txt">{{ mcpNames }}</strong></div>
                    <div>Env：<strong class="text-txt">{{ envSummary }}</strong></div>
                    <div class="mt-2">
                      {{ t('pages.agentStudio.teamWizard.review.roster') }}
                    </div>
                  </div>
                  <div class="rounded-lg mt-3 border border-line bg-base px-3 py-2">
                    <button type="button" class="mb-1 text-[12px] text-accent-2" @click="bgExpanded = !bgExpanded">
                      {{ t('pages.agentStudio.teamWizard.review.background') }}
                    </button>
                    <pre
                      v-if="bgExpanded"
                      class="m-0 whitespace-pre-wrap font-sans text-[12px] leading-5 text-txt2"
                    >{{ draft.background }}</pre>
                  </div>
                  <p
                    v-if="!hasArtifact"
                    class="rounded-lg mt-3 border border-warn/35 bg-warn/10 px-3 py-2 text-[12px] text-txt2"
                  >
                    {{ t('pages.agentStudio.teamWizard.review.noArtifactWarn') }}
                  </p>
                  <p v-if="!teamHasAuth(draft)" class="mt-3 text-[12px] text-txt3">
                    {{ t('pages.agentStudio.wizard.review.authReminderDetail') }}
                  </p>
                  <p v-if="submitError" class="mt-3 text-[12px] text-err">{{ submitError }}</p>
                </template>
              </div>
            </div>

            <div class="flex shrink-0 items-center justify-between gap-2 border-t border-line bg-surface px-5 py-3.5">
              <AppButton variant="ghost" :disabled="submitting" @click="close">
                {{ t('pages.agentStudio.dialogs.cancel') }}
              </AppButton>
              <div class="ml-auto flex items-center gap-2">
                <AppButton variant="outline" :disabled="draft.step === 0 || submitting" @click="goPrev">
                  {{ t('pages.agentStudio.wizard.prev') }}
                </AppButton>
                <AppButton v-if="currentStep.skip" variant="outline" :disabled="submitting" @click="goSkip">
                  {{ t('pages.agentStudio.wizard.skip') }}
                </AppButton>
                <AppButton variant="primary" :disabled="submitting" @click="goNext">
                  {{
                    submitting
                      ? t('pages.agentStudio.teamWizard.submitting')
                      : currentStep.id === 'review'
                        ? t('pages.agentStudio.teamWizard.confirmStart')
                        : t('pages.agentStudio.wizard.next')
                  }}
                </AppButton>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.wiz-head {
  background: linear-gradient(90deg, rgba(123, 97, 255, 0.12) 0%, transparent 42%), rgb(var(--c-surface));
}
.hero-mark {
  background: linear-gradient(145deg, rgba(123, 97, 255, 0.22), rgb(var(--c-elevated)));
}
.wiz-progress > span {
  display: block;
  height: 100%;
  background: rgb(var(--c-accent));
  transition: width 0.25s ease;
}
.sec-meta {
  margin: 0 0 1.1rem;
  font-size: 13px;
  line-height: 1.55;
  color: rgb(var(--c-txt3));
}
.rail-item {
  display: flex;
  gap: 10px;
  padding: 8px 6px;
  color: rgb(var(--c-txt3));
  font-size: 13px;
}
.rail-item.cur {
  color: rgb(var(--c-txt));
  background: rgba(123, 97, 255, 0.12);
}
.rail-item.done {
  color: rgb(var(--c-ok));
}
.rail-item .node {
  width: 10px;
  height: 10px;
  margin-top: 4px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 999px;
}
.rail-item.cur .node {
  border-color: rgb(var(--c-accent));
  background: rgb(var(--c-accent));
}
.pulse-dot {
  border-radius: 999px;
}
</style>
