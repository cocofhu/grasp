<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import type { Artifact } from '@/lib/shared/types'
import AnnotateBtn from './product/AnnotateBtn.vue'
import { withMermaidSerial } from './mermaidRenderQueue'
import { mermaidThemeName, themeVars } from './mermaidTheme'
import { nextMermaidRenderId } from './mermaidRenderId'

export type PlanDiagram = {
  kind?: string
  title?: string
  scope?: string
  format?: string
  source?: string
  fallback_artifact?: string
  caption?: string
}

const props = defineProps<{
  diagram: PlanDiagram
  jsonPath: string
  artifacts?: Artifact[]
}>()

const { t } = useI18n()
const host = ref<HTMLElement | null>(null)
const failed = ref(false)
const rendering = ref(false)
let renderGen = 0
let themeObserver: MutationObserver | null = null
/** Last format+source that failed parse — theme toggles must not re-enter. */
let failedSourceKey = ''
/** SVG produced before host mounted (watch immediate); applied onMounted / nextTick. */
let pendingSvg: string | null = null

const format = computed(() => (props.diagram.format || 'mermaid').trim().toLowerCase() || 'mermaid')
const source = computed(() => (props.diagram.source || '').trim())
const caption = computed(() => (props.diagram.caption || '').trim())
const annotateLabel = computed(() => (props.diagram.title || '').trim() || props.jsonPath)

const fallbackUrl = computed(() => {
  const name = (props.diagram.fallback_artifact || '').trim()
  if (!name || !props.artifacts?.length) return ''
  const hit = props.artifacts.find((a) => a.name === name)
  return hit?.id ? api.artifactDownloadUrl(hit.id) : ''
})

function sourceKey() {
  return `${format.value}\n${source.value}`
}

function clearHost() {
  if (host.value) host.value.innerHTML = ''
  pendingSvg = null
}

/** Remove this render's temp node and stray default error blocks.
 *  Do not sweep all `[id^="dplan-mmd-"]` — concurrent MermaidDiagram instances
 *  may still own in-flight temp nodes (PlanView multi-diagram). */
function cleanupMermaidDom(renderId?: string) {
  if (typeof document === 'undefined') return
  if (renderId) {
    document.getElementById(renderId)?.remove()
  }
  for (const el of Array.from(document.body.children)) {
    if (el.closest?.('[data-testid="plan-diagram"]')) continue
    const text = (el.textContent || '').trim()
    if (text.startsWith('Syntax error in text')) el.remove()
  }
}

class MermaidParseError extends Error {
  constructor(cause?: unknown) {
    super(cause instanceof Error ? cause.message : 'mermaid parse failed')
    this.name = 'MermaidParseError'
  }
}

/** Write SVG into host; wait briefly if watch(immediate) raced ahead of mount. */
async function applySvg(svg: string, gen: number) {
  for (let i = 0; i < 8; i++) {
    if (gen !== renderGen) return
    if (host.value) {
      host.value.innerHTML = svg
      pendingSvg = null
      return
    }
    pendingSvg = svg
    await nextTick()
  }
}

async function render() {
  const gen = ++renderGen
  const sk = sourceKey()
  if (!source.value) {
    failed.value = true
    failedSourceKey = sk
    clearHost()
    return
  }
  if (format.value !== 'mermaid') {
    failed.value = true
    failedSourceKey = sk
    clearHost()
    return
  }
  // Sticky only for confirmed parse failures — theme changes must not re-enter.
  if (failed.value && failedSourceKey === sk) {
    return
  }
  failed.value = false
  rendering.value = true
  clearHost()
  let lastRenderId = ''
  try {
    const svg = await withMermaidSerial(async (mermaid) => {
      // Stale after enqueue: skip initialize/parse/render so we do not burn the lock.
      if (gen !== renderGen) return null
      mermaid.initialize({
        startOnLoad: false,
        securityLevel: 'strict',
        // Prevent Mermaid from injecting "Syntax error in text" nodes into the document.
        suppressErrorRendering: true,
        theme: mermaidThemeName(),
        themeVariables: themeVars(),
      })
      // Parse first so illegal sources never call render() (avoids error SVG / DOM inject).
      try {
        await mermaid.parse(source.value)
      } catch (err) {
        throw new MermaidParseError(err)
      }
      if (gen !== renderGen) return null

      // Render may throw transiently under shared state; retry once before fallback.
      let lastErr: unknown
      for (let attempt = 0; attempt < 2; attempt++) {
        if (gen !== renderGen) return null
        const renderId = nextMermaidRenderId(gen)
        lastRenderId = renderId
        try {
          const out = await mermaid.render(renderId, source.value)
          cleanupMermaidDom(`d${renderId}`)
          return out.svg
        } catch (err) {
          lastErr = err
          cleanupMermaidDom(`d${renderId}`)
        }
      }
      throw lastErr instanceof Error ? lastErr : new Error('mermaid render failed')
    })
    if (gen !== renderGen) return
    if (svg == null) return
    await applySvg(svg, gen)
    if (gen !== renderGen) return
    failedSourceKey = ''
  } catch (err) {
    if (gen === renderGen) {
      failed.value = true
      // Sticky lock only for parse failures; render transient failures stay retryable.
      if (err instanceof MermaidParseError) {
        failedSourceKey = sk
      } else {
        failedSourceKey = ''
      }
      clearHost()
      cleanupMermaidDom(lastRenderId ? `d${lastRenderId}` : undefined)
    }
  } finally {
    if (lastRenderId) cleanupMermaidDom(`d${lastRenderId}`)
    if (gen === renderGen) rendering.value = false
  }
}

watch(
  () => [source.value, format.value, props.diagram.fallback_artifact],
  () => {
    // Source change clears sticky parse lock when key differs (checked via sourceKey above).
    void render()
  },
  { immediate: true },
)

onMounted(() => {
  if (pendingSvg && host.value) {
    host.value.innerHTML = pendingSvg
    pendingSvg = null
  }
  themeObserver = new MutationObserver(() => {
    void render()
  })
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
})

onBeforeUnmount(() => {
  // Invalidate in-flight work so a finished render never writes into a destroyed host.
  renderGen++
  themeObserver?.disconnect()
  themeObserver = null
  clearHost()
  cleanupMermaidDom()
})
</script>

<template>
  <div class="group mt-2 space-y-1.5" data-testid="plan-diagram" :data-json-path="jsonPath">
    <div class="flex items-center justify-end">
      <AnnotateBtn :json-path="jsonPath" :label="annotateLabel" />
    </div>
    <div v-show="!failed" ref="host" class="overflow-x-auto text-txt [&_svg]:max-w-full" />
    <div v-if="failed" class="space-y-2" data-testid="plan-diagram-fallback">
      <div class="text-[11px] text-txt2" data-testid="plan-diagram-fallback-hint">{{ t('pages.plan.diagramFallback') }}</div>
      <pre
        class="rounded-lg overflow-x-auto border border-line bg-base p-2 font-mono text-[11px] leading-relaxed text-txt2 whitespace-pre-wrap"
        data-testid="plan-diagram-fallback-source"
      >{{ source }}</pre>
      <img
        v-if="fallbackUrl"
        :src="fallbackUrl"
        :alt="caption || 'diagram fallback'"
        class="rounded-lg max-h-64 max-w-full border border-line object-contain"
        data-testid="plan-diagram-fallback-img"
      />
    </div>
    <div v-if="caption" class="text-center text-[11px] text-txt3">{{ caption }}</div>
    <div v-if="rendering && !failed" class="text-[11px] text-txt3">…</div>
  </div>
</template>
