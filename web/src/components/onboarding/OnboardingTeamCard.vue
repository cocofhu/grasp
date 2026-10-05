<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
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
  <div
    class="rounded-lg flex flex-col border px-4 py-3.5 transition"
    :class="member.enabled ? 'border-accent/55 bg-accent-dim/40' : 'border-line bg-base opacity-70'"
    :data-testid="`onboarding-team-card-${member.templateId}`"
  >
    <div class="flex items-start gap-2.5">
      <input
        type="checkbox"
        class="mt-0.5 h-4 w-4 shrink-0 accent-accent"
        :checked="member.enabled"
        :disabled="required"
        :data-testid="`onboarding-team-toggle-${member.templateId}`"
        @change="emit('toggle', ($event.target as HTMLInputElement).checked)"
      />
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2">
          <strong class="text-[14px] text-txt">{{ t(`${keyBase}.title`) }}</strong>
          <span class="rounded-md border border-line px-1.5 py-px text-[10px] text-txt3">
            {{ required ? t('pages.onboarding.team.required') : t('pages.onboarding.team.optional') }}
          </span>
        </div>
        <p class="mt-1 text-[12px] leading-5 text-txt2">{{ desc }}</p>
      </div>
    </div>

    <div v-if="caps" class="mt-3 space-y-2 text-[11px]" :data-testid="`onboarding-team-caps-${member.templateId}`">
      <div class="flex flex-wrap items-center gap-1.5">
        <span class="rounded-md border border-line px-1.5 py-0.5 text-txt2">
          {{
            caps.interaction === 'clarify'
              ? t('pages.onboarding.team.interactionClarify')
              : t('pages.onboarding.team.interactionAuto')
          }}
        </span>
        <span v-if="caps.review" class="rounded-md border border-line px-1.5 py-0.5 text-txt2">
          {{ t('pages.onboarding.team.review') }}
        </span>
        <span
          class="rounded-md border px-1.5 py-0.5"
          :class="caps.preview ? 'border-ok/40 bg-ok/10 text-ok' : 'border-line text-txt3'"
          :data-testid="`onboarding-team-preview-${member.templateId}`"
        >
          {{ caps.preview ? t('pages.onboarding.team.preview') : t('pages.onboarding.team.noPreview') }}
        </span>
      </div>
      <div v-if="caps.tools.length" class="flex flex-wrap items-center gap-1.5">
        <span class="w-9 shrink-0 text-txt3">{{ t('pages.onboarding.team.tools') }}</span>
        <span v-for="tool in caps.tools" :key="tool" class="rounded-md bg-elevated px-1.5 py-0.5 text-txt2">
          {{ toolLabel(tool) }}
        </span>
      </div>
      <div v-if="caps.writes.length" class="flex flex-wrap items-center gap-1.5">
        <span class="w-9 shrink-0 text-txt3">{{ t('pages.onboarding.team.writes') }}</span>
        <span
          v-for="w in caps.writes"
          :key="w.name"
          class="rounded-md bg-elevated px-1.5 py-0.5"
          :class="w.required ? 'text-txt' : 'text-txt3'"
          :title="w.required ? t('pages.onboarding.team.writeRequired') : t('pages.onboarding.team.writeOptional')"
        >
          {{ schemaLabel(w.name, w.label) }}<span v-if="!w.required">?</span>
        </span>
      </div>
    </div>

    <div v-if="member.enabled" class="mt-3 grid gap-2 sm:grid-cols-2">
      <label class="block">
        <span class="mb-1 block text-[11px] font-medium text-txt2">{{ t('pages.onboarding.team.nameLabel') }}</span>
        <input
          :value="member.name"
          type="text"
          autocomplete="off"
          :placeholder="namePlaceholder"
          class="rounded-md w-full border bg-surface px-2.5 py-1.5 text-[12px] text-txt outline-none focus:border-accent"
          :class="issue ? 'border-err' : 'border-line'"
          :data-testid="`onboarding-team-name-${member.templateId}`"
          @input="emit('update:name', ($event.target as HTMLInputElement).value)"
        />
        <p v-if="issue" class="mt-1 text-[11px] text-err" :data-testid="`onboarding-team-name-error-${member.templateId}`">
          {{ t(`pages.onboarding.team.nameIssues.${issue}`) }}
        </p>
      </label>
      <label class="block">
        <span class="mb-1 block text-[11px] font-medium text-txt2">{{ t('pages.onboarding.team.modelLabel') }}</span>
        <input
          :value="member.model"
          type="text"
          autocomplete="off"
          :placeholder="modelPlaceholder"
          class="rounded-md w-full border border-line bg-surface px-2.5 py-1.5 font-mono text-[12px] text-txt outline-none focus:border-accent"
          :data-testid="`onboarding-team-model-${member.templateId}`"
          @input="emit('update:model', ($event.target as HTMLInputElement).value)"
        />
      </label>
    </div>
  </div>
</template>
