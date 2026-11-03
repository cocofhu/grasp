<script setup lang="ts">
import Icon from '@/components/ui/Icon.vue'
import ChatImageThumb from '@/components/ui/ChatImageThumb.vue'
import type { ClarifyImage } from '@/lib/shared/types'
import { attachmentDisplayName, isImageAttachment } from '@/lib/shared/attachments'
import { chatImageSrc } from '@/lib/shared/compositeText'

defineProps<{ images: ClarifyImage[]; sent?: boolean }>()
defineEmits<{ preview: [src: string, label: string]; remove: [index: number] }>()
</script>

<template>
  <div v-if="images.length" class="mb-2 flex flex-wrap gap-1.5" :class="{ 'justify-end': sent }">
    <div v-for="(image, index) in images" :key="index" class="relative">
      <ChatImageThumb
        v-if="isImageAttachment(image)"
        mode="previewable"
        :size="sent ? 'md' : 'sm'"
        thumb-class="rounded-md"
        :src="chatImageSrc(image)"
        :label="attachmentDisplayName(image, index)"
        :alt="attachmentDisplayName(image, index)"
        @preview="$emit('preview', chatImageSrc(image), attachmentDisplayName(image, index))"
      />
      <div v-else class="chat-file-chip flex h-14 max-w-[160px] items-center gap-1.5 rounded-lg border border-line bg-elevated px-2">
        <Icon name="artifact" :size="15" class="shrink-0 text-txt2" />
        <span class="min-w-0 truncate text-[12px] text-txt2">{{ attachmentDisplayName(image, index) }}</span>
      </div>
      <button v-if="!sent" type="button" class="absolute -right-1.5 -top-1.5 flex h-4 w-4 items-center justify-center rounded-full pending-attach-remove" @click="$emit('remove', index)">
        <Icon name="close" :size="9" />
      </button>
    </div>
  </div>
</template>
