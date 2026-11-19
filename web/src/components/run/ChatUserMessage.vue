<script setup lang="ts">
/**
 * The conversation's user bubble. ClarifyChat and the run LLM transcript
 * both render user text through this one element. Same chat markdown as
 * assistant replies, so tags stay visible and never execute.
 */
import { computed, getCurrentInstance, h, onBeforeUnmount, ref, render, watch } from 'vue'
import AnnotationChip from './AnnotationChip.vue'
import Icon from '@/components/ui/Icon.vue'
import { chatCommandIcons, contextSlashCommand, readSkillCommand } from '@/lib/chat/chatCommands'
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
const context = computed(() => contextSlashCommand(message.value.text))
const skill = computed(() => readSkillCommand(message.value.text))
const body = computed(() => skill.value?.task ?? message.value.text)

const inline = computed(() =>
  props.references?.length ? inlineReferences(body.value, props.references, (r) => referenceLabel(r, props.defaultRepo)).inline : [],
)

/** Private-use markers survive markdown untouched, then become chip slots. */
const html = computed(() => {
  if (!inline.value.length) return renderChatMarkdown(body.value)
  let src = ''
  let at = 0
  inline.value.forEach((token, i) => {
    src += `${body.value.slice(at, token.start)}\uE000${i}\uE001`
    at = token.end
  })
  src += body.value.slice(at)
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
    :class="{ 'chat-user-command': context }"
    :data-testid="testId || undefined"
    :title="createdAt && Number.isFinite(Date.parse(createdAt)) ? fmtTime(createdAt) : undefined"
  >
    <div v-if="message.annotations.length" class="flex flex-wrap gap-1.5" :class="{ 'mb-1.5': message.text }" data-testid="chat-message-annotations">
      <AnnotationChip v-for="(annotation, index) in message.annotations" :key="index" :ann="annotation" />
    </div>
    <span v-if="context" class="chat-context-invocation">
      <Icon :name="chatCommandIcons[context]" :size="13" :interactive="false" />
      <code>{{ message.text.trim() }}</code>
    </span>
    <span v-if="skill" class="chat-skill-invocation" :class="{ 'mb-1.5': body }" data-testid="chat-skill-tag">
      <Icon :name="chatCommandIcons.skill" :size="13" :interactive="false" />
      <span class="sr-only">/skill</span><code>{{ skill.name }}</code>
    </span>
    <div v-if="body && !context" ref="prose" class="chat-prose md" v-html="html" />
  </div>
</template>

<style scoped>
.chat-user-command {
  padding: 5px 9px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 8px;
  background: rgb(var(--c-surface));
}
.chat-context-invocation,
.chat-skill-invocation {
  display: inline-flex;
  max-width: 100%;
  align-items: center;
  gap: 6px;
  color: rgb(var(--c-txt2));
  font-size: 12px;
  line-height: 1.5;
}
.chat-context-invocation svg,
.chat-skill-invocation svg { flex: none; color: rgb(var(--c-accent-2)); }
.chat-skill-invocation {
  padding: 2px 7px;
  border: 1px solid rgb(var(--c-accent) / 16%);
  border-radius: 6px;
  background: rgb(var(--c-accent) / 8%);
  color: rgb(var(--c-txt));
}
</style>
