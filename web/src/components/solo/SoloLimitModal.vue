<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppModal from '@/components/ui/AppModal.vue'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import SoloStatusDot from './SoloStatusDot.vue'
import { api } from '@/lib/api/api'
import { useSoloSessions } from '@/lib/solo/useSoloSessions'
import { soloErrorText } from '@/lib/solo/soloErrors'

defineProps<{ open: boolean }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'freed'): void }>()
const { t, te } = useI18n()
const store = useSoloSessions()
const closing = ref('')
const error = ref('')

async function closeOne(id: string) {
  closing.value = id
  error.value = ''
  try {
    store.upsert(await api.closeSoloSession(id))
    emit('freed')
  } catch (e) {
    error.value = soloErrorText(e, t, te)
  } finally {
    closing.value = ''
  }
}
</script>

<template>
  <AppModal :open="open" :title="t('pages.solo.limitTitle')" :width="460" close-on-esc @close="emit('close')">
    <div class="space-y-3 text-[13px] text-txt2" data-testid="solo-limit-modal">
      <p>{{ t('pages.solo.limitHint', { max: store.maxSandboxes.value }) }}</p>
      <ul class="divide-y divide-line/70 rounded-lg border border-line">
        <li v-for="s in store.active.value" :key="s.id" class="flex items-center gap-3 px-3 py-2.5">
          <SoloStatusDot :status="s.status" :busy="s.busy" />
          <div class="min-w-0 flex-1">
            <div class="truncate text-txt">{{ s.title || t('pages.solo.untitled') }}</div>
            <div class="truncate text-[11px] text-txt3">{{ s.agentName }}<template v-if="s.project"> · {{ s.project.name }}</template></div>
          </div>
          <AppButton size="sm" variant="outline" :loading="closing === s.id" @click="closeOne(s.id)">
            {{ t('pages.solo.closeSession') }}
          </AppButton>
        </li>
        <li v-if="!store.active.value.length" class="px-3 py-4 text-center text-txt3">
          <Icon name="check" :size="14" class="mr-1 inline text-ok" />{{ t('pages.solo.limitFreed') }}
        </li>
      </ul>
      <p v-if="error" class="text-[12px] text-err">{{ error }}</p>
    </div>
    <template #footer>
      <AppButton variant="ghost" @click="emit('close')">{{ t('common.buttons.close') }}</AppButton>
    </template>
  </AppModal>
</template>
