import { ref } from 'vue'

export type ToastType = 'default' | 'success' | 'error' | 'warn'

export interface ToastItem {
  id: number
  message: string
  type: ToastType
  sticky?: boolean
  action?: ToastAction
}

export interface ToastAction {
  label: string
  run: () => void
}

let idCounter = 0
const toasts = ref<ToastItem[]>([])

export function useToast() {
  function dismiss(id: number) {
    toasts.value = toasts.value.filter((t) => t.id !== id)
  }

  function show(message: string, type: ToastType = 'default', opts?: { sticky?: boolean; action?: ToastAction }): number {
    const id = ++idCounter
    toasts.value.push({ id, message, type, sticky: !!opts?.sticky, action: opts?.action })
    if (!opts?.sticky) {
      setTimeout(
        () => {
          toasts.value = toasts.value.filter((t) => t.id !== id)
        },
        opts?.action ? 5000 : 2600,
      )
    }
    return id
  }

  function runAction(id: number) {
    const item = toasts.value.find((t) => t.id === id)
    dismiss(id)
    item?.action?.run()
  }

  function showSticky(message: string, type: ToastType = 'default'): number {
    return show(message, type, { sticky: true })
  }

  return {
    toasts,
    show,
    showSticky,
    dismiss,
    runAction,
    success: (message: string) => show(message, 'success'),
    error: (message: string) => show(message, 'error'),
    warn: (message: string) => show(message, 'warn'),
  }
}
