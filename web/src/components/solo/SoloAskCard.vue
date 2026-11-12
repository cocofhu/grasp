<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import type { ReactForm, ReactQuestion } from '@/lib/shared/types'

const props = defineProps<{ questions?: ReactQuestion[]; forms?: ReactForm[]; disabled?: boolean }>()
const emit = defineEmits<{ answer: [text: string] }>()
const { t } = useI18n()

const questions = computed(() => props.questions ?? [])
const forms = computed(() => props.forms ?? [])
const picked = ref<Record<string, string[]>>({})
const other = ref<Record<string, string>>({})
const values = ref<Record<string, string>>({})

watch(questions, (qs) => {
  picked.value = Object.fromEntries(qs.map((q) => [q.id, q.options.filter((o) => o.recommended).slice(0, q.allowMultiple ? undefined : 1).map((o) => o.id)]))
  other.value = {}
}, { immediate: true })
watch(forms, () => { values.value = {} }, { immediate: true })

function toggle(q: ReactQuestion, id: string) {
  const cur = picked.value[q.id] ?? []
  picked.value[q.id] = q.allowMultiple ? (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]) : (cur.includes(id) ? [] : [id])
  if (!q.allowMultiple) other.value[q.id] = ''
}
function setOther(q: ReactQuestion, text: string) {
  other.value[q.id] = text
  if (text.trim() && !q.allowMultiple) picked.value[q.id] = []
}
function answers(q: ReactQuestion): string[] {
  const labels = q.options.filter((o) => picked.value[q.id]?.includes(o.id)).map((o) => o.label)
  const extra = other.value[q.id]?.trim()
  return extra ? [...labels, extra] : labels
}
const canAnswer = computed(() => questions.value.some((q) => answers(q).length > 0))
const fieldKey = (fi: number, name: string) => `${fi}:${name}`
const canFill = computed(() => forms.value.some((f) => f.fields.length) &&
  forms.value.every((f, fi) => f.fields.every((field) => values.value[fieldKey(fi, field.name)]?.trim())))

function submitChoices() {
  if (!canAnswer.value || props.disabled) return
  const lines = questions.value.filter((q) => answers(q).length).map((q) => `- ${q.prompt} → ${answers(q).join('、')}`)
  emit('answer', `${t('pages.clarify.choicePrefix')}\n${lines.join('\n')}`)
}
function submitForms() {
  if (!canFill.value || props.disabled) return
  const lines = forms.value.flatMap((f, fi) => f.fields.map((field) => `- ${(field.label || field.name).trim()} → ${values.value[fieldKey(fi, field.name)]!.trim()}`))
  emit('answer', `${t('pages.clarify.formPrefix')}\n${lines.join('\n')}`)
}
</script>

<template>
  <div class="space-y-3" data-testid="solo-ask-card">
    <div v-if="questions.length" class="solo-ask">
      <div v-for="q in questions" :key="q.id" class="space-y-2" data-testid="solo-ask-question">
        <div class="flex items-baseline gap-2">
          <span class="min-w-0 flex-1 break-words text-[13px] font-medium text-txt">{{ q.prompt }}</span>
          <span class="shrink-0 text-[11px] text-txt3">{{ q.allowMultiple ? t('pages.clarify.multiple') : t('pages.clarify.single') }}</span>
        </div>
        <div class="flex flex-wrap gap-2">
          <button
            v-for="o in q.options"
            :key="o.id"
            type="button"
            class="solo-ask-option"
            :class="{ 'is-picked': picked[q.id]?.includes(o.id) }"
            :aria-pressed="picked[q.id]?.includes(o.id) || false"
            :disabled="disabled"
            :data-testid="`solo-ask-option-${q.id}-${o.id}`"
            @click="toggle(q, o.id)"
          >
            {{ o.label }}<span v-if="o.recommended" class="solo-ask-badge">{{ t('pages.clarify.recommended') }}</span>
          </button>
        </div>
        <input
          :value="other[q.id] || ''"
          class="input w-full text-[12px]"
          :placeholder="t('pages.clarify.otherPlaceholder')"
          :disabled="disabled"
          :data-testid="`solo-ask-other-${q.id}`"
          @input="setOther(q, ($event.target as HTMLInputElement).value)"
        />
      </div>
      <div class="flex justify-end">
        <AppButton size="sm" variant="primary" :disabled="disabled || !canAnswer" data-testid="solo-ask-submit" @click="submitChoices">{{ t('pages.clarify.submitChoices') }}</AppButton>
      </div>
    </div>
    <div v-if="forms.length" class="solo-ask">
      <div v-for="(f, fi) in forms" :key="fi" class="space-y-2">
        <div v-if="f.title" class="text-[13px] font-medium text-txt">{{ f.title }}</div>
        <label v-for="field in f.fields" :key="field.name" class="block space-y-1 text-[12px] text-txt2">
          <span>{{ field.label || field.name }}</span>
          <input
            v-model="values[fieldKey(fi, field.name)]"
            :type="field.type === 'url' ? 'url' : 'text'"
            class="input w-full text-[12px]"
            :placeholder="field.placeholder"
            :disabled="disabled"
            :data-testid="`solo-ask-field-${field.name}`"
          />
        </label>
      </div>
      <div class="flex justify-end">
        <AppButton size="sm" variant="primary" :disabled="disabled || !canFill" data-testid="solo-ask-form-submit" @click="submitForms">{{ t('pages.clarify.submitForm') }}</AppButton>
      </div>
    </div>
  </div>
</template>

<style scoped>
.solo-ask {
  @apply space-y-4 rounded-xl border border-line p-3;
}
.solo-ask-option {
  @apply inline-flex items-center gap-1.5 rounded-lg border border-line px-2.5 py-1 text-left text-[12.5px] text-txt2;
  transition: border-color var(--dur-press) var(--ease-out-expo), background-color var(--dur-press) var(--ease-out-expo);
}
.solo-ask-option:hover:not(:disabled) {
  @apply border-accent/40 text-txt;
}
.solo-ask-option.is-picked {
  @apply border-accent/40 bg-accent/10 text-accent;
}
.solo-ask-option:disabled {
  @apply cursor-not-allowed opacity-60;
}
.solo-ask-badge {
  @apply rounded bg-accent/10 px-1 text-[10.5px] text-accent;
}
@media (prefers-reduced-motion: reduce) {
  .solo-ask-option {
    transition: none;
  }
}
</style>
