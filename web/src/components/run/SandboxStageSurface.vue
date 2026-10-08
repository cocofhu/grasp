<script setup lang="ts">
/**
 * Artifact-stage pane for one sandbox console surface: code-server, terminal, or container log.
 * Same endpoints and empty/unavailable copy as SandboxConsoleView; no navigation to the console page.
 */
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import HardLoadLayer from '@/components/run/HardLoadLayer.vue'
import RunSandboxPanel from '@/components/run/RunSandboxPanel.vue'
import { api } from '@/lib/api/api'
import { isAbortError } from '@/lib/run/liveLogRehydrate'

const props = withDefaults(
  defineProps<{
    kind: 'ide' | 'terminal' | 'log'
    sandbox?: { id: number; hasCodeServer?: boolean } | null
    loading?: boolean
    runId?: string
    nodeId?: string
    active?: boolean
  }>(),
  {
    sandbox: null,
    loading: false,
    runId: '',
    nodeId: '',
    active: false,
  },
)

const { t } = useI18n()
const IFRAME_BOOT_STUCK_MS = 60_000
const ideLoaded = ref(false)
const ideFrameKey = ref(0)

const ideReady = computed(() => !!props.sandbox?.id && props.sandbox.hasCodeServer === true)
const ideUnavailable = computed(() => props.kind === 'ide' && !props.loading && !ideReady.value)
const showIdeLoading = computed(
  () => props.kind === 'ide' && !!props.active && !ideUnavailable.value && !ideLoaded.value,
)

function onIdeLoad() {
  ideLoaded.value = true
}
function retryIde() {
  ideLoaded.value = false
  ideFrameKey.value += 1
}

const log = ref<{ content: string; live: boolean; found: boolean; error?: string } | null>(null)
const logLoading = ref(false)
let logGen = 0
let logAbort: AbortController | null = null

async function fetchLog() {
  const rid = String(props.runId || '').trim()
  const nid = String(props.nodeId || '').trim()
  if (!rid || !nid) {
    log.value = null
    logLoading.value = false
    return
  }
  logAbort?.abort()
  const gen = ++logGen
  logAbort = new AbortController()
  logLoading.value = true
  try {
    const next = await api.nodeSandboxLog(rid, nid, { signal: logAbort.signal })
    if (gen !== logGen) return
    log.value = next
  } catch (e) {
    if (gen !== logGen || isAbortError(e)) return
    log.value = {
      content: '',
      live: false,
      found: false,
      error: t('pages.sandboxConsole.logFailed'),
    }
  } finally {
    if (gen === logGen) logLoading.value = false
  }
}

const termHost = ref<HTMLElement | null>(null)
let term: Terminal | null = null
let fit: FitAddon | null = null
let ws: WebSocket | null = null
let ro: ResizeObserver | null = null
let lastCols = 0
let lastRows = 0
let termSandboxId = 0

function termVisible(): boolean {
  const el = termHost.value
  return !!props.active && !!el && el.clientWidth > 0 && el.clientHeight > 0
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
function disposeTerm() {
  ro?.disconnect()
  ro = null
  ws?.close()
  ws = null
  term?.dispose()
  term = null
  fit = null
  lastCols = 0
  lastRows = 0
}
function initTerminal() {
  const id = props.sandbox?.id
  if (!id || !termHost.value) return
  if (term && termSandboxId === id) {
    doFit()
    sendResize()
    return
  }
  disposeTerm()
  termSandboxId = id
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
  try {
    ws = new WebSocket(api.sandboxTerminalWsUrl(id))
  } catch {
    return
  }
  ws.binaryType = 'arraybuffer'
  ws.onopen = () => {
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
    term?.write(`\r\n\x1b[33m${t('pages.sandboxConsole.disconnected')}\x1b[0m\r\n`)
  }
  term.onData((d) => {
    if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'input', data: d }))
  })
  ro = new ResizeObserver(() => {
    doFit()
    sendResize()
  })
  ro.observe(termHost.value)
}

watch(
  () => [props.kind, props.active, props.sandbox?.id, props.runId, props.nodeId] as const,
  () => {
    if (props.kind === 'log' && props.active) void fetchLog()
    if (props.kind === 'terminal' && props.active) nextTick(() => initTerminal())
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  logAbort?.abort()
  logAbort = null
  logGen++
  disposeTerm()
})
</script>

<template>
  <div class="h-full min-h-0">
    <div
      v-if="kind === 'ide'"
      class="relative h-full"
      :aria-busy="showIdeLoading ? 'true' : 'false'"
      data-testid="react-artifact-ide-pane"
    >
      <iframe
        v-if="ideReady"
        :key="ideFrameKey"
        :src="api.sandboxIdeUrl(sandbox!.id)"
        class="h-full w-full border-0 bg-white"
        title="code-server"
        @load="onIdeLoad"
      />
      <div
        v-else-if="ideUnavailable"
        class="flex h-full items-center justify-center px-6 text-center text-sm text-txt3"
        data-testid="react-artifact-ide-unavailable"
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

    <div v-else-if="kind === 'terminal'" class="h-full bg-[#0b0e14] p-2" data-testid="react-artifact-terminal-pane">
      <div v-if="sandbox?.id" ref="termHost" class="h-full w-full" />
      <div
        v-else
        class="flex h-full items-center justify-center text-center text-[12px] text-[#cdd6f4]"
        data-testid="react-artifact-terminal-missing"
      >
        {{ loading ? t('pages.appPreview.loading') : t('pages.sandboxConsole.disconnected') }}
      </div>
    </div>

    <RunSandboxPanel
      v-else
      :loading="logLoading"
      :sbx-log="log"
      @refresh="fetchLog"
    />
  </div>
</template>
