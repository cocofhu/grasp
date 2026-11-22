<script setup lang="ts">
import { computed } from 'vue'
import { renderChatMarkdown } from '@/lib/shared/markdown'

const props = withDefaults(
  defineProps<{
    text: string
    imageUrls?: string[]
    label: string
  }>(),
  { imageUrls: () => [] },
)

const images = computed(() => {
  const seen = new Set<string>()
  return props.imageUrls
    .map((url) => url.trim())
    .filter((url) => {
      if (!url || seen.has(url) || !/^data:image\//i.test(url) && !/^https?:\/\//i.test(url)) return false
      seen.add(url)
      return true
    })
    .map((url, index) => ({
      url,
      name: `${props.label} ${index + 1}`,
    }))
})

const bodyText = computed(() => {
  if (!props.imageUrls.length) return props.text
  return props.text
    .replace(/(?:、)?图片 ×\d+/gu, '')
    .replace(/(?:^|\n)\[附件\]\s*$/u, '')
    .trim()
})
const html = computed(() => renderChatMarkdown(bodyText.value))
const hasBlockMarkdown = computed(() => /<\/p>\s*<p|<(?:ul|ol|pre|blockquote|table|hr|h[1-6])\b/i.test(html.value))
</script>

<template>
  <div v-if="images.length" class="agent-steer-attachments" data-testid="agent-steer-attachments">
    <div
      v-for="(image, index) in images"
      :key="image.url"
      class="agent-steer-image"
      :data-testid="`agent-steer-image-${index}`"
    >
      <img :src="image.url" :alt="image.name" loading="lazy" />
    </div>
  </div>
  <div class="agent-steer" data-testid="agent-timeline-steer">
    <div class="agent-steer-body chat-prose md" :class="{ 'agent-steer-body-block': hasBlockMarkdown }">
      <span class="agent-steer-label">{{ label }}</span>
      <span class="agent-steer-markdown" v-html="html" />
    </div>
  </div>
</template>

<style scoped>
.agent-steer {
  align-self: flex-end;
  max-width: 88%;
  margin: 0 0 8px;
  border-radius: 18px 18px 7px 18px;
  padding: 11px 16px;
  background: rgb(var(--c-elevated) / .8);
  color: rgb(var(--c-txt));
  font-size: 15px;
  line-height: 1.75;
  overflow-wrap: anywhere;
}
.agent-steer-attachments {
  align-self: flex-end;
  max-width: 88%;
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin: 0 0 6px;
}
.agent-steer-image {
  max-width: 100%;
  overflow: hidden;
  border: 1px solid rgb(var(--c-line));
  border-radius: 8px;
}
.agent-steer-image img {
  display: block;
  max-height: 240px;
  max-width: 100%;
  object-fit: contain;
}
.agent-steer-body {
  min-width: 0;
  overflow-wrap: anywhere;
}
.agent-steer-label {
  margin-right: 6px;
  color: rgb(var(--c-txt3));
  font-size: 11px;
}
.agent-steer-markdown {
  display: inline;
}
.agent-steer-body .agent-steer-markdown :deep(p) {
  display: inline;
  margin: 0;
}
.agent-steer-body-block .agent-steer-label {
  display: inline-block;
  margin-bottom: 4px;
}
.agent-steer-body-block .agent-steer-markdown {
  display: block;
}
.agent-steer-body-block .agent-steer-markdown :deep(p) {
  display: block;
  margin: 0 0 11px;
}
.agent-steer-body-block .agent-steer-markdown :deep(p:last-child) { margin-bottom: 0; }
.agent-steer-markdown :deep(img) {
  display: block;
  max-width: 100%;
  max-height: 240px;
  border-radius: 8px;
}
.agent-steer-markdown :deep(pre) {
  max-width: 100%;
  margin: 10px 0;
  overflow-x: auto;
}
.agent-steer-markdown :deep(:is(ul, ol)) { margin-block: 8px; }
.agent-steer-markdown :deep(:is(h1, h2, h3, h4)) { margin-block: 12px 6px; }
</style>
