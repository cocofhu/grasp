<script setup lang="ts">
import AgentGitCredentialPicker from '@/components/agent/AgentGitCredentialPicker.vue'
import { legacyGitBindings, setDraftGitBindings } from '@/lib/agent/gitBindings'
import EnvCredentialHelpModal from '@/components/agent/EnvCredentialHelpModal.vue'
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AgentFilesPanel from '@/components/agent/AgentFilesPanel.vue'
import AgentMcpPanel from '@/components/agent/AgentMcpPanel.vue'
import AgentEnvPanel from '@/components/agent/AgentEnvPanel.vue'
import { api, type SharedAgentConfig } from '@/lib/api/api'
import {
  DEFAULT_WORKSPACE_DIR,
  defaultConfigRootFor,
  draftPayloadJson,
  fromDraft,
  hydrateStudioDraft,
  kvToRec,
  recToKV,
  type AgentStudioDraft,
} from '@/lib/agent/agentStudioDraft'
import {
  getRegionPolicy,
  isManagedRegionKey,
  normalizeRegions,
  setRegion,
  switchBackendRegions,
  type BackendId,
} from '@/lib/shared/regionPolicy'
import { switchOpenCodeEnv } from '@/lib/agent/openCodeProvider'
import WorkspaceModelDefaults from './WorkspaceModelDefaults.vue'
import CodingToolPicker from '@/components/agent/CodingToolPicker.vue'
import RepoAccessPicker from '@/components/agent/RepoAccessPicker.vue'
import { switchCliModelEnv } from '@/lib/agent/cliModelEnv'
import { agentConfigRelPath } from '@/lib/agent/backendAuthGuide'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { useToast } from '@/lib/composables/useToast'

const { t } = useI18n()
const toast = useToast()
const { isMobile } = useBreakpoint()

type SubTab = 'files' | 'mcp' | 'env' | 'meta'

const gitHelpOpen = ref(false)
const loading = ref(true)
const loadError = ref('')
const saving = ref(false)
const draft = ref<AgentStudioDraft | null>(null)
const originalJson = ref('')
const subTab = ref<SubTab>('files')
const justSaved = ref(false)
const filesPanelRef = ref<InstanceType<typeof AgentFilesPanel> | null>(null)
let configRootTouched = false

const dirty = computed(() => {
  if (!draft.value) return false
  return draftPayloadJson(draft.value) !== originalJson.value
})

const subTabs = computed(() => {
  if (!draft.value) return [] as { k: SubTab; l: string }[]
  const d = draft.value
  return [
    { k: 'files' as const, l: t('pages.agentStudio.tabs.files', { n: d.files.length }) },
    { k: 'mcp' as const, l: t('pages.agentStudio.tabs.mcp', { n: d.mcp.length }) },
    {
      k: 'env' as const,
      l: t('pages.agentStudio.tabs.env', { n: d.env.filter((e) => !isManagedRegionKey(e.k)).length }),
    },
    { k: 'meta' as const, l: t('pages.agentStudio.tabs.meta') },
  ]
})

const currentRegionPolicy = computed(() =>
  draft.value ? getRegionPolicy(draft.value.acpBackend) : null,
)

const displayRegion = computed(() => {
  if (!draft.value) return ''
  const normalized = normalizeRegions(
    kvToRec(draft.value.env),
    draft.value.acpBackend,
    'preserve-special',
  )
  return normalized.special ? '' : normalized.region
})

const specialRegion = computed(() => {
  if (!draft.value) return ''
  const normalized = normalizeRegions(
    kvToRec(draft.value.env),
    draft.value.acpBackend,
    'preserve-special',
  )
  return normalized.special ? normalized.region : ''
})

function sharedToDraft(cfg: SharedAgentConfig): AgentStudioDraft {
  return hydrateStudioDraft({
    name: '__workspace_shared__',
    acpBackend: cfg.acpBackend,
    aiCredentialId: cfg.aiCredentialId || '',
    openCodeCredentialId: cfg.openCodeCredentialId || '',
    gitCredentialType: cfg.gitCredentialType as AgentStudioDraft['gitCredentialType'],
    ...(cfg.gitBindings ? { gitBindings: cfg.gitBindings } : {}),
    repoIds: cfg.repoIds || [],
    files: cfg.files || [],
    mcp: cfg.mcp || [],
    env: cfg.env || {},
    layout: cfg.layout || {},
  })
}

function applyLoaded(cfg: SharedAgentConfig) {
  const d = sharedToDraft(cfg)
  draft.value = d
  originalJson.value = draftPayloadJson(d)
  configRootTouched = false
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const cfg = await api.getSharedAgentConfig()
    applyLoaded(cfg)
  } catch (e: unknown) {
    loadError.value = String((e as { message?: string })?.message || e)
    draft.value = null
  } finally {
    loading.value = false
  }
}

async function save(): Promise<boolean> {
  if (!draft.value) return false
  saving.value = true
  try {
    const payload = fromDraft(draft.value)
    const saved = await api.putSharedAgentConfig({
      acpBackend: payload.acpBackend,
      gitCredentialType: payload.gitCredentialType || '',
      ...(payload.gitBindings ? { gitBindings: payload.gitBindings } : {}),
      aiCredentialId: draft.value.aiCredentialId || '',
      openCodeCredentialId: draft.value.openCodeCredentialId || '',
      repoIds: draft.value.repoIds || [],
      files: payload.files || [],
      mcp: payload.mcp || [],
      env: payload.env || {},
      layout: payload.layout || {},
    })
    applyLoaded(saved)
    toast.success(t('pages.workspace.saved'))
    justSaved.value = true
    return true
  } catch (e: unknown) {
    toast.error(String((e as { message?: string })?.message || e))
    return false
  } finally {
    saving.value = false
  }
}

function discard() {
  if (!draft.value) return
  void load()
}

function selectAcpBackend(id: BackendId) {
  if (!draft.value) return
  const prev = draft.value.acpBackend
  draft.value.acpBackend = id
  if (!configRootTouched) {
    draft.value.layout.configRoot = defaultConfigRootFor(id)
  }
  if (prev !== id) {
    draft.value.aiCredentialId = ''
    draft.value.env = recToKV(
      switchCliModelEnv(switchOpenCodeEnv(switchBackendRegions(kvToRec(draft.value.env), id), id), prev, id).env,
    )
  }
}

function selectRegion(id: string) {
  if (!draft.value) return
  draft.value.env = recToKV(setRegion(kvToRec(draft.value.env), draft.value.acpBackend, id))
}

async function focusCliModel() {
  subTab.value = 'meta'
  await nextTick()
  document.querySelector('[data-testid=workspace-model-defaults]')?.scrollIntoView({ block: 'nearest' })
}

function openSettingsInFiles() {
  if (!draft.value) return
  const backend = draft.value.acpBackend
  subTab.value = 'files'
  void Promise.resolve().then(() => {
    filesPanelRef.value?.openPathOrCreate?.(agentConfigRelPath(backend))
  })
}

onMounted(() => {
  void load()
})
// Agent Studio asks before switching away from unsaved defaults.
defineExpose({ dirty, save })
</script>

<template>
  <div
    class="flex min-h-0 flex-1 flex-col overflow-hidden"
    data-testid="workspace-shared-agent-panel"
  >
    <div
      v-if="loading"
      class="flex flex-1 items-center justify-center text-[13px] text-txt3"
    >
      {{ t('common.loading.inProgress') }}
    </div>

    <div
      v-else-if="loadError"
      class="flex flex-1 flex-col items-center justify-center gap-2 px-4 text-center"
    >
      <p class="text-[13px] text-err">{{ loadError }}</p>
      <AppButton size="sm" variant="outline" @click="load">{{ t('common.buttons.retry') }}</AppButton>
    </div>

    <template v-else-if="draft">
      <div class="toolbar-inline-row flex shrink-0 items-center gap-2 border-b border-line/60">
        <details class="relative shrink-0" data-testid="shared-agent-help">
          <summary
            class="flex h-[18px] w-[18px] cursor-pointer list-none items-center justify-center rounded-full border border-line text-[11px] font-semibold text-accent-2 transition hover:border-accent hover:bg-accent-dim [&::-webkit-details-marker]:hidden"
            :aria-label="t('pages.workspace.sharedAgent.extendHint')"
          >
            ?
          </summary>
          <p
            class="absolute left-0 top-[24px] z-30 m-0 w-[min(280px,calc(100vw-48px))] rounded-md border border-line-strong bg-elevated px-3 py-2 text-[11px] leading-5 text-txt2 shadow-lg"
            data-testid="shared-agent-help-text"
          >
            {{ t('pages.workspace.sharedAgent.extendHint') }}
          </p>
        </details>
        <div class="scroll-area flex min-w-0 flex-1 gap-1 overflow-x-auto">
          <button
            v-for="tabItem in subTabs"
            :key="tabItem.k"
            type="button"
            class="shrink-0 whitespace-nowrap px-2.5 py-1.5 text-[12px] transition"
            :class="
              subTab === tabItem.k
                ? 'border-b-2 border-accent text-txt'
                : 'text-txt3 hover:text-txt2'
            "
            :data-testid="`shared-agent-subtab-${tabItem.k}`"
            @click="subTab = tabItem.k"
          >
            {{ tabItem.l }}
          </button>
        </div>
        <AppButton
          size="sm"
          variant="outline"
          :disabled="!dirty || saving"
          @click="discard"
        >
          {{ t('pages.workspace.sharedAgent.discard') }}
        </AppButton>
        <AppButton
          size="sm"
          variant="primary"
          :disabled="!dirty || saving"
          data-testid="shared-agent-save"
          @click="save"
        >
          {{ saving ? t('common.buttons.saving') : t('common.buttons.save') }}
        </AppButton>
      </div>

      <AgentFilesPanel
        v-show="subTab === 'files'"
        ref="filesPanelRef"
        :draft="draft"
        :dirty="dirty"
        :is-mobile="isMobile"
        :save="save"
        @toast="toast.success($event)"
        @error="toast.error($event)"
        @update:just-saved="justSaved = $event"
        @discard="discard"
      />

      <AgentMcpPanel
        v-if="subTab === 'mcp'"
        :draft="draft"
        @toast="toast.success($event)"
      />

      <AgentEnvPanel
        v-if="subTab === 'env'"
        :draft="draft"
        context="shared"
        @toast="toast.success($event)"
        @open-settings-file="openSettingsInFiles"
        @edit-model="focusCliModel"
      />

      <div
        v-if="subTab === 'meta'"
        class="scroll-area min-h-0 flex-1 overflow-y-auto p-4"
        data-testid="shared-agent-meta"
      >
        <div class="max-w-3xl space-y-4">
          <section>
            <h3 class="mb-3 mt-0 text-[13px] font-semibold text-txt">{{ t('pages.agentStudio.toolPicker.title') }}</h3>
            <CodingToolPicker
              :model-value="draft.acpBackend"
              :region-policy="currentRegionPolicy"
              :current-region="displayRegion"
              :special-region="specialRegion"
              :disabled="saving"
              @update:model-value="selectAcpBackend"
              @select-region="selectRegion"
            />
          </section>

          <WorkspaceModelDefaults />

          <AgentGitCredentialPicker :model-value="draft.gitBindings" :legacy="legacyGitBindings(draft)" :disabled="saving" @update:model-value="setDraftGitBindings(draft, $event)" @help="gitHelpOpen = true" />
          <EnvCredentialHelpModal :open="gitHelpOpen" section="git" :backend="draft.acpBackend" @close="gitHelpOpen = false" />

          <RepoAccessPicker :model-value="draft.repoIds" shared @update:model-value="draft.repoIds = $event || []" />

          <label class="block">
            <span class="text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.meta.configRoot') }}</span>
            <input
              v-model="draft.layout.configRoot"
              :placeholder="defaultConfigRootFor(draft.acpBackend)"
              spellcheck="false"
              data-testid="meta-config-root"
              class="rounded-md mt-1.5 w-full border border-line bg-base px-3 py-2 font-mono text-[12px] text-txt outline-none focus:border-accent"
              @input="configRootTouched = true"
            />
          </label>
          <label class="block">
            <span class="text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.meta.workspaceDir') }}</span>
            <input
              v-model="draft.layout.workspaceDir"
              :placeholder="DEFAULT_WORKSPACE_DIR"
              spellcheck="false"
              data-testid="meta-workspace-dir"
              class="rounded-md mt-1.5 w-full border border-line bg-base px-3 py-2 font-mono text-[12px] text-txt outline-none focus:border-accent"
            />
          </label>
        </div>
      </div>
    </template>
  </div>
</template>
