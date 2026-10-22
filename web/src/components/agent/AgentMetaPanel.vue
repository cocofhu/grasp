<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  ACP_BACKENDS,
  getRegionPolicy,
  normalizeRegions,
  setRegion,
  switchBackendRegions,
  type BackendId,
} from '@/lib/shared/regionPolicy'
import {
  DEFAULT_WORKSPACE_DIR,
  defaultConfigRootFor,
  kvToRec,
  recToKV,
  type AgentStudioDraft,
} from '@/lib/agent/agentStudioDraft'
import {
  OPENCODE_BASE_URL_ENV,
  OPENCODE_MODEL_ENV,
  OPENCODE_MODEL_VISION_ENV,
  OPENCODE_PROVIDER_ENV,
  switchOpenCodeEnv,
} from '@/lib/agent/openCodeProvider'
import OpenCodeCredentialPicker from '@/components/agent/OpenCodeCredentialPicker.vue'
import CredentialProviderLogo from '@/components/workspace/CredentialProviderLogo.vue'
import CredentialAliasPicker from '@/components/agent/CredentialAliasPicker.vue'
import CliRuntimeCard from '@/components/agent/CliRuntimeCard.vue'
import { gitKind } from '@/lib/workspace/credentialKinds'
import { api } from '@/lib/api/api'
import { readCliEffort, readCliModel, switchCliModelEnv, writeCliEffort, writeCliModel } from '@/lib/agent/cliModelEnv'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import { useToast } from '@/lib/composables/useToast'
import AgentCredentialBindings from '@/components/agent/AgentCredentialBindings.vue'
import RepoAccessPicker from '@/components/agent/RepoAccessPicker.vue'

const props = defineProps<{
  draft: AgentStudioDraft
  agentName: string
  /** Bumped by the env tab's "Edit" link: scroll to the model card and pulse it. */
  focusModelTick?: number
}>()

const { t } = useI18n()
const toast = useToast()

let configRootTouched = false
/** Session-only. Turning the switch off keeps the pickers open until a choice is saved or the Agent changes. */
const customizing = ref(false)


watch(
  () => props.agentName,
  () => {
    configRootTouched = false
    customizing.value = false
  },
)

function selectAcpBackend(id: BackendId) {
  const prev = props.draft.acpBackend
  if (prev === id) return
  const before = {
    acpBackend: prev,
    configRoot: props.draft.layout.configRoot,
    aiCredentialId: props.draft.aiCredentialId,
    env: props.draft.env.map((e) => ({ ...e })),
  }
  props.draft.acpBackend = id
  if (!configRootTouched) {
    props.draft.layout.configRoot = defaultConfigRootFor(id)
  }
  props.draft.aiCredentialId = ''
  const switched = switchCliModelEnv(switchOpenCodeEnv(switchBackendRegions(kvToRec(props.draft.env), id), id), prev, id)
  const env = switched.env
  if (id === 'opencode') {
    delete env[OPENCODE_PROVIDER_ENV]
    delete env[OPENCODE_BASE_URL_ENV]
    delete env[OPENCODE_MODEL_VISION_ENV]
  }
  props.draft.env = recToKV(env)
  if (switched.cleared && prev !== 'opencode') {
    const label = ACP_BACKENDS.find((b) => b.id === id)?.label || id
    toast.show(t('pages.agentStudio.cliModel.switched', { cli: label }), 'default', {
      action: {
        label: t('pages.agentStudio.cliModel.undo'),
        run: () => {
          props.draft.acpBackend = before.acpBackend
          props.draft.layout.configRoot = before.configRoot
          props.draft.aiCredentialId = before.aiCredentialId
          props.draft.env = before.env
        },
      },
    })
  }
}

const showCliRuntime = computed(() => !!props.draft.acpBackend && props.draft.acpBackend !== 'opencode')

/** The workspace's shared model and credential apply only while the Agent runs the same CLI. */
const sharedBaseline = ref<{ acpBackend: string; model: string } | null>(null)
const sharedRepoIds = ref<string[]>([])
onMounted(async () => {
  try {
    const cfg = await api.getSharedAgentConfig()
    sharedBaseline.value = {
      acpBackend: cfg.acpBackend || '',
      model: readCliModel(cfg.env || {}),
    }
    sharedRepoIds.value = cfg.repoIds || []
  } catch {
    /* The status is optional; the card works without it. */
  }
})
const sameSharedCli = computed(() => {
  const s = sharedBaseline.value
  return !!s && s.acpBackend === props.draft.acpBackend
})
const inheritedModel = computed(() => (sameSharedCli.value ? sharedBaseline.value?.model || '' : ''))
const sharedBackend = computed(() => ACP_BACKENDS.find((b) => b.id === sharedBaseline.value?.acpBackend) || null)

function hasOwnRuntime(): boolean {
  const env = kvToRec(props.draft.env)
  return !!(readCliModel(env) || readCliEffort(env))
}

/** A stored credential or a pinned model leaves the workspace default. */
function hasOwnCredential(): boolean {
  return !!(props.draft.aiCredentialId?.trim() || props.draft.openCodeCredentialId?.trim())
}

const inheriting = computed(
  () =>
    !!sharedBackend.value &&
    !customizing.value &&
    props.draft.acpBackend === sharedBackend.value.id &&
    !hasOwnRuntime() &&
    !hasOwnCredential(),
)

function clearRuntimeOverrides() {
  props.draft.aiCredentialId = ''
  props.draft.openCodeCredentialId = ''
  const env = writeCliEffort(writeCliModel(kvToRec(props.draft.env), ''), '')
  delete env[OPENCODE_PROVIDER_ENV]
  delete env[OPENCODE_BASE_URL_ENV]
  delete env[OPENCODE_MODEL_VISION_ENV]
  props.draft.env = recToKV(env)
}

function setInherit(on: boolean) {
  if (!on) {
    customizing.value = true
    return
  }
  customizing.value = false
  const backend = sharedBackend.value
  if (backend && props.draft.acpBackend !== backend.id) props.draft.acpBackend = backend.id
  clearRuntimeOverrides()
}

function onSelectCredential(id: string) {
  if (props.draft.openCodeCredentialId === id) return
  props.draft.openCodeCredentialId = id
  const env = kvToRec(props.draft.env)
  delete env[OPENCODE_PROVIDER_ENV]
  delete env[OPENCODE_BASE_URL_ENV]
  delete env[OPENCODE_MODEL_VISION_ENV]
  delete env[OPENCODE_MODEL_ENV]
  delete env.GRASP_OPENCODE_API_KEY
  delete env.OPENCODE_API_KEY
  props.draft.env = recToKV(env)
}

const currentRegionPolicy = computed(() => getRegionPolicy(props.draft.acpBackend))
const showMetaRegionBlock = computed(() => !!currentRegionPolicy.value)
const metaRegionOptions = computed(() => currentRegionPolicy.value?.options || [])

const displayRegion = computed(() => {
  const normalized = normalizeRegions(kvToRec(props.draft.env), props.draft.acpBackend, 'preserve-special')
  return normalized.special ? '' : normalized.region
})

const specialRegion = computed(() => {
  const normalized = normalizeRegions(kvToRec(props.draft.env), props.draft.acpBackend, 'preserve-special')
  return normalized.special ? normalized.region : ''
})

function selectRegion(id: string) {
  props.draft.env = recToKV(setRegion(kvToRec(props.draft.env), props.draft.acpBackend, id))
}

const showOpenCode = computed(() => props.draft.acpBackend === 'opencode')
const gitAliasKind = computed(() => (props.draft.gitCredentialType ? gitKind(props.draft.gitCredentialType) : undefined))
const showSshHosts = computed(() => props.draft.gitCredentialType === 'ssh')

watch(
  () => props.draft.gitCredentialType,
  (next, prev) => {
    if (prev && next !== prev) {
      props.draft.gitCredentialId = ''
      props.draft.sshHostsCredentialId = ''
    }
  },
)

</script>

<template>
  <div class="scroll-area min-h-0 flex-1 overflow-auto p-4">
    <div class="max-w-3xl space-y-4">
      <div v-if="sharedBackend" class="rounded-xl border border-line bg-base px-3.5 py-3" data-test="agent-inherit-default">
        <div class="flex items-center justify-between gap-4">
          <div class="min-w-0 text-[13px] font-medium text-txt">{{ t('pages.agentStudio.cliModel.inheritDefault') }}</div>
          <AppSwitch
            :model-value="inheriting"
            data-test="agent-inherit-default-switch"
            :aria-label="t('pages.agentStudio.cliModel.inheritDefault')"
            @update:model-value="setInherit"
          />
        </div>
        <div v-if="inheriting" class="mt-3 flex items-center gap-2.5 border-t border-line/70 pt-3" data-test="agent-inherit-backend">
          <CredentialProviderLogo :provider="sharedBackend.id" match="provider" selected />
          <div class="min-w-0">
            <div class="text-[12px] font-semibold text-txt">{{ sharedBackend.label }}</div>
            <div class="text-[11px] text-txt3">{{ t('pages.agentStudio.cliModel.inheritDefaultFrom') }}</div>
          </div>
        </div>
      </div>
      <div v-if="!inheriting">
        <div class="text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.meta.acpBackend') }}</div>
        <div class="mt-2 grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
          <button
            v-for="b in ACP_BACKENDS"
            :key="b.id"
            type="button"
            class="flex flex-col items-center gap-2 rounded-lg border px-2 py-3 text-center transition"
            :class="
              draft.acpBackend === b.id
                ? 'border-accent bg-accent-dim text-txt'
                : 'border-line bg-base text-txt2 hover:border-line-strong'
            "
            :data-testid="`acp-backend-${b.id}`"
            :aria-pressed="draft.acpBackend === b.id"
            @click="selectAcpBackend(b.id)"
          >
            <CredentialProviderLogo :provider="b.id" match="provider" :selected="draft.acpBackend === b.id" />
            <div class="text-[12px] font-semibold">{{ b.label }}</div>
          </button>
        </div>
      </div>
      <div v-if="!inheriting && showMetaRegionBlock" class="border-t border-dashed border-line pt-4">
        <div class="text-[12px] font-medium text-txt2">
          {{ t('pages.agentStudio.meta.regionTitle') }}
          <span class="rounded-md ml-1.5 inline-block border border-accent/30 bg-accent-dim px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wider text-accent-2">{{ t('pages.agentStudio.meta.regionNewBadge') }}</span>
        </div>
        <p class="mb-2 text-[11px] text-txt3">{{ t('pages.agentStudio.meta.regionDesc') }}</p>
        <p v-if="specialRegion" class="mb-2 rounded-lg border border-warn/35 bg-warn/10 px-2.5 py-2 font-mono text-[11px] text-warn">
          {{ t('pages.agentStudio.region.special', { value: specialRegion }) }}
        </p>
        <div class="flex max-w-lg flex-wrap gap-2" role="radiogroup" :aria-label="t('pages.agentStudio.region.title')">
          <button
            v-for="r in metaRegionOptions"
            :key="r.id"
            type="button"
            class="min-h-11 rounded-md px-3 py-2 text-left transition"
            :class="displayRegion === r.id ? 'bg-elevated font-medium text-txt' : 'text-txt2 hover:bg-elevated/50'"
            role="radio"
            :aria-checked="displayRegion === r.id"
            :aria-label="`${t(r.labelKey)} (${r.id})`"
            @click="selectRegion(r.id)"
          >
            <div class="text-[12px] font-semibold">{{ t(r.labelKey) }}</div>
            <div class="mt-0.5 font-mono text-[10px] text-txt3">{{ r.id }}</div>
            <div class="mt-1.5 text-[10px] leading-snug" :class="displayRegion === r.id ? 'text-accent-2' : 'text-txt3'">{{ t(r.hintKey) }}</div>
          </button>
        </div>
      </div>
      <CliRuntimeCard
        v-if="!inheriting && showCliRuntime"
        :key="agentName"
        :draft="draft"
        :inherited-model="inheritedModel"
        :focus-tick="focusModelTick"
      />
      <div v-if="!inheriting && showOpenCode" class="border-t border-dashed border-line pt-4">
        <OpenCodeCredentialPicker
          mode="select"
          hide-hint
          :selected-id="draft.openCodeCredentialId"
          @update:selected-id="onSelectCredential"
        />
      </div>
      <div v-if="gitAliasKind" class="border-t border-dashed border-line pt-4">
        <CredentialAliasPicker
          :kind="gitAliasKind.id"
          :selected-id="draft.gitCredentialId"
          @update:selected-id="draft.gitCredentialId = $event"
        />
      </div>
      <div v-if="showSshHosts" class="border-t border-dashed border-line pt-4">
        <CredentialAliasPicker
          kind="ssh_hosts"
          :selected-id="draft.sshHostsCredentialId"
          @update:selected-id="draft.sshHostsCredentialId = $event"
        />
      </div>
      <RepoAccessPicker
        :key="agentName"
        :model-value="draft.repoIds"
        :inherited="sharedRepoIds"
        @update:model-value="draft.repoIds = $event"
      />
      <label v-if="!inheriting" class="block">
        <span class="text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.meta.configRoot') }}</span>
        <input
          v-model="draft.layout.configRoot"
          :placeholder="defaultConfigRootFor(draft.acpBackend)"
          spellcheck="false"
          data-testid="meta-config-root"
          class="mt-1.5 w-full rounded-md border border-line bg-base px-3 py-2 font-mono text-[12px] text-txt outline-none focus:border-accent"
          @input="configRootTouched = true"
        />
      </label>
      <label v-if="!inheriting" class="block">
        <span class="text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.meta.workspaceDir') }}</span>
        <input
          v-model="draft.layout.workspaceDir"
          :placeholder="DEFAULT_WORKSPACE_DIR"
          spellcheck="false"
          data-testid="meta-workspace-dir"
          class="mt-1.5 w-full rounded-md border border-line bg-base px-3 py-2 font-mono text-[12px] text-txt outline-none focus:border-accent"
        />
      </label>
    </div>

    <AgentCredentialBindings v-if="!inheriting" :draft="draft" />

    <p v-if="!inheriting" class="mt-3 max-w-3xl text-[11px] leading-5 text-txt3">
      {{ t('pages.agentStudio.meta.capabilitiesNote') }}
    </p>

  </div>
</template>

<style scoped>
.model-block {
  transition: background-color 600ms ease;
}
.model-block.is-pulse {
  animation: model-pulse 1.4s var(--ease-out-expo);
}
@keyframes model-pulse {
  0%,
  30% {
    background-color: rgb(var(--c-accent) / 0.12);
    box-shadow: inset 3px 0 0 rgb(var(--c-accent));
  }
  100% {
    background-color: transparent;
    box-shadow: inset 3px 0 0 transparent;
  }
}
@media (prefers-reduced-motion: reduce) {
  .model-block.is-pulse {
    animation: none;
    background-color: rgb(var(--c-accent) / 0.08);
  }
}
</style>
