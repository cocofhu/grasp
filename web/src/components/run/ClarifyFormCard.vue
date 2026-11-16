<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import { fieldWhy, formInputType, type FormDraft } from '@/lib/inbox/clarifyAnswers'
import type { ReactForm } from '@/lib/shared/types'

/** ask_form round: interactive with a `draft`, otherwise the read-only history view. */
defineProps<{ forms: ReactForm[]; draft?: FormDraft; busy?: boolean }>()
const emit = defineEmits<{ submit: [] }>()
const { t } = useI18n()
</script>

<template>
  <div v-if="draft" class="clarify-request relative min-w-0" data-testid="clarify-form-card">
    <div v-for="(form, fi) in forms" :key="fi" class="mb-3 last:mb-0">
      <div v-if="form.title" class="mb-2 flex min-w-0 items-center gap-2 text-[13px] font-medium text-txt">
        <Icon name="form" :size="16" class="shrink-0 text-txt2" />
        <span class="min-w-0 break-words [overflow-wrap:anywhere]">{{ form.title }}</span>
      </div>
      <div class="space-y-2.5">
        <label v-for="f in form.fields" :key="f.name" class="block min-w-0" data-testid="clarify-form-field">
          <span
            class="mb-1 block break-words text-[12px] text-txt2 [overflow-wrap:anywhere]"
            :title="fieldWhy(f) || undefined"
          >{{ f.label }}</span>
          <input
            v-model="draft.values.value[draft.fieldKey(fi, f.name)]"
            :type="formInputType(f)"
            class="input h-[34px] min-w-0 w-full rounded-lg border border-line bg-surface px-2.5 text-[13px] text-txt placeholder:text-txt3 focus:border-accent/50 focus:ring-0"
            :placeholder="f.placeholder || ''"
            autocomplete="off"
            data-testid="clarify-form-input"
          />
        </label>
      </div>
    </div>
    <div class="mt-3 flex justify-end">
      <button
        type="button"
        class="chat-action-confirm"
        :disabled="!draft.filled.value || busy"
        data-testid="clarify-form-submit"
        @click="emit('submit')"
      >
        <Icon name="chat-send" :size="16" /> {{ t('pages.clarify.submitForm') }}
      </button>
    </div>
  </div>
  <div v-else class="clarify-request clarify-request--history min-w-0" data-testid="clarify-form-readonly">
    <div class="mb-2 flex items-center gap-2 text-[12px] font-medium text-txt2">
      <Icon name="form" :size="16" />
      {{ t('pages.clarify.formsThisRound', { n: forms.length }) }}
      <span class="ml-auto text-[11px] font-normal text-txt2">{{ t('pages.clarify.readonly') }}</span>
    </div>
    <div v-for="(form, fi) in forms" :key="fi" class="mb-2 last:mb-0">
      <div v-if="form.title" class="mb-1 break-words text-[13px] text-txt2 [overflow-wrap:anywhere]">{{ form.title }}</div>
      <div
        v-for="f in form.fields"
        :key="f.name"
        class="mb-1 break-words text-[12px] text-txt2 [overflow-wrap:anywhere]"
        :title="fieldWhy(f) || undefined"
      >{{ f.label }}</div>
    </div>
  </div>
</template>

<style scoped>
@media (max-width: 767px) {
  [data-testid="clarify-form-input"] { min-height: 44px; }
}
</style>
