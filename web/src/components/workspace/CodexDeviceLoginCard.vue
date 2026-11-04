<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppSpinner from '@/components/ui/AppSpinner.vue'
import type { CodexDeviceLoginPoll } from '@/lib/api/api'
import { CODEX_DEVICE_URL, useCodexDeviceLogin, type CodexDeviceLoginState } from '@/lib/workspace/useCodexDeviceLogin'

const props = withDefaults(defineProps<{
  /** Alias for a new credential; ignored when credentialId is set. */
  name?: string
  /** Existing Codex login credential that receives the new login. */
  credentialId?: string
  autoStart?: boolean
  /** Runs before requesting a code; return false to stay idle. */
  validate?: () => boolean
}>(), { name: '', credentialId: '', autoStart: false, validate: undefined })
const emit = defineEmits<{ success: [result: CodexDeviceLoginPoll]; state: [state: CodexDeviceLoginState]; aliasTaken: [alias: string] }>()
const { t } = useI18n()
const K = 'pages.workspace.credentials.codexDevice'

const login = useCodexDeviceLogin({ onSuccess: (result) => emit('success', result) })
const copyState = ref<'idle' | 'copied' | 'failed'>('idle')
let copiedTimer: ReturnType<typeof setTimeout> | undefined

watch(login.state, (state) => emit('state', state), { immediate: true })
watch(login.failure, (failure) => { if (failure?.code === 'alias_taken') emit('aliasTaken', failure.alias || props.name) })

const codeChars = computed(() => login.userCode.value.split(''))
const verifyUrl = computed(() => /^https:\/\/auth\.openai\.com\//.test(login.verificationUri.value) ? login.verificationUri.value : CODEX_DEVICE_URL)
const remainingText = computed(() => {
  const seconds = login.remainingSeconds.value
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
})
const email = computed(() => login.result.value?.email || '')
const savedName = computed(() => login.result.value?.credential?.name || props.name)
const failureText = computed(() => {
  switch (login.state.value) {
    case 'expired': return t(`${K}.expired`)
    case 'denied': return t(`${K}.denied`)
    case 'not_enabled': return t(`${K}.notEnabled`)
  }
  const failure = login.failure.value
  if (!failure) return t(`${K}.errors.unknown`)
  const known = ['alias_taken', 'credential_unavailable', 'session_not_found', 'device_login_disabled', 'invalid_target', 'upstream_error', 'exchange_failed', 'invalid_response']
  return known.includes(failure.code) ? t(`${K}.errors.${failure.code}`, { alias: failure.alias || props.name }) : failure.message || t(`${K}.errors.unknown`)
})
const badge = computed(() => {
  switch (login.state.value) {
    case 'waiting': return { tone: 'info', label: t(`${K}.badgeWaiting`), lamp: true }
    case 'success': return { tone: 'ok', label: t(`${K}.badgeSuccess`), lamp: false }
    case 'expired': return { tone: 'err', label: t(`${K}.badgeExpired`), lamp: false }
    case 'denied': return { tone: 'err', label: t(`${K}.badgeDenied`), lamp: false }
    case 'not_enabled':
    case 'error': return { tone: 'err', label: t(`${K}.badgeError`), lamp: false }
    default: return null
  }
})

async function start() {
  if (login.busy.value) return
  if (props.validate && !props.validate()) return
  const failure = await login.start({ name: props.name, credentialId: props.credentialId })
  // An alias conflict found before contacting OpenAI belongs to the alias field.
  if (failure?.code === 'alias_taken') login.cancel()
}

async function copyCode() {
  try {
    await navigator.clipboard.writeText(login.userCode.value)
    copyState.value = 'copied'
  } catch {
    copyState.value = 'failed'
  }
  if (copiedTimer !== undefined) clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => { copyState.value = 'idle' }, 2000)
}

onMounted(() => { if (props.autoStart) void start() })
onUnmounted(() => { if (copiedTimer !== undefined) clearTimeout(copiedTimer) })
defineExpose({ start, cancel: login.cancel, state: login.state })
</script>

<template>
  <section class="cx-device" :data-state="login.state.value" data-testid="codex-device-card">
    <header class="cx-device__head">
      <Icon name="terminal" :size="14" :interactive="false" />
      <span>{{ t(login.state.value === 'success' ? `${K}.accountTitle` : `${K}.cardTitle`) }}</span>
      <span v-if="badge" class="cx-device__badge" :class="`is-${badge.tone}`" data-testid="codex-device-badge" role="status">
        <span v-if="badge.lamp" class="cx-device__lamp" aria-hidden="true"><i /></span>{{ badge.label }}
      </span>
    </header>

    <div v-if="login.state.value === 'idle' || login.state.value === 'starting'" class="cx-device__body">
      <p class="cx-device__msg">{{ t(`${K}.idleHint`) }}</p>
      <button type="button" class="cx-device__btn is-primary" :disabled="login.state.value === 'starting'" data-testid="codex-device-start" @click="start">
        <AppSpinner v-if="login.state.value === 'starting'" :size="14" />
        {{ t(`${K}.getCode`) }}
      </button>
    </div>

    <div v-else-if="login.state.value === 'waiting'" class="cx-device__body">
      <ol class="cx-device__steps">
        <li>{{ t(`${K}.step1`) }}</li>
        <li>{{ t(`${K}.step2`) }}</li>
      </ol>
      <div class="cx-device__code" role="img" :aria-label="t(`${K}.codeLabel`, { code: login.userCode.value })" data-testid="codex-device-code">
        <template v-for="(char, index) in codeChars" :key="index">
          <b v-if="char === '-'" aria-hidden="true" />
          <span v-else aria-hidden="true">{{ char }}</span>
        </template>
      </div>
      <div class="cx-device__actions">
        <button type="button" class="cx-device__btn" data-testid="codex-device-copy" @click="copyCode">
          <Icon :name="copyState === 'copied' ? 'check' : 'copy'" :size="14" :interactive="false" />
          {{ t(copyState === 'copied' ? `${K}.copied` : copyState === 'failed' ? `${K}.copyFailed` : `${K}.copyCode`) }}
        </button>
        <a class="cx-device__btn is-primary" :href="verifyUrl" target="_blank" rel="noopener noreferrer" :title="verifyUrl" data-testid="codex-device-open">
          {{ t(`${K}.openPage`) }}
          <svg viewBox="0 0 24 24" aria-hidden="true"><path fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" d="M7 17 17 7M8 7h9v9" /></svg>
        </a>
      </div>
      <div class="cx-device__timer">
        <div class="cx-device__bar" aria-hidden="true"><i :style="{ transform: `scaleX(${login.progress.value})` }" /></div>
        <div class="cx-device__timer-row">
          <span>{{ t(`${K}.autoSave`) }}</span>
          <span :aria-label="t(`${K}.remaining`, { time: remainingText })" data-testid="codex-device-remaining">{{ remainingText }}</span>
        </div>
      </div>
    </div>

    <div v-else-if="login.state.value === 'success'" class="cx-device__body" data-testid="codex-device-success">
      <div class="cx-device__account">
        <span class="cx-device__avatar" aria-hidden="true">{{ (email || savedName || 'C').slice(0, 1).toUpperCase() }}</span>
        <div class="min-w-0">
          <div v-if="email" class="cx-device__email" data-testid="codex-device-email">{{ email }}</div>
          <div class="cx-device__saved">{{ t(login.result.value?.updated ? `${K}.updatedAs` : `${K}.savedAs`, { name: savedName }) }}</div>
        </div>
      </div>
    </div>

    <div v-else class="cx-device__body" data-testid="codex-device-failed">
      <p class="cx-device__msg" role="alert">{{ failureText }}</p>
      <button type="button" class="cx-device__btn is-primary" data-testid="codex-device-retry" @click="start">{{ t(`${K}.retry`) }}</button>
    </div>

    <footer v-if="login.state.value !== 'success'" class="cx-device__foot">{{ t(`${K}.footer`) }}</footer>
  </section>
</template>

<style scoped>
.cx-device { overflow: hidden; border: 1px solid rgb(var(--c-line)); border-radius: 12px; background: rgb(var(--c-surface)); }
.cx-device__head { display: flex; align-items: center; gap: 8px; padding: 10px 14px; border-bottom: 1px solid rgb(var(--c-line)); color: rgb(var(--c-txt2)); font-size: 13px; font-weight: 500; }
.cx-device__badge {
  --ink: var(--c-txt3);
  display: inline-flex; height: 24px; align-items: center; gap: 6px; margin-left: auto; padding: 0 9px;
  border: 1px solid rgb(var(--ink) / .23); border-radius: 999px; background: rgb(var(--ink) / .08);
  color: rgb(var(--ink)); font-size: 12px; font-weight: 400; white-space: nowrap;
}
.cx-device__badge.is-info { --ink: var(--c-info); }
.cx-device__badge.is-ok { --ink: var(--c-ok); }
.cx-device__badge.is-err { --ink: var(--c-err); }
.cx-device__lamp { display: grid; width: 12px; height: 12px; place-items: center; }
.cx-device__lamp i { width: 5px; height: 5px; border-radius: 50%; background: currentColor; animation: cx-device-breathe 1.6s ease-in-out infinite; }
@keyframes cx-device-breathe { 50% { opacity: .45; box-shadow: 0 0 0 3px rgb(var(--ink) / .12); } }
.cx-device__body { display: grid; justify-items: center; gap: 14px; padding: 18px 14px 14px; }
.cx-device__msg { max-width: 360px; margin: 0; color: rgb(var(--c-txt3)); font-size: 13px; line-height: 1.6; text-align: center; }
.cx-device__steps { max-width: 360px; margin: 0; padding-left: 18px; color: rgb(var(--c-txt3)); font-size: 12px; line-height: 1.6; }
.cx-device__code { display: flex; align-items: center; gap: 6px; }
.cx-device__code span {
  display: grid; width: 34px; height: 42px; place-items: center;
  border: 1px solid rgb(var(--c-line)); border-radius: 8px; background: rgb(var(--c-base));
  color: rgb(var(--c-txt)); font-family: "JetBrains Mono", ui-monospace, SFMono-Regular, monospace; font-size: 20px; font-weight: 600; line-height: 1;
}
.cx-device__code b { width: 8px; height: 2px; margin: 0 2px; border-radius: 2px; background: rgb(var(--c-line-strong)); }
.cx-device__actions { display: flex; flex-wrap: wrap; justify-content: center; gap: 8px; }
.cx-device__btn {
  display: inline-flex; height: 34px; align-items: center; justify-content: center; gap: 6px; padding: 0 14px;
  border: 1px solid rgb(var(--c-line)); border-radius: 10px; background: rgb(var(--c-surface));
  color: rgb(var(--c-txt)); font-size: 13px; font-weight: 500; line-height: 1; white-space: nowrap; text-decoration: none; cursor: pointer;
  transition: color 160ms ease, background-color 160ms ease, border-color 160ms ease;
}
.cx-device__btn :deep(svg), .cx-device__btn > svg { width: 14px; height: 14px; flex: none; }
.cx-device__btn:hover { border-color: rgb(var(--c-line-strong)); background: rgb(var(--c-elevated)); }
.cx-device__btn.is-primary { border-color: transparent; background: rgb(var(--c-txt)); color: rgb(var(--c-surface)); }
.cx-device__btn.is-primary:hover { background: rgb(var(--c-txt) / .86); }
.cx-device__btn:disabled { cursor: not-allowed; opacity: .6; }
.cx-device__btn:focus-visible { outline: 2px solid rgb(var(--c-accent-2) / .6); outline-offset: 2px; }
.cx-device__timer { display: grid; width: 100%; gap: 6px; }
.cx-device__bar { height: 3px; overflow: hidden; border-radius: 999px; background: rgb(var(--c-elevated)); }
.cx-device__bar i { display: block; height: 100%; border-radius: 999px; background: rgb(var(--c-accent-2)); transform-origin: left; transition: transform 1s linear; }
.cx-device__timer-row { display: flex; justify-content: space-between; gap: 12px; color: rgb(var(--c-txt3)); font-size: 12px; font-variant-numeric: tabular-nums; }
.cx-device__account { display: flex; align-items: center; gap: 10px; }
.cx-device__avatar { display: grid; width: 36px; height: 36px; flex: none; place-items: center; border-radius: 50%; background: linear-gradient(135deg, #10a37f, #4fc3a1); color: #fff; font-weight: 600; }
.cx-device__email { color: rgb(var(--c-txt)); font-size: 14px; font-weight: 600; }
.cx-device__saved { color: rgb(var(--c-txt3)); font-size: 12px; }
.cx-device__foot { padding: 10px 14px; border-top: 1px solid rgb(var(--c-line)); background: rgb(var(--c-base)); color: rgb(var(--c-txt3)); font-size: 12px; line-height: 1.5; }
@media (prefers-reduced-motion: reduce) {
  .cx-device__lamp i { animation: none; }
  .cx-device__bar i, .cx-device__btn { transition: none; }
}
</style>
