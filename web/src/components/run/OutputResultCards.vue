<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import {
  isVisualHtmlCard,
  parseOutputCardDoc,
} from '@/lib/run/isVisualHtmlCard'
import OutputResultCardBody from './OutputResultCardBody.vue'
import AppButton from '../ui/AppButton.vue'
import AppModal from '../ui/AppModal.vue'
import Icon from '../ui/Icon.vue'
import type { OutputCard, Run } from '@/lib/shared/types'

const props = defineProps<{ cards: OutputCard[]; run: Run }>()

const { t } = useI18n()

/** Master-detail: always exactly one selected card. Default 0; never -1. */
const selectedIndex = ref(0)
const enlargeOpen = ref(false)

const contentCache = ref<Record<string, string>>({})
/** Artifact names whose content fetch failed (network / 404). Missing artifact uses markdown fallback when present. */
const loadErrors = ref<Record<string, boolean>>({})
const loading = ref(false)

function artifactCacheKey(card: OutputCard): string {
  const name = card.artifactName || ''
  return `${card.nodeId || ''}:${name}`
}

function findCardArtifact(card: OutputCard) {
  const name = card.artifactName
  if (!name) return undefined
  const matches = props.run.artifacts.filter((a) => a.name === name)
  if (card.nodeId) {
    const scoped = matches.find((a) => a.nodeId === card.nodeId)
    if (scoped) return scoped
  }
  return matches[0]
}

async function loadArtifactContent(card: OutputCard) {
  const name = card.artifactName
  if (!name) return
  const key = artifactCacheKey(card)
  if (contentCache.value[key] !== undefined) return
  const art = findCardArtifact(card)
  if (!art) {
    contentCache.value[key] = ''
    // Not a fetch error — node markdown may still render via HtmlPreview.
    loadErrors.value[key] = false
    return
  }
  loading.value = true
  try {
    const full = await api.artifactContent(art.id)
    contentCache.value[key] = full.content ?? ''
    loadErrors.value[key] = false
  } catch {
    contentCache.value[key] = ''
    loadErrors.value[key] = true
  } finally {
    loading.value = false
  }
}

watch(
  () => props.cards.map((c) => `${c.index}:${c.template}`).join(','),
  () => {
    selectedIndex.value = 0
    enlargeOpen.value = false
  },
)

watch(
  [selectedIndex, () => props.cards],
  () => {
    const c = props.cards[selectedIndex.value]
    if (!c) return
    const parsed = parseDoc(c)
    const name = c.artifactName
    if (isVisualHtmlCard(c, { parsedDoc: parsed }) && name) {
      void loadArtifactContent(c)
      return
    }
    if (c.typeTag === '自定义产物' && c.artifactName && !c.structuredArtifactName) {
      void loadArtifactContent(c)
    }
  },
  { immediate: true },
)

function selectCard(i: number) {
  if (enlargeOpen.value) return
  if (i < 0 || i >= props.cards.length) return
  if (i === selectedIndex.value) return
  selectedIndex.value = i
}

function parseDoc(card: OutputCard): unknown {
  return parseOutputCardDoc(card)
}

const showList = computed(() => props.cards.length > 1)
const currentCard = computed(() => props.cards[selectedIndex.value])
const currentDoc = computed(() => (currentCard.value ? parseDoc(currentCard.value) : null))

/** F3: enlarge only for success cards that actually have renderable body (review v2). */
function hasRenderableBody(card: OutputCard | undefined): boolean {
  if (!card || card.status === 'failed') return false
  const parsed = parseDoc(card)
  const fetched = artifactContent(card)
  if (isVisualHtmlCard(card, { artifactHtml: fetched, parsedDoc: parsed })) {
    const body = fetched.trim() || card.markdown?.trim() || ''
    return !!body
  }
  if (card.typeTag === '结构化产物') {
    if (card.structuredArtifactName && parsed != null) return true
    return !!card.markdown?.trim()
  }
  if (card.typeTag === '自定义产物') return !!card.artifactName
  if (card.typeTag === 'Markdown') return !!card.markdown?.trim()
  return false
}

const canEnlarge = computed(() => hasRenderableBody(currentCard.value))

function artifactContent(card: OutputCard | undefined): string {
  if (!card) return ''
  if (!card.artifactName) return ''
  return contentCache.value[artifactCacheKey(card)] ?? ''
}

function cardVisualHtml(card: OutputCard): boolean {
  return isVisualHtmlCard(card, {
    artifactHtml: artifactContent(card),
    parsedDoc: parseDoc(card),
  })
}

/** Short list label: failed → 失败; visual HTML → HTML; else map typeTag. */
function shortKindLabel(card: OutputCard): string {
  if (card.status === 'failed') return t('pages.nodeOutput.outputCards.kindFailed')
  if (cardVisualHtml(card)) return t('pages.nodeOutput.outputCards.kindHtml')
  if (card.typeTag === '结构化产物') return t('pages.nodeOutput.outputCards.kindStructured')
  if (card.typeTag === 'Markdown') return t('pages.nodeOutput.outputCards.kindMarkdown')
  if (card.typeTag === '自定义产物') return t('pages.nodeOutput.outputCards.kindCustom')
  return card.typeTag
}

/** Detail bar kind: visual HTML cards always show「自定义产物 · HTML」. */
function detailKindLabel(card: OutputCard): string {
  if (card.status === 'failed') return card.typeTag
  if (cardVisualHtml(card)) return t('pages.nodeOutput.outputCards.kindCustomHtml')
  return card.typeTag
}

function artifactLoadError(card: OutputCard | undefined): boolean {
  if (!card) return false
  if (!card.artifactName) return false
  return !!loadErrors.value[artifactCacheKey(card)]
}

function openEnlarge() {
  if (!canEnlarge.value) return
  enlargeOpen.value = true
}

function closeEnlarge() {
  enlargeOpen.value = false
}
</script>

<template>
  <div class="min-w-0" data-testid="output-result-cards">
    <!-- Multi-card name+status list (g1.2 / g3.2): no max-height / own overflow. -->
    <div
      v-if="showList"
      class="rounded-lg mb-3 overflow-hidden border border-line bg-base"
      role="listbox"
      :aria-label="t('pages.nodeOutput.outputCards.listTitle')"
      data-testid="output-result-list"
    >
      <div
        class="flex items-center justify-between border-b border-line px-2.5 py-1.5 text-[10px] font-bold uppercase tracking-wider text-txt3"
        data-testid="output-result-list-header"
      >
        <span>{{ t('pages.nodeOutput.outputCards.listTitle') }}</span>
        <b class="text-[11px] font-semibold normal-case tracking-normal text-txt2">{{ cards.length }}</b>
      </div>
      <button
        v-for="(card, i) in cards"
        :key="card.index + ':' + card.template"
        type="button"
        role="option"
        class="flex h-8 w-full items-center gap-2 border-l-2 border-solid px-2.5 text-left"
        :class="[
          i > 0 ? 'border-t border-t-line' : '',
          card.status === 'failed' ? 'text-err' : 'text-txt2',
          i === selectedIndex
            ? card.status === 'failed'
              ? 'border-l-err bg-err/10 text-err'
              : 'border-l-accent bg-accent-dim text-txt'
            : 'border-l-transparent hover:bg-elevated hover:text-txt',
        ]"
        :aria-selected="i === selectedIndex ? 'true' : 'false'"
        :data-testid="`output-result-card-toggle-${i}`"
        @click="selectCard(i)"
      >
        <Icon
          :name="card.status === 'failed' ? 'alert' : 'check'"
          :size="12"
          class="shrink-0"
          :class="card.status === 'failed' ? 'text-err' : 'text-ok'"
        />
        <span class="min-w-0 flex-1 truncate text-[12px]" :title="card.title">{{ card.title }}</span>
        <span
          class="shrink-0 text-[10px]"
          :class="card.status === 'failed' ? 'text-err' : 'text-txt3'"
          :data-testid="`output-result-list-kind-${i}`"
        >{{ shortKindLabel(card) }}</span>
      </button>
    </div>

    <template v-if="currentCard">
      <!-- Sticky detail bar: NodeOutputPanel padding moved inward so top-0 is flush (g3.1). -->
      <div
        class="sticky top-0 z-[2] -mx-4 mb-2.5 flex items-center gap-2 border-b border-line bg-surface px-4 py-2"
        data-testid="output-result-detail-bar"
      >
        <span
          class="min-w-0 flex-1 truncate text-[12px] font-semibold"
          :class="currentCard.status === 'failed' ? 'text-err' : 'text-txt'"
        >{{ currentCard.title }}</span>
        <span
          class="rounded-md shrink-0 border border-line px-1.5 py-0.5 text-[10px] text-txt3"
          data-testid="output-result-detail-kind"
        >{{ detailKindLabel(currentCard) }}</span>
        <AppButton
          v-if="canEnlarge"
          variant="outline"
          size="sm"
          icon="expand"
          class="shrink-0"
          data-testid="output-result-enlarge"
          @click="openEnlarge"
        >
          {{ t('pages.nodeOutput.outputCards.enlarge') }}
        </AppButton>
      </div>

      <div :data-testid="`output-result-card-body-${selectedIndex}`">
        <OutputResultCardBody
          :card="currentCard"
          :run="run"
          :doc="currentDoc"
          :loading="loading"
          :artifact-html="artifactContent(currentCard)"
          :artifact-load-error="artifactLoadError(currentCard)"
          variant="detail"
        />
      </div>
    </template>

    <AppModal
      :open="enlargeOpen"
      :title="currentCard?.title || ''"
      :width="960"
      :close-on-esc="true"
      @close="closeEnlarge"
    >
      <div v-if="currentCard && canEnlarge" data-testid="output-result-enlarge-body">
        <OutputResultCardBody
          :card="currentCard"
          :run="run"
          :doc="currentDoc"
          :loading="loading"
          :artifact-html="artifactContent(currentCard)"
          :artifact-load-error="artifactLoadError(currentCard)"
          variant="enlarge"
        />
      </div>
    </AppModal>
  </div>
</template>
