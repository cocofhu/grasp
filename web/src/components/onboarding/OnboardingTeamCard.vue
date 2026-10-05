<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import type { AgentCapabilities } from '@/lib/api/apiTypes'
import { summarizeCapabilities } from '@/lib/workflow/agentCapabilities'
import { isRequiredTemplate, type OnboardingTeamMember, type TeamNameIssue } from '@/lib/pm/onboardingWizard'

const props = defineProps<{
  member: OnboardingTeamMember
  capabilities?: AgentCapabilities | null
  /** Server summary, shown when the locale has no copy for this template. */
  summary?: string
  namePlaceholder?: string
  modelPlaceholder: string
  issue?: TeamNameIssue
  /** Position in the workflow, 0-based. */
  index: number
}>()

const emit = defineEmits<{
  toggle: [enabled: boolean]
  'update:name': [value: string]
  'update:model': [value: string]
}>()

const { t, te } = useI18n()

const required = computed(() => isRequiredTemplate(props.member.templateId))
const caps = computed(() => summarizeCapabilities(props.capabilities))
const keyBase = computed(() => `pages.onboarding.team.templates.${props.member.templateId}`)
const title = computed(() => t(`${keyBase.value}.title`))
const requiredWrites = computed(() => caps.value?.writes.filter((w) => w.required) ?? [])
const optionalWrites = computed(() => caps.value?.writes.filter((w) => !w.required) ?? [])
const desc = computed(() => (te(`${keyBase.value}.desc`) ? t(`${keyBase.value}.desc`) : props.summary || ''))

function schemaLabel(schema: string, fallback: string) {
  const key = `pages.onboarding.team.schemaNames.${schema}`
  return te(key) ? t(key) : fallback
}
</script>

<template>
  <article class="team-card" :class="{ 'is-off': !member.enabled }" :data-testid="`onboarding-team-card-${member.templateId}`">
    <header class="flex items-center gap-3">
      <span class="team-avatar">{{ title.slice(0, 1) }}</span>
      <div class="min-w-0 flex-1">
        <div class="truncate text-[14.5px] font-semibold leading-5 text-txt">{{ title }}</div>
        <div class="text-[11px] leading-4 text-txt3">{{ t('pages.onboarding.team.stage', { n: index + 1 }) }}</div>
      </div>
      <span v-if="required" class="team-badge">{{ t('pages.onboarding.team.required') }}</span>
      <label v-else class="team-switch" :title="t('pages.onboarding.team.optional')">
        <input
          type="checkbox"
          class="team-native"
          :checked="member.enabled"
          :aria-label="title"
          :data-testid="`onboarding-team-toggle-${member.templateId}`"
          @change="emit('toggle', ($event.target as HTMLInputElement).checked)"
        />
        <span class="team-switch-track" aria-hidden="true" />
      </label>
    </header>

    <p class="mt-3 text-[12.5px] leading-[1.65] text-txt2">{{ desc }}</p>

    <ul v-if="caps" class="team-caps" :data-testid="`onboarding-team-caps-${member.templateId}`">
      <li>
        <Icon :name="caps.interaction === 'clarify' ? 'chat' : 'play'" :size="13" />
        {{ caps.interaction === 'clarify' ? t('pages.onboarding.team.interactionClarify') : t('pages.onboarding.team.interactionAuto') }}
      </li>
      <li v-if="caps.review"><Icon name="user" :size="13" />{{ t('pages.onboarding.team.review') }}</li>
      <li :class="{ 'is-muted': !caps.preview }">
        <Icon name="monitor" :size="13" />
        <span :data-testid="`onboarding-team-preview-${member.templateId}`">{{
          caps.preview ? t('pages.onboarding.team.preview') : t('pages.onboarding.team.noPreview')
        }}</span>
      </li>
      <li v-if="requiredWrites.length">
        <Icon name="artifact" :size="13" />
        <span>
          {{ requiredWrites.map((w) => schemaLabel(w.name, w.label)).join(t('pages.onboarding.team.listSep')) }}
          <span
            v-if="optionalWrites.length"
            class="text-txt3"
            :title="optionalWrites.map((w) => schemaLabel(w.name, w.label)).join(t('pages.onboarding.team.listSep'))"
            >{{ t('pages.onboarding.team.moreWrites', { n: optionalWrites.length }) }}</span
          >
        </span>
      </li>
    </ul>

    <div v-if="member.enabled" class="team-fields">
      <label class="team-field" :class="{ 'is-invalid': issue }">
        <span class="team-field-label">{{ t('pages.onboarding.team.nameLabel') }}</span>
        <input
          :value="member.name"
          type="text"
          autocomplete="off"
          :placeholder="namePlaceholder"
          :data-testid="`onboarding-team-name-${member.templateId}`"
          @input="emit('update:name', ($event.target as HTMLInputElement).value)"
        />
      </label>
      <p v-if="issue" class="-mt-1 text-[11px] text-err" :data-testid="`onboarding-team-name-error-${member.templateId}`">
        {{ t(`pages.onboarding.team.nameIssues.${issue}`) }}
      </p>
      <label class="team-field">
        <span class="team-field-label">{{ t('pages.onboarding.team.modelLabel') }}</span>
        <input
          :value="member.model"
          type="text"
          autocomplete="off"
          class="font-mono"
          :placeholder="modelPlaceholder"
          :title="modelPlaceholder"
          :data-testid="`onboarding-team-model-${member.templateId}`"
          @input="emit('update:model', ($event.target as HTMLInputElement).value)"
        />
      </label>
    </div>
    <p v-else class="team-off">{{ t('pages.onboarding.team.offNote') }}</p>
  </article>
</template>

<style scoped>
.team-card {
  display: flex;
  flex-direction: column;
  min-width: 0;
  padding: 16px;
  border-radius: 14px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-base));
  transition:
    border-color 0.15s,
    opacity 0.15s;
}
.team-card.is-off {
  border-style: dashed;
  background: transparent;
}
.team-card.is-off > :not(header) {
  opacity: 0.5;
}
.team-avatar {
  display: grid;
  place-items: center;
  flex-shrink: 0;
  width: 34px;
  height: 34px;
  border-radius: 10px;
  font-size: 14px;
  font-weight: 600;
  color: rgb(var(--c-accent-2));
  background: rgb(var(--c-accent) / 0.12);
  box-shadow: inset 0 0 0 1px rgb(var(--c-accent) / 0.18);
}
.team-card.is-off .team-avatar {
  color: rgb(var(--c-txt3));
  background: rgb(var(--c-elevated));
  box-shadow: none;
}
.team-badge {
  flex-shrink: 0;
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 11px;
  color: rgb(var(--c-txt3));
  background: rgb(var(--c-elevated));
}
.team-switch {
  position: relative;
  flex-shrink: 0;
  cursor: pointer;
}
.team-native {
  position: absolute;
  inset: 0;
  z-index: 1;
  margin: 0;
  opacity: 0;
  cursor: inherit;
}
.team-switch-track {
  display: block;
  position: relative;
  width: 32px;
  height: 18px;
  border-radius: 999px;
  background: rgb(var(--c-line-strong));
  transition: background 0.15s;
}
.team-switch-track::after {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 14px;
  height: 14px;
  border-radius: 999px;
  background: #fff;
  box-shadow: 0 1px 2px rgb(0 0 0 / 0.25);
  transition: transform 0.15s;
}
.team-native:checked + .team-switch-track {
  background: rgb(var(--c-accent));
}
.team-native:checked + .team-switch-track::after {
  transform: translateX(14px);
}
.team-native:focus-visible + .team-switch-track {
  box-shadow: 0 0 0 3px rgb(var(--c-accent) / 0.25);
}
.team-caps {
  display: grid;
  gap: 7px;
  margin-top: 14px;
  padding-top: 14px;
  border-top: 1px solid rgb(var(--c-line));
  font-size: 12px;
  color: rgb(var(--c-txt2));
}
.team-caps li {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  line-height: 18px;
}
.team-caps li > :first-child {
  flex-shrink: 0;
  margin-top: 2.5px;
  color: rgb(var(--c-txt3));
}
.team-caps li.is-muted {
  color: rgb(var(--c-txt3));
}
.team-fields {
  display: grid;
  gap: 8px;
  margin-top: auto;
  padding-top: 16px;
}
.team-field {
  display: flex;
  min-width: 0;
  align-items: center;
  height: 34px;
  border-radius: 9px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-surface));
  transition:
    border-color 0.15s,
    box-shadow 0.15s;
}
.team-field:focus-within {
  border-color: rgb(var(--c-accent));
  box-shadow: 0 0 0 3px rgb(var(--c-accent) / 0.16);
}
.team-field.is-invalid {
  border-color: rgb(var(--c-err));
}
.team-field-label {
  flex-shrink: 0;
  padding: 0 10px;
  font-size: 11.5px;
  color: rgb(var(--c-txt3));
  border-right: 1px solid rgb(var(--c-line));
}
.team-field input {
  width: 0;
  min-width: 0;
  flex: 1;
  height: 100%;
  padding: 0 10px;
  background: transparent;
  font-size: 12.5px;
  color: rgb(var(--c-txt));
  outline: none;
}
.team-field input::placeholder {
  font-family: theme('fontFamily.sans');
  color: rgb(var(--c-txt3));
}
.team-off {
  margin-top: auto;
  padding-top: 16px;
  font-size: 12px;
  color: rgb(var(--c-txt3));
}
</style>
