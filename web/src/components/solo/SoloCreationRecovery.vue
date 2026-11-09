<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import type { SoloCreationOutbox } from '@/lib/solo/soloOutbox'
import type { Project } from '@/lib/api/apiTypes'

const props = withDefaults(defineProps<{ request: SoloCreationOutbox | null; busy: boolean; resolved: boolean; checked: boolean; projects?: Project[] }>(), { projects: () => [] })
defineEmits<{ check: []; retry: []; stop: [] }>()
const { t } = useI18n()
function projectName(id: string) { return props.projects.find(project => project.id === id)?.name || id }
</script>

<template>
  <section v-if="request" class="mb-3 rounded-xl border border-accent/30 bg-accent/5 p-4 text-[12px] text-txt2" data-testid="solo-creation-recovery" aria-live="polite">
    <p class="font-semibold text-txt">{{ t('pages.solo.creationRecovery.title') }}</p>
    <p class="mt-1 leading-relaxed">{{ t('pages.solo.creationRecovery.hint') }}</p>
    <p class="mt-2 text-[11px] text-txt3">{{ request.request.agentName }} · {{ request.request.initialModelId || t('pages.solo.creationRecovery.defaultModel') }}<template v-if="request.request.reasoningEffort"> · {{ t('pages.solo.modelSelect.reasoningEffort') }}: {{ request.request.reasoningEffort }}</template></p>
    <p v-if="request.request.repos?.length" class="mt-1 text-[11px] text-txt3">{{ t('pages.solo.creationRecovery.repositories', { n: request.request.repos.length }) }} · {{ request.request.repos.map(repo => projectName(repo.projectId) + (repo.branch ? ` (${repo.branch})` : '')).join(' · ') }}</p>
    <p v-if="request.request.text" class="mt-1 max-h-24 overflow-auto whitespace-pre-wrap break-words">{{ request.request.text }}</p>
    <p v-if="request.request.images?.length || request.request.references?.length" class="mt-1 text-[11px] text-txt3">{{ t('pages.solo.creationRecovery.attachments', { images: request.request.images?.length || 0, references: request.request.references?.length || 0 }) }}</p>
    <div class="mt-3 flex flex-wrap items-center gap-2">
      <AppButton type="button" variant="primary" size="sm" :loading="busy" :disabled="busy || !checked" data-testid="solo-creation-retry" @click="$emit('retry')">{{ t(resolved ? 'pages.solo.creationRecovery.open' : 'pages.solo.creationRecovery.retry') }}</AppButton>
      <AppButton type="button" variant="ghost" size="sm" :disabled="busy" data-testid="solo-creation-check" @click="$emit('check')">{{ t('pages.solo.creationRecovery.check') }}</AppButton>
      <AppButton type="button" variant="ghost" size="sm" :disabled="busy" data-testid="solo-creation-stop" @click="$emit('stop')">{{ t('pages.solo.stopLocalRetry') }}</AppButton>
    </div>
    <p class="mt-2 text-[11px] leading-relaxed text-txt3">{{ t('pages.solo.stopLocalRetryHint') }}</p>
  </section>
</template>
