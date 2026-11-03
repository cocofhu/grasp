<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AgentTimeline from '@/components/run/AgentTimeline.vue'
import ChatUserMessage from '@/components/run/ChatUserMessage.vue'
import { partsOf } from '@/lib/solo/acpTurn'
import { soloEmbedClient } from '@/lib/solo/soloEmbedClient'
import { useSoloChat } from '@/lib/solo/useSoloChat'
import { previewPickAnnotation, type AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'

const props = defineProps<{ runId: string; token: string }>()
const emit = defineEmits<{ status: [status: 'active' | 'invalid'] }>()
const { t } = useI18n()
const sessionId = ref('')
const client = soloEmbedClient(props.runId, props.token, () => emit('status', 'invalid'))
const { session, turns, canSend, busy, actionError, loadError, send, cancel } = useSoloChat(sessionId, client)
const input = ref('')
const picks = ref<ReturnType<typeof previewPickAnnotation>[]>([])
const scroll = ref<HTMLElement | null>(null)
const connectError = ref(false)

async function connect() {
  connectError.value = false
  try {
    sessionId.value = (await client.getSoloSession()).id
  } catch {
    connectError.value = true
  }
}
onMounted(connect)
watch(canSend, (ready) => {
  if (ready) emit('status', 'active')
})
watch(turns, () => {
  const el = scroll.value
  if (!el || el.scrollHeight - el.scrollTop - el.clientHeight > 80) return
  void nextTick(() => { el.scrollTop = el.scrollHeight })
}, { deep: true })

function addPick(payload: AppPreviewPickPayload) {
  const pick = previewPickAnnotation({
    ...payload,
    selector: payload.selector.trim().slice(0, 1024),
    url: payload.url?.slice(0, 2048),
    tagName: payload.tagName.slice(0, 32),
  })
  if (picks.value.length < 8 && !picks.value.some(p => p.selector === pick.selector && p.url === pick.url)) {
    picks.value.push(pick)
  }
}
defineExpose({ addPick })

async function submit() {
  const text = input.value.trim()
  const selected = picks.value
  if (!canSend.value || (!text && !selected.length)) return
  input.value = ''
  picks.value = []
  const context = selected.length ? `\n\nSelected page elements:\n${selected.map(pick => JSON.stringify(pick)).join('\n')}` : ''
  if (!await send(text + context)) {
    input.value = [text, input.value].filter(Boolean).join('\n')
    picks.value = [...selected, ...picks.value].slice(0, 8)
  }
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col" data-testid="solo-preview-chat">
    <div ref="scroll" class="scroll-area min-h-0 flex-1 space-y-4 overflow-y-auto p-4" role="log">
      <div v-if="connectError || loadError" class="space-y-2" role="alert">
        <p>{{ loadError || t('pages.embedChat.networkError') }}</p>
        <AppButton size="sm" @click="sessionId = ''; connect()">{{ t('common.buttons.retry') }}</AppButton>
      </div>
      <p v-else-if="!canSend" role="status" class="text-sm text-txt3">{{ t('pages.embedChat.connecting') }}</p>
      <template v-for="turn in turns" :key="turn.id">
        <ChatUserMessage v-if="turn.role === 'user'" :text="turn.text" :created-at="turn.createdAt" />
        <AgentTimeline v-else-if="turn.role === 'agent'" :parts="partsOf(turn)" :streaming="turn.streaming" :metadata="turn" />
        <p v-else class="text-xs text-txt3">{{ turn.text }}</p>
      </template>
    </div>
    <form class="shrink-0 space-y-2 border-t border-line p-3" @submit.prevent="submit">
      <p v-if="actionError" role="alert" class="text-xs text-err">{{ actionError }}</p>
      <div v-if="picks.length" class="flex flex-wrap gap-1">
        <button
          v-for="(pick, i) in picks"
          :key="i"
          type="button"
          class="max-w-full truncate rounded border border-line bg-overlay px-2 py-1 text-xs"
          data-testid="solo-preview-pick"
          @click="picks.splice(i, 1)"
        >{{ pick.label }} ×</button>
      </div>
      <textarea
        v-model="input"
        class="w-full resize-y rounded border border-line bg-surface p-2 text-sm"
        rows="2"
        :disabled="!canSend"
        :aria-label="t('pages.solo.chat.placeholder', { agent: session?.agentName || '' })"
        data-testid="solo-preview-input"
      />
      <div class="flex justify-end gap-2">
        <AppButton v-if="busy" type="button" size="sm" @click="cancel()">{{ t('pages.solo.chat.stop') }}</AppButton>
        <AppButton type="submit" size="sm" variant="primary" :disabled="!canSend || (!input.trim() && !picks.length)">
          {{ t('pages.solo.chat.send') }}
        </AppButton>
      </div>
    </form>
  </div>
</template>
