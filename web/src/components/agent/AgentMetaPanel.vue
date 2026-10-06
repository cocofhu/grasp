<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
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
  applyOpenCodeFields,
  openCodeCustomBaseRequired,
  openCodeFieldsFromEnv,
  openCodeModelRequired,
  switchOpenCodeEnv,
  type OpenCodeProviderId,
} from '@/lib/agent/openCodeProvider'
import OpenCodeProviderFields from '@/components/agent/OpenCodeProviderFields.vue'

const props = defineProps<{
  draft: AgentStudioDraft
  agentName: string
  projects: { id: string; name: string }[]
}>()

const { t } = useI18n()

let configRootTouched = false

const pendingProjectId = ref<string | null>(null)
const showProjectSwitch = ref(false)

watch(
  () => props.agentName,
  () => {
    configRootTouched = false
  },
)

function projectNameById(id: string): string {
  return props.projects.find((p) => p.id === id)?.name || id
}

const projectSelectValue = computed<string>({
  get: () => props.draft.projectId,
  set: (val) => {
    const old = props.draft.projectId
    if (val === old) return
    if (old) {
      pendingProjectId.value = val
      showProjectSwitch.value = true
      return
    }
    props.draft.projectId = val
  },
})

const pendingProjectLabel = computed(() =>
  pendingProjectId.value ? projectNameById(pendingProjectId.value) : '',
)

function confirmProjectChange() {
  if (pendingProjectId.value !== null) {
    props.draft.projectId = pendingProjectId.value
  }
  pendingProjectId.value = null
  showProjectSwitch.value = false
}

function cancelProjectChange() {
  pendingProjectId.value = null
  showProjectSwitch.value = false
}

function selectAcpBackend(id: BackendId) {
  const prev = props.draft.acpBackend
  props.draft.acpBackend = id
  if (!configRootTouched) {
    props.draft.layout.configRoot = defaultConfigRootFor(id)
  }
  if (prev !== id) {
    props.draft.env = recToKV(
      switchOpenCodeEnv(switchBackendRegions(kvToRec(props.draft.env), id), id),
    )
  }
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

const openCodeFields = computed(() => openCodeFieldsFromEnv(kvToRec(props.draft.env)))
const showOpenCode = computed(() => props.draft.acpBackend === 'opencode')

function patchOpenCode(fields: Parameters<typeof applyOpenCodeFields>[1]) {
  props.draft.env = recToKV(applyOpenCodeFields(kvToRec(props.draft.env), fields))
}

function onOpenCodeProvider(id: OpenCodeProviderId) {
  patchOpenCode({ provider: id })
}

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
      <p class="border-l-2 border-accent-2 bg-accent-dim px-2.5 py-1.5 text-[11px] leading-5 text-txt2">
        {{ t('pages.agentStudio.tree.metaRenameHint') }}
      </p>
    </div>

    <div class="mb-8 max-w-3xl">
      <div class="mb-1 text-[12px] font-medium text-txt2">
        {{ t('pages.agentStudio.project.label') }}
        <span class="text-err">*</span>
      </div>
      <p class="mb-2.5 text-[11px] leading-5 text-txt3">{{ t('pages.agentStudio.project.hint') }}</p>
      <select
        v-model="projectSelectValue"
        data-test="agent-project-select"
        class="max-w-sm w-full rounded border border-line bg-surface px-2 py-1.5 text-[12px] text-txt outline-none focus:border-accent"
      >
        <option v-if="!draft.projectId" value="" disabled>{{ t('pages.agentStudio.project.placeholder') }}</option>
        <option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</option>
      </select>
    </div>

    <div class="mb-4 max-w-3xl border-t border-line pt-5">
      <h3 class="text-sm font-semibold text-txt">{{ t('pages.agentStudio.meta.layoutTitle') }}</h3>
      <p class="mt-1 text-[12px] leading-6 text-txt3" v-html="t('pages.agentStudio.meta.layoutIntro')" />
    </div>

    <div class="max-w-3xl space-y-4">
      <div>
        <div class="text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.meta.acpBackend') }}</div>
        <p class="mb-2 text-[11px] text-txt3">{{ t('pages.agentStudio.meta.acpBackendDesc') }}</p>
        <div class="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
          <button
            v-for="b in ACP_BACKENDS"
            :key="b.id"
            type="button"
            class="rounded-lg border px-2 py-3 text-center transition"
            :class="draft.acpBackend === b.id ? 'border-accent bg-accent-dim text-txt' : 'border-line bg-base text-txt2 hover:border-line-strong'"
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
        <div class="grid max-w-md grid-cols-2 gap-2" role="radiogroup" :aria-label="t('pages.agentStudio.region.title')">
          <button
            v-for="r in metaRegionOptions"
            :key="r.id"
            type="button"
            class="rounded-lg border px-2 py-3 text-center transition"
            :class="displayRegion === r.id ? 'border-accent bg-accent-dim text-txt' : 'border-line bg-base text-txt2 hover:border-line-strong'"
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
      <div v-if="showOpenCode" class="border-t border-dashed border-line pt-4">
        <div class="mb-2 text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.openCode.title') }}</div>
        <p class="mb-3 text-[11px] text-txt3">{{ t('pages.agentStudio.openCode.desc') }}</p>
        <OpenCodeProviderFields
          :provider="openCodeFields.provider"
          :base-url="openCodeFields.baseURL"
          :model="openCodeFields.model"
          :vision="openCodeFields.vision"
          :require-base="openCodeCustomBaseRequired(openCodeFields.provider, openCodeFields.baseURL)"
          :require-model="openCodeModelRequired(openCodeFields.model)"
          @update:provider="onOpenCodeProvider"
          @update:base-url="patchOpenCode({ baseURL: $event })"
          @update:model="patchOpenCode({ model: $event })"
          @update:vision="patchOpenCode({ vision: $event })"
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
      <div class="overflow-hidden rounded-lg border border-line">
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

    <p class="mt-3 max-w-3xl text-[11px] leading-5 text-txt3">
      {{ t('pages.agentStudio.meta.capabilitiesNote') }}
    </p>

    <AppModal :open="showProjectSwitch" :title="t('pages.agentStudio.project.switchTitle')" :width="460" @close="cancelProjectChange">
      <div class="space-y-2 text-[13px] leading-6 text-txt2">
        <p>{{ t('pages.agentStudio.project.switchWarn') }}</p>
        <ul class="list-disc space-y-1 pl-5 text-[12px]">
          <li>{{ t('pages.agentStudio.project.switchItemMemory') }}</li>
          <li>{{ t('pages.agentStudio.project.switchItemContext') }}</li>
          <li>{{ t('pages.agentStudio.project.switchItemJobs') }}</li>
          <li>{{ t('pages.agentStudio.project.switchItemPm') }}</li>
        </ul>
        <p class="mt-2 text-[11.5px] text-txt3">{{ t('pages.agentStudio.project.switchApplyHint') }}</p>
      </div>
      <template #footer>
        <AppButton size="sm" variant="ghost" @click="cancelProjectChange">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton size="sm" variant="danger" @click="confirmProjectChange">
          {{ t('pages.agentStudio.project.switchConfirm', { name: pendingProjectLabel }) }}
        </AppButton>
      </template>
    </AppModal>
  </div>
</template>
