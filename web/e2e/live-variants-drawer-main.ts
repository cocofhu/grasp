import '../src/styles/global.css'
import { createApp, defineComponent, h, provide, ref } from 'vue'
import LiveVariantCard from '../src/components/run/LiveVariantCard.vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import {
  createLiveStore, EMBED_LIVE_ACK_MESSAGE, EMBED_LIVE_CAPS_MESSAGE, EMBED_LIVE_CMD_MESSAGE,
  EMBED_LIVE_SESSIONS_MESSAGE, LIVE_CARD_HOST, parseEmbedLiveMessage,
  type LiveEvent, type LiveRef,
} from '../src/lib/inbox/liveVariants'

// Vite rewrites the iframe request internally; its browser URL remains the
// production-shaped embed route, so the key must also come from that run ID.
const key = new URLSearchParams(location.search).get('key') || location.pathname.match(/^\/embed\/runs\/live-e2e-([^/]+)\//)?.[1] || 'default'
const endpoint = '/__e2e/live/'
const live = createLiveStore()
const messages = ref<Array<{ id: string; text: string; live: LiveRef }>>([])
const error = ref('')
const text = ref('')
let revision = -1
let polling = false

initLocale()
setTheme('dark')
live.setEnabled(true)

function post(message: Record<string, unknown>) {
  window.parent.postMessage(message, location.origin)
}

async function refresh() {
  if (polling) return
  polling = true
  try {
    const state = await fetch(endpoint + 'state?key=' + encodeURIComponent(key)).then((response) => response.json())
    if (state.revision !== revision) {
      revision = state.revision
      const sessions = live.replaceAll(state.sessions)
      messages.value = state.messages
      post({ type: EMBED_LIVE_SESSIONS_MESSAGE, replace: true, sessions })
    }
  } finally {
    polling = false
  }
}

async function reply(body: { live?: LiveEvent; text?: string; liveCtx?: unknown }) {
  const response = await fetch(endpoint + 'reply?key=' + encodeURIComponent(key), {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
  })
  const result = await response.json()
  if (!response.ok) throw new Error(result.error || 'Live request failed')
  if (result.live) live.apply(result.live)
  await refresh()
  return result
}

window.addEventListener('message', (event) => {
  if (event.source !== window.parent || event.origin !== location.origin) return
  const message = parseEmbedLiveMessage(event.data)
  if (!message) return
  if (message.kind === 'state') {
    // This is the only writer of the drawer's views. Tests must exercise the
    // actual overlay postMessage path, including initial/reloaded selection.
    live.setView(message.sid, message.view)
    return
  }
  void reply({ live: message.event }).then(
    (result) => post({ type: EMBED_LIVE_ACK_MESSAGE, reqId: message.reqId, ok: true, session: result.live }),
    (reason: unknown) => post({ type: EMBED_LIVE_ACK_MESSAGE, reqId: message.reqId, ok: false, error: String(reason) }),
  )
})

const App = defineComponent({
  setup() {
    provide(LIVE_CARD_HOST, {
      store: live.store, interactive: true,
      command: (sid, cmd, variant) => post({ type: EMBED_LIVE_CMD_MESSAGE, sid, cmd, variant }),
    })
    async function sendChat() {
      try {
        await reply({ text: text.value, liveCtx: live.activeCtx() })
        text.value = ''
      } catch (reason) {
        error.value = String(reason)
      }
    }
    return () => h('main', { class: 'p-3', 'data-testid': 'live-drawer' }, [
      h('p', { class: 'text-sm text-txt' }, 'Live browser protocol fixture'),
      ...messages.value.map((message) => h(LiveVariantCard, { key: message.id, liveRef: message.live })),
      h('pre', { 'data-testid': 'live-active-context', class: 'text-xs text-txt2 whitespace-pre-wrap' }, JSON.stringify(live.activeCtx())),
      h('textarea', { 'data-testid': 'live-chat-input', class: 'w-full rounded bg-surface p-2 text-txt', value: text.value, onInput: (event: Event) => { text.value = (event.target as HTMLTextAreaElement).value } }),
      h('button', { 'data-testid': 'live-chat-send', class: 'rounded bg-accent px-3 py-1 text-white', onClick: () => void sendChat() }, 'Send'),
      h('p', { role: 'alert' }, error.value),
    ])
  },
})
createApp(App).use(i18n).mount('#app')
post({ type: 'grasp-embed:ready' })
post({ type: EMBED_LIVE_CAPS_MESSAGE, enabled: true })
void refresh()
const timer = setInterval(() => void refresh(), 60)
window.addEventListener('pagehide', () => clearInterval(timer), { once: true })
