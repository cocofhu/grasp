import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type ChannelConfig, type ChannelConfigInput } from '@/lib/api/api'
import {
  deriveRecentPushTargets,
  pushTargetPrimaryLabel,
  type PushTargetOption,
} from '@/lib/pm/cronDeliverTargets'
import { useToast } from '@/lib/composables/useToast'
import type { Workspace, WorkspaceNotifyPolicy } from '@/lib/shared/types'

export interface UsePmChannelMultiProps {
  workspace: Workspace
  pmLeaderAgent?: string
}

export type UsePmChannelMultiEmit = (event: 'workspace-updated', workspace: Workspace) => void

export function usePmChannelMulti(props: UsePmChannelMultiProps, emit: UsePmChannelMultiEmit) {
const { t } = useI18n()
const toast = useToast()

const PM_MCP_OPTIONS = [
  { id: 'pm-progress', labelKey: 'pages.workspace.pm.mcpProgress' },
  { id: 'pm-workflow-read', labelKey: 'pages.workspace.pm.mcpWorkflowRead' },
  { id: 'pm-workflow-write', labelKey: 'pages.workspace.pm.mcpWorkflowWrite' },
  { id: 'pm-agent-fs', labelKey: 'pages.workspace.pm.mcpAgentFs' },
  { id: 'pm-prd-manager', labelKey: 'pages.workspace.pm.mcpPrdManager' },
] as const

type Tab = 'list' | 'edit' | 'notify'

const tab = ref<Tab>('list')
const loading = ref(true)
const saving = ref(false)
const channelList = ref<ChannelConfig[]>([])
const freeAgents = ref<string[]>([])
const secretsKeyConfigured = ref<boolean | undefined>(undefined)

const editingId = ref<string | null>(null)
const isNew = ref(false)
const chType = ref<'qq' | 'wecom' | 'feishu' | 'dingtalk'>('qq')
const saveError = ref('')
const notifyReceipts = ref<{ runId: string; kind: string; status?: string; error?: string; createdAt: string }[]>([])
const chRegion = ref<'cn' | 'lark'>('cn')
const chMarkdown = ref(true)
const chIntents = ref('')
const chEnabled = ref(true)
const chName = ref('')
const chAgent = ref('')
const chAppId = ref('')
const chAppSecret = ref('')
const chAppSecretSet = ref(false)
const chTurnTimeout = ref(0)
const chSandbox = ref(false)
const chAllowMemoryWrite = ref(true)
const chAllowSchedulerWrite = ref(true)
const chCronDeliver = ref(false)
const chCronDeliverTarget = ref('')
const chEnabledMcps = ref<string[]>([
  'pm-progress',
  'pm-workflow-read',
  'pm-workflow-write',
  'pm-agent-fs',
  'pm-prd-manager',
])
const chIsPrimary = ref(false)

const notifySelected = ref<string[]>([])
const notifySaving = ref(false)

const deleteOpen = ref(false)
const deleteTarget = ref<ChannelConfig | null>(null)
const deleteMode = ref<'promote' | 'none'>('promote')
const deleteNewPrimaryId = ref('')
const deleteSyncPmLeader = ref(true)

const TARGET_LISTBOX_ID = 'ch-multi-cron-target-listbox'
const recentTargets = ref<PushTargetOption[]>([])
const recentTargetsLoaded = ref(false)
const recentTargetsLoading = ref(false)
const targetComboOpen = ref(false)
const targetComboRoot = ref<HTMLElement | null>(null)
const targetActiveIndex = ref(-1)

const hasPrimary = computed(() => channelList.value.some((c) => c.isPrimary))
const addButtonLabel = computed(() =>
  hasPrimary.value
    ? t('pages.workspace.pm.channel.addSecondary')
    : t('pages.workspace.pm.channel.addPrimary'),
)

const agentOptions = computed(() => {
  const opts = new Set(freeAgents.value)
  if (chAgent.value) opts.add(chAgent.value)
  return [...opts].sort()
})

const editingChannel = computed(() =>
  editingId.value ? channelList.value.find((c) => c.id === editingId.value) || null : null,
)

function clearRecentTargetsCache() {
  recentTargets.value = []
  recentTargetsLoaded.value = false
  recentTargetsLoading.value = false
  targetComboOpen.value = false
  targetActiveIndex.value = -1
}

async function ensureRecentTargets() {
  if (!chCronDeliver.value) return
  if (recentTargetsLoaded.value || recentTargetsLoading.value) return
  recentTargetsLoading.value = true
  try {
    const res = await api.listPmThreads()
    recentTargets.value = deriveRecentPushTargets(res.items || [], chType.value)
    recentTargetsLoaded.value = true
  } catch {
    recentTargets.value = []
    recentTargetsLoaded.value = true
  } finally {
    recentTargetsLoading.value = false
  }
}

function setTargetComboOpen(open: boolean) {
  targetComboOpen.value = open
  if (!open) targetActiveIndex.value = -1
  if (open) void ensureRecentTargets()
}

function selectPushTarget(value: string) {
  chCronDeliverTarget.value = value
  setTargetComboOpen(false)
}

function onTargetComboDocClick(ev: MouseEvent) {
  if (!targetComboOpen.value) return
  const el = targetComboRoot.value
  if (el && !el.contains(ev.target as Node)) setTargetComboOpen(false)
}

function onTargetInputKeydown(e: KeyboardEvent) {
  const opts = recentTargets.value
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    if (!targetComboOpen.value) setTargetComboOpen(true)
    if (!opts.length) return
    targetActiveIndex.value =
      targetActiveIndex.value < 0 ? 0 : (targetActiveIndex.value + 1) % opts.length
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    if (!targetComboOpen.value) setTargetComboOpen(true)
    if (!opts.length) return
    targetActiveIndex.value =
      targetActiveIndex.value < 0
        ? opts.length - 1
        : (targetActiveIndex.value - 1 + opts.length) % opts.length
  } else if (e.key === 'Enter' && targetComboOpen.value && targetActiveIndex.value >= 0) {
    e.preventDefault()
    const opt = opts[targetActiveIndex.value]
    if (opt) selectPushTarget(opt.value)
  } else if (e.key === 'Escape') {
    setTargetComboOpen(false)
  }
}

function toggleChMcp(id: string) {
  const set = new Set(chEnabledMcps.value)
  if (set.has(id)) set.delete(id)
  else set.add(id)
  chEnabledMcps.value = PM_MCP_OPTIONS.map((o) => o.id).filter((x) => set.has(x))
}

function defaultChannelName(type: 'qq' | 'wecom' | 'feishu' | 'dingtalk'): string {
  switch (type) {
    case 'wecom':
      return t('pages.workspace.pm.channel.defaultNameWecom')
    case 'feishu':
      return t('pages.workspace.pm.channel.defaultNameFeishu')
    case 'dingtalk':
      return t('pages.workspace.pm.channel.defaultNameDingTalk')
    default:
      return t('pages.workspace.pm.channel.defaultNameQQ')
  }
}

function resetForm() {
  chType.value = 'qq'
  saveError.value = ''
  chRegion.value = 'cn'
  chMarkdown.value = true
  chIntents.value = ''
  chEnabled.value = true
  chName.value = defaultChannelName('qq')
  chAgent.value = ''
  chAppId.value = ''
  chAppSecret.value = ''
  chAppSecretSet.value = false
  chTurnTimeout.value = 0
  chSandbox.value = false
  chAllowMemoryWrite.value = true
  chAllowSchedulerWrite.value = true
  chCronDeliver.value = false
  chCronDeliverTarget.value = ''
  chEnabledMcps.value = [
    'pm-progress',
    'pm-workflow-read',
    'pm-workflow-write',
    'pm-agent-fs',
    'pm-prd-manager',
  ]
  chIsPrimary.value = false
  clearRecentTargetsCache()
}

function applyForm(ch: ChannelConfig) {
  editingId.value = ch.id
  isNew.value = false
  chType.value = ch.type === 'dingtalk' ? 'dingtalk' : ch.type === 'feishu' ? 'feishu' : ch.type === 'wecom' ? 'wecom' : 'qq'
  saveError.value = ''
  chEnabled.value = ch.enabled
  chName.value = ch.name || ''
  chAgent.value = ch.agentName || ''
  chAppId.value = ch.appId || ''
  chAppSecret.value = ''
  chAppSecretSet.value = !!ch.appSecretSet
  chTurnTimeout.value = ch.turnTimeoutSeconds || 0
  const cfg = (ch.config || {}) as Record<string, unknown>
  chSandbox.value = !!cfg.sandbox
  chMarkdown.value = cfg.markdown !== false
  chIntents.value = typeof cfg.intents === 'number' || typeof cfg.intents === 'string' ? String(cfg.intents) : ''
  chRegion.value = cfg.region === 'lark' ? 'lark' : 'cn'
  chAllowMemoryWrite.value = !!cfg.allowMemoryWrite
  chAllowSchedulerWrite.value = !!cfg.allowSchedulerWrite
  chCronDeliver.value = !!ch.cronDeliver
  chCronDeliverTarget.value = ch.cronDeliverTarget || ''
  chEnabledMcps.value = Array.isArray(ch.enabledMcps)
    ? [...ch.enabledMcps]
    : ['pm-progress', 'pm-workflow-read', 'pm-workflow-write', 'pm-agent-fs', 'pm-prd-manager']
  chIsPrimary.value = !!ch.isPrimary
  clearRecentTargetsCache()
  if (chCronDeliver.value) void ensureRecentTargets()
}

async function load() {
  loading.value = true
  try {
    const [listRes, proj] = await Promise.all([
      api.listChannels(),
      api.getWorkspace().catch(() => props.workspace),
    ])
    channelList.value = listRes.items || []
    freeAgents.value = listRes.freeAgents || []
    secretsKeyConfigured.value = listRes.secretsKeyConfigured
    const ids = Array.isArray(proj.notifyPolicy?.channelIds)
      ? [...(proj.notifyPolicy!.channelIds || [])]
      : Array.isArray(props.workspace.notifyPolicy?.channelIds)
        ? [...(props.workspace.notifyPolicy!.channelIds || [])]
        : []
    notifySelected.value = ids
    try {
      const rec = await api.listNotifyReceipts()
      notifyReceipts.value = rec.items || []
    } catch {
      notifyReceipts.value = []
    }
  } catch (e: any) {
    toast.error(String(e?.message || e) || t('pages.workspace.pm.channel.loadFailed'))
  } finally {
    loading.value = false
  }
}

function openAdd() {
  if (!freeAgents.value.length) {
    toast.error(t('pages.workspace.pm.channel.noFreeAgent'))
    return
  }
  resetForm()
  isNew.value = true
  editingId.value = null
  chType.value = 'feishu'
  chRegion.value = 'cn'
  chName.value = defaultChannelName('feishu')
  chIsPrimary.value = false
  chAllowMemoryWrite.value = true
  chAllowSchedulerWrite.value = true
  const prefer = props.pmLeaderAgent || ''
  if (chIsPrimary.value && prefer && freeAgents.value.includes(prefer)) {
    chAgent.value = prefer
  } else {
    chAgent.value = freeAgents.value[0] || ''
  }
  tab.value = 'edit'
}

function openEdit(ch: ChannelConfig) {
  applyForm(ch)
  tab.value = 'edit'
}

function cancelEdit() {
  resetForm()
  isNew.value = false
  editingId.value = null
  tab.value = 'list'
}

function setChannelType(next: 'qq' | 'wecom' | 'feishu' | 'dingtalk') {
  chType.value = next
  if (!isNew.value) return
  const defaults = [
    t('pages.workspace.pm.channel.defaultNameQQ'),
    t('pages.workspace.pm.channel.defaultNameWecom'),
    t('pages.workspace.pm.channel.defaultNameFeishu'),
    t('pages.workspace.pm.channel.defaultNameDingTalk'),
  ]
  if (!chName.value.trim() || defaults.includes(chName.value)) {
    chName.value = defaultChannelName(next)
  }
  clearRecentTargetsCache()
  if (chCronDeliver.value) void ensureRecentTargets()
}

function buildInput(): ChannelConfigInput {
  const config: Record<string, unknown> = {
    allowMemoryWrite: chAllowMemoryWrite.value,
    allowSchedulerWrite: chAllowSchedulerWrite.value,
  }
  if (chType.value === 'feishu') {
    config.region = chRegion.value === 'lark' ? 'lark' : 'cn'
  } else if (chType.value === 'qq') {
    config.sandbox = chSandbox.value
    config.markdown = chMarkdown.value
    const intents = Number(chIntents.value)
    if (Number.isFinite(intents) && intents > 0) config.intents = intents
  }
  return {
    type: chType.value,
    name: chName.value.trim(),
    enabled: chEnabled.value,
    agentName: chAgent.value.trim(),
    isPrimary: chIsPrimary.value,
    enabledMcps: [...chEnabledMcps.value],
    appId: chAppId.value.trim(),
    appSecret: chAppSecret.value,
    turnTimeoutSeconds: Number(chTurnTimeout.value) || 0,
    cronDeliver: chCronDeliver.value,
    cronDeliverTarget: chCronDeliverTarget.value.trim(),
    config,
  }
}

function connectionLabel(ch: ChannelConfig): string {
  switch (ch.connectionState) {
    case 'connected':
      return t('pages.workspace.pm.channel.connConnected')
    case 'auth_failed':
      return t('pages.workspace.pm.channel.connAuthFailed')
    case 'disconnected':
      return t('pages.workspace.pm.channel.connDisconnected')
    default:
      if (typeof ch.online === 'boolean') {
        return ch.online
          ? t('pages.workspace.pm.channel.online')
          : t('pages.workspace.pm.channel.offline')
      }
      return ch.enabled ? t('pages.workspace.pm.channel.connUnknown') : t('pages.workspace.pm.channel.statusOff')
  }
}

function connectionClass(ch: ChannelConfig): string {
  switch (ch.connectionState) {
    case 'connected':
      return 'text-ok'
    case 'auth_failed':
      return 'text-err'
    case 'disconnected':
      return 'text-warn'
    default:
      if (typeof ch.online === 'boolean') return ch.online ? 'text-ok' : 'text-txt3'
      return ch.enabled ? 'text-txt3' : 'text-txt3'
  }
}

function connectionDotClass(ch: ChannelConfig): string {
  switch (ch.connectionState) {
    case 'connected':
      return 'bg-ok'
    case 'auth_failed':
      return 'bg-err'
    case 'disconnected':
      return 'bg-warn'
    default:
      if (typeof ch.online === 'boolean') return ch.online ? 'bg-ok' : 'bg-txt3'
      return 'bg-txt3'
  }
}

function formConnectionHint(): { kind: 'ok' | 'err' | 'warn'; text: string } | null {
  const ch = editingChannel.value
  if (!ch) return null
  if (ch.connectionState === 'connected') {
    return { kind: 'ok', text: ch.connectionDetail || t('pages.workspace.pm.channel.connHintConnected') }
  }
  if (ch.connectionState === 'auth_failed') {
    return { kind: 'err', text: ch.connectionDetail || t('pages.workspace.pm.channel.connHintAuthFailed') }
  }
  if (ch.connectionState === 'disconnected') {
    return { kind: 'warn', text: ch.connectionDetail || t('pages.workspace.pm.channel.connHintDisconnected') }
  }
  return null
}

async function saveChannel() {
  saveError.value = ''
  if (!chName.value.trim()) {
    toast.error(t('pages.workspace.pm.channel.needName'))
    return
  }
  if (!chAgent.value.trim()) {
    toast.error(t('pages.workspace.pm.channel.needAgent'))
    return
  }
  if (!chAppId.value.trim()) {
    toast.error(
      chType.value === 'wecom'
        ? t('pages.workspace.pm.channel.needBotId')
        : t('pages.workspace.pm.channel.needAppId'),
    )
    return
  }
  if (isNew.value && !chAppSecret.value.trim()) {
    toast.error(t('pages.workspace.pm.channel.needSecret'))
    return
  }
  const input = buildInput()
  // Primary rebind → confirm PmLeader sync when agent changes.
  if (!isNew.value && chIsPrimary.value) {
    const prev = editingChannel.value?.agentName || ''
    if (prev && input.agentName && prev !== input.agentName && input.agentName !== props.pmLeaderAgent) {
      const ok = window.confirm(t('pages.workspace.pm.channel.confirmSyncPmLeader'))
      if (ok) input.syncPmLeader = true
    }
  }
  saving.value = true
  try {
    if (isNew.value) {
      await api.createChannel(input)
    } else if (editingId.value) {
      await api.updateChannel(editingId.value, input)
    }
    toast.success(t('pages.workspace.saved'))
    await load()
    tab.value = 'list'
    resetForm()
    isNew.value = false
    editingId.value = null
  } catch (e: any) {
    const msg = String(e?.message || e) || t('pages.workspace.pm.channel.saveFailed')
    saveError.value = msg
    toast.error(msg)
  } finally {
    saving.value = false
  }
}

function askDelete(ch: ChannelConfig) {
  deleteTarget.value = ch
  if (ch.isPrimary) {
    deleteOpen.value = true
    deleteMode.value = channelList.value.filter((c) => c.id !== ch.id).length ? 'promote' : 'none'
    deleteNewPrimaryId.value =
      channelList.value.find((c) => c.id !== ch.id)?.id || ''
    deleteSyncPmLeader.value = true
  } else {
    if (!window.confirm(t('pages.workspace.pm.channel.deleteConfirm'))) return
    void doDelete(ch, { confirmNoPrimary: false })
  }
}

async function confirmDeletePrimary() {
  const ch = deleteTarget.value
  if (!ch) return
  if (deleteMode.value === 'promote') {
    if (!deleteNewPrimaryId.value) {
      toast.error(t('pages.workspace.pm.channel.needNewPrimary'))
      return
    }
    await doDelete(ch, {
      newPrimaryId: deleteNewPrimaryId.value,
      syncPmLeader: deleteSyncPmLeader.value,
    })
  } else {
    await doDelete(ch, { confirmNoPrimary: true })
  }
  deleteOpen.value = false
  deleteTarget.value = null
}

async function doDelete(
  ch: ChannelConfig,
  opts: { newPrimaryId?: string; confirmNoPrimary?: boolean; syncPmLeader?: boolean },
) {
  saving.value = true
  try {
    await api.deleteChannel(ch.id, opts)
    toast.success(t('pages.workspace.saved'))
    if (editingId.value === ch.id) cancelEdit()
    await load()
  } catch (e: any) {
    toast.error(String(e?.message || e) || t('pages.workspace.pm.channel.deleteFailed'))
  } finally {
    saving.value = false
  }
}

function toggleNotify(id: string) {
  const set = new Set(notifySelected.value)
  if (set.has(id)) set.delete(id)
  else set.add(id)
  notifySelected.value = channelList.value.map((c) => c.id).filter((x) => set.has(x))
}

async function saveNotifyTargets() {
  notifySaving.value = true
  try {
    const notifyPolicy: WorkspaceNotifyPolicy = {
      ...(props.workspace.notifyPolicy || {}),
      enabled: props.workspace.notifyPolicy?.enabled !== false,
      defaultEvents: props.workspace.notifyPolicy?.defaultEvents || ['waiting_human', 'failed'],
      channelIds: [...notifySelected.value],
    }
    const updated = await api.updateWorkspace({ notifyPolicy })
    emit('workspace-updated', updated)
    notifySelected.value = Array.isArray(updated.notifyPolicy?.channelIds)
      ? [...updated.notifyPolicy!.channelIds!]
      : []
    toast.success(t('pages.workspace.saved'))
  } catch (e: any) {
    toast.error(String(e?.message || e))
  } finally {
    notifySaving.value = false
  }
}

watch(chCronDeliver, (on) => {
  if (!on) {
    clearRecentTargetsCache()
    return
  }
  void ensureRecentTargets()
})

onMounted(() => {
  document.addEventListener('mousedown', onTargetComboDocClick)
  void load()
})

onUnmounted(() => {
  document.removeEventListener('mousedown', onTargetComboDocClick)
})

  return {
  t,
  toast,
  PM_MCP_OPTIONS,
  tab,
  loading,
  saving,
  channelList,
  freeAgents,
  secretsKeyConfigured,
  editingId,
  isNew,
  chType,
  saveError,
  notifyReceipts,
  chRegion,
  chMarkdown,
  chIntents,
  chEnabled,
  chName,
  chAgent,
  chAppId,
  chAppSecret,
  chAppSecretSet,
  chTurnTimeout,
  chSandbox,
  chAllowMemoryWrite,
  chAllowSchedulerWrite,
  chCronDeliver,
  chCronDeliverTarget,
  chEnabledMcps,
  chIsPrimary,
  notifySelected,
  notifySaving,
  deleteOpen,
  deleteTarget,
  deleteMode,
  deleteNewPrimaryId,
  deleteSyncPmLeader,
  TARGET_LISTBOX_ID,
  recentTargets,
  recentTargetsLoaded,
  recentTargetsLoading,
  targetComboOpen,
  targetComboRoot,
  targetActiveIndex,
  hasPrimary,
  addButtonLabel,
  agentOptions,
  editingChannel,
  clearRecentTargetsCache,
  ensureRecentTargets,
  setTargetComboOpen,
  selectPushTarget,
  onTargetComboDocClick,
  onTargetInputKeydown,
  toggleChMcp,
  defaultChannelName,
  resetForm,
  applyForm,
  load,
  openAdd,
  openEdit,
  cancelEdit,
  setChannelType,
  buildInput,
  connectionLabel,
  connectionClass,
  connectionDotClass,
  formConnectionHint,
  saveChannel,
  askDelete,
  confirmDeletePrimary,
  doDelete,
  toggleNotify,
  saveNotifyTargets,
  pushTargetPrimaryLabel
  }
}
