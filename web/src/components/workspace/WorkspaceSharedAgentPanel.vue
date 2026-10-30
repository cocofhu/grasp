<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AgentFilesPanel from '@/components/agent/AgentFilesPanel.vue'
import AgentMcpPanel from '@/components/agent/AgentMcpPanel.vue'
import AgentEnvPanel from '@/components/agent/AgentEnvPanel.vue'
import AgentRuntimeBindings from '@/components/agent/AgentRuntimeBindings.vue'
import StudioTabStrip from '@/components/agent/StudioTabStrip.vue'
import EnvCredentialHelpModal from '@/components/agent/EnvCredentialHelpModal.vue'
import { api, type SharedAgentConfig } from '@/lib/api/api'
import {
  draftPayloadJson,
  fromDraft,
  hydrateStudioDraft,
  type AgentStudioDraft,
} from '@/lib/agent/agentStudioDraft'
import {
  isManagedRegionKey,
} from '@/lib/shared/regionPolicy'
import { agentConfigRelPath } from '@/lib/agent/backendAuthGuide'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { useToast } from '@/lib/composables/useToast'

const { t } = useI18n()
const toast = useToast()
const { isMobile } = useBreakpoint()

type SubTab = 'files' | 'mcp' | 'env' | 'meta'

const loading = ref(true)
const loadError = ref('')
const saving = ref(false)
const draft = ref<AgentStudioDraft | null>(null)
const originalJson = ref('')
const subTab = ref<SubTab>('files')
const justSaved = ref(false)
const gitHelpOpen = ref(false)
const filesPanelRef = ref<InstanceType<typeof AgentFilesPanel> | null>(null)

const dirty = computed(() => {
  if (!draft.value) return false
  return draftPayloadJson(draft.value) !== originalJson.value
})

const subTabs = computed(() => {
  if (!draft.value) return [] as { k: SubTab; l: string; n?: number }[]
  const d = draft.value
  return [
    { k: 'files' as const, l: t('pages.agentStudio.tabs.files'), n: d.files.length },
    { k: 'mcp' as const, l: t('pages.agentStudio.tabs.mcp'), n: d.mcp.length },
    { k: 'env' as const, l: t('pages.agentStudio.tabs.env'), n: d.env.filter((e) => !isManagedRegionKey(e.k)).length },
    { k: 'meta' as const, l: t('pages.agentStudio.tabs.meta') },
  ]
})

function sharedToDraft(cfg: SharedAgentConfig): AgentStudioDraft {
  return hydrateStudioDraft({
    name: '__workspace_shared__',
    acpBackend: cfg.acpBackend || 'cursor',
    modelBinding: cfg.modelBinding,
    gitBindings: cfg.gitBindings,
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
      ...(payload.modelBinding ? { acpBackend: payload.acpBackend, modelBinding: payload.modelBinding } : {}),
      ...(payload.gitBindings?.items.length ? { gitBindings: payload.gitBindings } : {}),
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
        <div class="scroll-area flex min-w-0 flex-1 overflow-x-auto">
          <StudioTabStrip v-model="subTab" :tabs="subTabs" :touch="isMobile" dense testid-prefix="shared-agent-subtab-" />
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
      />

      <div
        v-if="subTab === 'meta'"
        class="scroll-area min-h-0 flex-1 overflow-y-auto p-4"
        data-testid="shared-agent-meta"
      >
        <div class="max-w-3xl space-y-4">
          <AgentRuntimeBindings :draft="draft" hide-manage-link @help="gitHelpOpen = true" />
        </div>
      </div>
      <EnvCredentialHelpModal :open="gitHelpOpen" section="git" :backend="draft.acpBackend" @close="gitHelpOpen = false" />
    </template>
  </div>
</template>
