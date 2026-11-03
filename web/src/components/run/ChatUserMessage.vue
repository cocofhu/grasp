<script setup lang="ts">
/**
 * The conversation's user bubble. ClarifyChat and the run LLM transcript
 * both render user text through this one element. Same chat markdown as
 * assistant replies, so tags stay visible and never execute.
 */
import { computed, getCurrentInstance, h, onBeforeUnmount, ref, render, watch } from 'vue'
import AnnotationChip from './AnnotationChip.vue'
import { readPreviewMessage } from '@/lib/shared/previewPickUrl'
import type { ReactAnnotation } from '@/lib/shared/types'
import ReferenceChip from '@/components/solo/ReferenceChip.vue'
import type { MessageReference } from '@/lib/api/apiTypes'
import { inlineReferences, referenceLabel } from '@/lib/solo/referenceTokens'
import { renderChatMarkdown } from '@/lib/shared/markdown'
import { fmtTime } from '@/lib/shared/format'

const props = defineProps<{
  text: string
  /** Omit on history rows that are not the live human bubble. */
  testId?: string
  createdAt?: string
  /** `@label` spans of these references render as chips in place. */
  references?: MessageReference[]
  defaultRepo?: string
  annotations?: ReactAnnotation[]
}>()

const appContext = getCurrentInstance()!.appContext
const prose = ref<HTMLElement | null>(null)
const message = computed(() => readPreviewMessage(props.text, props.annotations))

const inline = computed(() =>
  props.references?.length ? inlineReferences(message.value.text, props.references, (r) => referenceLabel(r, props.defaultRepo)).inline : [],
)

/** Private-use markers survive markdown untouched, then become chip slots. */
const html = computed(() => {
  if (!inline.value.length) return renderChatMarkdown(message.value.text)
  let src = ''
  let at = 0
  inline.value.forEach((token, i) => {
    src += `${message.value.text.slice(at, token.start)}\uE000${i}\uE001`
    at = token.end
  })
  src += message.value.text.slice(at)
  return renderChatMarkdown(src).replace(/\uE000(\d+)\uE001/g, '<span class="chat-ref-slot" data-ref-slot="$1"></span>')
})

let mounted: HTMLElement[] = []
function unmountChips() {
  for (const el of mounted) render(null, el)
  mounted = []
}
watch(
  [html, inline, () => props.defaultRepo, prose],
  () => {
    unmountChips()
    prose.value?.querySelectorAll<HTMLElement>('[data-ref-slot]').forEach((el) => {
      const token = inline.value[Number(el.dataset.refSlot)]
      if (!token) return
      const vnode = h(ReferenceChip, { reference: token.ref, defaultRepo: props.defaultRepo, link: true })
      vnode.appContext = appContext
      render(vnode, el)
      mounted.push(el)
    })
  },
  { flush: 'post' },
)
onBeforeUnmount(unmountChips)
</script>

<template>
  <div
    class="chat-user-message break-words [overflow-wrap:anywhere]"
    :data-testid="testId || undefined"
    :title="createdAt && Number.isFinite(Date.parse(createdAt)) ? fmtTime(createdAt) : undefined"
  >
    <div v-if="message.annotations.length" class="flex flex-wrap gap-1.5" :class="{ 'mb-1.5': message.text }" data-testid="chat-message-annotations">
      <AnnotationChip v-for="(annotation, index) in message.annotations" :key="index" :ann="annotation" />
    </div>
    <div v-if="message.text" ref="prose" class="chat-prose md" v-html="html" />
  </div>
</template>
