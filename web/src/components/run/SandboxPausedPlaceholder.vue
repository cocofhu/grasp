<script setup lang="ts">
/** Stage placeholder for a Solo sandbox the provider paused; resume shares the composer's action. */
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'

defineProps<{ waking?: boolean }>()
const emit = defineEmits<{ resume: [] }>()
const { t } = useI18n()
</script>

<template>
  <div class="flex h-full items-center justify-center p-6" data-testid="react-artifact-sandbox-paused">
    <div class="max-w-[380px] text-center">
      <div class="paused-badge">
        <Icon name="user-hold" :size="20" />
      </div>
      <h3 class="mb-1.5 text-[14px] font-semibold text-txt">
        {{ waking ? t('pages.solo.pausedStage.waking') : t('pages.solo.pausedStage.title') }}
      </h3>
      <p class="mb-4 text-[12.5px] leading-relaxed text-txt2">{{ t('pages.solo.pausedStage.hint') }}</p>
      <AppButton
        size="sm"
        variant="primary"
        :icon="waking ? undefined : 'play'"
        :loading="waking"
        :disabled="waking"
        data-testid="react-artifact-sandbox-resume"
        @click="emit('resume')"
      >{{ waking ? t('pages.solo.wake.waking') : t('pages.solo.wake.action') }}</AppButton>
    </div>
  </div>
</template>

<style scoped>
.paused-badge {
  display: grid;
  width: 44px;
  height: 44px;
  margin: 0 auto 14px;
  place-items: center;
  border: 1px solid rgb(var(--c-warn) / 0.25);
  border-radius: 12px;
  background: rgb(var(--c-warn) / 0.1);
  color: rgb(var(--c-warn));
}
</style>
