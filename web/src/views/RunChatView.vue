<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import AppButton from '@/components/ui/AppButton.vue'
import StatusPill from '@/components/ui/StatusPill.vue'
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
    <header class="flex flex-wrap items-center gap-3 border-b border-line bg-surface px-4 py-3">
      <span class="min-w-0 flex-1 truncate text-sm font-semibold">{{ chat?.title || runId }}</span>
      <StatusPill v-if="chat" :status="chat.status" />
      <RouterLink :to="{ path: route.path, query: { view: 'details' } }" class="text-xs text-txt2 hover:text-accent" data-testid="run-chat-details">{{ t('pages.solo.workflows.details') }}</RouterLink>
      <AppButton v-if="chat && !chat.soloId && terminal" size="sm" icon="chat" data-testid="run-chat-continue" @click="createOpen = true">{{ t('pages.solo.workflows.continueChat') }}</AppButton>
    </header>
    <p v-if="failed && !chat" class="p-4 text-sm text-err" role="alert">
      {{ t('pages.solo.workflows.historyLoadFailed') }}
      <button class="ml-2 underline" @click="load">{{ t('common.buttons.retry') }}</button>
    </p>
    <div v-else-if="chat" class="flex min-h-0 flex-1 flex-col">
      <SoloSessionPane v-if="chat.soloId" :key="chat.soloId" :session-id="chat.soloId" @back="router.push('/dashboard')" />
      <RunLiveView v-else :key="runId" history />
    </div>
    <SoloCreateModal :open="createOpen" :continuation-run-id="runId" @close="createOpen = false; load()" />
  </div>
</template>
