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
const desc = computed(() => (te(`${keyBase.value}.desc`) ? t(`${keyBase.value}.desc`) : props.summary || ''))

function toolLabel(tool: string) {
  const key = `pages.onboarding.team.toolNames.${tool}`
  return te(key) ? t(key) : tool
}

function schemaLabel(schema: string, fallback: string) {
  const key = `pages.onboarding.team.schemaNames.${schema}`
  return te(key) ? t(key) : fallback
}
</script>

<template>
  <div class="team-card" :class="{ 'is-off': !member.enabled }" :data-testid="`onboarding-team-card-${member.templateId}`">
    <div class="flex items-center gap-2">
      <span class="team-avatar">{{ t(`${keyBase}.title`).slice(0, 1) }}</span>
      <strong class="min-w-0 truncate text-[14px] font-semibold text-txt">{{ t(`${keyBase}.title`) }}</strong>
      <span class="team-badge" :class="{ 'is-required': required }">
        {{ required ? t('pages.onboarding.team.required') : t('pages.onboarding.team.optional') }}
      </span>
      <label class="team-toggle ml-auto" :class="{ 'is-locked': required }">
        <input
          type="checkbox"
          class="team-native"
          :checked="member.enabled"
          :disabled="required"
          :data-testid="`onboarding-team-toggle-${member.templateId}`"
          @change="emit('toggle', ($event.target as HTMLInputElement).checked)"
        />
        <span class="team-box" aria-hidden="true"><Icon name="check" :size="12" /></span>
      </label>
    </div>
    <p class="mt-2 text-[12.5px] leading-[1.6] text-txt2">{{ desc }}</p>

    <div v-if="caps" class="mt-3 space-y-2 text-[11.5px]" :data-testid="`onboarding-team-caps-${member.templateId}`">
      <div class="flex flex-wrap gap-1.5">
        <span class="team-pill is-strong">
          <Icon :name="caps.interaction === 'clarify' ? 'chat' : 'play'" :size="11" />
          {{ caps.interaction === 'clarify' ? t('pages.onboarding.team.interactionClarify') : t('pages.onboarding.team.interactionAuto') }}
        </span>
        <span v-if="caps.review" class="team-pill is-strong"><Icon name="check" :size="11" />{{ t('pages.onboarding.team.review') }}</span>
        <span class="team-pill" :class="{ 'is-ok': caps.preview }" :data-testid="`onboarding-team-preview-${member.templateId}`">
          <Icon name="monitor" :size="11" />{{ caps.preview ? t('pages.onboarding.team.preview') : t('pages.onboarding.team.noPreview') }}
        </span>
      </div>
      <div v-if="caps.tools.length" class="team-line">
        <span class="team-line-label">{{ t('pages.onboarding.team.tools') }}</span>
        <span>{{ caps.tools.map(toolLabel).join(' · ') }}</span>
      </div>
      <div v-if="caps.writes.length" class="team-line">
        <span class="team-line-label">{{ t('pages.onboarding.team.writes') }}</span>
        <span>
          <template v-for="(w, i) in caps.writes" :key="w.name">
            <span v-if="i" class="text-txt3"> · </span>
            <span
              :class="w.required ? 'text-txt' : 'text-txt3'"
              :title="w.required ? t('pages.onboarding.team.writeRequired') : t('pages.onboarding.team.writeOptional')"
              >{{ schemaLabel(w.name, w.label) }}<span v-if="!w.required">?</span></span
            >
          </template>
        </span>
      </div>
    </div>

    <div v-if="member.enabled" class="team-fields">
      <label class="block">
        <span class="team-label">{{ t('pages.onboarding.team.nameLabel') }}</span>
        <input
          :value="member.name"
          type="text"
          autocomplete="off"
          :placeholder="namePlaceholder"
          class="team-input"
          :class="{ 'is-invalid': issue }"
          :data-testid="`onboarding-team-name-${member.templateId}`"
          @input="emit('update:name', ($event.target as HTMLInputElement).value)"
        />
        <p v-if="issue" class="mt-1 text-[11px] text-err" :data-testid="`onboarding-team-name-error-${member.templateId}`">
          {{ t(`pages.onboarding.team.nameIssues.${issue}`) }}
        </p>
      </label>
      <label class="block">
        <span class="team-label">{{ t('pages.onboarding.team.modelLabel') }}</span>
        <input
          :value="member.model"
          type="text"
          autocomplete="off"
          :placeholder="modelPlaceholder"
          :title="modelPlaceholder"
          class="team-input font-mono"
          :data-testid="`onboarding-team-model-${member.templateId}`"
          @input="emit('update:model', ($event.target as HTMLInputElement).value)"
        />
      </label>
    </div>
  </div>
</template>

<style scoped>
.team-card {
  display: flex;
  flex-direction: column;
  min-width: 0;
  padding: 14px 16px 16px;
  border-radius: 14px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-base));
  transition:
    border-color 0.15s,
    opacity 0.15s;
}
.team-card:not(.is-off) {
  border-color: rgb(var(--c-accent) / 0.45);
}
.team-card.is-off {
  opacity: 0.62;
}
.team-toggle {
  position: relative;
  flex-shrink: 0;
  cursor: pointer;
}
.team-native {
  position: absolute;
  inset: 0;
  margin: 0;
  opacity: 0;
  cursor: inherit;
}
.team-toggle.is-locked {
  cursor: not-allowed;
}
.team-box {
  display: grid;
  place-items: center;
  width: 18px;
  height: 18px;
  border-radius: 5px;
  border: 1px solid rgb(var(--c-line-strong));
  background: rgb(var(--c-surface));
  color: transparent;
  transition: background 0.15s;
}
.team-toggle input:checked + .team-box {
  border-color: rgb(var(--c-accent));
  background: rgb(var(--c-accent));
  color: #fff;
}
.team-toggle.is-locked input:checked + .team-box {
  opacity: 0.55;
}
.team-toggle input:focus-visible + .team-box {
  box-shadow: 0 0 0 3px rgb(var(--c-accent) / 0.25);
}
.team-avatar {
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 600;
  color: rgb(var(--c-accent-2));
  background: rgb(var(--c-accent) / 0.12);
}
.team-badge {
  padding: 1px 7px;
  border-radius: 999px;
  font-size: 10.5px;
  color: rgb(var(--c-txt3));
  background: rgb(var(--c-elevated));
}
.team-badge.is-required {
  color: rgb(var(--c-accent-2));
  background: rgb(var(--c-accent) / 0.1);
}
.team-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 22px;
  padding: 0 8px;
  border-radius: 999px;
  color: rgb(var(--c-txt3));
  background: rgb(var(--c-elevated));
}
.team-pill.is-strong {
  color: rgb(var(--c-txt2));
}
.team-pill.is-ok {
  color: rgb(var(--c-ok));
  background: rgb(var(--c-ok) / 0.1);
}
.team-line {
  display: flex;
  gap: 8px;
  color: rgb(var(--c-txt2));
  line-height: 1.6;
}
.team-line-label {
  flex-shrink: 0;
  min-width: 28px;
  color: rgb(var(--c-txt3));
}
.team-fields {
  display: grid;
  gap: 10px;
  margin-top: auto;
  padding-top: 14px;
}
.team-label {
  display: block;
  margin-bottom: 4px;
  font-size: 11.5px;
  font-weight: 500;
  color: rgb(var(--c-txt2));
}
.team-input {
  width: 100%;
  height: 32px;
  border-radius: 8px;
  border: 1px solid rgb(var(--c-line));
  background: rgb(var(--c-surface));
  padding: 0 10px;
  font-size: 12.5px;
  color: rgb(var(--c-txt));
  outline: none;
}
.team-input::placeholder {
  color: rgb(var(--c-txt3));
}
.team-input:focus {
  border-color: rgb(var(--c-accent));
  box-shadow: 0 0 0 3px rgb(var(--c-accent) / 0.16);
}
.team-input.is-invalid {
  border-color: rgb(var(--c-err));
}
</style>
