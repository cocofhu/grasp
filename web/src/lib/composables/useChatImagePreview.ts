import { ref } from 'vue'

type ChatImagePreviewState = { src: string; label: string }

/** Single-slot chat image preview state (no gallery). */
export function useChatImagePreview() {
  const preview = ref<ChatImagePreviewState | null>(null)

  function openChatImagePreview(src: string, label: string) {
    preview.value = { src: src || '', label }
  }

  function closeChatImagePreview() {
    preview.value = null
  }

  return { preview, openChatImagePreview, closeChatImagePreview }
}
