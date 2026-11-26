<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import SoloPickerShell from '@/components/solo/SoloPickerShell.vue'
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
const preferred = ref<PreferredIDE | null>(loadPreferredIDE())
const showInfo = ref(false)
const dialog = ref<HTMLElement | null>(null)
const panelWidth = ref(320)
const root = ref<HTMLElement | null>(null)
const copied = ref('')
let copiedTimer: ReturnType<typeof setTimeout> | undefined

const endpoint = computed(() => sshEndpoint(props.info, window.location.hostname))
const command = computed(() => sshCommand(props.sandboxId, endpoint.value))
const configSnippet = computed(() => sshConfigSnippet(endpoint.value))
const preferredOption = computed(() => preferred.value ? remoteIDEOption(preferred.value.id) : undefined)
/** China editions are their own rows, not a badge beside the international app. */
const menuEntries = computed(() => REMOTE_IDES.flatMap((option) => {
  const rows = [{ option, cn: false, scheme: option.scheme }]
  if (option.cnScheme) rows.push({ option, cn: true, scheme: option.cnScheme })
  return rows
}))

function link(option: RemoteIDEOption, cn = false) {
  return props.blocked ? '' : remoteIDELink(option.id, props.sandboxId, endpoint.value, { cn })
}

function label(option: RemoteIDEOption, cn = false) {
  return cn ? `${option.label} ${t('pages.solo.sandboxInfo.ide.cnEdition')}` : option.label
}

const triggerLabel = computed(() => {
  if (!preferred.value || !preferredOption.value) return t('pages.solo.sandboxInfo.ide.sshConnect')
  return label(preferredOption.value, preferred.value.cn)
})

function choose(option: RemoteIDEOption, cn: boolean, close: (refocus?: boolean) => void) {
  preferred.value = { id: option.id, cn }
  savePreferredIDE(preferred.value)
  if (!props.blocked) emit('launch')
  close()
}

function openInfo(close: (refocus?: boolean) => void) {
  showInfo.value = true
  close(false)
}

watch(showInfo, async (open) => {
  if (!open) return
  await nextTick()
  dialog.value?.focus()
})

function syncWidth() {
  const trigger = root.value?.querySelector('.solo-picker__trigger')
  const width = trigger?.getBoundingClientRect().width
  if (width && width > 0) panelWidth.value = Math.round(width)
}

onMounted(syncWidth)

async function copy(key: string, text: string) {
  if (!(await copyToClipboard(text))) return
  copied.value = key
  if (copiedTimer) clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => { copied.value = '' }, 1500)
}

onBeforeUnmount(() => {
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
  <div ref="root" class="ide-menu-root" data-testid="sandbox-ide-menu">
    <SoloPickerShell
      class="ide-picker"
      :label="triggerLabel"
      test-id="sandbox-ide-picker"
      :panel-width="panelWidth"
      @open="syncWidth"
    >
      <template #trigger>
        <IdeLogo v-if="preferred" :ide="preferred.id" :size="16" />
        <Icon v-else name="terminal" :size="16" :interactive="false" class="shrink-0 text-txt2" />
      </template>
      <template #default="{ close }">
        <component
          :is="link(entry.option, entry.cn) ? 'a' : 'button'"
          v-for="entry in menuEntries"
          :key="entry.cn ? `${entry.option.id}-cn` : entry.option.id"
          :type="link(entry.option, entry.cn) ? undefined : 'button'"
          role="option"
          data-picker-opt
          :href="link(entry.option, entry.cn) || undefined"
          :disabled="blocked && entry.scheme ? true : undefined"
          :aria-disabled="blocked && entry.scheme ? 'true' : undefined"
          :aria-selected="!!preferred && preferred.id === entry.option.id && preferred.cn === entry.cn"
          :data-testid="entry.cn ? `sandbox-ide-${entry.option.id}-cn` : `sandbox-ide-${entry.option.id}`"
          @click="blocked && entry.scheme ? undefined : choose(entry.option, entry.cn, close)"
        >
          <IdeLogo :ide="entry.option.id" :size="16" />
          <span class="min-w-0 flex-1 truncate">{{ label(entry.option, entry.cn) }}</span>
          <Icon v-if="preferred && preferred.id === entry.option.id && preferred.cn === entry.cn" name="check" :size="14" class="shrink-0 text-accent" aria-hidden="true" />
        </component>
      </template>
      <template #footer="{ close }">
        <button type="button" data-picker-opt data-testid="sandbox-ide-info" @click="openInfo(close)">
          <Icon name="info" :size="15" class="shrink-0 text-txt3" aria-hidden="true" />
          <span class="flex-1">{{ t('pages.solo.sandboxInfo.ide.info') }}</span>
        </button>
      </template>
    </SoloPickerShell>

    <Teleport to="body">
      <div v-if="showInfo" class="ide-dialog-layer" data-testid="sandbox-ide-details" @keydown.escape.stop="showInfo = false">
        <div class="ide-dialog-scrim" @click="showInfo = false" />
        <div ref="dialog" class="ide-dialog" role="dialog" aria-modal="true" tabindex="-1" :aria-label="t('pages.solo.sandboxInfo.ide.info')">
          <header class="ide-dialog-head">
            <h2>{{ t('pages.solo.sandboxInfo.ide.info') }}</h2>
            <button type="button" class="ide-dialog-close" :aria-label="t('common.buttons.close')" data-testid="sandbox-ide-details-close" @click="showInfo = false">
              <Icon name="close" :size="16" :interactive="false" aria-hidden="true" />
            </button>
          </header>
          <div class="ide-dialog-body">
            <section class="ide-command">
              <div class="ide-command-label">{{ t('pages.solo.sandboxInfo.ide.command') }}</div>
              <div class="ide-code" :title="t('pages.solo.sandboxInfo.ide.commandHint')">
                <code data-testid="sandbox-info-command">{{ command }}</code>
                <button type="button" class="ide-copy" :aria-label="t('pages.solo.sandboxInfo.copy')" data-testid="sandbox-info-copy-command" @click="copy('command', command)">
                  <Icon :name="copied === 'command' ? 'check' : 'copy'" :size="14" aria-hidden="true" />
                </button>
              </div>
            </section>
            <div class="ide-fields">
              <div v-for="row in infoRows" :key="row.key" class="ide-field">
                <span class="ide-field-label">{{ row.label }}</span>
                <span class="ide-field-value font-mono">{{ row.value }}</span>
                <button type="button" class="ide-copy" :aria-label="t('pages.solo.sandboxInfo.copy')" :data-testid="`sandbox-ide-copy-${row.key}`" @click="copy(row.key, row.value)">
                  <Icon :name="copied === row.key ? 'check' : 'copy'" :size="14" aria-hidden="true" />
                </button>
              </div>
              <div v-if="info.hostKeyFingerprint" class="ide-field">
                <span class="ide-field-label">{{ t('pages.solo.sandboxInfo.ide.hostKey') }}</span>
                <span class="ide-field-value font-mono">{{ info.hostKeyFingerprint }}</span>
                <button type="button" class="ide-copy" :aria-label="t('pages.solo.sandboxInfo.copy')" data-testid="sandbox-ide-copy-fingerprint" @click="copy('fingerprint', info.hostKeyFingerprint)">
                  <Icon :name="copied === 'fingerprint' ? 'check' : 'copy'" :size="14" aria-hidden="true" />
                </button>
              </div>
            </div>
            <details class="ide-details">
              <summary>
                <Icon name="chevron-right" :size="14" :interactive="false" aria-hidden="true" />
                <span>{{ t('pages.solo.sandboxInfo.ide.config') }}</span>
              </summary>
              <div class="ide-code is-block">
                <pre data-testid="sandbox-info-config">{{ configSnippet }}</pre>
                <button type="button" class="ide-copy" :aria-label="t('pages.solo.sandboxInfo.copy')" data-testid="sandbox-info-copy-config" @click="copy('config', configSnippet)">
                  <Icon :name="copied === 'config' ? 'check' : 'copy'" :size="14" aria-hidden="true" />
                </button>
              </div>
            </details>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.ide-menu-root { display: flex; flex-direction: column; gap: 8px; }
.ide-picker.solo-picker { max-width: none; width: 100%; }
.ide-picker :deep(.solo-picker__trigger) {
  height: 34px;
  border-radius: 8px;
  background: rgb(var(--c-surface));
  font-size: 13px;
}
.ide-dialog-layer { position: fixed; z-index: 80; inset: 0; display: flex; align-items: center; justify-content: center; padding: 24px; }
.ide-dialog-scrim { position: absolute; inset: 0; background: rgb(0 0 0 / .55); backdrop-filter: blur(6px); }
.ide-dialog {
  position: relative; display: flex; width: min(600px, 100%); max-height: min(640px, 100%);
  flex-direction: column; overflow: hidden;
  border: 1px solid rgb(var(--c-line)); border-radius: 12px; background: rgb(var(--c-surface));
  box-shadow: 0 24px 64px rgb(0 0 0 / .28); outline: none;
}
.ide-dialog-head {
  display: flex; height: 56px; flex: none; align-items: center; gap: 8px;
  padding: 0 12px 0 20px; border-bottom: 1px solid rgb(var(--c-line));
}
.ide-dialog-head h2 { margin: 0; flex: 1; font-size: 15px; font-weight: 600; color: rgb(var(--c-txt)); }
.ide-dialog-close {
  display: inline-flex; width: 32px; height: 32px; flex: none; align-items: center; justify-content: center;
  border-radius: 8px; color: rgb(var(--c-txt3));
}
.ide-dialog-close:hover { background: rgb(var(--c-elevated)); color: rgb(var(--c-txt)); }
.ide-dialog-body { display: flex; min-height: 0; flex: 1; flex-direction: column; gap: 16px; overflow: auto; padding: 16px 20px 18px; }
.ide-command { display: flex; flex-direction: column; gap: 8px; }
.ide-command-label { font-size: 12px; font-weight: 600; color: rgb(var(--c-txt2)); }
.ide-fields { overflow: hidden; border: 1px solid rgb(var(--c-line)); border-radius: 10px; }
.ide-field { display: grid; grid-template-columns: 6.5rem minmax(0, 1fr) 32px; align-items: center; gap: 8px; min-height: 40px; padding: 0 6px 0 12px; }
.ide-field + .ide-field { border-top: 1px solid rgb(var(--c-line) / .8); }
.ide-field-label { font-size: 12px; white-space: nowrap; color: rgb(var(--c-txt3)); }
.ide-field-value { overflow-x: auto; font-size: 12.5px; white-space: nowrap; color: rgb(var(--c-txt)); }
.ide-code {
  display: flex; align-items: center; gap: 8px; min-height: 40px; padding: 8px 6px 8px 12px;
  border: 1px solid rgb(var(--c-line)); border-radius: 10px; background: rgb(var(--c-base));
  font-family: var(--font-mono, ui-monospace, monospace); font-size: 12.5px;
}
.ide-code code, .ide-code pre { min-width: 0; flex: 1; margin: 0; overflow-x: auto; white-space: pre; color: rgb(var(--c-txt)); }
.ide-code.is-block { align-items: flex-start; margin-top: 8px; }
.ide-copy {
  display: inline-flex; width: 28px; height: 28px; flex: none; align-items: center; justify-content: center;
  border-radius: 7px; color: rgb(var(--c-txt3));
}
.ide-copy:hover { background: rgb(var(--c-txt) / .06); color: rgb(var(--c-txt)); }
.ide-details summary {
  display: flex; align-items: center; gap: 6px; width: fit-content; cursor: pointer;
  font-size: 13px; color: rgb(var(--c-txt2)); list-style: none;
}
.ide-details summary::-webkit-details-marker { display: none; }
.ide-details summary :deep(svg) { transition: transform var(--dur-ui); }
.ide-details[open] summary :deep(svg) { transform: rotate(90deg); }
</style>
