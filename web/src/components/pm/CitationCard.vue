<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import { useConversationArtifacts } from '@/lib/chat/conversationArtifacts'
import type { ProgressCitation } from '@/lib/shared/types'
import {
  isBareExtractSnippet,
  isValidPmCitationShape,
  shortRunId,
} from '@/lib/pm/pmCitationShape'

const props = defineProps<{ citation: ProgressCitation }>()
const artifacts = useConversationArtifacts()
const emit = defineEmits<{ 'open-artifact': [target: { id?: string; name?: string }] }>()
const { t } = useI18n()
const router = useRouter()
const expanded = ref(false)
const citationIcons: Record<string, string> = { run: 'runs', workflow: 'workflow', artifact: 'artifact', gate: 'gate', plan: 'doc' }
const typeIcon = computed(() => citationIcons[props.citation.type] || 'doc')

const shapeValid = computed(() =>
  isValidPmCitationShape(props.citation.type, props.citation.targetId),
)

const mainLabel = computed(() => {
  const c = props.citation
  if (!shapeValid.value) {
    return c.summarySnippet || `${c.type}:${c.targetId}`
  }
  switch (c.type) {
    case 'run':
      return `Run #${shortRunId(c.targetId)}`
    case 'workflow':
      return c.summarySnippet && !isBareExtractSnippet(c.type, c.summarySnippet, c.targetId)
        ? c.summarySnippet
        : `Workflow #${c.targetId.replace(/^wf-/i, '')}`
    case 'artifact':
      return c.summarySnippet && !isBareExtractSnippet(c.type, c.summarySnippet, c.targetId)
        ? c.summarySnippet
        : c.targetId
    case 'gate':
      return c.summarySnippet && !isBareExtractSnippet(c.type, c.summarySnippet, c.targetId)
        ? c.summarySnippet
        : `Gate · ${c.targetId}`
    case 'plan':
      return c.summarySnippet && !isBareExtractSnippet(c.type, c.summarySnippet, c.targetId)
        ? c.summarySnippet
        : `Plan ${c.targetId}`
    default:
      return c.summarySnippet || `${c.type}: ${c.targetId}`
  }
})

const subLabel = computed(() => {
  const c = props.citation
  if (!shapeValid.value) return ''
  if (c.type === 'run') {
    const sn = c.summarySnippet?.trim()
    if (!sn || isBareExtractSnippet(c.type, sn, c.targetId)) return ''
    return sn
  }
  return ''
})

function hrefFor(): string | null {
  if (!shapeValid.value) return null
  const c = props.citation
  switch (c.type) {
    case 'run':
      return `/runs/${c.targetId}`
    case 'gate': {
      const runId = c.targetId.includes(':') ? c.targetId.split(':')[0] : c.targetId
      return `/runs/${runId}`
    }
    case 'artifact':
      return `/artifacts?q=${encodeURIComponent(c.targetId)}`
    case 'workflow':
      return `/workflows/${c.targetId}`
    case 'plan': {
      if (c.targetId.includes(':')) {
        const [runId] = c.targetId.split(':')
        return `/runs/${runId}`
      }
      // Bare plan id has no stable run route; treat as non-navigable.
      return null
    }
    default:
      return null
  }
}

const canJump = computed(() => shapeValid.value && !!hrefFor())

function open() {
  if (!canJump.value) return
  if (props.citation.type === 'artifact') {
    const target = /^art-[0-9a-f]{8}$/i.test(props.citation.targetId)
      ? { id: props.citation.targetId }
      : { name: props.citation.targetId }
    if (artifacts) void artifacts.open(target)
    else emit('open-artifact', target)
    return
  }
  const href = hrefFor()
  if (!href) {
    expanded.value = true
    return
  }
  router.push(href)
}
</script>

<template>
  <div
    class="citation-source"
    :class="
      shapeValid
        ? 'text-txt2'
        : 'text-txt3 opacity-70'
    "
    :data-testid="shapeValid ? 'citation-card-valid' : 'citation-card-invalid'"
  >
    <div class="flex flex-wrap items-center justify-between gap-2 py-1.5">
      <button
        type="button"
        class="citation-label flex min-w-0 flex-1 items-center gap-2 text-left"
        :class="shapeValid ? 'text-txt2' : 'text-txt3'"
        :aria-expanded="expanded"
        @click="expanded = !expanded"
      >
        <Icon :key="citation.type" :name="typeIcon" :size="16" class="shrink-0 text-txt2" aria-hidden="true" />
        <span class="min-w-0">
          <span class="citation-title block truncate">{{ mainLabel }}</span>
          <span v-if="subLabel" class="chat-meta mt-0.5 block truncate">{{ subLabel }}</span>
        </span>
      </button>
      <button
        type="button"
        class="citation-action shrink-0"
        :class="canJump ? 'text-accent-2 hover:underline' : 'cursor-not-allowed text-txt3'"
        :disabled="!canJump"
        :aria-disabled="!canJump"
        data-testid="citation-open-source"
        @click="open"
      >
        {{ t(citation.type === 'artifact' ? 'pages.conversationWorkspace.openArtifacts' : 'pages.projectDetail.pm.openSource') }}
        <Icon name="arrow-right" :size="12" aria-hidden="true" />
      </button>
      <p
        v-if="!shapeValid"
        class="basis-full text-[11px] text-err"
        data-testid="citation-invalid-note"
      >
        {{ t('pages.projectDetail.pm.citationInvalid') }}
      </p>
    </div>
    <div v-if="expanded" class="citation-detail border-l border-line pl-3 py-1 text-xs leading-6 text-txt2">
      <pre v-if="citation.detail" class="whitespace-pre-wrap break-all">{{ citation.detail }}</pre>
      <p v-else>{{ citation.summarySnippet || citation.targetId }}</p>
      <p v-if="shapeValid && !hrefFor()" class="mt-1 text-warn">{{ t('pages.projectDetail.pm.citationMissing') }}</p>
    </div>
  </div>
</template>

<style scoped>
.citation-source { font-size: 12px; line-height: 1.6; }
.citation-label { min-height: 32px; }
.citation-title { font-weight: 500; }
.citation-label:hover .citation-title { color: rgb(var(--c-accent-2)); text-decoration: underline; text-underline-offset: 3px; }
.citation-label:focus-visible, .citation-action:focus-visible { outline: 2px solid rgb(var(--c-accent) / .4); outline-offset: 3px; border-radius: 4px; }
.citation-action { display: inline-flex; min-height: 32px; align-items: center; gap: 6px; font-size: 11px; text-underline-offset: 3px; }
.citation-detail { margin: 2px 0 6px 6px; }
</style>
