<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import CliModelSelect from '@/components/agent/CliModelSelect.vue'
import CredentialAliasPicker from '@/components/agent/CredentialAliasPicker.vue'
import { kvToRec, recToKV, type AgentStudioDraft } from '@/lib/agent/agentStudioDraft'
import { backendKind } from '@/lib/workspace/credentialKinds'
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
  draft: Pick<AgentStudioDraft, 'acpBackend' | 'aiCredentialId' | 'env'>
  credentialTitle?: string
  /**
   * Workspace shared model for this same CLI. When set and this draft has no
   * model of its own, the card follows that config instead of offering Auto.
   * An empty model is still "not set" — there is no saved "ignore the workspace" value.
   */
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

/** Transient: the user opened "set separately" but has not picked a model yet. Not saved. */
const specifying = ref(false)

function setCliModel(id: string) {
  if (id.trim()) specifying.value = false
  let env = writeCliModel(kvToRec(props.draft.env), id)
  const effort = readCliEffort(env)
  const listed = cliCatalog.value.find((m) => m.id === id)
  if (effort && !cliEffortsFor(listed).includes(effort)) env = writeCliEffort(env, '')
  props.draft.env = recToKV(env)
}
const inheritedId = computed(() => (props.inheritedModel || '').trim())
const canFollow = computed(() => !!inheritedId.value)
const showingFollow = computed(() => !showsInheritance.value && canFollow.value && !cliModel.value && !specifying.value)
const inheritedTitle = computed(() => {
  const id = inheritedId.value
  if (!id) return ''
  return cliCatalog.value.find((m) => m.id === id)?.name || id
})

watch(
  () => props.draft.acpBackend,
  () => {
    specifying.value = false
  },
)

/** Agent meta passes inheritedModel (possibly empty). The workspace page omits it. */
const showsInheritance = computed(() => props.inheritedModel !== undefined)

function followShared() {
  specifying.value = false
  if (cliModel.value) setCliModel('')
}

function specifyOwn() {
  if (!cliModel.value) specifying.value = true
}

function setCliEffort(v: string) {
  props.draft.env = recToKV(writeCliEffort(kvToRec(props.draft.env), v))
}

const modelEfforts = computed(() => cliEffortsFor(cliCatalog.value.find((x) => x.id === cliModel.value)))
const effortSupported = computed(() => modelEfforts.value.length > 0)
const effortOptions = computed(() => ['auto', ...(effortSupported.value ? modelEfforts.value : CLI_COMMON_EFFORTS)])

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
    <div v-if="aiKind" class="py-4">
      <CredentialAliasPicker
        :kind="aiKind.id"
        :selected-id="draft.aiCredentialId"
        :title="credentialTitle"
        hide-hint
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
      <div
        v-if="canFollow && !showsInheritance"
        class="mb-2.5 flex rounded-[10px] border border-line bg-base p-0.5"
        role="radiogroup"
        :aria-label="t('pages.agentStudio.cliModel.label')"
        data-test="agent-cli-model-mode"
      >
        <button
          type="button"
          role="radio"
          class="inline-flex min-h-8 flex-1 items-center justify-center gap-1.5 rounded-lg px-2.5 py-1.5 text-[12px] transition"
          :class="showingFollow ? 'bg-surface font-semibold text-txt shadow-sm' : 'font-medium text-txt3 hover:text-txt2'"
          :aria-checked="showingFollow"
          data-test="agent-cli-model-follow"
          @click="followShared"
        >
          {{ t('pages.agentStudio.cliModel.follow') }}
          <span class="inline-flex h-4 items-center rounded-full bg-accent-dim px-1.5 text-[10px] font-semibold leading-4 text-accent-2">{{ t('pages.agentStudio.cliModel.followDefault') }}</span>
        </button>
        <button
          type="button"
          role="radio"
          class="inline-flex min-h-8 flex-1 items-center justify-center rounded-lg px-2.5 py-1.5 text-[12px] transition"
          :class="!showingFollow ? 'bg-surface font-semibold text-txt shadow-sm' : 'font-medium text-txt3 hover:text-txt2'"
          :aria-checked="!showingFollow"
          data-test="agent-cli-model-own"
          @click="specifyOwn"
        >
          {{ t('pages.agentStudio.cliModel.specify') }}
        </button>
      </div>
      <div
        v-if="showingFollow"
        class="rounded-xl border border-line bg-surface px-3.5 py-3"
        data-test="agent-cli-model-inherited"
      >
        <p class="m-0 text-[10px] font-bold tracking-[0.08em] text-accent-2">{{ t('pages.agentStudio.cliModel.sharedKicker') }}</p>
        <p
          class="m-0 mt-1 text-[14px] font-semibold tracking-tight text-txt"
          :class="inheritedTitle === inheritedId ? 'font-mono' : ''"
          data-test="agent-cli-model-inherited-name"
        >
          {{ inheritedTitle }}
        </p>
        <p class="m-0 mt-1.5 text-[11px] leading-5 text-txt3">{{ t('pages.agentStudio.cliModel.followNote') }}</p>
      </div>
      <template v-else>
        <CliModelSelect
          :key="draft.acpBackend"
          :model-value="cliModel"
          :backend="draft.acpBackend"
          :empty-mode="canFollow && !showsInheritance ? 'required' : 'auto'"
          hide-command
          @update:model-value="setCliModel"
        />
        <button
          v-if="canFollow && !showsInheritance"
          type="button"
          class="mt-2 bg-transparent p-0 text-[11px] font-semibold text-accent-2 hover:underline"
          data-test="agent-cli-model-back"
          @click="followShared"
        >
          {{ t('pages.agentStudio.cliModel.backToFollow') }}
        </button>
      </template>
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
