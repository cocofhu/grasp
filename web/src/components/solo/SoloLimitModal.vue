<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppModal from '@/components/ui/AppModal.vue'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import SoloStatusDot from './SoloStatusDot.vue'
import { api } from '@/lib/api/api'
import { isSoloComputing, useSoloSessions } from '@/lib/solo/useSoloSessions'
import { soloErrorText } from '@/lib/solo/soloErrors'
import { sessionRepoLabel } from '@/lib/solo/sessionRepos'

defineProps<{ open: boolean }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'freed'): void }>()
const { t, te, locale } = useI18n()
const store = useSoloSessions()
const closing = ref('')
const stopping = ref('')
const error = ref('')
// A failed session still holds quota while its environment binding remains.
const holders = computed(() => [
  ...store.active.value.filter(isSoloComputing),
  ...store.ended.value.filter((s) => s.status === 'error' && !!s.sandboxId),
])
const others = computed(() => Math.max(0, store.usedSandboxes.value - holders.value.length))

/** Stopping frees the quota once the backup finishes; the session stays resumable. */
async function stopOne(id: string) {
  stopping.value = id
  error.value = ''
  try {
    store.upsert(await api.stopSoloSession(id))
  } catch (e) {
    error.value = soloErrorText(e, t, te, locale.value)
  } finally {
    stopping.value = ''
  }
}

async function closeOne(id: string) {
  closing.value = id
  error.value = ''
  try {
    store.upsert(await api.closeSoloSession(id))
    emit('freed')
  } catch (e) {
    error.value = soloErrorText(e, t, te, locale.value)
  } finally {
    closing.value = ''
  }
}
</script>

<template>
  <AppModal :open="open" :title="t('pages.solo.limitTitle')" :width="460" close-on-esc @close="emit('close')">
    <div class="space-y-3 text-[13px] text-txt2" data-testid="solo-limit-modal">
      <p v-if="store.usedSandboxes.value > 0">{{ t('pages.solo.limitUsage', { used: store.usedSandboxes.value, max: store.maxSandboxes.value }) }}</p>
      <p v-else>{{ t('pages.solo.limitHint', { max: store.maxSandboxes.value }) }}</p>
      <ul v-if="holders.length || !others" class="divide-y divide-line/70 rounded-lg border border-line">
        <li v-for="s in holders" :key="s.id" class="flex items-center gap-3 px-3 py-2.5">
          <SoloStatusDot :status="s.status" :busy="s.busy" />
          <div class="min-w-0 flex-1">
            <div class="truncate text-txt">{{ s.title || t('pages.solo.untitled') }}</div>
            <div class="truncate text-[11px] text-txt3">{{ sessionRepoLabel(s) ? `${s.agentName} · ${sessionRepoLabel(s)}` : s.agentName }}</div>
          </div>
          <AppButton
            v-if="s.status === 'running' && !s.busy"
            size="sm"
            variant="ghost"
            :loading="stopping === s.id"
            :data-testid="`solo-limit-stop-${s.id}`"
            @click="stopOne(s.id)"
          >
            {{ t('pages.solo.stop.action') }}
          </AppButton>
          <AppButton size="sm" variant="outline" :loading="closing === s.id" @click="closeOne(s.id)">
            {{ t('pages.solo.closeSession') }}
          </AppButton>
        </li>
        <li v-if="!holders.length && !others" class="px-3 py-4 text-center text-txt3">
          <Icon name="check" :size="14" class="mr-1 inline text-ok" />{{ t('pages.solo.limitFreed') }}
        </li>
      </ul>
      <p v-if="others" class="text-[12px] text-txt3" data-testid="solo-limit-others">{{ t('pages.solo.limitOthers', { count: others }) }}</p>
      <p v-if="error" class="text-[12px] text-err">{{ error }}</p>
    </div>
    <template #footer>
      <AppButton variant="ghost" @click="emit('close')">{{ t('common.buttons.close') }}</AppButton>
    </template>
  </AppModal>
</template>
