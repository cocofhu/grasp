import { ref } from 'vue'
import type { ClarifyImage } from '@/lib/shared/types'
import {
  SITE_ATTACH_MAX_BYTES,
  SITE_ATTACH_MAX_MIB,
  filesFromClipboard,
  findOversizedAttachments,
  formatSelectRejectMessage,
  formatSendRejectMessage,
  attachmentDisplayName,
  readFilesAsAttachments,
} from '@/lib/shared/attachments'

export type AttachNotice = { kind: 'error' | 'ok'; text: string } | null

/** Shared paste/upload/preview/delete attachment logic (any type + 50 MiB gate). */
export function useImageAttachments(opts?: { maxBytes?: number; maxMiB?: number }) {
  const maxBytes = opts?.maxBytes ?? SITE_ATTACH_MAX_BYTES
  const maxMiB = opts?.maxMiB ?? SITE_ATTACH_MAX_MIB
  const attachments = ref<ClarifyImage[]>([])
  const fileInput = ref<HTMLInputElement | null>(null)
  const notice = ref<AttachNotice>(null)

  function setNotice(kind: 'error' | 'ok', text: string) {
    notice.value = { kind, text }
  }

  function clearNotice() {
    notice.value = null
  }

  function addFiles(files: ArrayLike<File> | null | undefined) {
    if (!files) return
    const { rejected, accepted } = readFilesAsAttachments(files, {
      maxBytes,
      onRead: ({ data, mimeType, name }) => attachments.value.push({ data, mimeType, name }),
    })
    if (rejected.length) {
      setNotice('error', formatSelectRejectMessage(rejected, maxMiB))
      return
    }
    if (accepted) clearNotice()
  }

  function onPickFiles(e: Event) {
    addFiles((e.target as HTMLInputElement).files)
    if (fileInput.value) fileInput.value.value = ''
  }

  function onPaste(e: ClipboardEvent) {
    const picked = filesFromClipboard(e)
    if (picked.length) {
      e.preventDefault()
      addFiles(picked)
    }
  }

  function removeAttachment(i: number) {
    attachments.value.splice(i, 1)
  }

  function clearAttachments() {
    attachments.value = []
  }

  function takeAttachments(): ClarifyImage[] {
    const imgs = attachments.value.slice()
    attachments.value = []
    return imgs
  }

  /** Returns oversized names if send should be blocked; empty = ok. */
  function validateForSend(images: ClarifyImage[] = attachments.value): string[] {
    return findOversizedAttachments(images, maxBytes).map((im, i) => attachmentDisplayName(im, i))
  }

  function blockSendIfOversized(images: ClarifyImage[] = attachments.value): boolean {
    const names = validateForSend(images)
    if (!names.length) return false
    setNotice('error', formatSendRejectMessage(names, maxMiB))
    return true
  }

  return {
    attachments,
    fileInput,
    notice,
    addFiles,
    onPickFiles,
    onPaste,
    removeAttachment,
    clearAttachments,
    takeAttachments,
    validateForSend,
    blockSendIfOversized,
    clearNotice,
    setNotice,
    maxBytes,
    maxMiB,
  }
}
