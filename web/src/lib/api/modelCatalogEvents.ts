import { ref } from 'vue'

/** Successful saves invalidate model choices in this page and sibling tabs. */
export const modelCatalogRevision = ref(0)
const channelName = 'grasp:model-catalog'
let channel: BroadcastChannel | undefined
let listening = false
let lastNotice = ''

function receive(notice: string) {
  if (!notice || notice === lastNotice) return
  lastNotice = notice
  modelCatalogRevision.value++
}

export function ensureModelCatalogEvents() {
  if (listening || typeof window === 'undefined') return
  listening = true
  if (typeof BroadcastChannel === 'function') {
    try {
      channel = new BroadcastChannel(channelName)
      channel.onmessage = (event) => receive(typeof event.data === 'string' ? event.data : '')
      // Node-based renderers may expose BroadcastChannel too; it must not keep them alive.
      ;(channel as BroadcastChannel & { unref?: () => void }).unref?.()
    } catch { /* storage events remain available when channels are restricted */ }
  }
  window.addEventListener('storage', (event) => {
    if (event.key === channelName) receive(event.newValue || '')
  })
}

export function invalidateModelCatalog() {
  ensureModelCatalogEvents()
  const notice = `${Date.now()}:${Math.random()}`
  receive(notice)
  try { channel?.postMessage(notice) } catch { /* saving must not fail after the server accepted it */ }
  try { window.localStorage.setItem(channelName, notice) } catch { /* in-page refresh still works */ }
}
