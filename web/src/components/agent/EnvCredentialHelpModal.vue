<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import { BACKEND_AUTH_HINTS } from '@/lib/agent/backendAuthGuide'
import type { BackendId } from '@/lib/shared/regionPolicy'

export type EnvCredentialHelpSection = 'inject' | 'git' | 'acp'

const SECTIONS: EnvCredentialHelpSection[] = ['inject', 'git', 'acp']

const props = withDefaults(
  defineProps<{
    open: boolean
    section?: string | null
    backend?: BackendId
    /** Raise overlay above create-wizard (z-50) without changing AppModal API. */
    elevated?: boolean
  }>(),
  { section: 'inject', backend: 'cursor', elevated: false },
)

const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()

const modalRef = ref<{ scrollAreaEl: HTMLElement | null } | null>(null)
const activeSection = ref<EnvCredentialHelpSection>('inject')
let lastTrigger: HTMLElement | null = null

function normalizeSection(raw: unknown): EnvCredentialHelpSection {
  return raw === 'git' || raw === 'acp' || raw === 'inject' ? raw : 'inject'
}

const authHint = computed(() => BACKEND_AUTH_HINTS[props.backend || 'cursor'])
const authKeysLabel = computed(() => authHint.value.key)

function bumpElevatedZ() {
  if (!props.elevated) return
  const overlay = modalRef.value?.scrollAreaEl?.closest('.fixed.inset-0') as HTMLElement | null
  if (overlay) overlay.style.zIndex = '60'
}

async function scrollToSection(id: EnvCredentialHelpSection) {
  activeSection.value = id
  await nextTick()
  bumpElevatedZ()
  const root = modalRef.value?.scrollAreaEl
  const el = root?.querySelector(`[data-help-section="${id}"]`) as HTMLElement | null
  el?.scrollIntoView({ block: 'start' })
}

watch(
  () => [props.open, props.section] as const,
  async ([open]) => {
    if (!open) return
    await scrollToSection(normalizeSection(props.section))
  },
  { immediate: true },
)

watch(
  () => props.open,
  (open, wasOpen) => {
    if (open && !wasOpen) {
      lastTrigger = document.activeElement instanceof HTMLElement ? document.activeElement : null
    }
    if (!open && wasOpen) {
      nextTick(() => lastTrigger?.focus())
    }
  },
)

function onClose() {
  emit('close')
}

function onChip(id: EnvCredentialHelpSection) {
  void scrollToSection(id)
}
</script>

<template>
  <AppModal
    ref="modalRef"
    :open="open"
    :title="t('pages.agentStudio.envHelp.title')"
    :width="640"
    close-on-esc
    @close="onClose"
  >
    <div class="mb-3.5 flex flex-wrap gap-1.5" data-test="env-credential-help">
      <button
        v-for="id in SECTIONS"
        :key="id"
        type="button"
        class="rounded-md border px-2 py-1 text-[11px]"
        :class="activeSection === id ? 'border-accent bg-accent-dim text-txt' : 'border-line bg-base text-txt2'"
        :data-help-chip="id"
        @click="onChip(id)"
      >
        {{ t(`pages.agentStudio.envHelp.chip.${id}`) }}
      </button>
    </div>

    <section data-help-section="inject" class="mb-[18px] scroll-mt-2">
      <h3 class="mb-2 mt-0 text-[13px] font-semibold text-txt">
        {{ t('pages.agentStudio.envHelp.chip.inject') }}
      </h3>
      <p class="mb-2 mt-0 text-[13px] leading-[1.7] text-txt2">
        {{ t('pages.agentStudio.env.hint') }}
      </p>
    </section>

    <section data-help-section="git" class="mb-[18px] scroll-mt-2">
      <h3 class="mb-2 mt-0 text-[13px] font-semibold text-txt">
        {{ t('pages.agentStudio.envHelp.chip.git') }}
      </h3>
      <p class="mb-2 mt-0 text-[13px] leading-[1.7] text-txt2">
        {{ t('pages.agentStudio.git.envHint') }}
      </p>
      <p class="mb-2 mt-0 text-[13px] leading-[1.7] text-txt2">
        {{ t('pages.agentStudio.git.boundary') }}
      </p>
      <div class="rounded-md border border-line bg-base px-3 py-2.5 text-[12px] text-txt2">
        <b class="font-semibold text-txt">{{ t('pages.agentStudio.git.runtimeResolve') }}</b>
        — {{ t('pages.agentStudio.git.runtimeResolveHint') }}
      </div>
    </section>

    <section data-help-section="acp" class="scroll-mt-2">
      <h3 class="mb-2 mt-0 text-[13px] font-semibold text-txt">
        {{ t('pages.agentStudio.envHelp.chip.acp') }}
      </h3>
      <p class="mb-2 mt-0 text-[13px] leading-[1.7] text-txt2">
        {{ t('pages.agentStudio.env.backendAuthIntro', { backend: backend || 'cursor' }) }}
      </p>
      <p class="mb-2 font-mono text-[12px] text-accent-2">{{ authKeysLabel }}</p>
      <p class="m-0 text-[13px] leading-[1.7] text-txt2">
        {{ authHint.note }} — {{ t('pages.agentStudio.env.backendAuthMasked') }}
      </p>
    </section>

    <template #footer>
      <AppButton variant="primary" data-test="env-help-got-it" @click="onClose">
        {{ t('pages.agentStudio.envHelp.gotIt') }}
      </AppButton>
    </template>
  </AppModal>
</template>
