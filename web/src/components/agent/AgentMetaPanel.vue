<script setup lang="ts">
import { computed, ref, watch } from 'vue'
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
  DEFAULT_CONFIG_ROOT,
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
import CredentialAliasPicker from '@/components/agent/CredentialAliasPicker.vue'
import CliRuntimeCard from '@/components/agent/CliRuntimeCard.vue'
import { gitKind } from '@/lib/project/credentialKinds'
import { api } from '@/lib/api/api'
import { readCliModel, switchCliModelEnv } from '@/lib/agent/cliModelEnv'
import { useToast } from '@/lib/composables/useToast'
import AgentCredentialBindings from '@/components/agent/AgentCredentialBindings.vue'

const props = defineProps<{
  draft: AgentStudioDraft
  agentName: string
  projects: { id: string; name: string }[]
  /** Bumped by the env tab's "Edit" link: scroll to the model card and pulse it. */
  focusModelTick?: number
}>()

const { t } = useI18n()
const toast = useToast()

let configRootTouched = false


watch(
  () => props.agentName,
  () => {
    configRootTouched = false
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

/** The project's shared model applies only while the Agent runs the same CLI (see ExtendOverlay). */
const sharedBaseline = ref<{ acpBackend: string; model: string } | null>(null)
watch(
  () => props.draft.projectId,
  async (pid) => {
    sharedBaseline.value = null
    if (!pid) return
    try {
      const cfg = await api.getProjectSharedAgentConfig(pid)
      if (props.draft.projectId === pid) {
        sharedBaseline.value = { acpBackend: cfg.acpBackend || '', model: readCliModel(cfg.env || {}) }
      }
    } catch {
      /* The hint is optional; the card works without it. */
    }
  },
  { immediate: true },
)
const inheritedModel = computed(() => {
  const s = sharedBaseline.value
  return s && s.acpBackend === props.draft.acpBackend ? s.model : ''
})

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

function joinConfigPath(root: string, sub: string): string {
  return (root || DEFAULT_CONFIG_ROOT).replace(/\/+$/, '') + '/' + sub
}

const derivedPaths = computed(() => {
  const root = props.draft.layout.configRoot || DEFAULT_CONFIG_ROOT
  return [
    { label: t('pages.agentStudio.configPaths.mcp'), path: joinConfigPath(root, 'mcp.json'), note: t('pages.agentStudio.configPaths.mcpNote') },
    { label: t('pages.agentStudio.configPaths.rules'), path: joinConfigPath(root, 'rules/'), note: t('pages.agentStudio.configPaths.rulesNote') },
    { label: t('pages.agentStudio.configPaths.skills'), path: joinConfigPath(root, 'skills/'), note: t('pages.agentStudio.configPaths.skillsNote') },
    { label: t('pages.agentStudio.configPaths.commands'), path: joinConfigPath(root, 'commands/'), note: t('pages.agentStudio.configPaths.commandsNote') },
    { label: t('pages.agentStudio.configPaths.env'), path: 'container-env', note: t('pages.agentStudio.configPaths.envNote') },
  ]
})
</script>

<template>
  <div class="scroll-area min-h-0 flex-1 overflow-auto p-4">
    <div class="mb-4 max-w-3xl">
      <p class="text-[12px] leading-5 text-txt3">
        {{ t('pages.agentStudio.tree.metaRenameHint') }}
      </p>
    </div>

    <div class="mb-4 max-w-3xl border-t border-line pt-5">
      <h3 class="text-sm font-semibold text-txt">{{ t('pages.agentStudio.meta.layoutTitle') }}</h3>
      <p class="mt-1 text-[12px] leading-6 text-txt3" v-html="t('pages.agentStudio.meta.layoutIntro')" />
    </div>

    <div class="max-w-3xl space-y-4">
      <div>
        <div class="text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.meta.acpBackend') }}</div>
        <p class="mb-2 text-[11px] text-txt3">{{ t('pages.agentStudio.meta.acpBackendDesc') }}</p>
        <div class="flex flex-wrap gap-1 border-b border-line/60 pb-3">
          <button
            v-for="b in ACP_BACKENDS"
            :key="b.id"
            type="button"
            class="min-h-11 rounded-md px-3 py-2 text-left transition"
            :class="draft.acpBackend === b.id ? 'bg-elevated font-medium text-txt' : 'text-txt2 hover:bg-elevated/50'"
            @click="selectAcpBackend(b.id)"
          >
            <div class="text-[12px] font-semibold">{{ b.label }}</div>
            <div class="mt-0.5 font-mono text-[10px] text-txt3">{{ b.id }}</div>
          </button>
        </div>
      </div>
      <div v-if="showMetaRegionBlock" class="border-t border-dashed border-line pt-4">
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
        v-if="showCliRuntime"
        :draft="draft"
        :inherited-model="inheritedModel"
        :focus-tick="focusModelTick"
      />
      <div v-if="showOpenCode" class="border-t border-dashed border-line pt-4">
        <OpenCodeCredentialPicker
          mode="select"
          :project-id="draft.projectId"
          :selected-id="draft.openCodeCredentialId"
          @update:selected-id="onSelectCredential"
        />
      </div>
      <div v-if="gitAliasKind" class="border-t border-dashed border-line pt-4">
        <CredentialAliasPicker
          :project-id="draft.projectId"
          :kind="gitAliasKind.id"
          :selected-id="draft.gitCredentialId"
          @update:selected-id="draft.gitCredentialId = $event"
        />
      </div>
      <div v-if="showSshHosts" class="border-t border-dashed border-line pt-4">
        <CredentialAliasPicker
          :project-id="draft.projectId"
          kind="ssh_hosts"
          :selected-id="draft.sshHostsCredentialId"
          @update:selected-id="draft.sshHostsCredentialId = $event"
        />
      </div>
      <label class="block">
        <span class="text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.meta.configRoot') }}</span>
        <p class="mb-1.5 text-[11px] text-txt3">{{ t('pages.agentStudio.meta.configRootDesc') }}</p>
        <input
          v-model="draft.layout.configRoot"
          :placeholder="defaultConfigRootFor(draft.acpBackend)"
          spellcheck="false"
          class="w-full rounded-md border border-line bg-base px-3 py-2 font-mono text-[12px] text-txt outline-none focus:border-accent"
          @input="configRootTouched = true"
        />
      </label>
      <label class="block">
        <span class="text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.meta.workspaceDir') }}</span>
        <p class="mb-1.5 text-[11px] text-txt3">{{ t('pages.agentStudio.meta.workspaceDirDesc') }}</p>
        <input
          v-model="draft.layout.workspaceDir"
          :placeholder="DEFAULT_WORKSPACE_DIR"
          spellcheck="false"
          class="w-full rounded-md border border-line bg-base px-3 py-2 font-mono text-[12px] text-txt outline-none focus:border-accent"
        />
      </label>
    </div>

    <div class="mt-5 max-w-3xl">
      <div class="mb-1.5 text-[11px] uppercase tracking-wider text-txt3">{{ t('pages.agentStudio.meta.derivedPaths') }}</div>
      <div class="overflow-x-auto border-y border-line/60">
        <table class="w-full text-left text-[12px]">
          <tbody>
            <tr v-for="(e, i) in derivedPaths" :key="e.label" :class="i % 2 ? 'bg-base/40' : ''">
              <td class="px-3 py-2 text-txt2">{{ e.label }}</td>
              <td class="px-3 py-2"><code class="rounded bg-base px-1.5 py-0.5 font-mono text-accent-2">{{ e.path }}</code></td>
              <td class="px-3 py-2 text-txt3">{{ e.note }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <AgentCredentialBindings :draft="draft" />

    <p class="mt-3 max-w-3xl text-[11px] leading-5 text-txt3">
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
