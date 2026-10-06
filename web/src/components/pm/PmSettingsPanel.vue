<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import PmChannelMultiPanel from '@/components/pm/PmChannelMultiPanel.vue'
import { api } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import type { Agent } from '@/lib/api/api'
import type { PmLeaderBinding, Project } from '@/lib/shared/types'

const props = defineProps<{ projectId: string; project?: Project | null }>()
const emit = defineEmits<{
  changed: [PmLeaderBinding]
  'project-updated': [project: Project]
}>()

const { t } = useI18n()
const toast = useToast()

const PM_MCP_OPTIONS = [
  { id: 'pm-progress', labelKey: 'pages.projectDetail.pm.mcpProgress' },
  { id: 'pm-workflow-read', labelKey: 'pages.projectDetail.pm.mcpWorkflowRead' },
  { id: 'pm-workflow-write', labelKey: 'pages.projectDetail.pm.mcpWorkflowWrite' },
  { id: 'pm-agent-fs', labelKey: 'pages.projectDetail.pm.mcpAgentFs' },
  { id: 'pm-prd-manager', labelKey: 'pages.projectDetail.pm.mcpPrdManager' },
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
const localProject = ref<Project | null>(props.project || null)

const noAgents = computed(() => !loading.value && agents.value.length === 0)
const controlsDisabled = computed(() => noAgents.value || saving.value)

type BadgeKind = 'ok' | 'off' | 'err'

const statusBadge = computed(() => {
  if (loading.value) return null
  if (agents.value.length === 0) {
    return {
      kind: 'err' as BadgeKind,
      label: t('pages.projectDetail.pm.badgeUnavailable'),
      agentName: '',
    }
  }
  if (binding.value?.agentError) {
    return {
      kind: 'err' as BadgeKind,
      label: t('pages.projectDetail.pm.badgeError'),
      agentName: selectedAgent.value,
    }
  }
  if (enabled.value && selectedAgent.value) {
    return {
      kind: 'ok' as BadgeKind,
      label: t('pages.projectDetail.pm.badgeEnabled'),
      agentName: selectedAgent.value,
    }
  }
  if (enabled.value && !selectedAgent.value) {
    return {
      kind: 'err' as BadgeKind,
      label: t('pages.projectDetail.pm.badgePending'),
      agentName: '',
    }
  }
  return {
    kind: 'off' as BadgeKind,
    label: t('pages.projectDetail.pm.badgeDisabled'),
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
  () => binding.value?.aclNote || t('pages.projectDetail.pm.aclMemoryNote'),
)

const channelProject = computed<Project>(() => {
  if (localProject.value) return localProject.value
  if (props.project) return props.project
  return {
    id: props.projectId,
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
      api.getPmLeader(props.projectId),
      api.listAgents(),
      props.project
        ? Promise.resolve(props.project)
        : api.getProject(props.projectId).catch(() => null),
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
    if (proj) localProject.value = proj
  } catch (e: any) {
    toast.error(String(e?.message || e))
  } finally {
    loading.value = false
  }
}

async function save() {
  if (noAgents.value) {
    toast.error(t('pages.projectDetail.pm.noAgents'))
    return
  }
  if (enabled.value && !selectedAgent.value) {
    toast.error(t('pages.projectDetail.pm.needAgent'))
    return
  }
  saving.value = true
  try {
    binding.value = await api.updatePmLeader(props.projectId, {
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
    toast.success(t('pages.projectDetail.saved'))
  } catch (e: any) {
    toast.error(String(e?.message || e))
  } finally {
    saving.value = false
  }
}

function onProjectUpdated(p: Project) {
  localProject.value = p
  emit('project-updated', p)
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
  () => props.projectId,
  () => {
    void load()
  },
)

watch(
  () => props.project,
  (p) => {
    if (p) localProject.value = p
  },
)

void load()
</script>

<template>
  <div
    class="rounded-lg flex min-h-0 flex-1 flex-col overflow-hidden border border-line bg-surface shadow-[var(--shadow-card)]"
  >
    <div
      class="flex shrink-0 flex-wrap items-start justify-between gap-3 border-b border-line bg-elevated/55 px-4 py-3.5"
    >
      <div class="min-w-0 max-w-[42em]">
        <h2 class="m-0 text-sm font-semibold text-txt">
          {{ t('pages.projectDetail.pm.settingsTitle') }}
        </h2>
      </div>
      <div
        v-if="statusBadge"
        class="rounded-md inline-flex max-w-full items-center gap-1.5 whitespace-nowrap border px-2.5 py-1 text-xs"
        :class="badgeClass"
        role="status"
      >
        <span class="h-1.5 w-1.5 shrink-0" :class="badgeDotClass" aria-hidden="true" />
        <span>{{ statusBadge.label }}</span>
        <span
          v-if="statusBadge.agentName"
          class="max-w-[160px] truncate opacity-90"
          :title="statusBadge.agentName"
        >
          {{ statusBadge.agentName }}
        </span>
      </div>
    </div>

    <div class="scroll-area flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4">
      <div
        class="rounded-lg flex items-start gap-2.5 border border-accent-2/20 bg-accent-dim/85 px-3 py-2.5 text-[13px] leading-snug text-txt"
      >
        <svg
          class="mt-0.5 h-3.5 w-3.5 shrink-0 stroke-accent-2"
          viewBox="0 0 24 24"
          fill="none"
          stroke-width="1.75"
          aria-hidden="true"
        >
          <circle cx="12" cy="12" r="9" />
          <path d="M12 8v5M12 16h.01" stroke-linecap="square" />
        </svg>
        <span>{{ aclText }}</span>
      </div>

      <div v-if="loading" class="text-sm text-txt3">{{ t('common.buttons.loading') }}</div>

      <template v-else>
        <div class="flex items-start justify-between gap-4 pt-1 max-sm:flex-col max-sm:items-stretch">
          <div class="min-w-0">
            <strong class="block text-sm font-medium text-txt">
              {{ t('pages.projectDetail.pm.enable') }}
            </strong>
            <p class="m-0 mt-1 text-xs leading-snug text-txt3">
              {{ t('pages.projectDetail.pm.enablePermissionHint') }}
            </p>
          </div>
          <AppSwitch
            v-model="enabled"
            class="max-sm:self-end"
            :disabled="controlsDisabled"
            :aria-label="t('pages.projectDetail.pm.enable')"
          />
        </div>

        <div>
          <label class="label" for="pm-bind-agent">{{ t('pages.projectDetail.pm.bindAgent') }}</label>
          <select
            id="pm-bind-agent"
            v-model="selectedAgent"
            class="input max-w-md disabled:cursor-not-allowed disabled:opacity-55"
            :disabled="controlsDisabled"
          >
            <option value="">{{ t('pages.projectDetail.pm.selectAgent') }}</option>
            <option v-for="a in agentOptions" :key="a.name" :value="a.name">
              {{ a.stale ? `${a.name}${t('pages.projectDetail.pm.agentStaleSuffix')}` : a.name }}
            </option>
          </select>
          <p v-if="noAgents" class="mt-2 text-[13px] leading-snug text-err">
            {{ t('pages.projectDetail.pm.noAgents') }}
            <RouterLink to="/agents" class="ml-1 text-accent-2 underline underline-offset-2 hover:text-accent">
              {{ t('pages.projectDetail.pm.goAgents') }}
            </RouterLink>
          </p>
          <p
            v-else-if="binding?.agentError"
            class="mt-2 text-[13px] leading-snug text-err"
          >
            {{ binding.agentError }}
            <RouterLink to="/agents" class="ml-1 text-accent-2 underline underline-offset-2 hover:text-accent">
              {{ t('pages.projectDetail.pm.goAgents') }}
            </RouterLink>
          </p>
        </div>

        <div>
          <strong class="block text-sm font-medium text-txt">
            {{ t('pages.projectDetail.pm.enabledMcps') }}
          </strong>
          <p class="m-0 mt-1 text-xs leading-snug text-txt3">
            {{ t('pages.projectDetail.pm.enabledMcpsHint') }}
          </p>
          <div class="mt-2.5 flex flex-col gap-2">
            <label
              v-for="opt in PM_MCP_OPTIONS"
              :key="opt.id"
              class="flex cursor-pointer items-start gap-2.5 text-[13px] text-txt"
              :class="controlsDisabled ? 'cursor-not-allowed opacity-55' : ''"
            >
              <AppSwitch
                class="mt-0.5"
                :model-value="enabledMcps.includes(opt.id)"
                :disabled="controlsDisabled"
                :aria-label="opt.id"
                @update:model-value="toggleMcp(opt.id)"
              />
              <span>
                <code class="font-mono text-[12px] text-accent-2">{{ opt.id }}</code>
                <span class="mt-0.5 block text-xs text-txt3">{{ t(opt.labelKey) }}</span>
              </span>
            </label>
          </div>
        </div>

        <div>
          <label class="label" for="pm-gate-auto-var">
            {{ t('pages.projectDetail.pm.gateAutoVar') }}
          </label>
          <input
            id="pm-gate-auto-var"
            v-model="gateAutoVar"
            class="input max-w-md font-mono text-[13px] disabled:cursor-not-allowed disabled:opacity-55"
            data-testid="pm-gate-auto-var"
            :disabled="controlsDisabled"
            :placeholder="t('pages.projectDetail.pm.gateAutoVarPlaceholder')"
            autocomplete="off"
            spellcheck="false"
          />
          <p class="m-0 mt-1 text-xs leading-snug text-txt3">
            {{ t('pages.projectDetail.pm.gateAutoVarHint') }}
          </p>
        </div>

        <div>
          <label class="label" for="pm-gate-auto-prompt">
            {{ t('pages.projectDetail.pm.gateAutoPrompt') }}
          </label>
          <textarea
            id="pm-gate-auto-prompt"
            v-model="gateAutoPrompt"
            class="input min-h-[88px] max-w-xl font-mono text-[12px] leading-snug disabled:cursor-not-allowed disabled:opacity-55"
            data-testid="pm-gate-auto-prompt"
            :disabled="controlsDisabled"
            :placeholder="t('pages.projectDetail.pm.gateAutoPromptPlaceholder')"
            rows="4"
          />
          <p class="m-0 mt-1 text-xs leading-snug text-txt3">
            {{ t('pages.projectDetail.pm.gateAutoPromptHint') }}
          </p>
        </div>

        <PmChannelMultiPanel
          :project-id="projectId"
          :project="channelProject"
          :pm-leader-agent="selectedAgent"
          @project-updated="onProjectUpdated"
        />
      </template>
    </div>

    <div class="flex shrink-0 flex-wrap justify-end gap-2 border-t border-line bg-surface p-3">
      <AppButton
        variant="primary"
        data-testid="pm-leader-save"
        :disabled="saving || noAgents || loading"
        @click="save"
      >
        {{ saving ? t('common.buttons.saving') : t('pages.projectDetail.pm.saveLeaderButton') }}
      </AppButton>
    </div>
  </div>
</template>
