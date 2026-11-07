<script setup lang="ts">
import ChatFileAttachment from '@/components/chat/ChatFileAttachment.vue'
/**
 * History attachments on a conversation turn: image thumbs and filename chips.
 * ClarifyChat and the run LLM transcript both render them through this one
 * element, so the two views cannot drift into a second skin.
 */
import { useI18n } from 'vue-i18n'
import type { ClarifyImage } from '@/lib/shared/types'
import { isImageAttachment } from '@/lib/shared/attachments'
import { imgSrc } from '@/lib/shared/compositeText'
import ChatImageThumb from '../ui/ChatImageThumb.vue'
import Icon from '../ui/Icon.vue'

const props = defineProps<{
  images: ClarifyImage[]
  role: 'human' | 'agent'
}>()

const emit = defineEmits<{ preview: [index: number] }>()

const { t: translate } = useI18n()

function labelOf(index: number): string {
  const named = props.images[index]?.name?.trim()
  if (named) return named
  if (props.images.length <= 1) return translate('common.chatImage.imageFallback')
  return translate('common.chatImage.imageFallbackN', { n: index + 1 })
}
</script>

<template>
  <div
    v-if="images.length"
    class="mb-1.5 flex flex-wrap gap-1.5"
    :class="role === 'human' ? 'justify-end' : ''"
  >
    <template v-if="role === 'human'">
      <template v-for="(im, ii) in images" :key="ii">
        <ChatImageThumb
          v-if="isImageAttachment(im) && im.readState !== 'reading'"
          mode="previewable"
          size="md"
          fit="contain"
          thumb-class="rounded-md"
          :src="imgSrc(im)"
          :label="labelOf(ii)"
          test-id="clarify-history-image-thumb"
          @preview="emit('preview', ii)"
        />
        <ChatFileAttachment v-else :attachment="im" :label="labelOf(ii)" data-testid="clarify-history-file-chip" />
      </template>
    </template>
    <template v-else>
      <template v-for="(im, ii) in images" :key="ii">
        <ChatImageThumb
          v-if="isImageAttachment(im) && im.readState !== 'reading'"
          mode="locked"
          size="md"
          thumb-class="rounded-md"
          :src="imgSrc(im)"
          :label="labelOf(ii)"
          test-id="clarify-agent-image-thumb"
        />
        <ChatFileAttachment v-else :attachment="im" :label="labelOf(ii)" data-testid="clarify-agent-file-chip" />
      </template>
    </template>
  </div>
</template>
