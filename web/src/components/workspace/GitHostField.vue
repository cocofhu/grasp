<script setup lang="ts">
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import { defaultGitCredentialHost, normalizeGitCredentialHost } from '@/lib/workspace/credentialKinds'
import { GITHUB_MARK } from '@/lib/workspace/useGitHubDeviceLogin'

/**
 * Git host input with the scheme shown as a prefix. The model value keeps an
 * explicit http:// (self-hosted) and otherwise stays a bare host, which the
 * server treats as HTTPS.
 */
const props = withDefaults(defineProps<{ modelValue: string; provider: string; disabled?: boolean }>(), { disabled: false })
const emit = defineEmits<{ 'update:modelValue': [value: string]; reset: [] }>()
const { t } = useI18n()
const inputId = `git-host-${useId().replace(/:/g, '')}`

const scheme = computed(() => /^http:\/\//i.test(props.modelValue.trim()) ? 'http://' : 'https://')
const bare = computed(() => props.modelValue.trim().replace(/^https?:\/\//i, ''))
const defaultHost = computed(() => defaultGitCredentialHost(props.provider))
const normalized = computed(() => normalizeGitCredentialHost(props.modelValue))
const official = computed(() => !!defaultHost.value && normalized.value === `https://${defaultHost.value}`)
const status = computed(() => !normalized.value ? 'invalid' : official.value ? 'official' : 'custom')

function onInput(event: Event) {
  const input = event.target as HTMLInputElement
  const raw = input.value
  const match = raw.trim().match(/^(https?):\/\//i)
  const host = raw.replace(/^\s*https?:\/\//i, '')
  if (host !== raw) input.value = host
  const nextScheme = match ? `${match[1].toLowerCase()}://` : scheme.value
  emit('update:modelValue', nextScheme === 'http://' ? `http://${host}` : host)
}

function reset() {
  emit('update:modelValue', defaultHost.value)
  emit('reset')
}
</script>

<template>
  <div class="git-host-field" :data-host-status="status">
    <label class="label git-host-field__label" :for="inputId">
      <span>{{ t('pages.workspace.credentials.githubDevice.site') }}</span>
      <span class="git-host-field__aside">{{ t('pages.workspace.credentials.githubDevice.siteAside') }}</span>
    </label>
    <div class="git-host-field__box" :class="{ 'is-invalid': status === 'invalid', 'is-disabled': disabled }">
      <span class="git-host-field__prefix">
        <svg v-if="provider === 'github' && status !== 'custom'" class="git-host-field__mark" viewBox="0 0 16 16" aria-hidden="true"><path fill="currentColor" :d="GITHUB_MARK" /></svg>
        <Icon v-else name="globe" :size="14" :interactive="false" class="git-host-field__mark" />
        <span data-testid="workspace-credential-create-host-scheme">{{ scheme }}</span>
      </span>
      <input
        :id="inputId"
        :value="bare"
        class="git-host-field__input"
        data-testid="workspace-credential-create-host"
        :aria-invalid="status === 'invalid'"
        :disabled="disabled"
        :placeholder="defaultHost"
        autocomplete="url"
        spellcheck="false"
        @input="onInput"
      />
      <span class="git-host-field__suffix">
        <span class="git-host-field__pill" :class="`is-${status}`" data-testid="workspace-credential-host-status">{{ t(status === 'invalid' ? 'pages.workspace.credentials.githubDevice.siteInvalid' : status === 'official' ? 'pages.workspace.credentials.githubDevice.siteOfficial' : 'pages.workspace.credentials.githubDevice.siteCustom') }}</span>
        <button v-if="!official && defaultHost && !disabled" type="button" class="git-host-field__reset" data-testid="workspace-credential-host-reset" @click="reset">{{ t('pages.workspace.credentials.githubDevice.siteReset') }}</button>
      </span>
    </div>
    <p v-if="status === 'invalid'" class="mb-0 mt-1.5 text-xs text-err">{{ t('pages.workspace.credentials.invalidHost') }}</p>
  </div>
</template>

<style scoped>
.git-host-field__label { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.git-host-field__aside { color: rgb(var(--c-txt3)); font-weight: 400; }
.git-host-field__box {
  display: flex;
  height: 36px;
  align-items: center;
  overflow: hidden;
  border: 1px solid rgb(var(--c-line));
  border-radius: 8px;
  background: rgb(var(--c-surface));
  font-size: 13px;
  transition: border-color 160ms ease, box-shadow 160ms ease;
}
.git-host-field__box:hover { border-color: rgb(var(--c-line-strong)); }
.git-host-field__box:focus-within { border-color: rgb(var(--c-accent-2) / .55); box-shadow: 0 0 0 3px rgb(var(--c-accent-2) / .12); }
.git-host-field__box.is-invalid { border-color: rgb(var(--c-err) / .5); }
.git-host-field__box.is-disabled { opacity: .6; }
.git-host-field__prefix { display: flex; height: 100%; flex: none; align-items: center; gap: 6px; padding-left: 10px; color: rgb(var(--c-txt3)); white-space: nowrap; }
.git-host-field__mark { width: 14px; height: 14px; flex: none; color: rgb(var(--c-txt2)); }
.git-host-field__input { height: 100%; min-width: 0; flex: 1; border: 0; padding: 0 8px 0 0; background: transparent; color: rgb(var(--c-txt)); font: inherit; outline: none; }
.git-host-field__input::placeholder { color: rgb(var(--c-txt3)); }
.git-host-field__suffix { display: flex; flex: none; align-items: center; gap: 4px; padding-right: 6px; }
.git-host-field__pill { display: inline-flex; height: 22px; align-items: center; border: 1px solid transparent; border-radius: 999px; padding: 0 8px; font-size: 12px; white-space: nowrap; }
.git-host-field__pill.is-official { border-color: rgb(var(--c-ok) / .2); background: rgb(var(--c-ok) / .08); color: rgb(var(--c-ok)); }
.git-host-field__pill.is-custom { border-color: rgb(var(--c-line)); background: rgb(var(--c-elevated)); color: rgb(var(--c-txt2)); }
.git-host-field__pill.is-invalid { border-color: rgb(var(--c-err) / .2); background: rgb(var(--c-err) / .08); color: rgb(var(--c-err)); }
.git-host-field__reset { height: 22px; border: 0; border-radius: 6px; padding: 0 6px; background: transparent; color: rgb(var(--c-txt3)); font-size: 12px; cursor: pointer; }
.git-host-field__reset:hover { background: rgb(var(--c-elevated)); color: rgb(var(--c-txt)); }
.git-host-field__reset:focus-visible { outline: 2px solid rgb(var(--c-accent-2) / .5); outline-offset: -2px; }
@media (prefers-reduced-motion: reduce) { .git-host-field__box { transition: none; } }
</style>
