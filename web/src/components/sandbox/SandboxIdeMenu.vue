<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import type { SSHGatewayInfo } from '@/lib/api/apiTypes'
import { copyToClipboard } from '@/lib/shared/copyToClipboard'
import {
  REMOTE_IDES, SANDBOX_WORKSPACE, loadPreferredIDE, remoteIDELink, remoteIDEOption, savePreferredIDE,
  sshCommand, sshConfigSnippet, sshEndpoint, sshUser, type PreferredIDE, type RemoteIDEOption,
} from '@/lib/sandbox/sshConnect'
import IdeLogo from './IdeLogo.vue'

const props = defineProps<{
  sandboxId: number
  info: SSHGatewayInfo
  /** Why connecting is unavailable now; '' when it works. */
  blocked: string
}>()

const emit = defineEmits<{
  /** An IDE link was opened; a paused sandbox should start waking. */
  launch: []
}>()

const { t } = useI18n()
const preferred = ref<PreferredIDE>(loadPreferredIDE())
const menuOpen = ref(false)
const showInfo = ref(false)
const copied = ref('')
const root = ref<HTMLElement | null>(null)
let copiedTimer: ReturnType<typeof setTimeout> | undefined

const endpoint = computed(() => sshEndpoint(props.info, window.location.hostname))
const command = computed(() => sshCommand(props.sandboxId, endpoint.value))
const configSnippet = computed(() => sshConfigSnippet(endpoint.value))
const preferredOption = computed(() => remoteIDEOption(preferred.value.id)!)

function link(option: RemoteIDEOption, cn = false) {
  return props.blocked ? '' : remoteIDELink(option.id, props.sandboxId, endpoint.value, { cn })
}

function label(option: RemoteIDEOption, cn = false) {
  return cn ? `${option.label} ${t('pages.solo.sandboxInfo.ide.cnEdition')}` : option.label
}

const primaryLink = computed(() => link(preferredOption.value, preferred.value.cn))
const primaryLabel = computed(() => preferredOption.value.scheme
  ? t('pages.solo.sandboxInfo.ide.openIn', { ide: label(preferredOption.value, preferred.value.cn) })
  : t('pages.solo.sandboxInfo.ide.connectWith', { ide: preferredOption.value.label }))

function choose(option: RemoteIDEOption, cn = false) {
  preferred.value = { id: option.id, cn }
  savePreferredIDE(preferred.value)
  menuOpen.value = false
  if (option.scheme) emit('launch')
  else showInfo.value = true
}

function onPrimary() {
  if (props.blocked) return
  if (preferredOption.value.scheme) emit('launch')
  else showInfo.value = !showInfo.value
}

function toggleInfo() {
  menuOpen.value = false
  showInfo.value = !showInfo.value
}

async function copy(key: string, text: string) {
  if (!(await copyToClipboard(text))) return
  copied.value = key
  if (copiedTimer) clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => { copied.value = '' }, 1500)
}

function onDocPointer(e: Event) {
  if (!root.value?.contains(e.target as Node)) menuOpen.value = false
}

/** Escape closes the open menu first; otherwise it reaches the panel. */
function onMenuKeydown(e: KeyboardEvent) {
  if (e.key !== 'Escape' || !menuOpen.value) return
  e.stopPropagation()
  menuOpen.value = false
}

watch(menuOpen, (open) => {
  if (open) document.addEventListener('pointerdown', onDocPointer, true)
  else document.removeEventListener('pointerdown', onDocPointer, true)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocPointer, true)
  if (copiedTimer) clearTimeout(copiedTimer)
})

const infoRows = computed(() => [
  { key: 'user', label: t('pages.solo.sandboxInfo.ide.user'), value: sshUser(props.sandboxId) },
  { key: 'host', label: t('pages.solo.sandboxInfo.ide.host'), value: endpoint.value.host },
  { key: 'port', label: t('pages.solo.sandboxInfo.ide.port'), value: String(endpoint.value.port) },
  { key: 'path', label: t('pages.solo.sandboxInfo.ide.path'), value: SANDBOX_WORKSPACE },
])
</script>

<template>
  <div ref="root" class="ide-menu-root" data-testid="sandbox-ide-menu" @keydown="onMenuKeydown">
    <div class="ide-split" :class="{ 'is-disabled': !!blocked }">
      <component
        :is="primaryLink ? 'a' : 'button'"
        :type="primaryLink ? undefined : 'button'"
        class="ide-primary"
        :href="primaryLink || undefined"
        :disabled="!primaryLink && !!blocked ? true : undefined"
        :aria-disabled="blocked ? 'true' : undefined"
        data-testid="sandbox-ide-primary"
        @click="onPrimary"
      >
        <IdeLogo :ide="preferred.id" :size="16" />
        <span class="truncate">{{ primaryLabel }}</span>
      </component>
      <button
        type="button"
        class="ide-caret"
        :aria-label="t('pages.solo.sandboxInfo.ide.choose')"
        aria-haspopup="menu"
        :aria-expanded="menuOpen ? 'true' : 'false'"
        data-testid="sandbox-ide-caret"
        @click="menuOpen = !menuOpen"
      >
        <Icon name="chevron-down" :size="14" aria-hidden="true" />
      </button>
    </div>

    <div v-if="menuOpen" class="ide-menu" role="menu" data-testid="sandbox-ide-list">
      <div class="ide-menu-title">{{ t('pages.solo.sandboxInfo.ide.menuTitle') }}</div>
      <div v-for="option in REMOTE_IDES" :key="option.id" class="ide-menu-row">
        <component
          :is="link(option) ? 'a' : 'button'"
          :type="link(option) ? undefined : 'button'"
          role="menuitem"
          class="ide-menu-item"
          :href="link(option) || undefined"
          :aria-disabled="option.scheme && blocked ? 'true' : undefined"
          :data-testid="`sandbox-ide-${option.id}`"
          @click="option.scheme && blocked ? undefined : choose(option)"
        >
          <IdeLogo :ide="option.id" :size="16" />
          <span class="min-w-0 flex-1 truncate">{{ option.label }}</span>
          <Icon v-if="preferred.id === option.id && !preferred.cn" name="check" :size="13" class="ide-check" aria-hidden="true" />
        </component>
        <component
          :is="link(option, true) ? 'a' : 'span'"
          v-if="option.cnScheme"
          role="menuitem"
          class="ide-menu-cn"
          :class="{ 'is-active': preferred.id === option.id && preferred.cn }"
          :href="link(option, true) || undefined"
          :title="label(option, true)"
          :aria-label="label(option, true)"
          :data-testid="`sandbox-ide-${option.id}-cn`"
          @click="blocked ? undefined : choose(option, true)"
        >CN</component>
      </div>
      <div class="ide-menu-sep" role="separator" />
      <button type="button" role="menuitem" class="ide-menu-item" data-testid="sandbox-ide-info" @click="toggleInfo">
        <Icon name="info" :size="15" class="text-txt3" aria-hidden="true" />
        <span class="flex-1">{{ t('pages.solo.sandboxInfo.ide.info') }}</span>
      </button>
    </div>

    <div v-if="showInfo" class="ide-info" data-testid="sandbox-ide-details">
      <p v-if="preferred.id === 'jetbrains'" class="ide-note" data-testid="sandbox-ide-jetbrains-hint">{{ t('pages.solo.sandboxInfo.ide.jetbrainsHint') }}</p>
      <dl class="ide-rows">
        <template v-for="row in infoRows" :key="row.key">
          <dt>{{ row.label }}</dt>
          <dd>
            <span class="truncate font-mono">{{ row.value }}</span>
            <button type="button" class="ide-copy" :aria-label="t('pages.solo.sandboxInfo.copy')" :data-testid="`sandbox-ide-copy-${row.key}`" @click="copy(row.key, row.value)">
              <Icon :name="copied === row.key ? 'check' : 'copy'" :size="12" aria-hidden="true" />
            </button>
          </dd>
        </template>
      </dl>
      <div class="ide-code" :title="t('pages.solo.sandboxInfo.ide.commandHint')">
        <code data-testid="sandbox-info-command">{{ command }}</code>
        <button type="button" class="ide-copy" :aria-label="t('pages.solo.sandboxInfo.copy')" data-testid="sandbox-info-copy-command" @click="copy('command', command)">
          <Icon :name="copied === 'command' ? 'check' : 'copy'" :size="13" aria-hidden="true" />
        </button>
      </div>
      <p class="ide-note">{{ t('pages.solo.sandboxInfo.ide.timeoutHint') }}</p>
      <p v-if="info.hostKeyFingerprint" class="ide-note break-all">
        {{ t('pages.solo.sandboxInfo.ide.hostKey') }}: <span class="font-mono">{{ info.hostKeyFingerprint }}</span>
      </p>
      <details class="ide-details">
        <summary>{{ t('pages.solo.sandboxInfo.ide.config') }}</summary>
        <p class="ide-note">{{ t('pages.solo.sandboxInfo.ide.configHint') }}</p>
        <div class="ide-code is-block">
          <pre data-testid="sandbox-info-config">{{ configSnippet }}</pre>
          <button type="button" class="ide-copy" :aria-label="t('pages.solo.sandboxInfo.copy')" data-testid="sandbox-info-copy-config" @click="copy('config', configSnippet)">
            <Icon :name="copied === 'config' ? 'check' : 'copy'" :size="13" aria-hidden="true" />
          </button>
        </div>
      </details>
    </div>
  </div>
</template>

<style scoped>
.ide-menu-root { display: flex; flex-direction: column; gap: 8px; }
.ide-split { display: flex; height: 34px; border: 1px solid rgb(var(--c-line)); border-radius: 8px; background: rgb(var(--c-surface)); }
.ide-primary {
  display: inline-flex; min-width: 0; flex: 1; align-items: center; justify-content: center; gap: 8px;
  padding: 0 12px; border-radius: 7px 0 0 7px; font-size: 13px; font-weight: 500; color: rgb(var(--c-txt));
  transition: background var(--dur-ui);
}
.ide-caret {
  display: inline-flex; width: 34px; flex: none; align-items: center; justify-content: center;
  border-left: 1px solid rgb(var(--c-line)); border-radius: 0 7px 7px 0; color: rgb(var(--c-txt2));
  transition: background var(--dur-ui);
}
.ide-primary:hover, .ide-caret:hover, .ide-caret[aria-expanded="true"] { background: rgb(var(--c-txt) / .05); }
.ide-split.is-disabled .ide-primary { cursor: not-allowed; opacity: .5; }
.ide-split.is-disabled .ide-primary:hover { background: transparent; }
.ide-primary:focus-visible, .ide-caret:focus-visible, .ide-menu-item:focus-visible, .ide-menu-cn:focus-visible { outline: 2px solid rgb(var(--c-accent) / .5); outline-offset: -2px; }
/* In flow, not floating: the panel scrolls, so a floating list would be clipped. */
.ide-menu {
  padding: 6px; border: 1px solid rgb(var(--c-line)); border-radius: 10px; background: rgb(var(--c-surface));
  box-shadow: 0 6px 18px rgb(0 0 0 / .08); animation: ide-menu-in 120ms ease-out;
}
@keyframes ide-menu-in { from { opacity: 0; transform: translateY(-4px); } to { opacity: 1; transform: none; } }
.ide-menu-title { padding: 4px 8px 6px; font-size: 11.5px; color: rgb(var(--c-txt3)); }
.ide-menu-row { display: flex; align-items: center; gap: 4px; }
.ide-menu-item {
  display: flex; min-width: 0; flex: 1; align-items: center; gap: 10px; height: 34px; padding: 0 8px;
  border-radius: 7px; font-size: 13px; color: rgb(var(--c-txt)); text-align: left;
}
.ide-menu-item:hover { background: rgb(var(--c-txt) / .05); }
.ide-menu-item[aria-disabled="true"] { cursor: not-allowed; opacity: .5; }
.ide-menu-item[aria-disabled="true"]:hover { background: transparent; }
.ide-check { color: rgb(var(--c-accent)); }
.ide-menu-cn {
  display: inline-flex; height: 22px; flex: none; align-items: center; padding: 0 7px; margin-right: 4px;
  border: 1px solid rgb(var(--c-line)); border-radius: 6px; font-size: 10.5px; font-weight: 600; letter-spacing: .03em;
  color: rgb(var(--c-txt3));
}
.ide-menu-cn:hover, .ide-menu-cn.is-active { border-color: rgb(var(--c-accent) / .5); color: rgb(var(--c-accent)); }
.ide-menu-sep { height: 1px; margin: 6px 4px; background: rgb(var(--c-line) / .8); }
.ide-info { display: flex; flex-direction: column; gap: 8px; padding: 10px; border-radius: 8px; background: rgb(var(--c-base)); }
.ide-rows { display: grid; grid-template-columns: max-content minmax(0, 1fr); align-items: center; gap: 4px 12px; margin: 0; font-size: 12px; }
.ide-rows dt { color: rgb(var(--c-txt3)); }
.ide-rows dd { display: flex; min-width: 0; align-items: center; gap: 4px; margin: 0; color: rgb(var(--c-txt)); }
.ide-code {
  display: flex; align-items: center; gap: 6px; padding: 6px 6px 6px 10px; border: 1px solid rgb(var(--c-line) / .7);
  border-radius: 7px; background: rgb(var(--c-surface)); font-family: var(--font-mono, ui-monospace, monospace); font-size: 11.5px;
}
.ide-code code, .ide-code pre { min-width: 0; flex: 1; margin: 0; overflow-x: auto; white-space: pre; color: rgb(var(--c-txt)); }
.ide-code.is-block { align-items: flex-start; margin-top: 6px; }
.ide-copy { display: inline-flex; width: 22px; height: 22px; flex: none; align-items: center; justify-content: center; border-radius: 6px; color: rgb(var(--c-txt3)); }
.ide-copy:hover { background: rgb(var(--c-txt) / .06); color: rgb(var(--c-txt)); }
.ide-note { margin: 0; font-size: 11.5px; line-height: 1.5; color: rgb(var(--c-txt3)); }
.ide-details summary { cursor: pointer; font-size: 12px; color: rgb(var(--c-txt2)); }
@media (prefers-reduced-motion: reduce) {
  .ide-primary, .ide-caret { transition: none; }
  .ide-menu { animation: none; }
}
</style>
