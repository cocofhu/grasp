<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import SettingsSection from '@/components/settings/SettingsSection.vue'
import SettingsRow from '@/components/settings/SettingsRow.vue'
import { api } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import { copyToClipboard } from '@/lib/shared/copyToClipboard'
import { fmtTime } from '@/lib/shared/format'

const { t } = useI18n()
const toast = useToast()

const PM_MCP_OPTIONS = [
  { id: 'pm-progress', labelKey: 'pages.workspace.pm.mcpProgress' },
  { id: 'pm-workflow-read', labelKey: 'pages.workspace.pm.mcpWorkflowRead' },
  { id: 'pm-workflow-write', labelKey: 'pages.workspace.pm.mcpWorkflowWrite' },
  { id: 'pm-agent-fs', labelKey: 'pages.workspace.pm.mcpAgentFs' },
  { id: 'pm-prd-manager', labelKey: 'pages.workspace.pm.mcpPrdManager' },
] as const

interface ExternalMcpSettings {
  enabled: boolean
  enabledPacks: string[]
  mcpBaseUrl: string
}

interface McpKeyItem {
  id: string
  name: string
  key_prefix: string
  created_at: string
}

const loading = ref(true)
const saving = ref(false)
const enabled = ref(false)
const enabledPacks = ref<string[]>([])
const mcpBaseUrl = ref('')
const keys = ref<McpKeyItem[]>([])
const loadingKeys = ref(false)
const showCreateKey = ref(false)
const keyName = ref('')
const newKeyPlain = ref('')
const creatingKey = ref(false)
const copied = ref('')

const mcpBaseForHint = computed(() => mcpBaseUrl.value || `${window.location.origin}/mcp/workspace`)

const packExampleUrl = computed(() => `${mcpBaseForHint.value}/pm-progress`)

const mcpJsonExample = computed(() => JSON.stringify({
  mcpServers: {
    'workspace-assistant': {
      url: packExampleUrl.value,
      headers: { Authorization: 'Bearer YOUR_WORKSPACE_MCP_KEY' },
    },
  },
}, null, 2))

async function loadSettings() {
  loading.value = true
  try {
    const s = await api.getExternalMcp()
    enabled.value = s.enabled
    enabledPacks.value = [...(s.enabledPacks || [])]
    mcpBaseUrl.value = s.mcpBaseUrl || ''
  } catch (e: unknown) {
    toast.error(String((e as Error)?.message || e))
  } finally {
    loading.value = false
  }
}

async function loadKeys() {
  loadingKeys.value = true
  try {
    keys.value = await api.listMcpKeys()
  } catch {
    keys.value = []
  } finally {
    loadingKeys.value = false
  }
}

function togglePack(id: string) {
  if (enabledPacks.value.includes(id)) {
    enabledPacks.value = enabledPacks.value.filter((x) => x !== id)
  } else {
    enabledPacks.value = [...enabledPacks.value, id]
  }
}

async function saveSettings() {
  saving.value = true
  try {
    const s = await api.updateExternalMcp({
      enabled: enabled.value,
      enabledPacks: enabledPacks.value,
    })
    enabled.value = s.enabled
    enabledPacks.value = [...(s.enabledPacks || [])]
    mcpBaseUrl.value = s.mcpBaseUrl || mcpBaseUrl.value
    toast.success(t('pages.workspace.externalMcp.saved'))
  } catch (e: unknown) {
    toast.error(String((e as Error)?.message || e))
  } finally {
    saving.value = false
  }
}

function openCreateKey() {
  keyName.value = ''
  newKeyPlain.value = ''
  showCreateKey.value = true
}

async function confirmCreateKey() {
  creatingKey.value = true
  try {
    const res = await api.createMcpKey(keyName.value)
    newKeyPlain.value = res.key
    await loadKeys()
  } catch (e: unknown) {
    toast.error(String((e as Error)?.message || e))
  } finally {
    creatingKey.value = false
  }
}

async function revokeKey(id: string) {
  try {
    await api.revokeMcpKey(id)
    await loadKeys()
    toast.success(t('pages.workspace.externalMcp.keyRevoked'))
  } catch (e: unknown) {
    toast.error(String((e as Error)?.message || e))
  }
}

async function copyText(text: string, label = '') {
  const ok = await copyToClipboard(text)
  if (ok) {
    copied.value = label
    toast.success(t('pages.workspace.externalMcp.copySuccess'))
    setTimeout(() => { copied.value = '' }, 2000)
  } else {
    toast.error(t('pages.workspace.externalMcp.copyFailed'))
  }
}

onMounted(async () => {
  await Promise.all([loadSettings(), loadKeys()])
})
</script>

<template>
  <div class="space-y-8" data-testid="workspace-external-mcp-panel">
    <div class="space-y-2 text-[13px] leading-6 text-txt3">
      <p>{{ t('pages.workspace.externalMcp.subtitle') }}</p>
      <p>{{ t('pages.workspace.externalMcp.distinctHint') }}</p>
    </div>

    <SettingsSection :title="t('pages.workspace.externalMcp.connectionTitle')">
      <SettingsRow :label="t('pages.workspace.externalMcp.enable')" :description="t('pages.workspace.externalMcp.enableHint')" inline>
        <AppSwitch v-model="enabled" data-testid="external-mcp-enabled" :disabled="loading || saving" :aria-label="t('pages.workspace.externalMcp.enable')" />
      </SettingsRow>
    </SettingsSection>

    <SettingsSection :title="t('pages.workspace.externalMcp.packsTitle')" :description="t('pages.workspace.externalMcp.packsHint')">
      <SettingsRow v-for="opt in PM_MCP_OPTIONS" :key="opt.id" :label="opt.id" :description="t(opt.labelKey)" inline>
        <AppSwitch :model-value="enabledPacks.includes(opt.id)" :disabled="loading || saving" :aria-label="opt.id" @update:model-value="togglePack(opt.id)" />
      </SettingsRow>
    </SettingsSection>
    <div class="flex justify-end">
      <AppButton variant="primary" data-testid="external-mcp-save" :disabled="loading || saving" @click="saveSettings">
        {{ saving ? t('common.buttons.saving') : t('pages.workspace.externalMcp.save') }}
      </AppButton>
    </div>

    <SettingsSection :title="t('pages.workspace.externalMcp.urlTitle')" :description="t('pages.workspace.externalMcp.urlHint')">
      <div class="space-y-3 py-4">
        <div class="flex items-center gap-2 rounded-lg border border-line bg-base px-3 py-2.5 font-mono text-[12px]">
          <code class="min-w-0 flex-1 break-all text-txt2">{{ mcpBaseUrl || t('pages.workspace.externalMcp.urlPending') }}</code>
          <button type="button" class="chip shrink-0 hover:border-line-strong" data-testid="external-mcp-copy-url" @click="copyText(mcpBaseUrl, 'url')">
            {{ copied === 'url' ? t('pages.workspace.externalMcp.copied') : t('pages.workspace.externalMcp.copy') }}
          </button>
        </div>
        <p class="text-xs leading-5 text-txt3">{{ t('pages.workspace.externalMcp.urlPackSuffix', { base: mcpBaseForHint }) }}</p>
      </div>
    </SettingsSection>

    <SettingsSection :title="t('pages.workspace.externalMcp.keysTitle')" :description="t('pages.workspace.externalMcp.keysHint')">
      <div class="space-y-3 py-4">
        <div class="flex justify-end">
          <AppButton size="sm" icon="plus" data-testid="external-mcp-create-key" @click="openCreateKey">{{ t('pages.workspace.externalMcp.createKey') }}</AppButton>
        </div>
        <div v-if="loadingKeys" class="py-4 text-center text-sm text-txt3">{{ t('common.loading') }}</div>
        <div v-else-if="!keys.length" class="py-5 text-center text-[13px] text-txt3">{{ t('pages.workspace.externalMcp.noKeys') }}</div>
        <div v-else class="divide-y divide-line">
          <div v-for="k in keys" :key="k.id" class="flex flex-wrap items-center gap-3 py-3" data-testid="external-mcp-key-row">
            <span class="min-w-0 flex-1 truncate text-[13px] font-medium text-txt">{{ k.name }}</span>
            <span class="font-mono text-[12px] text-txt3">{{ k.key_prefix }}</span>
            <span class="text-[11px] text-txt3">{{ fmtTime(k.created_at) }}</span>
            <AppButton variant="ghost" size="sm" class="!text-err" @click="revokeKey(k.id)">{{ t('pages.workspace.externalMcp.revoke') }}</AppButton>
          </div>
        </div>
      </div>
    </SettingsSection>

    <SettingsSection :title="t('pages.workspace.externalMcp.exampleTitle')" :description="t('pages.workspace.externalMcp.exampleHint')">
      <div class="py-4">
        <div class="relative rounded-lg border border-line bg-base p-3">
          <button type="button" class="chip absolute right-2 top-2 text-[11px]" data-testid="external-mcp-copy-example" @click="copyText(mcpJsonExample, 'example')">
            {{ copied === 'example' ? t('pages.workspace.externalMcp.copied') : t('pages.workspace.externalMcp.copy') }}
          </button>
          <pre class="scroll-area overflow-x-auto pr-16 font-mono text-[12px] leading-5 text-txt2">{{ mcpJsonExample }}</pre>
        </div>
      </div>
    </SettingsSection>

    <AppModal
      :open="showCreateKey"
      :title="newKeyPlain ? t('pages.workspace.externalMcp.keyCreatedTitle') : t('pages.workspace.externalMcp.createKeyTitle')"
      :width="440"
      @close="!creatingKey && (showCreateKey = false)"
    >
      <template v-if="!newKeyPlain">
        <label class="label" for="external-mcp-key-name">{{ t('pages.workspace.externalMcp.keyName') }}</label>
        <input
          id="external-mcp-key-name"
          v-model="keyName"
          class="input w-full"
          data-testid="external-mcp-key-name"
          :placeholder="t('pages.workspace.externalMcp.keyNamePlaceholder')"
        />
        <div class="mt-4 flex justify-end gap-2">
          <AppButton variant="ghost" @click="showCreateKey = false">{{ t('common.buttons.cancel') }}</AppButton>
          <AppButton variant="primary" :disabled="creatingKey || !keyName.trim()" @click="confirmCreateKey">
            {{ creatingKey ? t('common.buttons.saving') : t('pages.workspace.externalMcp.createKey') }}
          </AppButton>
        </div>
      </template>
      <template v-else>
        <p class="mb-3 text-[13px] text-warn">{{ t('pages.workspace.externalMcp.keyOnceHint') }}</p>
        <div class="flex items-center gap-2 rounded-md border border-line bg-base px-3 py-2 font-mono text-[12px]">
          <code class="min-w-0 flex-1 break-all text-accent-2">{{ newKeyPlain }}</code>
          <button type="button" class="chip shrink-0" @click="copyText(newKeyPlain, 'plain')">
            {{ copied === 'plain' ? t('pages.workspace.externalMcp.copied') : t('pages.workspace.externalMcp.copy') }}
          </button>
        </div>
        <div class="mt-4 flex justify-end">
          <AppButton variant="primary" @click="showCreateKey = false">{{ t('common.buttons.close') }}</AppButton>
        </div>
      </template>
    </AppModal>
  </div>
</template>
