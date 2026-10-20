<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'

withDefaults(defineProps<{ titleKey: string; descriptionKey?: string; layout?: 'workspace' | 'settings' }>(), { layout: 'workspace' })
const { t } = useI18n()
const { workspace, loading, error, reload } = useWorkspaceContext()
</script>

<template>
  <section class="flex min-h-0 min-w-0 flex-col" :class="layout === 'settings' ? 'settings-document' : 'h-full'" data-testid="workspace-page">
    <header class="flex shrink-0 items-start justify-between gap-3" :class="layout === 'settings' ? 'mb-8' : 'mb-4'">
      <div class="min-w-0">
        <h1 class="font-semibold tracking-tight text-txt" :class="layout === 'settings' ? 'text-2xl' : 'truncate text-xl'">{{ t(titleKey) }}</h1>
        <p v-if="descriptionKey" class="mt-1 text-sm text-txt3">{{ t(descriptionKey) }}</p>
      </div>
      <slot name="actions" />
    </header>
    <div v-if="error" class="flex flex-col items-center justify-center gap-3 rounded-xl border border-err/30 bg-err/5 px-5 py-12" role="alert">
      <Icon name="alert" :size="24" class="text-err" />
      <p class="text-sm text-txt2">{{ error }}</p>
      <AppButton icon="refresh" @click="reload">{{ t('nav.workspaceRetry') }}</AppButton>
    </div>
    <div v-else-if="!workspace" class="flex min-h-0 flex-1 flex-col gap-4" :aria-busy="loading" role="status">
      <span class="sr-only">{{ t('nav.workspaceLoading') }}</span>
      <div class="h-12 w-full animate-pulse rounded-xl bg-elevated" />
      <div class="min-h-40 flex-1 animate-pulse rounded-xl border border-line bg-surface" />
    </div>
    <slot v-else :workspace="workspace" />
  </section>
</template>

<style scoped>
.settings-document {
  width: 100%;
  max-width: 860px;
  flex: none;
  margin: 0 auto;
  padding: 44px 0 80px;
}
@media (max-width: 767px) {
  .settings-document { padding-top: 28px; padding-bottom: 72px; }
}
</style>
