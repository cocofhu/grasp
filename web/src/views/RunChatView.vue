<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import AppButton from '@/components/ui/AppButton.vue'
import SoloSessionPane from '@/components/solo/SoloSessionPane.vue'
import SoloCreateModal from '@/components/solo/SoloCreateModal.vue'
import RunLiveView from './RunLiveView.vue'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const runId = computed(() => String(route.params.id || ''))
const chat = ref<{ soloId: string; title: string; status: string } | null>(null)
const failed = ref(false)
const createOpen = ref(false)
const terminal = computed(() => ['completed', 'failed', 'cancelled'].includes(chat.value?.status || ''))
let request: AbortController | null = null
let generation = 0
async function load() {
  request?.abort()
  request = new AbortController()
  const current = ++generation
  try {
    const result = await api.workflowChat(runId.value, request.signal)
    if (current !== generation) return
    chat.value = result
    failed.value = false
  } catch {
    if (current === generation) failed.value = true
  }
}
watch(runId, () => { chat.value = null; createOpen.value = false; void load() }, { immediate: true })
const timer = setInterval(() => void load(), 5000)
onBeforeUnmount(() => { generation++; request?.abort(); clearInterval(timer) })
</script>

<template>
  <div class="flex h-full min-h-0 flex-col" data-testid="run-chat-view">
    <p v-if="failed && !chat" class="p-4 text-sm text-err" role="alert">
      {{ t('pages.solo.workflows.historyLoadFailed') }}
      <button class="ml-2 underline" @click="load">{{ t('common.buttons.retry') }}</button>
    </p>
    <div v-else-if="chat" class="flex min-h-0 flex-1 flex-col">
      <SoloSessionPane v-if="chat.soloId" :key="chat.soloId" :session-id="chat.soloId" @back="router.push('/dashboard')" />
      <RunLiveView v-else :key="runId" history>
        <template v-if="terminal" #actions>
          <AppButton size="sm" icon="chat" data-testid="run-chat-continue" @click="createOpen = true">{{ t('pages.solo.workflows.continueChat') }}</AppButton>
        </template>
      </RunLiveView>
    </div>
    <SoloCreateModal :open="createOpen" :continuation-run-id="runId" @close="createOpen = false; load()" />
  </div>
</template>
