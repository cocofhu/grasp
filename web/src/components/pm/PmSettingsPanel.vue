<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import SettingsSection from '@/components/settings/SettingsSection.vue'
import SettingsRow from '@/components/settings/SettingsRow.vue'
import PmChannelMultiPanel from '@/components/pm/PmChannelMultiPanel.vue'
import { api } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import type { Agent } from '@/lib/api/api'
import type { PmLeaderBinding, Workspace } from '@/lib/shared/types'

const props = defineProps<{ workspace?: Workspace | null }>()
const emit = defineEmits<{
  changed: [PmLeaderBinding]
  'workspace-updated': [workspace: Workspace]
}>()

const { t } = useI18n()
const toast = useToast()

const PM_MCP_OPTIONS = [
  { id: 'pm-progress', labelKey: 'pages.workspace.pm.mcpProgress' },
  { id: 'pm-workflow-read', labelKey: 'pages.workspace.pm.mcpWorkflowRead' },
  { id: 'pm-workflow-write', labelKey: 'pages.workspace.pm.mcpWorkflowWrite' },
  { id: 'pm-agent-fs', labelKey: 'pages.workspace.pm.mcpAgentFs' },
  { id: 'pm-prd-manager', labelKey: 'pages.workspace.pm.mcpPrdManager' },
] as const

const binding = ref<PmLeaderBinding | null>(null)
const agents = ref<Agent[]>([])
const selectedAgent = ref('')
const enabled = ref(false)
const enabledMcps = ref<string[]>(['pm-progress', 'pm-workflow-read', 'pm-workflow-write', 'pm-agent-fs', 'pm-prd-manager'])
const gateAutoVar = ref('')
const gateAutoPrompt = ref('')
const loading = ref(true)
const saving = ref(false)
const localWorkspace = ref<Workspace | null>(props.workspace || null)

const noAgents = computed(() => !loading.value && agents.value.length === 0)
const controlsDisabled = computed(() => noAgents.value || saving.value)

type BadgeKind = 'ok' | 'off' | 'err'

const statusBadge = computed(() => {
  if (loading.value) return null
  if (agents.value.length === 0) {
    return {
      kind: 'err' as BadgeKind,
      label: t('pages.workspace.pm.badgeUnavailable'),
      agentName: '',
    }
  }
  if (binding.value?.agentError) {
    return {
      kind: 'err' as BadgeKind,
      label: t('pages.workspace.pm.badgeError'),
      agentName: selectedAgent.value,
    }
  }
  if (enabled.value && selectedAgent.value) {
    return {
      kind: 'ok' as BadgeKind,
      label: t('pages.workspace.pm.badgeEnabled'),
      agentName: selectedAgent.value,
    }
  }
  if (enabled.value && !selectedAgent.value) {
    return {
      kind: 'err' as BadgeKind,
      label: t('pages.workspace.pm.badgePending'),
      agentName: '',
    }
  }
  return {
    kind: 'off' as BadgeKind,
    label: t('pages.workspace.pm.badgeDisabled'),
    agentName: '',
  }
})

const badgeClass = computed(() => {
  if (!statusBadge.value) return ''
  if (statusBadge.value.kind === 'ok') return 'border-ok/35 bg-ok/10 text-ok'
  if (statusBadge.value.kind === 'err') return 'border-err/40 bg-err/12 text-err'
  return 'border-line bg-elevated text-txt2'
})

const badgeDotClass = computed(() => {
  if (!statusBadge.value) return ''
  if (statusBadge.value.kind === 'ok') return 'bg-ok'
  if (statusBadge.value.kind === 'err') return 'bg-err'
  return 'bg-txt3'
})

const agentOptions = computed((): { name: string; stale: boolean }[] => {
  const names = agents.value.map((a) => a.name)
  const current = selectedAgent.value
  const options = agents.value.map((a) => ({ name: a.name, stale: false }))
  if (current && !names.includes(current)) {
    options.push({ name: current, stale: true })
  }
  return options
})

const aclText = computed(
  () => binding.value?.aclNote || t('pages.workspace.pm.aclMemoryNote'),
)

const channelWorkspace = computed<Workspace>(() => {
  if (localWorkspace.value) return localWorkspace.value
  if (props.workspace) return props.workspace
  return {
    name: '',
    description: '',
    variables: [],
    notifyPolicy: { enabled: true, defaultEvents: ['waiting_human', 'failed'], channelIds: [] },
  }
})

function toggleMcp(id: string) {
  if (controlsDisabled.value) return
  const set = new Set(enabledMcps.value)
  if (set.has(id)) set.delete(id)
  else set.add(id)
  enabledMcps.value = PM_MCP_OPTIONS.map((o) => o.id).filter((x) => set.has(x))
}

async function load() {
  loading.value = true
  try {
    const [b, list, proj] = await Promise.all([
      api.getPmLeader(),
      api.listAgents(),
      props.workspace
        ? Promise.resolve(props.workspace)
        : api.getWorkspace().catch(() => null),
    ])
    binding.value = b
    agents.value = list || []
    selectedAgent.value = b.agentConfigRef || ''
    enabled.value = agents.value.length === 0 ? false : b.enabled
    enabledMcps.value = Array.isArray(b.enabledMcps)
      ? [...b.enabledMcps]
      : ['pm-progress', 'pm-workflow-read', 'pm-workflow-write', 'pm-agent-fs', 'pm-prd-manager']
    gateAutoVar.value = b.gateAutoVar || ''
    gateAutoPrompt.value = b.gateAutoPrompt || ''
    if (proj) localWorkspace.value = proj
  } catch (e: any) {
    toast.error(String(e?.message || e))
  } finally {
    loading.value = false
  }
}

async function save() {
  if (noAgents.value) {
    toast.error(t('pages.workspace.pm.noAgents'))
    return
  }
  if (enabled.value && !selectedAgent.value) {
    toast.error(t('pages.workspace.pm.needAgent'))
    return
  }
  saving.value = true
  try {
    binding.value = await api.updatePmLeader({
      enabled: enabled.value,
      agentConfigRef: selectedAgent.value,
      enabledMcps: [...enabledMcps.value],
      gateAutoVar: gateAutoVar.value.trim(),
      gateAutoPrompt: gateAutoPrompt.value,
    })
    enabled.value = binding.value.enabled
    selectedAgent.value = binding.value.agentConfigRef || ''
    enabledMcps.value = Array.isArray(binding.value.enabledMcps)
      ? [...binding.value.enabledMcps]
      : ['pm-progress', 'pm-workflow-read', 'pm-workflow-write', 'pm-agent-fs', 'pm-prd-manager']
    gateAutoVar.value = binding.value.gateAutoVar || ''
    gateAutoPrompt.value = binding.value.gateAutoPrompt || ''
    emit('changed', binding.value)
    toast.success(t('pages.workspace.saved'))
  } catch (e: any) {
    toast.error(String(e?.message || e))
  } finally {
    saving.value = false
  }
}

function onWorkspaceUpdated(p: Workspace) {
  localWorkspace.value = p
  emit('workspace-updated', p)
}

watch(noAgents, (empty) => {
  if (empty) enabled.value = false
})

watch(selectedAgent, (name) => {
  if (!binding.value?.agentError) return
  if (name && agents.value.some((a) => a.name === name)) {
    binding.value = { ...binding.value, agentError: '' }
  }
})

watch(
  () => props.workspace,
  (p) => {
    if (p) localWorkspace.value = p
  },
)

void load()
</script>

<template>
  <div class="space-y-7" data-testid="pm-settings-panel" :aria-busy="loading">
    <p v-if="loading" class="text-sm text-txt3" role="status">
      {{ t('common.buttons.loading') }}
    </p>

    <template v-else>
      <SettingsSection
        :title="t('pages.workspace.pm.defaultAssistantSection')"
        :description="aclText"
      >
        <SettingsRow
          :label="t('pages.workspace.pm.enable')"
          :description="t('pages.workspace.pm.enablePermissionHint')"
          inline
        >
          <template #meta>
            <span
              v-if="statusBadge"
              class="inline-flex max-w-full items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px]"
              :class="badgeClass"
              role="status"
            >
              <span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="badgeDotClass" aria-hidden="true" />
              <span>{{ statusBadge.label }}</span>
              <span
                v-if="statusBadge.agentName"
                class="max-w-[160px] truncate opacity-90"
                :title="statusBadge.agentName"
              >
                {{ statusBadge.agentName }}
              </span>
            </span>
          </template>
          <AppSwitch
            v-model="enabled"
            :disabled="controlsDisabled"
            :aria-label="t('pages.workspace.pm.enable')"
          />
        </SettingsRow>

        <SettingsRow :label="t('pages.workspace.pm.bindAgent')" for="pm-bind-agent">
          <select
            id="pm-bind-agent"
            v-model="selectedAgent"
            class="input w-full disabled:cursor-not-allowed disabled:opacity-55 sm:w-60"
            :disabled="controlsDisabled"
          >
            <option value="">{{ t('pages.workspace.pm.selectAgent') }}</option>
            <option v-for="a in agentOptions" :key="a.name" :value="a.name">
              {{ a.stale ? `${a.name}${t('pages.workspace.pm.agentStaleSuffix')}` : a.name }}
            </option>
          </select>
          <template v-if="noAgents || binding?.agentError" #details>
            <p class="m-0 text-err" role="alert">
              {{ noAgents ? t('pages.workspace.pm.noAgents') : binding?.agentError }}
              <RouterLink to="/agents" class="ml-1 text-accent-2 underline underline-offset-2 hover:text-accent">
                {{ t('pages.workspace.pm.goAgents') }}
              </RouterLink>
            </p>
          </template>
        </SettingsRow>
      </SettingsSection>

      <SettingsSection
        :title="t('pages.workspace.pm.enabledMcps')"
        :description="t('pages.workspace.pm.enabledMcpsHint')"
      >
        <SettingsRow
          v-for="opt in PM_MCP_OPTIONS"
          :key="opt.id"
          :label="t(opt.labelKey)"
          inline
        >
          <template #meta>
            <code class="font-mono text-[11px]">{{ opt.id }}</code>
          </template>
          <AppSwitch
            :model-value="enabledMcps.includes(opt.id)"
            :disabled="controlsDisabled"
            :aria-label="opt.id"
            @update:model-value="toggleMcp(opt.id)"
          />
        </SettingsRow>
      </SettingsSection>

      <SettingsSection :title="t('pages.workspace.pm.automaticApprovalSection')">
        <SettingsRow
          :label="t('pages.workspace.pm.gateAutoVar')"
          :description="t('pages.workspace.pm.gateAutoVarHint')"
          for="pm-gate-auto-var"
          stacked
        >
          <input
            id="pm-gate-auto-var"
            v-model="gateAutoVar"
            class="input w-full font-mono text-[13px] disabled:cursor-not-allowed disabled:opacity-55 sm:max-w-sm"
            data-testid="pm-gate-auto-var"
            :disabled="controlsDisabled"
            :placeholder="t('pages.workspace.pm.gateAutoVarPlaceholder')"
            autocomplete="off"
            spellcheck="false"
          />
        </SettingsRow>
        <SettingsRow
          :label="t('pages.workspace.pm.gateAutoPrompt')"
          :description="t('pages.workspace.pm.gateAutoPromptHint')"
          for="pm-gate-auto-prompt"
          stacked
        >
          <textarea
            id="pm-gate-auto-prompt"
            v-model="gateAutoPrompt"
            class="input min-h-24 w-full font-mono text-[12px] leading-relaxed disabled:cursor-not-allowed disabled:opacity-55"
            data-testid="pm-gate-auto-prompt"
            :disabled="controlsDisabled"
            :placeholder="t('pages.workspace.pm.gateAutoPromptPlaceholder')"
            rows="4"
          />
        </SettingsRow>
      </SettingsSection>

      <div class="flex justify-end" data-testid="pm-leader-actions">
        <AppButton
          variant="primary"
          data-testid="pm-leader-save"
          :disabled="saving || noAgents || loading"
          @click="save"
        >
          {{ saving ? t('common.buttons.saving') : t('pages.workspace.pm.saveLeaderButton') }}
        </AppButton>
      </div>

      <PmChannelMultiPanel
        :workspace="channelWorkspace"
        :pm-leader-agent="selectedAgent"
        @workspace-updated="onWorkspaceUpdated"
      />
    </template>
  </div>
</template>
