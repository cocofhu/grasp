import { ref, type Ref } from 'vue'
import { i18n } from '@/lib/shared/i18n'
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
  attachmentsReading,
  type ReadAttachment,
} from '@/lib/shared/attachments'

type AttachNotice = { kind: 'error' | 'ok'; text: string } | null

/** Shared paste/upload/preview/delete attachment logic (any type + 50 MiB gate). */
export function useImageAttachments(opts?: { maxBytes?: number; maxMiB?: number; attachments?: Ref<ClarifyImage[]>; retainUrl?: boolean }) {
  const maxBytes = opts?.maxBytes ?? SITE_ATTACH_MAX_BYTES
  const maxMiB = opts?.maxMiB ?? SITE_ATTACH_MAX_MIB
  const attachments = opts?.attachments ?? ref<ClarifyImage[]>([])
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
    if (files.length) clearNotice()
    const targets = new WeakMap<ReadAttachment, ClarifyImage[]>()
    const { rejected } = readFilesAsAttachments(files, {
      maxBytes,
      onStart: pending => {
        targets.set(pending, attachments.value)
        attachments.value.push(pending)
      },
      onRead: (ready, pending) => {
        const target = targets.get(pending)
        targets.delete(pending)
        const index = target?.indexOf(pending) ?? -1
        // Complete the original draft if the user switched chats in the meantime.
        // Removed/cleared files stay removed and never appear in another draft.
        if (target && index >= 0) {
          const { data, mimeType, name, sizeBytes } = ready
          target[index] = { data, mimeType, name, sizeBytes }
          if (opts?.retainUrl) target[index]!.url = ready.url
          if (target === attachments.value && !attachmentsReading(target) && notice.value?.text === i18n.global.t('common.attachments.reading')) clearNotice()
        }
      },
      onError: (name, pending) => {
        const target = targets.get(pending)
        targets.delete(pending)
        const index = target?.indexOf(pending) ?? -1
        if (!target || index < 0) return
        target.splice(index, 1)
        setNotice('error', i18n.global.t('common.attachments.readFailed', { name }))
      },
    })
    if (rejected.length) {
      setNotice('error', formatSelectRejectMessage(rejected, maxMiB))
      return
    }
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
    if (attachmentsReading(images)) {
      setNotice('error', i18n.global.t('common.attachments.reading'))
      return true
    }
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
