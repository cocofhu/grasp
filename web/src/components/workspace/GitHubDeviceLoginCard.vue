<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppSpinner from '@/components/ui/AppSpinner.vue'
import type { GitHubDeviceLoginPoll } from '@/lib/api/api'
import { GITHUB_MARK, useGitHubDeviceLogin, type GitHubDeviceLoginState } from '@/lib/workspace/useGitHubDeviceLogin'

const props = withDefaults(defineProps<{
  /** Alias for a new credential; ignored when credentialId is set. */
  name?: string
  /** Existing github.com credential that receives the new token. */
  credentialId?: string
  autoStart?: boolean
  /** Runs before requesting a code; return false to stay idle. */
  validate?: () => boolean
}>(), { name: '', credentialId: '', autoStart: false, validate: undefined })
const emit = defineEmits<{ authorized: [result: GitHubDeviceLoginPoll]; state: [state: GitHubDeviceLoginState]; aliasTaken: [alias: string] }>()
const { t } = useI18n()
const SCOPES = ['repo', 'read:org', 'workflow']
const DEVICE_URL = 'https://github.com/login/device'

const login = useGitHubDeviceLogin({ onAuthorized: (result) => emit('authorized', result) })
const copyState = ref<'idle' | 'copied' | 'failed'>('idle')
let copiedTimer: ReturnType<typeof setTimeout> | undefined

watch(login.state, (state) => emit('state', state), { immediate: true })
watch(login.failure, (failure) => { if (failure?.code === 'alias_taken') emit('aliasTaken', failure.alias || props.name) })

const codeChars = computed(() => login.userCode.value.split(''))
const verifyUrl = computed(() => /^https:\/\/github\.com\//.test(login.verificationUri.value) ? login.verificationUri.value : DEVICE_URL)
const remainingText = computed(() => {
  const seconds = login.remainingSeconds.value
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
})
const account = computed(() => login.result.value?.githubLogin || '')
const savedName = computed(() => login.result.value?.credential?.name || props.name)
const failureText = computed(() => {
  const failure = login.failure.value
  if (!failure) return ''
  const key = `pages.workspace.credentials.githubDevice.errors.${failure.code}`
  const known = ['device_flow_disabled', 'incorrect_client_credentials', 'alias_taken', 'credential_unavailable', 'session_not_found', 'device_login_disabled', 'invalid_target']
  return known.includes(failure.code) ? t(key, { alias: failure.alias || props.name }) : failure.message || t('pages.workspace.credentials.githubDevice.errors.unknown')
})
const badge = computed(() => {
  switch (login.state.value) {
    case 'waiting': return { tone: 'info', label: t('pages.workspace.credentials.githubDevice.badgeWaiting'), lamp: true }
    case 'authorized': return { tone: 'ok', label: t('pages.workspace.credentials.githubDevice.badgeAuthorized'), lamp: false }
    case 'expired': return { tone: 'err', label: t('pages.workspace.credentials.githubDevice.badgeExpired'), lamp: false }
    case 'denied': return { tone: 'err', label: t('pages.workspace.credentials.githubDevice.badgeDenied'), lamp: false }
    case 'error': return { tone: 'err', label: t('pages.workspace.credentials.githubDevice.badgeError'), lamp: false }
    default: return null
  }
})

async function start() {
  if (login.busy.value) return
  if (props.validate && !props.validate()) return
  const failure = await login.start({ name: props.name, credentialId: props.credentialId })
  // An alias conflict found before contacting GitHub belongs to the alias field.
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
  <section class="gh-device" :data-state="login.state.value" data-testid="github-device-card">
    <header class="gh-device__head">
      <svg class="gh-device__icon" viewBox="0 0 16 16" aria-hidden="true"><path fill="currentColor" :d="GITHUB_MARK" /></svg>
      <span>{{ t(login.state.value === 'authorized' ? 'pages.workspace.credentials.githubDevice.accountTitle' : 'pages.workspace.credentials.githubDevice.cardTitle') }}</span>
      <span v-if="badge" class="gh-device__badge" :class="`is-${badge.tone}`" data-testid="github-device-badge" role="status">
        <span v-if="badge.lamp" class="gh-device__lamp" aria-hidden="true"><i /></span>{{ badge.label }}
      </span>
    </header>

    <div v-if="login.state.value === 'idle' || login.state.value === 'starting'" class="gh-device__body">
      <p class="gh-device__msg">{{ t('pages.workspace.credentials.githubDevice.idleHint') }}</p>
      <button type="button" class="gh-device__btn is-primary" :disabled="login.state.value === 'starting'" data-testid="github-device-start" @click="start">
        <AppSpinner v-if="login.state.value === 'starting'" :size="14" />
        <svg v-else viewBox="0 0 16 16" aria-hidden="true"><path fill="currentColor" :d="GITHUB_MARK" /></svg>
        {{ t('pages.workspace.credentials.githubDevice.getCode') }}
      </button>
    </div>

    <div v-else-if="login.state.value === 'waiting'" class="gh-device__body">
      <div class="gh-device__code" role="img" :aria-label="t('pages.workspace.credentials.githubDevice.codeLabel', { code: login.userCode.value })" data-testid="github-device-code">
        <template v-for="(char, index) in codeChars" :key="index">
          <b v-if="char === '-'" aria-hidden="true" />
          <span v-else aria-hidden="true">{{ char }}</span>
        </template>
      </div>
      <div class="gh-device__actions">
        <button type="button" class="gh-device__btn" data-testid="github-device-copy" @click="copyCode">
          <Icon :name="copyState === 'copied' ? 'check' : 'copy'" :size="14" :interactive="false" />
          {{ t(copyState === 'copied' ? 'pages.workspace.credentials.githubDevice.copied' : copyState === 'failed' ? 'pages.workspace.credentials.githubDevice.copyFailed' : 'pages.workspace.credentials.githubDevice.copyCode') }}
        </button>
        <a class="gh-device__btn is-primary" :href="verifyUrl" target="_blank" rel="noopener noreferrer" :title="verifyUrl" data-testid="github-device-open">
          <svg viewBox="0 0 16 16" aria-hidden="true"><path fill="currentColor" :d="GITHUB_MARK" /></svg>
          {{ t('pages.workspace.credentials.githubDevice.openGitHub') }}
          <svg viewBox="0 0 24 24" aria-hidden="true"><path fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" d="M7 17 17 7M8 7h9v9" /></svg>
        </a>
      </div>
      <div class="gh-device__timer">
        <div class="gh-device__bar" aria-hidden="true"><i :style="{ transform: `scaleX(${login.progress.value})` }" /></div>
        <div class="gh-device__timer-row">
          <span>{{ t('pages.workspace.credentials.githubDevice.autoSave') }}</span>
          <span :aria-label="t('pages.workspace.credentials.githubDevice.remaining', { time: remainingText })" data-testid="github-device-remaining">{{ remainingText }}</span>
        </div>
      </div>
    </div>

    <div v-else-if="login.state.value === 'authorized'" class="gh-device__body" data-testid="github-device-authorized">
      <div class="gh-device__account">
        <span class="gh-device__avatar" aria-hidden="true">{{ (account || savedName || 'G').slice(0, 1).toUpperCase() }}</span>
        <div class="min-w-0">
          <div v-if="account" class="gh-device__login" data-testid="github-device-login">@{{ account }}</div>
          <div class="gh-device__saved">{{ t(login.result.value?.updated ? 'pages.workspace.credentials.githubDevice.updatedAs' : 'pages.workspace.credentials.githubDevice.savedAs', { name: savedName }) }}</div>
        </div>
      </div>
      <p v-if="login.result.value?.scopeWarning" class="gh-device__note" role="alert" data-testid="github-device-scope-warning">
        <Icon name="warning" :size="14" :interactive="false" class="shrink-0" />
        <span>{{ t('pages.workspace.credentials.githubDevice.scopeWarning') }}</span>
      </p>
    </div>

    <div v-else class="gh-device__body" data-testid="github-device-failed">
      <p class="gh-device__msg" role="alert">{{ login.state.value === 'expired' ? t('pages.workspace.credentials.githubDevice.expired') : login.state.value === 'denied' ? t('pages.workspace.credentials.githubDevice.denied') : failureText }}</p>
      <button type="button" class="gh-device__btn is-primary" data-testid="github-device-retry" @click="start">{{ t('pages.workspace.credentials.githubDevice.retry') }}</button>
    </div>

    <footer v-if="login.state.value !== 'authorized'" class="gh-device__foot">
      {{ t('pages.workspace.credentials.githubDevice.scopes') }}
      <span v-for="scope in SCOPES" :key="scope" class="gh-device__chip">{{ scope }}</span>
    </footer>
  </section>
</template>

<style scoped>
.gh-device { overflow: hidden; border: 1px solid rgb(var(--c-line)); border-radius: 12px; background: rgb(var(--c-surface)); }
.gh-device__head { display: flex; align-items: center; gap: 8px; padding: 10px 14px; border-bottom: 1px solid rgb(var(--c-line)); color: rgb(var(--c-txt2)); font-size: 13px; font-weight: 500; }
.gh-device__icon { width: 14px; height: 14px; flex: none; }
.gh-device__badge {
  --ink: var(--c-txt3);
  display: inline-flex; height: 24px; align-items: center; gap: 6px; margin-left: auto; padding: 0 9px;
  border: 1px solid rgb(var(--ink) / .23); border-radius: 999px; background: rgb(var(--ink) / .08);
  color: rgb(var(--ink)); font-size: 12px; font-weight: 400; white-space: nowrap;
}
.gh-device__badge.is-info { --ink: var(--c-info); }
.gh-device__badge.is-ok { --ink: var(--c-ok); }
.gh-device__badge.is-err { --ink: var(--c-err); }
.gh-device__lamp { display: grid; width: 12px; height: 12px; place-items: center; }
.gh-device__lamp i { width: 5px; height: 5px; border-radius: 50%; background: currentColor; animation: gh-device-breathe 1.6s ease-in-out infinite; }
@keyframes gh-device-breathe { 50% { opacity: .45; box-shadow: 0 0 0 3px rgb(var(--ink) / .12); } }
.gh-device__body { display: grid; justify-items: center; gap: 14px; padding: 18px 14px 14px; }
.gh-device__msg { max-width: 340px; margin: 0; color: rgb(var(--c-txt3)); font-size: 13px; line-height: 1.6; text-align: center; }
.gh-device__code { display: flex; align-items: center; gap: 6px; }
.gh-device__code span {
  display: grid; width: 34px; height: 42px; place-items: center;
  border: 1px solid rgb(var(--c-line)); border-radius: 8px; background: rgb(var(--c-base));
  color: rgb(var(--c-txt)); font-family: "JetBrains Mono", ui-monospace, SFMono-Regular, monospace; font-size: 20px; font-weight: 600; line-height: 1;
}
.gh-device__code b { width: 8px; height: 2px; margin: 0 2px; border-radius: 2px; background: rgb(var(--c-line-strong)); }
.gh-device__actions { display: flex; flex-wrap: wrap; justify-content: center; gap: 8px; }
.gh-device__btn {
  display: inline-flex; height: 34px; align-items: center; justify-content: center; gap: 6px; padding: 0 14px;
  border: 1px solid rgb(var(--c-line)); border-radius: 10px; background: rgb(var(--c-surface));
  color: rgb(var(--c-txt)); font-size: 13px; font-weight: 500; line-height: 1; white-space: nowrap; text-decoration: none; cursor: pointer;
  transition: color 160ms ease, background-color 160ms ease, border-color 160ms ease;
}
.gh-device__btn :deep(svg), .gh-device__btn > svg { width: 14px; height: 14px; flex: none; }
.gh-device__btn:hover { border-color: rgb(var(--c-line-strong)); background: rgb(var(--c-elevated)); }
.gh-device__btn.is-primary { border-color: transparent; background: rgb(var(--c-txt)); color: rgb(var(--c-surface)); }
.gh-device__btn.is-primary:hover { background: rgb(var(--c-txt) / .86); }
.gh-device__btn:disabled { cursor: not-allowed; opacity: .6; }
.gh-device__btn:focus-visible { outline: 2px solid rgb(var(--c-accent-2) / .6); outline-offset: 2px; }
.gh-device__timer { display: grid; width: 100%; gap: 6px; }
.gh-device__bar { height: 3px; overflow: hidden; border-radius: 999px; background: rgb(var(--c-elevated)); }
.gh-device__bar i { display: block; height: 100%; border-radius: 999px; background: rgb(var(--c-accent-2)); transform-origin: left; transition: transform 1s linear; }
.gh-device__timer-row { display: flex; justify-content: space-between; gap: 12px; color: rgb(var(--c-txt3)); font-size: 12px; font-variant-numeric: tabular-nums; }
.gh-device__account { display: flex; align-items: center; gap: 10px; }
.gh-device__avatar { display: grid; width: 36px; height: 36px; flex: none; place-items: center; border-radius: 50%; background: linear-gradient(135deg, #7b61ff, #a08aff); color: #fff; font-weight: 600; }
.gh-device__login { color: rgb(var(--c-txt)); font-size: 14px; font-weight: 600; }
.gh-device__saved { color: rgb(var(--c-txt3)); font-size: 12px; }
.gh-device__note { display: flex; width: 100%; align-items: flex-start; gap: 8px; margin: 0; padding: 8px 10px; border: 1px solid rgb(var(--c-warn) / .18); border-radius: 8px; background: rgb(var(--c-warn) / .07); color: rgb(var(--c-warn)); font-size: 12px; line-height: 1.5; }
.gh-device__foot { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; padding: 10px 14px; border-top: 1px solid rgb(var(--c-line)); background: rgb(var(--c-base)); color: rgb(var(--c-txt3)); font-size: 12px; }
.gh-device__chip { display: inline-flex; height: 22px; align-items: center; padding: 0 8px; border: 1px solid rgb(var(--c-line)); border-radius: 999px; background: rgb(var(--c-surface)); color: rgb(var(--c-txt2)); font-size: 12px; }
@media (prefers-reduced-motion: reduce) {
  .gh-device__lamp i { animation: none; }
  .gh-device__bar i, .gh-device__btn { transition: none; }
}
</style>
