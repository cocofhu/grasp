<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppSpinner from '@/components/ui/AppSpinner.vue'
import { relTime } from '@/lib/shared/format'
import {
  inboxBadgeLabelKey,
  inboxBadgeTone,
  inboxBadgeToneClass,
  inboxIconToneClass,
  inboxSecondaryLine,
  isInboxProgressItem,
  isReplyingInboxItem,
  isStartingInboxItem,
} from '@/lib/inbox/inboxDisplay'
import { isShareableInboxItem, shareStatusLabel } from '@/lib/inbox/gateShareLink'
import type { GateShareInboxStatus, InboxItem } from '@/lib/shared/types'

const props = defineProps<{
  item: InboxItem
  active?: boolean
  disabled?: boolean
  showChevron?: boolean
}>()

const emit = defineEmits<{
  (e: 'select'): void
  (e: 'open-share'): void
}>()

const { t, locale } = useI18n()

const title = computed(() => (props.item.type === 'gate' ? props.item.title : props.item.label))
const secondary = computed(() => inboxSecondaryLine(props.item))
const timeLabel = computed(() =>
  props.item.type === 'gate' ? relTime(props.item.requestedAt) : relTime(props.item.updatedAt),
)
const starting = computed(() => isStartingInboxItem(props.item))
const replying = computed(() => isReplyingInboxItem(props.item))
const inProgress = computed(() => isInboxProgressItem(props.item))
const iconName = computed(() => {
  if (props.item.type === 'gate') return 'gate'
  if (props.item.kind === 'app_preview') return 'monitor'
  if (props.item.kind === 'preflight') return 'ci'
  return 'chat'
})
const iconClass = computed(() => inboxIconToneClass(inboxBadgeTone(props.item)))
const badgeClass = computed(() => inboxBadgeToneClass(inboxBadgeTone(props.item)))
const badgeText = computed(() => t(inboxBadgeLabelKey(props.item)))
const showShare = computed(() => isShareableInboxItem(props.item))
const itemShareLink = computed((): GateShareInboxStatus | undefined => {
  if (!showShare.value) return undefined
  return 'shareLink' in props.item ? props.item.shareLink : undefined
})
const shareLabel = computed(() => (showShare.value ? shareStatusLabel(itemShareLink.value, t) : ''))
const shareUsed = computed(() => showShare.value && itemShareLink.value?.state === 'used')
/** covers session startup, card `disabled` (incl. parent processingLock), and used share tokens */
const shareDisabled = computed(() => starting.value || Boolean(props.disabled) || shareUsed.value)

function onOpenShare() {
  if (shareDisabled.value) return
  emit('open-share')
}
</script>

<template>
  <article
    class="list-card-lift flex w-full shrink-0 flex-col rounded-lg border p-3"
    :class="active ? 'border-accent/50 bg-accent-dim/40' : 'border-line bg-surface hover:border-line-strong hover:bg-elevated'"
    data-testid="inbox-item-card"
    :data-starting="starting ? 'true' : undefined"
    :data-replying="replying ? 'true' : undefined"
  >
    <button
      type="button"
      class="flex w-full items-start gap-3 text-left disabled:cursor-not-allowed disabled:opacity-45"
      :disabled="disabled"
      :aria-pressed="active ? 'true' : 'false'"
      @click="emit('select')"
    >
      <div
        class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md"
        :class="iconClass"
      >
        <AppSpinner v-if="inProgress" :size="18" />
        <Icon v-else :name="iconName" :size="18" />
      </div>
      <div class="min-w-0 flex-1">
        <div class="truncate text-sm font-medium text-txt">{{ title }}</div>
        <div class="truncate text-[11px] text-txt3" :title="secondary">{{ secondary }}</div>
        <div class="mt-1 flex items-center gap-1.5">
          <span class="rounded border px-1.5 py-px text-[10px]" :class="badgeClass">{{ badgeText }}</span>
          <span class="text-[10px] text-txt3">{{ locale && timeLabel }}</span>
        </div>
        <div v-if="item.tags?.length" class="mt-1 flex flex-wrap gap-1.5">
          <span v-for="tag in item.tags" :key="tag" class="chip text-txt2">{{ tag }}</span>
        </div>
      </div>
      <Icon v-if="showChevron" name="chevron-right" :size="16" class="mt-2 shrink-0 text-txt3" />
    </button>
    <div
      v-if="showShare"
      class="mt-2 flex flex-wrap items-center gap-2 pl-12"
      data-testid="gate-share-row"
    >
      <span
        role="status"
        class="rounded border border-line bg-elevated px-1.5 py-0.5 text-[10px] text-txt2"
        data-testid="gate-share-status"
      >
        {{ shareLabel }}
      </span>
      <!-- max-md hit-wrap: Demo .hit-wrap.mobile-hit — vertical only, no dashed ghost -->
      <span
        class="inline-flex items-center justify-end max-md:-my-2.5 max-md:min-h-[44px] max-md:min-w-[44px] max-md:py-2.5"
        data-testid="gate-share-hit-wrap"
        @click.stop="onOpenShare"
      >
        <button
          type="button"
          class="inline-flex min-h-6 items-center gap-1 rounded border border-accent/40 bg-accent/10 px-1.5 py-0.5 text-[10px] font-medium leading-[1.4] text-accent-2 hover:bg-accent/20 disabled:cursor-not-allowed disabled:opacity-45"
          data-testid="gate-share-copy-btn"
          :disabled="shareDisabled"
          :aria-label="t('pages.gatesInbox.share.copyLinkAria')"
          @click.stop="onOpenShare"
        >
          <Icon name="copy" :size="12" />
          {{ t('pages.gatesInbox.share.copyLink') }}
        </button>
      </span>
    </div>
  </article>
</template>
