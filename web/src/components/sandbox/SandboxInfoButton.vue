<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import { api } from '@/lib/api/api'
import type { SoloSession } from '@/lib/api/apiTypes'
import { copyToClipboard } from '@/lib/shared/copyToClipboard'
import { formatBytes, relTime } from '@/lib/shared/format'
import { sessionRepos } from '@/lib/solo/sessionRepos'
import { sandboxDisplayState, sandboxLifecycle, type SandboxAction } from '@/lib/sandbox/sandboxDisplayState'
import { SANDBOX_WORKSPACE } from '@/lib/sandbox/sshConnect'
import { useSSHGateway } from '@/lib/sandbox/useSSHGateway'
import { formatRemaining } from './idleCountdown'
import SandboxIdeMenu from './SandboxIdeMenu.vue'

const props = withDefaults(defineProps<{
  session: SoloSession
  busy: boolean
  queued?: number
  paused: boolean
  waking: boolean
  /** Embedded views show state only. */
  readonly?: boolean
  /** The action whose request is in flight. */
  pending?: SandboxAction
  keepAliveSaving?: boolean
}>(), { queued: 0, readonly: false, pending: null, keepAliveSaving: false })

const emit = defineEmits<{
  action: [action: Exclude<SandboxAction, null>]
  keepAlive: [value: boolean]
  /** The panel opened; the parent should refresh the live session view. */
  refresh: []
}>()

const { t } = useI18n()
const gateway = useSSHGateway()

const open = ref(false)
const detailsOpen = ref(false)
const detailsId = useId()
const trigger = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const panelStyle = ref<Record<string, string>>({})
const sheet = ref(false)
const now = ref(Date.now())
const copied = ref('')
let tick: ReturnType<typeof setInterval> | undefined
let refreshTimer: ReturnType<typeof setInterval> | undefined
/** IDE connections change outside this page, so an open panel re-reads the session. */
const REFRESH_MS = 10_000
let copiedTimer: ReturnType<typeof setTimeout> | undefined

const sandboxId = computed(() => props.session.sandbox?.id || props.session.sandboxId || 0)
const display = computed(() => sandboxDisplayState({
  status: props.session.status,
  sandboxStatus: props.session.sandbox?.status,
  paused: props.paused,
  waking: props.waking,
  busy: props.busy,
  queued: props.queued,
}))
const lifecycle = computed(() => sandboxLifecycle(props.session))
const stateLabel = computed(() => t(`pages.solo.sandboxInfo.state.${display.value.kind}`))
const environmentRunning = computed(() => display.value.kind === 'running' || display.value.kind === 'busy')

const duration = computed(() => {
  const { reclaim, destroyAt } = lifecycle.value
  if (display.value.kind !== 'running' || reclaim !== 'countdown' || !destroyAt) return ''
  return formatRemaining(destroyAt.getTime() - now.value, '', (hours, minutes, seconds) => {
    const unit = hours ? 'hours' : minutes ? 'minutes' : 'seconds'
    return t(`pages.solo.sandboxInfo.lifecycle.duration.${unit}`, { hours, minutes, seconds })
  })
})
const reclaimText = computed(() => {
  if (!environmentRunning.value) return ''
  const l = lifecycle.value
  switch (l.reclaim) {
    case 'exempt': return t('pages.solo.sandboxInfo.lifecycle.exempt')
    case 'keepAlive': return t('pages.solo.sandboxInfo.lifecycle.keptAlive')
    case 'connected': return t('pages.solo.sandboxInfo.lifecycle.connected')
    case 'countdown':
      if (display.value.kind === 'busy') return t('pages.solo.sandboxInfo.lifecycle.afterTurn')
      return duration.value
        ? t('pages.solo.sandboxInfo.lifecycle.countdown', { time: duration.value })
        : t('pages.solo.sandboxInfo.lifecycle.soon')
    default:
      return display.value.kind === 'busy' && l.kind === 'snapshot'
        ? t('pages.solo.sandboxInfo.lifecycle.afterTurn')
        : ''
  }
})
const showRuntimeStatus = computed(() => !!(reclaimText.value || lifecycle.value.connections))
const kindText = computed(() => {
  if (lifecycle.value.kind === 'snapshot') return t('pages.solo.sandboxInfo.lifecycle.kindSnapshot')
  if (lifecycle.value.kind === 'files') return t('pages.solo.sandboxInfo.lifecycle.kindFiles')
  return ''
})
const keepAliveVisible = computed(() => !props.readonly && lifecycle.value.kind === 'snapshot' && !props.session.idleReclaimExempt)
const backupText = computed(() => {
  const { backupAt, backupSize } = props.session
  if (!backupAt) return ''
  const age = relTime(backupAt)
  return backupSize && backupSize > 0 ? `${age} · ${formatBytes(backupSize)}` : age
})

const repos = computed(() => sessionRepos(props.session))
const repoText = computed(() => {
  if (!repos.value.length) return t('pages.solo.sandboxInfo.details.emptyRepo')
  const names = repos.value.map((r) => r.name).join(', ')
  return props.session.branch ? `${names} · ${props.session.branch}` : names
})

const ssh = computed(() => gateway.info.value)
const sshVisible = computed(() => !!ssh.value?.enabled && sandboxId.value > 0)
const canUseSSH = computed(() => sshVisible.value && ssh.value?.allowed !== false && ssh.value?.hasKey === true)
/** Why local IDE connections are unavailable right now, or '' when they work. */
const ideBlocked = computed(() => {
  switch (display.value.kind) {
    case 'preparing':
    case 'stopping':
    case 'stopped':
    case 'closed':
    case 'error':
      return t(`pages.solo.sandboxInfo.ide.unavailable.${display.value.kind}`)
    default:
      return ''
  }
})
const ideWakes = computed(() => display.value.kind === 'paused' || display.value.kind === 'waking')

const actionLabel = computed(() => display.value.action ? t(`pages.solo.sandboxInfo.action.${display.value.action}`) : '')
const actionIcon = computed(() => ({ stop: 'stop', wake: 'play', resume: 'play', restart: 'refresh' } as const)[display.value.action || 'stop'])
const webIdeUrl = computed(() => environmentRunning.value && props.session.sandbox?.hasCodeServer && sandboxId.value
  ? api.sandboxIdeUrl(sandboxId.value)
  : '')

function place() {
  const el = trigger.value
  if (!el) return
  sheet.value = window.innerWidth < 640
  if (sheet.value) {
    panelStyle.value = {}
    return
  }
  const rect = el.getBoundingClientRect()
  const width = Math.min(360, window.innerWidth - 16)
  const left = Math.max(8, Math.min(rect.right - width, window.innerWidth - width - 8))
  panelStyle.value = {
    top: `${Math.round(rect.bottom + 6)}px`,
    left: `${Math.round(left)}px`,
    width: `${width}px`,
    maxHeight: `${Math.max(240, window.innerHeight - rect.bottom - 18)}px`,
  }
}

async function show() {
  detailsOpen.value = false
  open.value = true
  now.value = Date.now()
  emit('refresh')
  void gateway.load(true)
  tick = setInterval(() => { now.value = Date.now() }, 1000)
  refreshTimer = setInterval(() => emit('refresh'), REFRESH_MS)
  await nextTick()
  place()
  panel.value?.focus()
}

function hide(returnFocus = false) {
  if (!open.value) return
  open.value = false
  if (tick) clearInterval(tick)
  if (refreshTimer) clearInterval(refreshTimer)
  tick = undefined
  refreshTimer = undefined
  if (returnFocus) trigger.value?.focus()
}

function toggle() {
  if (open.value) hide()
  else void show()
}

function onDocPointer(e: Event) {
  const target = e.target as Node | null
  if (!target || trigger.value?.contains(target) || panel.value?.contains(target)) return
  // The IDE picker is teleported to body, so a choice inside it is outside this panel.
  const el = target instanceof Element ? target : target.parentElement
  if (el?.closest('[data-testid="sandbox-ide-picker-panel"], [data-testid="sandbox-ide-details"]')) return
  hide()
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && open.value) {
    e.stopPropagation()
    hide(true)
  }
}

watch(open, (value) => {
  if (value) {
    document.addEventListener('pointerdown', onDocPointer, true)
    window.addEventListener('resize', place)
  } else {
    document.removeEventListener('pointerdown', onDocPointer, true)
    window.removeEventListener('resize', place)
  }
})

onBeforeUnmount(() => {
  hide()
  document.removeEventListener('pointerdown', onDocPointer, true)
  window.removeEventListener('resize', place)
  if (copiedTimer) clearTimeout(copiedTimer)
})

async function copy(key: string, text: string) {
  if (!(await copyToClipboard(text))) return
  copied.value = key
  if (copiedTimer) clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => { copied.value = '' }, 1500)
}

function runAction() {
  if (display.value.action && !display.value.actionDisabled) emit('action', display.value.action)
}

/** The IDE link opens the app; a paused sandbox starts waking at the same time. */
function onIDELaunch() {
  if (display.value.kind === 'paused' && !props.readonly) emit('action', 'wake')
}
</script>

<template>
  <div class="sandbox-info" data-testid="sandbox-info">
    <button
      ref="trigger"
      type="button"
      class="sandbox-info-trigger"
      data-testid="sandbox-info-toggle"
      :aria-label="t('pages.solo.sandboxInfo.button')"
      :title="`${t('pages.solo.sandboxInfo.button')} · ${stateLabel}`"
      aria-haspopup="dialog"
      :aria-expanded="open ? 'true' : 'false'"
      :aria-pressed="open"
      @click="toggle"
    >
      <Icon name="info" :size="16" aria-hidden="true" />
      <span class="sandbox-info-dot" :data-tone="display.tone" data-testid="sandbox-info-dot" aria-hidden="true" />
    </button>

    <Teleport to="body">
      <div v-if="open && sheet" class="sandbox-info-scrim" aria-hidden="true" />
      <div
        v-if="open"
        ref="panel"
        class="sandbox-info-panel"
        :class="{ 'is-sheet': sheet }"
        role="dialog"
        :aria-label="t('pages.solo.sandboxInfo.button')"
        tabindex="-1"
        :style="panelStyle"
        data-testid="sandbox-info-panel"
        @keydown="onKeydown"
      >
        <div class="sandbox-info-body">
          <header class="sandbox-info-head">
            <div class="min-w-0">
              <h2
                class="truncate text-[14px] font-semibold leading-tight text-txt"
                :class="{ 'font-mono': session.sandbox?.name }"
                :title="session.sandbox?.name"
              >
                {{ session.sandbox?.name || t('pages.solo.sandboxInfo.title') }}
              </h2>
              <p v-if="sandboxId || kindText" class="sandbox-info-meta">
                <span v-if="sandboxId" class="font-mono">#{{ sandboxId }}</span>
                <span v-if="sandboxId && kindText" aria-hidden="true">·</span>
                <span v-if="kindText">{{ kindText }}</span>
              </p>
            </div>
            <span class="sandbox-info-status">
              <span class="sandbox-info-dot is-inline" :data-tone="display.tone" aria-hidden="true" />
              <span data-testid="sandbox-info-state">{{ stateLabel }}</span>
            </span>
          </header>
          <p v-if="display.step" class="sandbox-info-note mt-2">{{ t(`pages.solo.sandboxInfo.step.${display.step}`) }}</p>
          <p v-if="display.kind === 'error' && (session.error || session.sandbox?.error)" class="mt-2 break-words text-xs text-err" data-testid="sandbox-info-error">
            {{ session.error || session.sandbox?.error }}
          </p>

          <div v-if="sshVisible || webIdeUrl" class="sandbox-info-connect">
            <div v-if="sshVisible && ssh" class="min-w-0" data-testid="sandbox-info-ide">
              <p v-if="ssh.allowed === false" class="sandbox-info-note">{{ t('pages.solo.sandboxInfo.ide.adminsOnly') }}</p>
              <div v-else-if="!ssh.hasKey" class="sandbox-info-note flex flex-wrap items-center gap-x-2" data-testid="sandbox-info-no-key">
                <span>{{ t('pages.solo.sandboxInfo.ide.noKey') }}</span>
                <RouterLink to="/settings/ssh-keys" class="sandbox-info-link" @click="hide()">{{ t('pages.solo.sandboxInfo.ide.addKey') }}</RouterLink>
              </div>
              <SandboxIdeMenu v-else :sandbox-id="sandboxId" :info="ssh" :blocked="ideBlocked" @launch="onIDELaunch" />
            </div>
            <a v-if="webIdeUrl" :href="webIdeUrl" target="_blank" rel="noopener" class="sandbox-info-web-ide" data-testid="sandbox-info-web-ide">
              <Icon name="stage-ide" :size="14" aria-hidden="true" />{{ t('pages.solo.sandboxInfo.webIde') }}
              <Icon name="arrow-right" :size="11" class="-rotate-45 text-txt3" aria-hidden="true" />
            </a>
            <p v-if="canUseSSH && ideBlocked" class="sandbox-info-note basis-full" data-testid="sandbox-info-ide-blocked">{{ ideBlocked }}</p>
            <p v-else-if="canUseSSH && ideWakes" class="sandbox-info-note basis-full">{{ t('pages.solo.sandboxInfo.ide.pausedHint') }}</p>
          </div>

          <section v-if="showRuntimeStatus || keepAliveVisible || backupText" class="sandbox-info-runtime" data-testid="sandbox-info-lifecycle">
            <div v-if="showRuntimeStatus">
              <div v-if="reclaimText" class="sandbox-info-runtime-row">
                <span class="sandbox-info-row-label">
                  {{ lifecycle.reclaim === 'exempt' ? t('pages.solo.sandboxInfo.lifecycle.idlePolicy') : t('pages.solo.sandboxInfo.lifecycle.autoPause') }}
                </span>
                <span class="text-right font-medium tabular-nums" data-testid="sandbox-info-reclaim">{{ reclaimText }}</span>
              </div>
              <p v-if="lifecycle.connections" class="sandbox-info-note">
                <span data-testid="sandbox-info-connections">{{ t('pages.solo.sandboxInfo.ide.connections', lifecycle.connections) }}</span>
                <template v-if="lifecycle.reclaim === 'connected' && environmentRunning"> · {{ t('pages.solo.sandboxInfo.lifecycle.afterDisconnect') }}</template>
              </p>
            </div>
            <label v-if="keepAliveVisible" class="sandbox-info-runtime-row">
              <span class="sandbox-info-row-label">{{ t('pages.solo.sandboxInfo.lifecycle.keepAlive') }}</span>
              <AppSwitch
                :model-value="!!session.keepAlive"
                :disabled="keepAliveSaving"
                :aria-label="t('pages.solo.sandboxInfo.lifecycle.keepAlive')"
                data-testid="sandbox-info-keep-alive"
                @update:model-value="emit('keepAlive', $event)"
              />
            </label>
            <div v-if="backupText" class="sandbox-info-runtime-row">
              <span class="sandbox-info-row-label">{{ t('pages.solo.sandboxInfo.lifecycle.backup') }}</span>
              <span class="text-right tabular-nums text-txt2">{{ backupText }}</span>
            </div>
          </section>
        </div>

        <footer class="sandbox-info-footer">
          <button
            type="button"
            class="sandbox-info-details-toggle"
            :aria-expanded="detailsOpen"
            :aria-controls="detailsId"
            data-testid="sandbox-info-details-toggle"
            @click="detailsOpen = !detailsOpen"
          >
            <Icon :name="detailsOpen ? 'chevron-down' : 'chevron-right'" :size="14" aria-hidden="true" />
            {{ t('pages.solo.sandboxInfo.details.title') }}
          </button>
          <AppButton
            v-if="!readonly && display.action"
            size="sm"
            :variant="display.action === 'stop' ? 'ghost' : 'primary'"
            :class="{ 'sandbox-info-stop': display.action === 'stop' }"
            :icon="actionIcon"
            :loading="pending === display.action"
            :disabled="display.actionDisabled"
            :title="display.actionDisabled ? t('pages.solo.sandboxInfo.action.stopBusy') : undefined"
            data-testid="sandbox-info-action"
            @click="runAction"
          >{{ actionLabel }}</AppButton>
        </footer>
        <dl v-show="detailsOpen" :id="detailsId" class="sandbox-info-rows" data-testid="sandbox-info-details">
          <dt>{{ t('pages.solo.sandboxInfo.details.agent') }}</dt>
          <dd>{{ session.agentName }}</dd>
          <dt>{{ t('pages.solo.sandboxInfo.details.repo') }}</dt>
          <dd class="truncate" :title="repoText">{{ repoText }}</dd>
          <dt>{{ t('pages.solo.sandboxInfo.details.workdir') }}</dt>
          <dd class="flex items-center gap-1">
            <span class="font-mono">{{ SANDBOX_WORKSPACE }}</span>
            <button type="button" class="sandbox-info-copy" :aria-label="t('pages.solo.sandboxInfo.copy')" @click="copy('workdir', SANDBOX_WORKSPACE)">
              <Icon :name="copied === 'workdir' ? 'check' : 'copy'" :size="12" aria-hidden="true" />
            </button>
          </dd>
          <template v-if="lifecycle.modelActiveAt">
            <dt>{{ t('pages.solo.sandboxInfo.lifecycle.lastModel') }}</dt>
            <dd :title="t('pages.solo.sandboxInfo.lifecycle.lastModelHint')">{{ relTime(lifecycle.modelActiveAt.toISOString()) }}</dd>
          </template>
          <template v-if="session.sandbox?.createdAt">
            <dt>{{ t('pages.solo.sandboxInfo.details.created') }}</dt>
            <dd>{{ new Date(session.sandbox.createdAt).toLocaleString() }}</dd>
          </template>
        </dl>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.sandbox-info { display: inline-flex; }
/* Matches the conversation toolbar's mode toggles beside it. */
.sandbox-info-trigger {
  position: relative;
  display: inline-flex;
  width: 32px;
  height: 32px;
  flex: none;
  align-items: center;
  justify-content: center;
  padding: 0;
  border: 1px solid transparent;
  border-radius: 8px;
  background: transparent;
  color: rgb(var(--c-txt3));
  transition: background var(--dur-ui), color var(--dur-ui), border-color var(--dur-ui);
}
.sandbox-info-trigger:hover { background: rgb(var(--c-txt) / .04); color: rgb(var(--c-txt)); }
.sandbox-info-trigger[aria-pressed="true"] { border-color: rgb(var(--c-line)); background: rgb(var(--c-base)); color: rgb(var(--c-txt)); }
.sandbox-info-trigger:focus-visible { outline: 2px solid rgb(var(--c-accent) / .5); outline-offset: 2px; }
.sandbox-info-dot {
  position: absolute;
  top: 6px;
  right: 6px;
  width: 6px;
  height: 6px;
  border-radius: 999px;
  box-shadow: 0 0 0 1.5px rgb(var(--c-surface));
  background: rgb(var(--c-txt3));
}
.sandbox-info-dot.is-inline { position: static; flex: none; box-shadow: none; }
.sandbox-info-dot[data-tone="ok"] { background: rgb(var(--c-ok)); }
.sandbox-info-dot[data-tone="err"] { background: rgb(var(--c-err)); }
.sandbox-info-dot[data-tone="pending"] { background: rgb(var(--c-warn)); animation: sandbox-info-breathe 1.6s ease-in-out infinite; }
.sandbox-info-panel {
  position: fixed;
  z-index: 60;
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  border: 1px solid rgb(var(--c-line));
  border-radius: 12px;
  background: rgb(var(--c-surface));
  box-shadow: 0 10px 32px rgb(0 0 0 / .12);
  outline: none;
}
.sandbox-info-panel.is-sheet { inset: auto 0 0 0; max-height: 85vh; border-radius: 14px 14px 0 0; }
.sandbox-info-scrim { position: fixed; z-index: 59; inset: 0; background: rgb(0 0 0 / .28); }
.sandbox-info-body { padding: 14px 15px 7px; }
.sandbox-info-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }
.sandbox-info-meta {
  display: flex; align-items: center; gap: 6px; margin-top: 4px;
  font-size: 11.5px; color: rgb(var(--c-txt2));
}
.sandbox-info-status {
  display: inline-flex; align-items: center; gap: 6px; max-width: 52%;
  font-size: 11.5px; font-weight: 500; color: rgb(var(--c-txt2));
}
.sandbox-info-connect { display: flex; flex-wrap: wrap; align-items: center; gap: 6px 8px; margin-top: 12px; }
.sandbox-info-web-ide, .sandbox-info-details-toggle {
  display: inline-flex; align-items: center; gap: 5px; min-height: 28px;
  padding: 0 6px; border-radius: 6px; font-size: 12px; font-weight: 500;
  color: rgb(var(--c-txt2));
}
.sandbox-info-web-ide:hover, .sandbox-info-details-toggle:hover, .sandbox-info-stop:hover:enabled { background: rgb(var(--c-elevated)); }
.sandbox-info-web-ide:focus-visible, .sandbox-info-details-toggle:focus-visible {
  outline: 2px solid rgb(var(--c-accent) / .5); outline-offset: 2px;
}
.sandbox-info-runtime {
  margin-top: 11px; padding-top: 5px; border-top: 1px solid rgb(var(--c-line));
}
.sandbox-info-runtime-row {
  display: flex; align-items: center; justify-content: space-between; gap: 8px;
  min-height: 31px; font-size: 12.5px;
}
.sandbox-info-row-label { flex: none; color: rgb(var(--c-txt2)); }
.sandbox-info-runtime .sandbox-info-note { margin: -1px 0 4px; }
.sandbox-info-footer {
  display: flex; align-items: center; justify-content: space-between; gap: 8px; min-height: 43px;
  padding: 5px 10px 5px 9px; border-top: 1px solid rgb(var(--c-line));
}
.sandbox-info-stop:is(:hover, :focus-visible):enabled {
  color: color-mix(in srgb, rgb(var(--c-err)) 76%, rgb(var(--c-txt)));
}
.sandbox-info-rows {
  display: grid; grid-template-columns: max-content minmax(0, 1fr); align-items: center; gap: 6px 14px;
  margin: 0; padding: 0 15px 14px; font-size: 12px;
}
.sandbox-info-rows .sandbox-info-copy { width: 20px; height: 20px; }
.sandbox-info-rows dt { color: rgb(var(--c-txt3)); }
.sandbox-info-rows dd { min-width: 0; margin: 0; color: rgb(var(--c-txt)); }
.sandbox-info-note {
  font-size: 11.5px; line-height: 1.5;
  color: color-mix(in srgb, rgb(var(--c-txt3)), rgb(var(--c-txt2)) 45%);
}
.sandbox-info-link { font-size: 12px; color: rgb(var(--c-accent-2)); }
.sandbox-info-link:hover { text-decoration: underline; }
.sandbox-info-copy {
  display: inline-flex;
  width: 24px;
  height: 24px;
  flex: none;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  color: rgb(var(--c-txt3));
}
.sandbox-info-copy:hover { background: rgb(var(--c-txt) / .06); color: rgb(var(--c-txt)); }
@keyframes sandbox-info-breathe { 0%, 100% { opacity: 1; } 50% { opacity: .35; } }
@media (prefers-reduced-motion: reduce) {
  .sandbox-info-dot[data-tone="pending"] { animation: none; }
}
</style>
