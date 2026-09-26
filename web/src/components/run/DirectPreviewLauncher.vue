<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { directPreviewEmbedUrl, type EmbedTicket } from '@/lib/inbox/embedChat'
import { theme } from '@/lib/shared/theme'

const props = defineProps<{
  directUrl: string
  /** Mints the chat-drawer ticket; the preview opens without a drawer when this fails. */
  issueTicket?: () => Promise<EmbedTicket>
}>()

const { t } = useI18n()
const opening = ref(false)
const tip = ref<'chat' | 'blocked' | null>(null)
const blockedUrl = ref('')

async function open() {
  if (opening.value) return
  tip.value = null
  // Open synchronously inside the click so the popup is not blocked; navigate
  // once the ticket is in hand.
  const win = window.open('about:blank', '_blank')
  if (win) win.opener = null
  opening.value = true
  let url = props.directUrl
  try {
    if (props.issueTicket) url = directPreviewEmbedUrl(props.directUrl, await props.issueTicket(), theme.value)
  } catch {
    tip.value = 'chat'
  } finally {
    opening.value = false
  }
  if (win && !win.closed) {
    win.location.href = url
    return
  }
  blockedUrl.value = url
  tip.value = 'blocked'
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col items-center justify-center gap-3 px-6 text-center" data-testid="app-preview-direct">
    <button
      type="button"
      class="max-w-full truncate rounded border border-line bg-base px-2 py-1 font-mono text-[12px] text-txt2 hover:border-accent hover:text-txt"
      data-testid="direct-preview-address"
      :disabled="opening"
      @click="open"
    >
      {{ directUrl }}
    </button>
    <button
      type="button"
      class="inline-flex min-h-9 items-center rounded-md bg-accent px-3.5 text-sm font-medium text-white hover:bg-accent-hover disabled:opacity-60"
      :disabled="opening"
      data-testid="app-preview-direct-open"
      @click="open"
    >
      {{ t('pages.appPreview.directOpenTab') }}
    </button>
    <p class="max-w-[46ch] text-[12px] text-txt3">{{ t('pages.appPreview.directLauncherHint') }}</p>
    <p
      v-if="tip === 'chat'"
      class="max-w-[46ch] rounded-md border border-warn/40 bg-warn/10 px-2.5 py-1.5 text-[11px] text-warn"
      role="status"
      data-testid="direct-preview-tip"
    >
      {{ t('pages.appPreview.directChatUnavailable') }}
    </p>
    <a
      v-else-if="tip === 'blocked'"
      :href="blockedUrl"
      target="_blank"
      rel="noopener"
      class="text-[12px] text-accent hover:underline"
      data-testid="direct-preview-tip"
    >{{ t('pages.appPreview.directPopupBlocked') }}</a>
  </div>
</template>
