<script setup lang="ts">
import Icon from '@/components/ui/Icon.vue'
import { attachmentDetails, attachmentDisplayName } from '@/lib/shared/attachments'
import type { ClarifyImage } from '@/lib/shared/types'
defineProps<{ attachment: ClarifyImage; index: number }>()
</script>

<template>
  <div class="mt-1 max-w-[140px] text-[10px] text-txt3" :title="attachmentDisplayName(attachment, index)">
    <span class="block truncate text-[11px] text-txt2">{{ attachmentDisplayName(attachment, index) }}</span>
    <span class="flex items-center gap-1"><Icon name="image" :size="11" />{{ attachmentDetails(attachment) }}</span>
  </div>
</template>
