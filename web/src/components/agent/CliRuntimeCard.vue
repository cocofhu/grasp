<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import CliModelSelect from '@/components/agent/CliModelSelect.vue'
import CredentialAliasPicker from '@/components/agent/CredentialAliasPicker.vue'
import { kvToRec, recToKV, type AgentStudioDraft } from '@/lib/agent/agentStudioDraft'
import { backendKind } from '@/lib/project/credentialKinds'
import type { CliCatalogModel } from '@/lib/api/apiTypes'
import { loadCliModels } from '@/lib/agent/cliCatalog'
import {
  CLI_COMMON_EFFORTS,
  cliEffortsFor,
  readCliEffort,
  readCliModel,
  writeCliEffort,
  writeCliModel,
} from '@/lib/agent/cliModelEnv'

/**
 * Credential, model and (Codex) reasoning effort for a coding CLI. Edits the
 * draft's env in place, like the panels around it.
 */
const props = defineProps<{
  draft: Pick<AgentStudioDraft, 'acpBackend' | 'projectId' | 'aiCredentialId' | 'env'>
  description?: string
  credentialTitle?: string
  credentialHint?: string
  modelHint?: string
  /** The project's shared model, shown while this draft stays on auto. */
  inheritedModel?: string
  focusTick?: number
}>()

const { t } = useI18n()

const aiKind = computed(() => {
  const kind = backendKind(props.draft.acpBackend)
  return kind && kind.id !== 'opencode' ? kind : undefined
})

const cliModel = computed(() => readCliModel(kvToRec(props.draft.env)))
const cliEffort = computed(() => readCliEffort(kvToRec(props.draft.env)) || 'auto')

const cliCatalog = ref<CliCatalogModel[]>([])
watch(
  () => props.draft.acpBackend,
  async (backend) => {
    cliCatalog.value = []
    if (!backend || backend === 'opencode') return
    const list = await loadCliModels(backend)
    if (props.draft.acpBackend === backend) cliCatalog.value = list
  },
  { immediate: true },
)

function setCliModel(id: string) {
  let env = writeCliModel(kvToRec(props.draft.env), id)
  const effort = readCliEffort(env)
  const listed = cliCatalog.value.find((m) => m.id === id)
  if (effort && !cliEffortsFor(listed).includes(effort)) env = writeCliEffort(env, '')
  props.draft.env = recToKV(env)
}

function setCliEffort(v: string) {
  props.draft.env = recToKV(writeCliEffort(kvToRec(props.draft.env), v))
}

const modelEfforts = computed(() => cliEffortsFor(cliCatalog.value.find((x) => x.id === cliModel.value)))
const effortSupported = computed(() => modelEfforts.value.length > 0)
const effortOptions = computed(() => ['auto', ...(effortSupported.value ? modelEfforts.value : CLI_COMMON_EFFORTS)])

const inheritedLabel = computed(() => {
  const id = (props.inheritedModel || '').trim()
  if (!id || cliModel.value) return ''
  return cliCatalog.value.find((m) => m.id === id)?.name || id
})

const modelCard = ref<HTMLElement | null>(null)
const modelPulse = ref(false)
watch(
  () => props.focusTick,
  async (tick) => {
    if (!tick) return
    await nextTick()
    modelCard.value?.scrollIntoView({ block: 'center', behavior: 'smooth' })
    modelPulse.value = false
    await nextTick()
    modelPulse.value = true
    setTimeout(() => (modelPulse.value = false), 1400)
  },
  { immediate: true },
)
</script>

<template>
  <section class="runtime-section border-y border-line/60" data-test="agent-cli-runtime">
    <header class="flex items-start gap-2 border-b border-line/60 py-3">
      <span class="flex h-5 w-5 shrink-0 items-center justify-center text-txt3">
        <Icon name="terminal" :size="14" />
      </span>
      <div class="min-w-0">
        <h4 class="m-0 text-[13px] font-semibold text-txt">{{ t('pages.agentStudio.cliModel.runtimeTitle') }}</h4>
        <p class="m-0 text-[11px] text-txt3">{{ description ?? t('pages.agentStudio.cliModel.runtimeDesc') }}</p>
      </div>
    </header>
    <div v-if="aiKind" class="py-4">
      <CredentialAliasPicker
        :project-id="draft.projectId"
        :kind="aiKind.id"
        :selected-id="draft.aiCredentialId"
        :title="credentialTitle"
        :hint="credentialHint ?? t('pages.projectDetail.projectCredentials.codingPickHint')"
        @update:selected-id="draft.aiCredentialId = $event"
      />
    </div>
    <div
      ref="modelCard"
      class="model-block border-t border-line/60 py-4"
      :class="{ 'is-pulse': modelPulse }"
      data-test="agent-cli-model"
    >
      <div class="mb-2 text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.cliModel.label') }}</div>
      <p v-if="modelHint" class="-mt-1 mb-2 text-[11px] text-txt3">{{ modelHint }}</p>
      <CliModelSelect :key="draft.acpBackend" :model-value="cliModel" :backend="draft.acpBackend" @update:model-value="setCliModel" />
      <p
        v-if="inheritedLabel"
        class="mt-2 flex items-center gap-1.5 text-[11px] text-txt3"
        data-test="agent-cli-model-inherited"
      >
        <Icon name="branch" :size="11" />
        <span>{{ t('pages.agentStudio.cliModel.inherited') }}</span>
        <code class="rounded bg-base px-1 py-px font-mono text-[10.5px] text-txt2">{{ inheritedLabel }}</code>
      </p>
    </div>
    <div v-if="draft.acpBackend === 'codex'" class="border-t border-line/60 py-4" data-test="agent-cli-effort">
      <div class="mb-0.5 flex items-center gap-2 text-[12px] font-medium text-txt2">
        <Icon name="brain" :size="13" class="text-txt3" />
        {{ t('pages.agentStudio.cliModel.effortLabel') }}
      </div>
      <p class="mb-2 text-[11px] text-txt3">
        {{ effortSupported ? t('pages.agentStudio.cliModel.effortDesc') : t('pages.agentStudio.cliModel.effortUnsupported') }}
      </p>
      <div
        class="segmented inline-flex flex-wrap rounded-lg border border-line bg-base p-0.5"
        :class="{ 'pointer-events-none opacity-50': !effortSupported }"
        role="radiogroup"
        :aria-label="t('pages.agentStudio.cliModel.effortLabel')"
        :aria-disabled="!effortSupported"
      >
        <button
          v-for="opt in effortOptions"
          :key="opt"
          type="button"
          role="radio"
          :aria-checked="cliEffort === opt"
          :disabled="!effortSupported"
          class="segmented-item rounded-md px-3 py-1 text-[12px] transition"
          :class="cliEffort === opt ? 'is-on bg-surface text-txt shadow-sm' : 'text-txt3 hover:text-txt2'"
          :data-test="`agent-cli-effort-${opt}`"
          @click="setCliEffort(opt)"
        >
          {{ t(`pages.agentStudio.cliModel.effort.${opt}`) }}
        </button>
      </div>
    </div>
  </section>
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
