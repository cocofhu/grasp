<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import {
  CLI_BACKENDS,
  START_PATH_OPTIONS,
  type StartPath,
  type WizardBackendId,
} from '@/lib/agent/agentCreateWizard'
import { OPENCODE_FALLBACK_PROVIDERS } from '@/lib/agent/openCodeProvider'
import type { RegionPolicy } from '@/lib/shared/regionPolicy'

defineProps<{
  startPath: StartPath
  acpBackend: WizardBackendId
  configRoot: string
  regionPolicy: RegionPolicy | null | undefined
  currentRegion: string
  /** When true, region buttons expose role=radio + aria (create wizard). */
  regionAsRadios?: boolean
  showRegionHints?: boolean
  showConfigRoot?: boolean
  testIdPrefix?: string
}>()

const emit = defineEmits<{
  selectStartPath: [path: StartPath]
  selectBackend: [id: WizardBackendId]
  selectRegion: [id: string]
}>()

const { t } = useI18n()
</script>

<template>
  <div class="grid gap-3 sm:grid-cols-2">
    <button
      v-for="option in START_PATH_OPTIONS"
      :key="option.id"
      type="button"
      class="rounded-lg border px-4 py-4 text-left transition"
      :class="
        startPath === option.id
          ? 'border-accent bg-accent-dim'
          : 'border-line bg-base hover:border-line-strong'
      "
      :data-testid="`${testIdPrefix || 'wizard'}-path-${option.id}`"
      @click="emit('selectStartPath', option.id)"
    >
      <strong class="block text-[13px] font-semibold text-txt">{{ t(option.titleKey) }}</strong>
      <span class="mt-1.5 block text-[11px] leading-5 text-txt3">{{ t(option.descKey) }}</span>
    </button>
  </div>

  <div
    v-if="startPath === 'apiKey'"
    class="mt-5 border-t border-dashed border-line pt-4"
    :data-testid="`${testIdPrefix || 'wizard'}-path-apikey-detail`"
  >
    <div class="mb-2 text-[12px] font-medium text-txt2">
      {{ t('pages.onboarding.acp.apiKeyVendorsLabel') }}
    </div>
    <div class="flex flex-wrap gap-1.5">
      <span
        v-for="p in OPENCODE_FALLBACK_PROVIDERS"
        :key="p.id"
        class="rounded-md border border-line px-2 py-1 text-[11px] text-txt2"
      >{{ t(p.labelKey) }}</span>
    </div>
    <p class="mt-3 text-[12px] text-txt3">
      {{ t('pages.onboarding.acp.apiKeyVendorsHint') }}
      <code class="ml-1 font-mono text-[11px] text-accent-2">{{ configRoot }}</code>
    </p>
  </div>

  <div
    v-else
    class="mt-5 border-t border-dashed border-line pt-4"
    :data-testid="`${testIdPrefix || 'wizard'}-path-cli-detail`"
  >
    <div class="mb-2 text-[12px] font-medium text-txt2">
      {{ t('pages.onboarding.acp.cliLabel') }}
    </div>
    <div class="grid grid-cols-2 gap-2.5 sm:grid-cols-3 lg:grid-cols-5">
      <button
        v-for="b in CLI_BACKENDS"
        :key="b.id"
        type="button"
        class="rounded-lg border px-3 py-3.5 text-center transition"
        :class="
          acpBackend === b.id
            ? 'border-accent bg-accent-dim'
            : 'border-line bg-base hover:border-line-strong'
        "
        :data-testid="`${testIdPrefix || 'wizard'}-backend-${b.id}`"
        @click="emit('selectBackend', b.id)"
      >
        <strong class="block text-[13px] font-semibold text-txt">{{ b.label }}</strong>
        <span class="mt-1 block font-mono text-[10px] text-txt3">{{ b.configRoot }}</span>
      </button>
    </div>
  </div>

  <div v-if="regionPolicy" class="mt-5 border-t border-dashed border-line pt-4">
    <div class="mb-2 text-[12px] font-medium text-txt2">
      {{ t('pages.agentStudio.region.title') }}
    </div>
    <div
      class="grid max-w-lg grid-cols-2 gap-2.5"
      :role="regionAsRadios ? 'radiogroup' : undefined"
      :aria-label="regionAsRadios ? t('pages.agentStudio.region.title') : undefined"
    >
      <button
        v-for="option in regionPolicy.options"
        :key="option.id"
        type="button"
        :role="regionAsRadios ? 'radio' : undefined"
        :aria-checked="regionAsRadios ? currentRegion === option.id : undefined"
        :aria-label="
          regionAsRadios ? `${t(option.labelKey)} (${option.id})` : undefined
        "
        class="rounded-lg border px-3 py-3 text-left transition"
        :class="
          currentRegion === option.id
            ? 'border-accent bg-accent-dim'
            : 'border-line bg-base hover:border-line-strong'
        "
        @click="emit('selectRegion', option.id)"
      >
        <strong class="block text-[13px] font-semibold text-txt">{{ t(option.labelKey) }}</strong>
        <span
          v-if="regionAsRadios || showRegionHints"
          class="mt-1 block font-mono text-[10px] text-accent-2"
        >{{ option.id }}</span>
        <span v-if="showRegionHints" class="mt-1 block text-[10px] text-txt3">
          {{ t(option.hintKey) }}
        </span>
      </button>
    </div>
  </div>

  <p v-if="showConfigRoot" class="mt-3 font-mono text-[11px] text-txt3">
    configRoot → <span class="text-accent-2">{{ configRoot }}</span>
  </p>
</template>
