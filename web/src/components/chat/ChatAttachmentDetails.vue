<script setup lang="ts">
import { attachmentDetails, attachmentDisplayName, attachmentSizeLabel } from '@/lib/shared/attachments'
import type { ClarifyImage } from '@/lib/shared/types'
defineProps<{ attachment: ClarifyImage; index: number }>()
</script>

<template>
  <!-- w-0 min-w-full: follow the thumbnail width instead of widening the remove-button anchor. -->
  <div class="mt-1 w-0 min-w-full text-[10px] text-txt3" :title="`${attachmentDisplayName(attachment, index)}\n${attachmentDetails(attachment)}`">
    <span class="block truncate text-[11px] text-txt2">{{ attachmentDisplayName(attachment, index) }}</span>
    <span class="block truncate">{{ attachmentSizeLabel(attachment) }}</span>
  </div>
</template>
