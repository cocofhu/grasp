import { computed, ref, type Ref } from 'vue'
import type ChatComposerTextarea from '@/components/chat/ChatComposerTextarea.vue'
import { useImageAttachments } from '@/lib/composables/useImageAttachments'
import { useReferenceComposer } from '@/lib/solo/useReferenceComposer'

export function useChatComposerDraft(defaultRepo: Ref<string> = ref('')) {
  const input = ref('')
  const composing = ref(false)
  const textareaComponent = ref<InstanceType<typeof ChatComposerTextarea>>()
  const textarea = computed(() => textareaComponent.value?.textarea ?? null)
  const attach = useImageAttachments()
  const mention = useReferenceComposer({ input, textarea, composing, defaultRepo })
  return { input, composing, textareaComponent, textarea, attach, mention, defaultRepo }
}

export type ChatComposerDraft = ReturnType<typeof useChatComposerDraft>
