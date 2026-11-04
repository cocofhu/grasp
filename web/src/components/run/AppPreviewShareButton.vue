<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppModal from '@/components/ui/AppModal.vue'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import { copyToClipboard } from '@/lib/shared/copyToClipboard'
import { isLoopbackShareHost } from '@/lib/inbox/gateShareLink'
import { createPreviewShare, previewShareURL, revokePreviewShare, type PreviewChatMode, type PreviewShareLink, type PreviewShareScope } from '@/lib/inbox/appPreviewShare'

const props = defineProps<{ scope: PreviewShareScope }>()
const { t } = useI18n()
const modes = ['isolated', 'main'] as const
const open = ref(false)
const mode = ref<PreviewChatMode>('isolated')
const links = ref<Partial<Record<PreviewChatMode, PreviewShareLink>>>({})
const working = ref<'copy' | 'revoke' | ''>('')
const notice = ref('')
const error = ref('')
const manual = ref(false)
const currentLink = computed(() => links.value[mode.value])
const localAddress = isLoopbackShareHost(window.location.origin)

watch(mode, resetFeedback)

function resetFeedback() {
  notice.value = ''
  error.value = ''
  manual.value = false
}

function show() {
  mode.value = 'isolated'
  resetFeedback()
  open.value = true
}

async function copy() {
  if (working.value) return
  working.value = 'copy'
  resetFeedback()
  const selected = mode.value
  try {
    let link = links.value[selected]
    if (!link || Date.parse(link.expiresAt) <= Date.now()) {
      link = await createPreviewShare(props.scope, selected)
      links.value[selected] = link
    }
    link.url = previewShareURL(link.url)
    const copied = await copyToClipboard(link.url)
    notice.value = t(copied ? 'pages.appPreview.share.copied' : 'pages.appPreview.share.manual')
    manual.value = !copied
  } catch (e) {
    error.value = t((e as { code?: string }).code === 'main_chat_unavailable'
      ? 'pages.appPreview.share.mainUnavailable' : 'pages.appPreview.share.failed')
  } finally {
    working.value = ''
  }
}

async function revoke() {
  if (working.value) return
  working.value = 'revoke'
  resetFeedback()
  const selected = mode.value
  try {
    await revokePreviewShare(props.scope, selected)
    delete links.value[selected]
    notice.value = t('pages.appPreview.share.revoked')
  } catch {
    error.value = t('pages.appPreview.share.failed')
  } finally {
    working.value = ''
  }
}
</script>

<template>
  <button
    type="button"
    class="inline-flex items-center gap-1 rounded px-2 py-0.5 text-[11px] text-txt2 transition-colors hover:bg-overlay hover:text-txt focus-visible:outline-accent"
    data-testid="app-preview-share"
    @click="show"
  >
    <Icon name="link" :size="12" />
    {{ t('pages.appPreview.share.copy') }}
  </button>
  <AppModal
    :open="open"
    :title="t('pages.appPreview.share.title')"
    :width="480"
    :close-label="t('common.buttons.close')"
    close-on-esc
    trap-focus
    @close="open = false"
  >
    <p class="mb-4 text-sm text-txt2">{{ t('pages.appPreview.share.intro') }}</p>
    <fieldset class="grid gap-2" :disabled="!!working">
      <legend class="sr-only">{{ t('pages.appPreview.share.choose') }}</legend>
      <label
        v-for="choice in modes"
        :key="choice"
        class="flex cursor-pointer items-start gap-3 rounded-xl border px-3.5 py-3 transition-colors focus-within:ring-2 focus-within:ring-accent/40"
        :class="mode === choice ? 'border-accent/60 bg-accent/10' : 'border-line bg-elevated/40 hover:border-line-strong'"
      >
        <input
          v-model="mode"
          type="radio"
          name="preview-chat-mode"
          :value="choice"
          :autofocus="choice === 'isolated'"
          class="mt-0.5 accent-[rgb(var(--c-accent))]"
        />
        <span class="min-w-0 flex-1">
          <span class="flex flex-wrap items-center gap-2 text-sm font-medium text-txt">
            {{ t(`pages.appPreview.share.${choice}`) }}
            <span v-if="choice === 'isolated'" class="rounded-full bg-accent/15 px-2 py-0.5 text-[10px] text-accent">{{ t('pages.appPreview.share.recommended') }}</span>
          </span>
          <span class="mt-1 block text-xs leading-relaxed text-txt2">{{ t(`pages.appPreview.share.${choice}Hint`) }}</span>
        </span>
      </label>
    </fieldset>
    <p class="mt-3 text-xs leading-relaxed text-txt3">{{ t('pages.appPreview.share.duration') }}</p>
    <p v-if="localAddress" class="mt-1 text-xs leading-relaxed text-txt3">{{ t('pages.appPreview.share.loopback') }}</p>
    <div class="mt-3 min-h-5 text-sm">
      <p v-if="error" role="alert" class="text-err">{{ error }}</p>
      <p v-else role="status" class="text-txt2">{{ notice }}</p>
    </div>
    <input
      v-if="manual && currentLink"
      :value="currentLink.url"
      readonly
      :aria-label="t('pages.appPreview.share.title')"
      class="mt-2 w-full rounded-lg border border-line bg-base px-2 py-1.5 text-xs text-txt"
      @focus="($event.target as HTMLInputElement).select()"
    />
    <template #footer>
      <div class="flex w-full flex-wrap items-center gap-2">
        <AppButton variant="ghost" :loading="working === 'revoke'" :disabled="working === 'copy'" @click="revoke">
          {{ t('pages.appPreview.share.revoke') }}
        </AppButton>
        <AppButton class="ml-auto" variant="primary" icon="copy" :loading="working === 'copy'" :disabled="working === 'revoke'" @click="copy">
          {{ t('pages.appPreview.share.copy') }}
        </AppButton>
      </div>
    </template>
  </AppModal>
</template>
