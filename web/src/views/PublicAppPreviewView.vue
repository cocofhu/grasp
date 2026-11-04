<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import { parseShareTokenFromHash } from '@/lib/inbox/gateShareLink'
import { publicAppPreviewClient } from '@/lib/inbox/appPreviewShare'
import { directPreviewEmbedUrl, parseEmbedLocaleFromHash } from '@/lib/inbox/embedChat'
import { applyPublicLocale, locale } from '@/lib/shared/locale'
import { theme } from '@/lib/shared/theme'

const { t } = useI18n()
const client = publicAppPreviewClient(parseShareTokenFromHash(window.location.hash))
const sharedLocale = parseEmbedLocaleFromHash(window.location.hash)
const controller = new AbortController()
const status = ref<'loading' | 'inactive' | 'directUnavailable' | 'network'>('loading')

async function openPreview() {
  status.value = 'loading'
  try {
    await applyPublicLocale(sharedLocale)
    const target = await client.open(controller.signal)
    if (!controller.signal.aborted) {
      window.location.replace(directPreviewEmbedUrl(target.directUrl, target, theme.value, locale.value, true))
    }
  } catch (error) {
    if (!controller.signal.aborted) {
      const code = (error as { status?: number }).status
      status.value = code === 410 ? 'inactive' : code === 409 ? 'directUnavailable' : 'network'
    }
  }
}

onMounted(openPreview)
onBeforeUnmount(() => controller.abort())
</script>

<template>
  <main class="flex h-[100dvh] flex-col items-center justify-center gap-3 bg-base px-6 text-center text-txt" role="status">
    <Icon :name="status === 'loading' ? 'spinner' : 'alert'" :size="26" class="text-accent" />
    <p class="text-sm text-txt2">{{ t(`pages.appPreview.share.${status}`) }}</p>
    <AppButton v-if="status === 'network' || status === 'directUnavailable'" size="sm" @click="openPreview">{{ t('common.buttons.retry') }}</AppButton>
  </main>
</template>
