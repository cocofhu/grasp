<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, nextTick, watch, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import NovncPreviewPanel from '@/components/run/NovncPreviewPanel.vue'
import RefreshStrip from '@/components/run/RefreshStrip.vue'
import HardLoadLayer from '@/components/run/HardLoadLayer.vue'
import { isAbortError } from '@/lib/run/liveLogRehydrate'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { api, type SandboxView } from '@/lib/api/api'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { copyToClipboard } from '@/lib/shared/copyToClipboard'
import { useToast } from '@/lib/composables/useToast'
import { theme } from '@/lib/shared/theme'
import { EMBED_THEME_MESSAGE } from '@/lib/inbox/embedChat'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const toast = useToast()
const { isMobile } = useBreakpoint()
const showSessionPeek = ref(false)
const metaLoading = ref(true)
const id = Number(route.params.id)

const CONSOLE_TABS = ['terminal', 'ide', 'acp', 'novnc', 'log'] as const
type ConsoleTab = (typeof CONSOLE_TABS)[number]

function initialConsoleTab(): ConsoleTab {
  const q = route.query.tab
  if (typeof q !== 'string') return 'terminal'
  // Legacy ACP native bridge deep link → new unified ACP tab.
  if (q === 'acp-native') return 'acp'
  // Legacy embedded AgentChatTester ACP deep link → terminal (do not restore chat).
  if (q === 'acp') return 'terminal'
  if ((CONSOLE_TABS as readonly string[]).includes(q)) {
    return q as ConsoleTab
  }
  return 'terminal'
}

const sandbox = ref<SandboxView | null>(null)
const tab = ref<ConsoleTab>(initialConsoleTab())
// Lazily mount heavy panes on first open, then keep alive (v-show) so
// sessions survive tab switches — same pattern as the terminal pane.
const novncMounted = ref(false)
const ideMounted = ref(false)
const acpMounted = ref(false)
/** First iframe `load` — keep-alive tab switches must not replay loading. */
const ideLoaded = ref(false)
const acpLoaded = ref(false)
/** Bumped on stuck-retry so the iframe remounts without tearing down keep-alive on tab switch. */
const ideFrameKey = ref(0)
const acpFrameKey = ref(0)
/** Boot-class HardLoadLayer threshold (not the short REST 10s). */
const IFRAME_BOOT_STUCK_MS = 60_000
/** Must match sandbox web static/js/app/embed.js READY_MESSAGE. */
const AGENTCHAT_READY_MESSAGE = 'grasp:agentchat-ready'
const acpFrame = ref<HTMLIFrameElement | null>(null)
/** Theme baked into the iframe URL at mount; later switches go over postMessage so the frame never reloads. */
const acpFrameTheme = ref(theme.value)
const acpSrc = computed(() => `${api.sandboxBridgeUrl(id)}#theme=${acpFrameTheme.value}`)

const tabItems = computed(() => [
  { k: 'terminal', l: t('pages.sandboxConsole.tabs.terminal'), i: 'terminal' },
  { k: 'ide', l: t('pages.sandboxConsole.tabs.ide'), i: 'doc' },
  { k: 'acp', l: t('pages.sandboxConsole.tabs.acp'), i: 'robot' },
  { k: 'novnc', l: t('pages.sandboxConsole.tabs.novnc'), i: 'globe' },
  { k: 'log', l: t('pages.sandboxConsole.tabs.log'), i: 'doc' },
])

const sandboxTitle = computed(() => sandbox.value?.name || t('pages.sandboxConsole.fallbackName', { id }))
const showPassword = ref(false)
const passwordCopied = ref(false)

async function copyPassword() {
  const pw = sandbox.value?.password
  if (!pw) return
  const ok = await copyToClipboard(pw)
  if (!ok) {
    toast.error(t('common.toast.copyFailed'))
    return
  }
  passwordCopied.value = true
  window.setTimeout(() => { passwordCopied.value = false }, 1500)
}

// Raw container logs (docker logs): live while running, else archived snapshot.
const log = ref<{ content: string; live: boolean; found: boolean } | null>(null)
const logLoading = ref(false)
const logError = ref<string | null>(null)
let logGen = 0
let logAbort: AbortController | null = null
async function fetchLog() {
  logAbort?.abort()
  const gen = ++logGen
  logAbort = new AbortController()
  logLoading.value = true
  logError.value = null
  try {
    const next = await api.sandboxLog(id, { signal: logAbort.signal })
    if (gen !== logGen) return
    log.value = next
  } catch (e) {
    if (gen !== logGen || isAbortError(e) || logAbort.signal.aborted) return
    logError.value = t('pages.sandboxConsole.logFailed')
  } finally {
    if (gen === logGen) logLoading.value = false
  }
}
const termHost = ref<HTMLElement | null>(null)
const termStatus = ref<'connecting' | 'open' | 'closed'>('connecting')

const termStatusLabel = computed(() => {
  if (termStatus.value === 'open') return t('pages.sandboxConsole.terminalStatus.open')
  if (termStatus.value === 'closed') return t('pages.sandboxConsole.terminalStatus.closed')
  return t('pages.sandboxConsole.terminalStatus.connecting')
})

let term: Terminal | null = null
let fit: FitAddon | null = null
let ws: WebSocket | null = null
let ro: ResizeObserver | null = null
// Last dimensions sent to the PTY. Used to suppress redundant resize frames:
// every SIGWINCH makes the shell redraw its prompt line, so spurious resizes
// (e.g. the ResizeObserver firing when the terminal is hidden/shown by a tab
// switch) would litter the buffer with duplicated prompt fragments.
let lastCols = 0
let lastRows = 0

async function loadMeta() {
  metaLoading.value = true
  try {
    // Prefer getSandbox so password is available even if list is filtered/cached.
    sandbox.value = await api.getSandbox(id)
  } catch {
    try {
      const list = await api.listSandboxes()
      sandbox.value = list.find((s) => s.id === id) || null
    } catch {
      sandbox.value = null
    }
  } finally {
    metaLoading.value = false
  }
}

const sessionUptime = computed(() => {
  const raw = sandbox.value?.createdAt
  if (!raw) return ''
  const ms = Date.now() - new Date(raw).getTime()
  if (!Number.isFinite(ms) || ms < 0) return ''
  const minutes = Math.floor(ms / 60_000)
  if (minutes < 60) return `${minutes}m`
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`
})

const endpointPorts = computed(() => {
  const ep = sandbox.value?.endpoints
  if (!ep) return ''
  return Object.keys(ep).filter(Boolean).join(', ')
})

function initTerminal() {
  if (term || !termHost.value) return
  term = new Terminal({
    fontSize: 12.5,
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
    cursorBlink: true,
    theme: { background: '#0b0e14', foreground: '#cdd6f4' },
  })
  fit = new FitAddon()
  term.loadAddon(fit)
  term.open(termHost.value)
  doFit()

  ws = new WebSocket(api.sandboxTerminalWsUrl(id))
  ws.binaryType = 'arraybuffer'
  ws.onopen = () => {
    termStatus.value = 'open'
    sendResize()
  }
  ws.onmessage = (ev) => {
    if (typeof ev.data === 'string') {
      try {
        const m = JSON.parse(ev.data)
        if (m.type === 'error') term?.write(`\r\n\x1b[31m${m.data}\x1b[0m\r\n`)
        return
      } catch {
        term?.write(ev.data)
        return
      }
    }
    term?.write(new Uint8Array(ev.data as ArrayBuffer))
  }
  ws.onclose = () => {
    termStatus.value = 'closed'
    term?.write(`\r\n\x1b[33m${t('pages.sandboxConsole.disconnected')}\x1b[0m\r\n`)
  }

  term.onData((d) => {
    if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'input', data: d }))
  })
}

// termVisible reports whether the terminal pane is actually laid out. When a
// tab switch hides it via v-show (display:none) the host collapses to 0×0;
// fitting/resizing then would compute a bogus geometry and reflow the shell.
function termVisible(): boolean {
  const el = termHost.value
  return !!el && el.clientWidth > 0 && el.clientHeight > 0
}
function doFit() {
  if (!fit || !termVisible()) return
  try {
    fit.fit()
  } catch {
    /* host not laid out yet */
  }
}
function sendResize() {
  if (!term || ws?.readyState !== WebSocket.OPEN || !termVisible()) return
  if (term.cols === lastCols && term.rows === lastRows) return
  lastCols = term.cols
  lastRows = term.rows
  ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
}

const ideUnavailable = computed(
  () => !metaLoading.value && !!sandbox.value && sandbox.value.hasCodeServer === false,
)
const acpUnavailable = computed(
  () => !metaLoading.value && !!sandbox.value && sandbox.value.hasAcp === false,
)
const showIdeLoading = computed(
  () => tab.value === 'ide' && !ideUnavailable.value && !ideLoaded.value,
)
const showAcpLoading = computed(
  () => tab.value === 'acp' && !acpUnavailable.value && !acpLoaded.value,
)

function onIdeLoad() {
  ideLoaded.value = true
}
function onAcpLoad() {
  acpLoaded.value = true
}
function retryIde() {
  ideLoaded.value = false
  ideFrameKey.value += 1
}
function retryAcp() {
  acpLoaded.value = false
  acpFrameTheme.value = theme.value
  acpFrameKey.value += 1
}
function postAcpTheme() {
  acpFrame.value?.contentWindow?.postMessage({ type: EMBED_THEME_MESSAGE, theme: theme.value }, window.location.origin)
}
// AgentChat announces itself once its UI is wired; iframe `load` also waits
// for every subresource, so it stays as the fallback only.
function onAcpMessage(e: MessageEvent) {
  const win = acpFrame.value?.contentWindow
  if (!win || e.source !== win || e.origin !== window.location.origin) return
  if ((e.data as { type?: unknown } | null)?.type !== AGENTCHAT_READY_MESSAGE) return
  acpLoaded.value = true
  postAcpTheme()
}
watch(theme, postAcpTheme)

watch(tab, (t) => {
  if (t === 'terminal') nextTick(() => { initTerminal(); doFit() })
  if (t === 'ide') ideMounted.value = true
  if (t === 'acp') acpMounted.value = true
  if (t === 'novnc') novncMounted.value = true
  if (t === 'log') fetchLog()
})

onMounted(async () => {
  window.addEventListener('message', onAcpMessage)
  tab.value = initialConsoleTab()
  if (isMobile.value) {
    await loadMeta()
    return
  }
  if (tab.value === 'ide') ideMounted.value = true
  if (tab.value === 'acp') acpMounted.value = true
  if (tab.value === 'novnc') novncMounted.value = true
  await loadMeta()
  nextTick(() => {
    initTerminal()
    ro = new ResizeObserver(() => {
      doFit()
      sendResize()
    })
    if (termHost.value) ro.observe(termHost.value)
  })
})

onBeforeUnmount(() => {
  window.removeEventListener('message', onAcpMessage)
  logAbort?.abort()
  logAbort = null
  logGen++
  ro?.disconnect()
  ws?.close()
  term?.dispose()
})
</script>

<template>
  <div
    v-if="isMobile"
    class="flex h-full min-h-0 flex-col overflow-auto bg-base"
    data-testid="sandbox-console-mobile"
  >
    <div class="flex shrink-0 items-center gap-2 border-b border-line px-3 py-2">
      <button
        type="button"
        class="flex min-h-11 items-center gap-1 text-[13px] text-txt3 hover:text-txt"
        @click="router.push('/sandboxes')"
      >
        <Icon name="arrow-left" :size="15" />{{ t('pages.sandboxConsole.back') }}
      </button>
      <span class="truncate text-[13px] font-medium text-txt">{{ sandboxTitle }}</span>
    </div>
    <div class="flex flex-1 flex-col items-center px-5 py-8 text-center">
      <div class="rounded-lg mb-3 flex h-10 w-10 items-center justify-center border border-info/35 bg-info/10 text-info">⌁</div>
      <h3 class="text-[14px] font-semibold text-txt">{{ t('pages.sandboxConsole.mobile.title') }}</h3>
      <p class="mt-2 max-w-[32ch] text-[12.5px] leading-relaxed text-txt2">{{ t('pages.sandboxConsole.mobile.desc') }}</p>
      <button
        type="button"
        class="rounded-md mt-4 min-h-11 border border-line bg-transparent px-4 text-[13px] text-txt2 hover:border-accent hover:text-txt"
        data-testid="sandbox-console-peek"
        @click="showSessionPeek = !showSessionPeek"
      >
        {{ t('pages.sandboxConsole.mobile.peek') }}
      </button>
    </div>
    <div
      v-if="showSessionPeek"
      class="rounded-lg mx-4 mb-6 border border-line bg-surface p-3 text-left"
      data-testid="sandbox-console-summary"
    >
      <div class="mb-2 text-[11px] uppercase tracking-wider text-txt3">{{ t('pages.sandboxConsole.mobile.summaryLabel') }}</div>
      <p v-if="metaLoading" class="text-[12px] text-txt3">{{ t('pages.sandboxConsole.mobile.loading') }}</p>
      <p v-else-if="!sandbox" class="text-[12px] text-txt3">{{ t('pages.sandboxConsole.mobile.empty') }}</p>
      <pre
        v-else
        class="whitespace-pre-wrap break-words font-mono text-[12px] leading-relaxed text-txt2"
      >{{ t('pages.sandboxConsole.mobile.name') }}: {{ sandbox.name || sandboxTitle }}
{{ t('pages.sandboxConsole.mobile.status') }}: {{ sandbox.status }}
{{ t('pages.sandboxConsole.mobile.containerStatus') }}: {{ sandbox.containerStatus }}
{{ t('pages.sandboxConsole.mobile.endpoints') }}: {{ endpointPorts || '—' }}
{{ t('pages.sandboxConsole.mobile.uptime') }}: {{ sessionUptime || '—' }}

{{ t('pages.sandboxConsole.mobile.desktopHint') }}</pre>
    </div>
  </div>
  <div v-else class="flex h-full flex-col">
    <div class="flex h-12 shrink-0 items-center gap-3 border-b border-line px-4">
      <button class="flex items-center gap-1 text-[13px] text-txt3 hover:text-txt" @click="router.push('/sandboxes')">
        <Icon name="arrow-left" :size="15" />{{ t('pages.sandboxConsole.back') }}
      </button>
      <span class="text-[13px] font-medium text-txt">{{ sandboxTitle }}</span>
      <span v-if="sandbox" class="chip border-line text-txt3">{{ sandbox.profile }}</span>
      <div
        v-if="sandbox?.password"
        class="ml-2 flex max-w-[min(28rem,50vw)] items-center gap-1.5 rounded border border-line bg-surface px-2 py-0.5 text-[11px] text-txt3"
        :title="t('pages.sandboxConsole.passwordHint')"
      >
        <span class="shrink-0">{{ t('pages.sandboxConsole.password') }}</span>
        <code class="truncate font-mono text-txt2">{{ showPassword ? sandbox.password : '••••••••' }}</code>
        <button type="button" class="shrink-0 text-txt3 hover:text-txt" @click="showPassword = !showPassword">
          {{ showPassword ? t('pages.sandboxConsole.hidePassword') : t('pages.sandboxConsole.showPassword') }}
        </button>
        <button type="button" class="shrink-0 text-accent hover:underline" @click="copyPassword">
          {{ passwordCopied ? t('pages.sandboxConsole.copied') : t('pages.sandboxConsole.copyPassword') }}
        </button>
      </div>

      <div class="ml-4 flex gap-1">
        <button
          v-for="item in tabItems"
          :key="item.k"
          class="flex items-center gap-1 rounded px-3 py-1 text-[12px] transition"
          :class="tab === item.k ? 'bg-accent-dim text-txt' : 'text-txt3 hover:text-txt2'"
          @click="tab = item.k as any"
        ><Icon :name="item.i" :size="13" />{{ item.l }}</button>
      </div>

      <span
        v-if="tab === 'terminal'"
        class="ml-auto chip"
        :class="termStatus === 'open' ? 'border-ok/30 text-ok' : termStatus === 'closed' ? 'border-err/30 text-err' : 'border-line text-txt3'"
      >{{ termStatusLabel }}</span>
    </div>

    <div class="min-h-0 flex-1">
      <!-- terminal kept mounted; toggled with the IDE iframe -->
      <div v-show="tab === 'terminal'" class="h-full bg-[#0b0e14] p-2">
        <div ref="termHost" class="h-full w-full" />
      </div>

      <div
        v-show="tab === 'ide'"
        class="relative h-full"
        :aria-busy="showIdeLoading ? 'true' : 'false'"
        data-testid="sandbox-console-ide-pane"
      >
        <iframe
          v-if="ideMounted && sandbox?.hasCodeServer"
          :key="ideFrameKey"
          :src="api.sandboxIdeUrl(id)"
          class="h-full w-full border-0 bg-white"
          title="code-server"
          @load="onIdeLoad"
        />
        <div
          v-else-if="ideMounted && ideUnavailable"
          class="flex h-full items-center justify-center px-6 text-center text-sm text-txt3"
          data-testid="sandbox-console-ide-unavailable"
        >
          {{ t('pages.sandboxConsole.ideUnavailable') }}
        </div>
        <HardLoadLayer
          v-if="showIdeLoading"
          overlay
          :stuck-after-ms="IFRAME_BOOT_STUCK_MS"
          :stage="t('pages.sandboxConsole.ideLoading')"
          @retry="retryIde"
        />
      </div>

      <!-- ACP: in-container acp-bridge web UI (8765); was acp-native -->
      <div
        v-show="tab === 'acp'"
        class="relative h-full"
        :aria-busy="showAcpLoading ? 'true' : 'false'"
        data-testid="sandbox-console-acp-pane"
      >
        <iframe
          v-if="acpMounted && sandbox?.hasAcp"
          ref="acpFrame"
          :key="acpFrameKey"
          :src="acpSrc"
          class="h-full w-full border-0 bg-base"
          title="ACP bridge"
          @load="onAcpLoad"
        />
        <div
          v-else-if="acpMounted && acpUnavailable"
          class="flex h-full items-center justify-center px-6 text-center text-sm text-txt3"
          data-testid="sandbox-console-acp-unavailable"
        >
          {{ t('pages.sandboxConsole.acpNativeUnavailable') }}
        </div>
        <HardLoadLayer
          v-if="showAcpLoading"
          overlay
          :stuck-after-ms="IFRAME_BOOT_STUCK_MS"
          :stage="t('pages.sandboxConsole.acpLoading')"
          @retry="retryAcp"
        />
      </div>

      <!-- noVNC browser: sandbox-scoped desktop; lazy mount + keep-alive -->
      <div v-show="tab === 'novnc'" class="h-full">
        <NovncPreviewPanel
          v-if="novncMounted"
          :sandbox-id="id"
          fill
        />
      </div>

      <!-- container logs (docker logs): live or archived snapshot -->
      <div
        v-show="tab === 'log'"
        class="relative flex h-full flex-col"
        :aria-busy="logLoading ? 'true' : 'false'"
      >
        <RefreshStrip v-if="logLoading && log?.content" :message="t('pages.sandboxConsole.logRefreshing')" />
        <div class="flex items-center gap-2 border-b border-line px-3 py-1.5 text-[11px] text-txt3">
          <Icon name="terminal" :size="12" />
          <span>{{ t('pages.sandboxConsole.logTitle') }}</span>
          <span v-if="log?.live" class="inline-flex items-center rounded-full border border-accent/40 bg-accent-dim px-2 py-0.5 text-[10px] text-accent">{{ t('pages.runDetail.sandboxLog.live') }}</span>
          <span v-else-if="log?.found" class="chip">{{ t('pages.runDetail.sandboxLog.archived') }}</span>
          <div class="flex-1" />
          <button class="text-txt3 hover:text-txt" :title="t('common.buttons.refresh')" @click="fetchLog"><Icon name="refresh" :size="12" /></button>
        </div>
        <div class="scroll-area relative min-h-0 flex-1 overflow-auto bg-[#0b0e14] p-3">
          <div
            v-if="logError"
            class="mb-2 rounded-lg border border-err/30 bg-err/10 px-3 py-2.5 text-[12px] text-err"
            role="alert"
            data-testid="sandbox-console-log-error"
          >
            <p>{{ logError }}</p>
            <button
              type="button"
              class="rounded-lg mt-2 inline-flex min-h-11 items-center border border-line px-3 text-[12px] text-txt"
              @click="fetchLog"
            >
              {{ t('common.chatImage.retry') }}
            </button>
          </div>
          <pre v-if="log?.content" class="whitespace-pre-wrap break-all font-mono text-[11px] leading-relaxed text-[#cdd6f4]">{{ log.content }}</pre>
          <HardLoadLayer
            v-else-if="logLoading && !logError"
            :overlay="false"
            :stuck-after-ms="10_000"
            :stage="t('common.loading.label')"
            @retry="fetchLog"
          />
          <div v-else-if="!logError" class="flex h-full items-center justify-center text-center text-[12px] text-[#cdd6f4]" data-testid="sandbox-console-log-empty">
            {{ t('pages.sandboxConsole.logEmpty') }}
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
