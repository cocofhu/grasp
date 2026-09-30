<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>

<template>
  <section
    class="mx-3 mb-2 mt-3 shrink-0 rounded-lg border border-line bg-surface"
    :aria-label="t('pages.embedChat.pageCollaboration')"
    data-page-agent-not-interactive
    data-testid="page-collaboration-controls"
  >
    <h3 class="m-0 px-3 pb-1 pt-2 text-[11px] font-medium text-txt3">{{ t('pages.embedChat.pageCollaboration') }}</h3>
    <div class="divide-y divide-line">
      <slot />
    </div>
  </section>
</template>
