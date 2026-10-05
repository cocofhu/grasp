<script setup lang="ts">
/**
 * Agent Studio「能力」tab: edits draft.capabilities (interaction, review,
 * tools, reads, writes, maxRounds). Validation mirrors the server; saving goes
 * through the regular Agent save.
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import type { AgentCapabilities, AgentInteraction } from '@/lib/api/apiTypes'
import type { AgentStudioDraft } from '@/lib/agent/agentStudioDraft'
import {
  GRANTABLE_TOOLS,
  PRODUCT_SCHEMAS,
  READ_ALL,
  capabilityIssueKey,
  isGated,
  validateCapabilities,
} from '@/lib/workflow/agentCapabilities'

const props = defineProps<{ draft: AgentStudioDraft }>()

const { t } = useI18n()

const caps = computed(() => props.draft.capabilities)

const issue = computed(() => (caps.value ? validateCapabilities(caps.value) : null))
const issueText = computed(() =>
  issue.value ? t(capabilityIssueKey(issue.value), { value: issue.value.value ?? '' }) : '',
)
const gated = computed(() => isGated(caps.value))
const isClarify = computed(() => caps.value?.interaction === 'clarify')

function declare() {
  props.draft.capabilities = { interaction: 'auto', reads: [READ_ALL] }
}

function edit(fn: (c: AgentCapabilities) => void) {
  if (props.draft.capabilities) fn(props.draft.capabilities)
}

function setInteraction(v: AgentInteraction) {
  edit((c) => {
    c.interaction = v
    if (v === 'clarify') {
      c.review = false
      if (!c.tools?.includes('ask_question')) c.tools = [...(c.tools || []), 'ask_question']
    }
  })
}

function setReview(on: boolean) {
  edit((c) => {
    c.review = on && c.interaction !== 'clarify'
  })
}

function hasTool(tool: string) {
  return !!caps.value?.tools?.includes(tool)
}

function toggleTool(tool: string, on: boolean) {
  edit((c) => {
    const rest = (c.tools || []).filter((x) => x !== tool)
    c.tools = on ? [...rest, tool] : rest
  })
}

const readsAll = computed(() => !!caps.value?.reads?.some((r) => r.trim() === READ_ALL))
const schemaArtifacts = new Set(PRODUCT_SCHEMAS.map((s) => s.artifactName))

function setReadsAll(on: boolean) {
  edit((c) => {
    c.reads = on ? [READ_ALL] : []
  })
}

function readsArtifact(name: string) {
  return !!caps.value?.reads?.includes(name)
}

function toggleRead(name: string, on: boolean) {
  edit((c) => {
    const rest = (c.reads || []).filter((x) => x !== name && x !== READ_ALL)
    c.reads = on ? [...rest, name] : rest
  })
}

const otherReads = ref('')
watch(
  () => caps.value?.reads,
  (reads) => {
    const extra = (reads || []).filter((r) => r !== READ_ALL && !schemaArtifacts.has(r))
    if (extra.join(', ') !== parseList(otherReads.value).join(', ')) otherReads.value = extra.join(', ')
  },
  { immediate: true, deep: true },
)

function parseList(s: string): string[] {
  return s
    .split(/[,，\n]/)
    .map((x) => x.trim())
    .filter(Boolean)
}

function onOtherReads(value: string) {
  otherReads.value = value
  edit((c) => {
    const known = (c.reads || []).filter((r) => schemaArtifacts.has(r))
    c.reads = [...known, ...parseList(value).filter((r) => r !== READ_ALL)]
  })
}

function writeOf(schema: string) {
  return caps.value?.writes?.find((w) => w.schema === schema)
}

function toggleWrite(schema: string, on: boolean) {
  edit((c) => {
    const rest = (c.writes || []).filter((w) => w.schema !== schema)
    c.writes = on ? [...rest, { schema }] : rest
  })
}

function setRequired(schema: string, required: boolean) {
  edit((c) => {
    const w = c.writes?.find((x) => x.schema === schema)
    if (w) w.required = required
  })
}

function onMaxRounds(raw: string) {
  edit((c) => {
    const n = Number(raw)
    c.maxRounds = raw.trim() === '' || !Number.isFinite(n) ? undefined : Math.trunc(n)
  })
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col" data-testid="agent-capabilities-panel">
    <div class="toolbar-inline-row flex items-center justify-between gap-2 border-b border-line px-4">
      <span class="text-[12px] text-txt3">{{ t('pages.agentStudio.capabilities.subtitle') }}</span>
      <span v-if="gated" class="chip border-ok/40 text-ok" data-testid="caps-gated">{{ t('nodes.capabilities.gated') }}</span>
    </div>

    <div v-if="!caps" class="scroll-area min-h-0 flex-1 overflow-y-auto p-4">
      <div class="max-w-lg rounded border border-dashed border-line bg-base p-6 text-center">
        <p class="text-[13px] font-medium text-txt">{{ t('pages.agentStudio.capabilities.undeclaredTitle') }}</p>
        <p class="mt-1.5 text-[12px] leading-6 text-txt3">{{ t('pages.agentStudio.capabilities.undeclaredDesc') }}</p>
        <AppButton class="mt-3" size="sm" variant="primary" icon="plus" data-testid="caps-declare" @click="declare">
          {{ t('pages.agentStudio.capabilities.declare') }}
        </AppButton>
      </div>
    </div>

    <div v-else class="scroll-area min-h-0 flex-1 space-y-4 overflow-y-auto p-4">
      <div
        v-if="issue"
        class="max-w-3xl rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err"
        role="alert"
        data-testid="caps-error"
      >
        {{ issueText }}
      </div>

      <section class="max-w-3xl rounded-lg border border-line bg-base/50 p-4">
        <h3 class="text-[13px] font-semibold text-txt">{{ t('pages.agentStudio.capabilities.interactionTitle') }}</h3>
        <div class="mt-3 grid gap-2 sm:grid-cols-2" role="radiogroup" :aria-label="t('pages.agentStudio.capabilities.interactionTitle')">
          <button
            v-for="mode in (['auto', 'clarify'] as const)"
            :key="mode"
            type="button"
            role="radio"
            :aria-checked="caps.interaction === mode"
            class="rounded-md border px-3 py-2 text-left transition-colors"
            :class="caps.interaction === mode ? 'border-accent bg-accent-dim/40' : 'border-line hover:border-line-strong'"
            :data-testid="`caps-interaction-${mode}`"
            @click="setInteraction(mode)"
          >
            <span class="block text-[12px] font-medium text-txt">{{ t(`nodes.capabilities.interaction.${mode}`) }}</span>
            <span class="mt-0.5 block text-[11px] leading-5 text-txt3">{{ t(`pages.agentStudio.capabilities.interactionDesc.${mode}`) }}</span>
          </button>
        </div>
        <div class="mt-4 flex items-start justify-between gap-4">
          <div>
            <p class="text-[12px] font-medium text-txt2">{{ t('nodes.capabilities.review') }}</p>
            <p class="mt-0.5 text-[11px] leading-5 text-txt3">
              {{ isClarify ? t('pages.agentStudio.capabilities.reviewDisabledClarify') : t('pages.agentStudio.capabilities.reviewDesc') }}
            </p>
          </div>
          <AppSwitch
            :model-value="!!caps.review && !isClarify"
            :disabled="isClarify"
            :aria-label="t('nodes.capabilities.review')"
            data-testid="caps-review"
            @update:model-value="setReview"
          />
        </div>
      </section>

      <section class="max-w-3xl rounded-lg border border-line bg-base/50 p-4">
        <h3 class="text-[13px] font-semibold text-txt">{{ t('pages.agentStudio.capabilities.toolsTitle') }}</h3>
        <p class="mt-0.5 text-[11px] leading-5 text-txt3">{{ t('pages.agentStudio.capabilities.toolsDesc') }}</p>
        <ul class="mt-3 space-y-2">
          <li v-for="tool in GRANTABLE_TOOLS" :key="tool">
            <label class="flex cursor-pointer items-start gap-2">
              <input
                type="checkbox"
                class="mt-1 accent-accent"
                :checked="hasTool(tool)"
                :data-testid="`caps-tool-${tool}`"
                @change="toggleTool(tool, ($event.target as HTMLInputElement).checked)"
              />
              <span>
                <span class="block text-[12px] text-txt">
                  {{ t(`nodes.capabilities.tools.${tool}.label`) }}
                  <span class="ml-1 font-mono text-[11px] text-txt3">{{ tool }}</span>
                </span>
                <span class="block text-[11px] leading-5 text-txt3">{{ t(`nodes.capabilities.tools.${tool}.desc`) }}</span>
              </span>
            </label>
          </li>
        </ul>
      </section>

      <section class="max-w-3xl rounded-lg border border-line bg-base/50 p-4">
        <h3 class="text-[13px] font-semibold text-txt">{{ t('pages.agentStudio.capabilities.readsTitle') }}</h3>
        <p class="mt-0.5 text-[11px] leading-5 text-txt3">{{ t('pages.agentStudio.capabilities.readsDesc') }}</p>
        <label class="mt-3 flex cursor-pointer items-center gap-2 text-[12px] text-txt">
          <input
            type="checkbox"
            class="accent-accent"
            :checked="readsAll"
            data-testid="caps-reads-all"
            @change="setReadsAll(($event.target as HTMLInputElement).checked)"
          />
          {{ t('nodes.capabilities.readsAll') }} <span class="font-mono text-[11px] text-txt3">*</span>
        </label>
        <template v-if="!readsAll">
          <div class="mt-2 grid gap-1.5 sm:grid-cols-2">
            <label v-for="s in PRODUCT_SCHEMAS" :key="s.name" class="flex cursor-pointer items-center gap-2 text-[12px] text-txt2">
              <input
                type="checkbox"
                class="accent-accent"
                :checked="readsArtifact(s.artifactName)"
                :data-testid="`caps-read-${s.name}`"
                @change="toggleRead(s.artifactName, ($event.target as HTMLInputElement).checked)"
              />
              {{ t(`nodes.schemas.${s.name}.label`) }}
              <span class="font-mono text-[11px] text-txt3">{{ s.artifactName }}</span>
            </label>
          </div>
          <label class="mt-3 block">
            <span class="label">{{ t('pages.agentStudio.capabilities.readsOther') }}</span>
            <input
              :value="otherReads"
              class="w-full rounded border border-line bg-surface px-2 py-1.5 font-mono text-[12px] text-txt outline-none focus:border-accent"
              :placeholder="t('pages.agentStudio.capabilities.readsOtherPlaceholder')"
              data-testid="caps-reads-other"
              @input="onOtherReads(($event.target as HTMLInputElement).value)"
            />
          </label>
        </template>
      </section>

      <section class="max-w-3xl rounded-lg border border-line bg-base/50 p-4">
        <h3 class="text-[13px] font-semibold text-txt">{{ t('pages.agentStudio.capabilities.writesTitle') }}</h3>
        <p class="mt-0.5 text-[11px] leading-5 text-txt3">{{ t('pages.agentStudio.capabilities.writesDesc') }}</p>
        <ul class="mt-3 divide-y divide-line">
          <li v-for="s in PRODUCT_SCHEMAS" :key="s.name" class="flex items-center gap-3 py-2">
            <label class="flex min-w-0 flex-1 cursor-pointer items-center gap-2">
              <input
                type="checkbox"
                class="accent-accent"
                :checked="!!writeOf(s.name)"
                :data-testid="`caps-write-${s.name}`"
                @change="toggleWrite(s.name, ($event.target as HTMLInputElement).checked)"
              />
              <span class="text-[12px] text-txt">{{ t(`nodes.schemas.${s.name}.label`) }}</span>
              <span class="truncate font-mono text-[11px] text-txt3">{{ s.artifactName }}</span>
              <span
                v-if="s.verdict"
                class="chip border-ok/40 text-ok"
                :title="t('pages.agentStudio.capabilities.verdictHint')"
              >{{ t('pages.agentStudio.capabilities.verdictBadge') }}</span>
            </label>
            <label
              v-if="writeOf(s.name)"
              class="flex shrink-0 cursor-pointer items-center gap-1.5 text-[11px] text-txt2"
            >
              <input
                type="checkbox"
                class="accent-accent"
                :checked="!!writeOf(s.name)?.required"
                :data-testid="`caps-write-required-${s.name}`"
                @change="setRequired(s.name, ($event.target as HTMLInputElement).checked)"
              />
              {{ t('nodes.capabilities.required') }}
            </label>
          </li>
        </ul>
      </section>

      <section class="max-w-3xl rounded-lg border border-line bg-base/50 p-4">
        <label class="block">
          <span class="text-[13px] font-semibold text-txt">{{ t('pages.agentStudio.capabilities.maxRoundsTitle') }}</span>
          <span class="mt-0.5 block text-[11px] leading-5 text-txt3">{{ t('pages.agentStudio.capabilities.maxRoundsDesc') }}</span>
          <input
            type="number"
            min="0"
            step="1"
            :value="caps.maxRounds ?? ''"
            class="mt-2 w-32 rounded border border-line bg-surface px-2 py-1.5 font-mono text-[12px] text-txt outline-none focus:border-accent"
            placeholder="3"
            data-testid="caps-max-rounds"
            @input="onMaxRounds(($event.target as HTMLInputElement).value)"
          />
        </label>
      </section>
    </div>
  </div>
</template>
