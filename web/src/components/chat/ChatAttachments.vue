<script setup lang="ts">
import ChatFileAttachment from '@/components/chat/ChatFileAttachment.vue'
import ChatAttachmentDetails from '@/components/chat/ChatAttachmentDetails.vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import ChatImageThumb from '@/components/ui/ChatImageThumb.vue'
import type { ClarifyImage } from '@/lib/shared/types'
import { attachmentDisplayName, isImageAttachment } from '@/lib/shared/attachments'
import { chatImageSrc } from '@/lib/shared/compositeText'

const { t } = useI18n()

defineProps<{ images: ClarifyImage[]; sent?: boolean }>()
defineEmits<{ preview: [src: string, label: string]; remove: [index: number] }>()
</script>

<template>
  <div v-if="images.length" class="mb-2 flex flex-wrap gap-1.5" :class="{ 'justify-end': sent }">
    <div v-for="(image, index) in images" :key="index" class="relative">
      <ChatImageThumb
        v-if="isImageAttachment(image) && image.readState !== 'reading'"
        mode="previewable"
        :size="sent ? 'md' : 'sm'"
        thumb-class="rounded-md"
        :src="chatImageSrc(image)"
        :label="attachmentDisplayName(image, index)"
        :alt="attachmentDisplayName(image, index)"
        @preview="$emit('preview', chatImageSrc(image), attachmentDisplayName(image, index))"
      />
      <ChatFileAttachment v-else :attachment="image" :compact="!sent" />
      <ChatAttachmentDetails v-if="!sent && isImageAttachment(image) && image.readState !== 'reading'" :attachment="image" :index="index" />
      <button v-if="!sent" type="button" class="absolute -right-1.5 -top-1.5 flex h-4 w-4 items-center justify-center rounded-full pending-attach-remove" @click="$emit('remove', index)" :aria-label="t('common.attachments.remove', { name: attachmentDisplayName(image, index) })">
        <Icon name="close" :size="9" />
      </button>
    </div>
  </div>
</template>
