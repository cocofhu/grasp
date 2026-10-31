<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, useId, watch, type CSSProperties } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import CredentialEditorSurface from './CredentialEditorSurface.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import Icon from '@/components/ui/Icon.vue'
import ModelConnectionEditor from './ModelConnectionEditor.vue'
import ModelProviderSelect from './ModelProviderSelect.vue'
import ModelConfigurationManager from './ModelConfigurationManager.vue'
import ModelRecordActions from './ModelRecordActions.vue'
import { isModelCredential, modelProviderPreset, MODEL_PROTOCOLS, useRoutingCopy } from '@/lib/agent/modelRouting'
import CredentialProviderLogo from '@/components/workspace/CredentialProviderLogo.vue'
import SettingsPageHeader from '@/components/settings/SettingsPageHeader.vue'
import SettingsStatStrip from '@/components/settings/SettingsStatStrip.vue'
import type { SettingsStatItem } from '@/components/settings/settingsStats'
import { api, type CredentialItem, type ModelConfiguration, type ModelConnection, type ModelProtocol } from '@/lib/api/api'
import { fmtTime } from '@/lib/shared/format'
import { useToast } from '@/lib/composables/useToast'
import {
  CREDENTIAL_KINDS,
  aliasKey,
  conflictingAlias,
  kindById,
  kindOfItem,
  defaultGitCredentialHost,
  normalizeGitCredentialHost,
  type CredentialKindId,
} from '@/lib/workspace/credentialKinds'


const props = withDefaults(defineProps<{ inline?: boolean; scope?: 'all' | 'model' | 'git'; view?: 'credentials' | 'models'; setup?: boolean; configurationId?: string }>(), { inline: false, scope: 'all', setup: false, configurationId: '' })
const emit = defineEmits<{ changed: []; credentialSaved: [credential: CredentialItem]; modelSaved: [configuration: ModelConfiguration]; editing: [active: boolean] }>()
const { t } = useI18n()
const toast = useToast()
const routingCopy = useRoutingCopy()
const addMenuOpen = ref(false)
const addTrigger = ref<InstanceType<typeof AppButton> | null>(null)
const addMenu = ref<HTMLElement | null>(null)
const addMenuStyle = ref<CSSProperties>({ visibility: 'hidden' })
const addMenuId = `credential-add-${useId().replace(/:/g, '')}`
function addButton() { return addTrigger.value?.$el as HTMLButtonElement | undefined }
function addMenuItems() { return Array.from(addMenu.value?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') || []) }
function closeAddMenu(restore = false) {
  addMenuOpen.value = false
  if (restore && addButton()?.isConnected) addButton()?.focus({ preventScroll: true })
}
async function showAddMenu(last = false) {
  addMenuStyle.value = { visibility: 'hidden' }
  addMenuOpen.value = true
  await nextTick()
  if (!addMenuOpen.value || !addMenu.value || !addButton()) return
  const box = addButton()!.getBoundingClientRect()
  const width = Math.min(192, window.innerWidth - 16)
  const height = addMenu.value.offsetHeight || 82
  const top = box.bottom + 6 + height <= window.innerHeight - 8 ? box.bottom + 6 : box.top - height - 6
  addMenuStyle.value = { width: `${width}px`, maxHeight: `${window.innerHeight - 16}px`, left: `${Math.max(8, Math.min(box.right - width, window.innerWidth - width - 8))}px`, top: `${Math.max(8, Math.min(top, window.innerHeight - height - 8))}px` }
  await nextTick()
  if (!addMenuOpen.value) return
  const buttons = addMenuItems()
  buttons[last ? buttons.length - 1 : 0]?.focus({ preventScroll: true })
}
function addTriggerKeydown(event: KeyboardEvent) {
  if (!['ArrowDown', 'ArrowUp', 'Enter', ' '].includes(event.key)) return
  event.preventDefault()
  if (addMenuOpen.value && ['Enter', ' '].includes(event.key)) closeAddMenu(true)
  else void showAddMenu(event.key === 'ArrowUp')
}
function addMenuKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); closeAddMenu(true); return }
  if (event.key === 'Tab') { closeAddMenu(true); return }
  const buttons = addMenuItems()
  const current = buttons.indexOf(document.activeElement as HTMLButtonElement)
  if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); buttons[current]?.click(); return }
  const index = event.key === 'ArrowDown' ? (current + 1) % buttons.length : event.key === 'ArrowUp' ? (current + buttons.length - 1) % buttons.length : event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : -1
  if (index >= 0) { event.preventDefault(); buttons[index]?.focus({ preventScroll: true }) }
}
function dismissAddMenu(event: PointerEvent) {
  if (event.target instanceof Node && !addButton()?.contains(event.target) && !addMenu.value?.contains(event.target)) closeAddMenu()
}
function escapeAddMenu(event: KeyboardEvent) { if (addMenuOpen.value && event.key === 'Escape') { event.preventDefault(); closeAddMenu(true) } }
function hideAddMenu() { closeAddMenu() }
async function addFromMenu(kind: 'credential' | 'model') {
  closeAddMenu(true)
  if (kind === 'credential') openCreate()
  else { activeTab.value = 'models'; await nextTick(); await modelConfigurationManager.value?.openCreate() }
}
const activeTab = ref<'credentials' | 'models'>(props.view || (props.scope === 'model' ? 'models' : 'credentials'))
watch(() => props.view, (view) => { if (view) activeTab.value = view })
const setupStarting = ref(false)
const modelEditorOpen = ref(false)
const connectionEditorOpen = ref(false)
const tabs = computed(() => props.scope === 'git' ? ['credentials'] as const : ['credentials', 'models'] as const)
const serviceConnections = ref<ModelConnection[]>([])
const connectionManager = ref<InstanceType<typeof ModelConnectionEditor> | null>(null)
const modelConfigurationManager = ref<InstanceType<typeof ModelConfigurationManager> | null>(null)
const createConnectionAfter = ref(false)
const modelCredentialBackend = ref('')
const createIssuer = ref('deepseek')
const customProviderLabel = ref('')
const savedCredential = ref<CredentialItem | null>(null)
const savedConnection = ref<ModelConnection | null>(null)
const createError = ref('')
const connectionForm = reactive({ name: '', protocol: 'openai-chat-completions' as ModelProtocol, baseUrl: 'https://api.deepseek.com/v1', scheme: 'bearer' as 'bearer' | 'x-api-key' })
const protocolOptions = MODEL_PROTOCOLS.map((protocol) => ({ value: protocol, label: protocol }))
const authOptions = [{ value: 'bearer', label: 'Authorization: Bearer' }, { value: 'x-api-key', label: 'x-api-key' }]
function setConnectionProtocol(value: string) { if (MODEL_PROTOCOLS.includes(value as ModelProtocol)) connectionForm.protocol = value as ModelProtocol }
function setConnectionAuth(value: string) { if (value === 'bearer' || value === 'x-api-key') connectionForm.scheme = value }
const effectiveIssuer = computed(() => createIssuer.value === 'custom' ? customProviderLabel.value.trim() : createIssuer.value)
const providerLabel = computed(() => createIssuer.value === 'custom' ? effectiveIssuer.value : modelProviderPreset(createIssuer.value)?.label || createIssuer.value)
const connectionName = computed(() => connectionForm.name.trim() || `${providerLabel.value} · ${createAlias.value.trim()}`)
const inlineConnection = computed(() => createKind.value === 'model_api' && createConnectionAfter.value)
const connectionReady = computed(() => {
  if (!inlineConnection.value) return true
  try { const url = new URL(connectionForm.baseUrl); if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) return false } catch { return false }
  return !!connectionName.value
})
const creationKinds = computed(() => CREDENTIAL_KINDS.filter((kind) => {
  if (modelCredentialBackend.value) return kind.id === modelCredentialBackend.value
  return props.scope === 'model' ? kind.type === 'ai' : props.scope === 'git' ? ['git', 'ssh'].includes(kind.type) : true
}))
const showModelKeyKind = computed(() => props.scope !== 'git' && !['cursor', 'trae'].includes(modelCredentialBackend.value))

const loading = ref(true)
const loadError = ref('')
const items = ref<CredentialItem[]>([])
const drafts = reactive<Record<string, string>>({})
const editing = reactive<Record<string, boolean>>({})
const saving = reactive<Record<string, boolean>>({})
const clearing = reactive<Record<string, boolean>>({})
const removing = reactive<Record<string, boolean>>({})
const showCreate = ref(false)
const creating = ref(false)
const editorActive = computed(() => setupStarting.value || showCreate.value || modelEditorOpen.value || connectionEditorOpen.value || Object.values(editing).some(Boolean))
watch(editorActive, (active) => emit('editing', active))
const createStep = ref<1 | 2 | 3>(1)
const createKind = ref<CredentialKindId | 'model_api' | ''>('')
const createAlias = ref('')
const createSecret = ref('')
const createHost = ref('')
const hostDrafts = reactive<Record<string, string>>({})
const revealSecret = ref(false)
const aliasError = ref('')
const createAttempted = ref(false)
const stepMotion = ref<'forward' | 'back'>('forward')
const stepLock = ref(false)
const stepPane = ref<HTMLElement | null>(null)
const paneHeight = ref(0)
const STEP_LOCK_MS = 200
let stepTimer = 0
let paneResizeObserver: ResizeObserver | null = null

/** Prefer min-height so async form growth can expand the shell into AppModal's scroll area. */
const stepShellStyle = computed((): CSSProperties | undefined => {
  if (!paneHeight.value) return undefined
  return { minHeight: `${paneHeight.value}px` }
})


const credentialTypeKeys = {
  ai: 'pages.workspace.credentials.typeAi',
  git: 'pages.workspace.credentials.typeGit',
  ssh: 'pages.workspace.credentials.typeSsh',
  mcp: 'pages.workspace.credentials.typeMcp',
  custom: 'pages.workspace.credentials.typeCustom',
} as const
const selectedKind = computed(() => (createKind.value ? kindById(createKind.value) : undefined))
const createSecretLabel = computed(() => createKind.value === 'model_api' ? routingCopy.value.secret : selectedKind.value ? secretLabel(selectedKind.value.id) : t('pages.workspace.credentials.value'))
const createHasHost = computed(() => selectedKind.value?.type === 'git')
const createHostValid = computed(() => !createHasHost.value || !!normalizeGitCredentialHost(createHost.value))
watch(createStep, async () => {
  await nextTick()
  if (!showCreate.value || !stepPane.value) return
  const target = stepPane.value.querySelector<HTMLElement>('input:not([disabled]), textarea:not([disabled]), select:not([disabled]), button:not([disabled])') || stepPane.value
  if (target === stepPane.value) target.tabIndex = -1
  target.focus({ preventScroll: true })
})
function kindLabel(id: CredentialKindId) { return t(`pages.workspace.credentials.kinds.${id}`) }
const secretReady = computed(() => Boolean(createKind.value && (savedCredential.value || createSecret.value.trim()) && (createKind.value !== 'model_api' || (createIssuer.value === 'custom' ? !!effectiveIssuer.value : /^[a-zA-Z0-9][a-zA-Z0-9._-]*$/.test(effectiveIssuer.value)))))

const orderedItems = computed(() =>
  items.value.filter((item) => (!isWorkspaceOwned(item) || !!item.purpose) && (props.scope === 'model' ? ['model_api', 'tool_auth'].includes(item.purpose || '') : props.scope === 'git' ? ['git', 'ssh'].includes(item.purpose || '') : true)).sort((a, b) => {
    // Put the model API key first so the primary setup path is visible without
    // making users scan past Git/SSH slots.
    const aModel = isModelCredential(a) ? 0 : 1
    const bModel = isModelCredential(b) ? 0 : 1
    if (aModel !== bModel) return aModel - bModel
    const aName = (a.name || a.provider || a.id).toLocaleLowerCase()
    const bName = (b.name || b.provider || b.id).toLocaleLowerCase()
    return aName.localeCompare(bName)
  }),
)

const credentialGroups = computed(() => {
  const buckets = new Map<string, CredentialItem[]>()
  const other: CredentialItem[] = []
  for (const item of orderedItems.value) {
    if (isModelCredential(item)) { buckets.set('model_api', [...(buckets.get('model_api') || []), item]); continue }
    const kind = kindOfItem(item)
    if (!kind) {
      other.push(item)
      continue
    }
    buckets.set(kind.id, [...(buckets.get(kind.id) || []), item])
  }
  const groups: { id: CredentialKindId | 'model_api' | 'other'; items: CredentialItem[] }[] = CREDENTIAL_KINDS
    .filter((kind) => (buckets.get(kind.id) || []).length > 0)
    .map((kind) => ({ id: kind.id, items: buckets.get(kind.id) || [] }))
  if (buckets.get('model_api')?.length) groups.push({ id: 'model_api', items: buckets.get('model_api') || [] })
  if (other.length) groups.push({ id: 'other', items: other })
  return groups
})

type CredentialSectionId = 'coding' | 'api' | 'git' | 'ssh' | 'other'

const credentialSections = computed(() => {
  const sections: { id: CredentialSectionId; groups: typeof credentialGroups.value; icon: string }[] = [
    { id: 'coding', groups: [], icon: 'terminal' },
    { id: 'api', groups: [], icon: 'key' },
    { id: 'git', groups: [], icon: 'git' },
    { id: 'ssh', groups: [], icon: 'key' },
    { id: 'other', groups: [], icon: 'lock' },
  ]
  for (const group of credentialGroups.value) {
    const kind = kindById(group.id)
    const sectionId = group.id === 'model_api' ? 'api' : kind?.type === 'ai' ? 'coding' : kind?.type || 'other'
    sections.find((section) => section.id === sectionId)?.groups.push(group)
  }
  return sections.filter((section) => section.groups.length)
})

const standalone = computed(() => !props.inline && !props.view)
const modelCount = ref<number | undefined>(undefined)
function tabCount(tab: 'credentials' | 'models'): number | undefined {
  return tab === 'credentials' ? orderedItems.value.length : modelCount.value
}
const credentialStats = computed<SettingsStatItem[]>(() => {
  const list = orderedItems.value
  const configured = list.filter((item) => item.configured).length
  const hosts = new Set(list.filter((item) => kindOfItem(item)?.type === 'git').map((item) => displayHost(item)).filter(Boolean))
  const pending = list.filter((item) => !item.configured && isWorkspaceEditable(item)).length
  return [
    { key: 'total', icon: 'key', label: t('pages.settingsOverview.stats.credentialsTotal'), value: list.length, tone: 'accent' },
    { key: 'configured', icon: 'check', label: t('pages.settingsOverview.stats.credentialsConfigured'), value: configured, total: list.length, tone: 'ok' },
    { key: 'git-hosts', icon: 'git', label: t('pages.settingsOverview.stats.credentialsGitHosts'), value: hosts.size },
    { key: 'pending', icon: 'warning', label: t('pages.settingsOverview.stats.credentialsPending'), value: pending, tone: pending ? 'warn' : 'neutral' },
  ]
})

function itemLabel(item: CredentialItem): string {
  return item.name || item.provider || item.id
}

function isCodexLogin(item: CredentialItem): boolean {
  return item.purpose === 'tool_auth' && item.toolId === 'codex' && item.secretKind === 'login_file'
}

function isMultiline(item: CredentialItem): boolean {
  return ['login_file', 'ssh_private_key', 'ssh_known_hosts'].includes(item.secretKind || '')
}

function configuredText(item: CredentialItem): string {
  return item.configured ? t('pages.workspace.credentials.configured') : t('pages.workspace.credentials.notConfigured')
}

function typeLabel(item: CredentialItem): string {
  if (isModelCredential(item)) return (item.provider === 'custom' ? item.issuer : '') || modelProviderPreset(item.issuer || '')?.label || item.issuer || item.provider || routingCopy.value.modelKeys
  const kind = kindOfItem(item)
  if (kind) return t(`pages.workspace.credentials.kinds.${kind.id}`)
  const key = (item.type || 'custom') as keyof typeof credentialTypeKeys
  return t(credentialTypeKeys[key] || credentialTypeKeys.custom)
}

function isWorkspaceOwned(item: CredentialItem): boolean {
  return !item.source || item.source === 'workspace'
}
function isWorkspaceEditable(item: CredentialItem): boolean {
  return isWorkspaceOwned(item)
}
function hasGitHost(item: CredentialItem): boolean {
  return item.purpose === 'git' && !!defaultGitCredentialHost(item.provider || '')
}
function displayHost(item: CredentialItem): string {
  return (item.host || defaultGitCredentialHost(item.provider || '')).replace(/^https:\/\//, '').replace(/\/$/, '')
}
function hostChanged(item: CredentialItem): boolean {
  return hasGitHost(item) && normalizeGitCredentialHost(hostDrafts[item.id] || '') !== normalizeGitCredentialHost(item.host || defaultGitCredentialHost(item.provider || ''))
}
function canSave(item: CredentialItem): boolean {
  return isWorkspaceEditable(item) && (!hasGitHost(item) || !!normalizeGitCredentialHost(hostDrafts[item.id] || '')) && (!!drafts[item.id]?.trim() || hostChanged(item))
}

function updateItems(next: CredentialItem) {
  const i = items.value.findIndex((item) => item.id === next.id)
  if (i < 0) items.value = [...items.value, next]
  else items.value = items.value.map((item, index) => (index === i ? next : item))
  hostDrafts[next.id] = displayHost(next)
  drafts[next.id] = ''
  editing[next.id] = false
}

function setItems(next: CredentialItem[]) {
  items.value = next
  for (const key of Object.keys(drafts)) delete drafts[key]
  for (const key of Object.keys(editing)) delete editing[key]
  for (const key of Object.keys(hostDrafts)) delete hostDrafts[key]
  for (const item of next) drafts[item.id] = ''
}

function startEdit(item: CredentialItem) {
  if (!isWorkspaceEditable(item)) return
  drafts[item.id] = ''
  hostDrafts[item.id] = displayHost(item)
  editing[item.id] = true
}

function cancelEdit(item: CredentialItem) {
  drafts[item.id] = ''
  editing[item.id] = false
}


function clearSecrets() {
  createSecret.value = ''
  revealSecret.value = false

}

function releaseStepLock() {
  window.clearTimeout(stepTimer)
  stepLock.value = false
}

function lockStep() {
  stepLock.value = true
  window.clearTimeout(stepTimer)
  stepTimer = window.setTimeout(() => {
    stepLock.value = false
  }, STEP_LOCK_MS)
}

async function measurePane() {
  await nextTick()
  if (stepPane.value) paneHeight.value = stepPane.value.offsetHeight
}

function disconnectPaneObserver() {
  paneResizeObserver?.disconnect()
  paneResizeObserver = null
}

function observePane() {
  disconnectPaneObserver()
  if (typeof ResizeObserver === 'undefined' || !stepPane.value) return
  paneResizeObserver = new ResizeObserver(() => {
    if (!stepPane.value) return
    // Keep the animated floor in sync when fields or validation grow the pane.
    const next = stepPane.value.offsetHeight
    if (next > 0) paneHeight.value = next
  })
  paneResizeObserver.observe(stepPane.value)
}

function openCreate() {
  if (props.view === 'models' && !props.setup) return
  modelCredentialBackend.value = ''
  releaseStepLock()
  createStep.value = 1
  createConnectionAfter.value = false
  savedCredential.value = null
  savedConnection.value = null
  createError.value = ''
  createIssuer.value = 'deepseek'
  customProviderLabel.value = ''
  Object.assign(connectionForm, { name: '', protocol: 'openai-chat-completions', baseUrl: 'https://api.deepseek.com/v1', scheme: 'bearer' })
  createKind.value = ''
  createAlias.value = ''
  createHost.value = ''
  aliasError.value = ''
  createAttempted.value = false
  stepMotion.value = 'forward'
  paneHeight.value = 0
  clearSecrets()
  showCreate.value = true
  void measurePane().then(observePane)
}

async function closeCreate() {
  if (creating.value) return
  const resumeModel = !!modelCredentialBackend.value
  modelCredentialBackend.value = ''
  showCreate.value = false
  if (resumeModel) {
    await nextTick()
    await modelConfigurationManager.value?.resumeWithCredential()
  }
}

function selectKind(id: CredentialKindId | 'model_api') {
  if (stepLock.value || creating.value || createStep.value !== 1) return
  if (createKind.value !== id) { clearSecrets(); createHost.value = defaultGitCredentialHost(id) }
  createKind.value = id
  createConnectionAfter.value = id === 'model_api'
}

function selectIssuer(value: string) {
  if (savedCredential.value || value === createIssuer.value) return
  createIssuer.value = value
  const preset = modelProviderPreset(value)
  connectionForm.protocol = preset?.protocol || 'openai-chat-completions'
  connectionForm.baseUrl = preset?.baseUrl || ''
  connectionForm.scheme = preset?.scheme || 'bearer'
}

function goNext() {
  if (stepLock.value || creating.value || createStep.value === 3 || !createKind.value) return
  if (createStep.value === 2) {
    const alias = createAlias.value.trim()
    if (!aliasKey(alias)) { aliasError.value = t('pages.workspace.credentials.aliasRequired'); return }
    const kind = selectedKind.value
    const taken = savedCredential.value ? '' : kind ? conflictingAlias(items.value, kind, alias) : items.value.find((item) => isModelCredential(item) && aliasKey(item.name) === aliasKey(alias))?.name
    if (taken) { aliasError.value = t('pages.workspace.credentials.aliasTaken', { alias: taken }); return }
    if (!secretReady.value || !connectionReady.value || !createHostValid.value) return
  }
  stepMotion.value = 'forward'
  createAttempted.value = false
  createStep.value = createStep.value === 1 ? 2 : 3
  lockStep()
  void measurePane().then(observePane)
}

function goBack() {
  if (stepLock.value || creating.value || createStep.value === 1 || (savedCredential.value && createStep.value === 2)) return
  stepMotion.value = 'back'
  aliasError.value = ''
  createStep.value = createStep.value === 3 ? 2 : 1
  lockStep()
  void measurePane().then(observePane)
}

function secretLabel(kindId: CredentialKindId): string {
  const kind = kindById(kindId)
  if (!kind) return t('pages.workspace.credentials.value')
  if (kind.secret === 'loginFile') return t('pages.workspace.credentials.secretLoginFile')
  if (kind.id === 'ssh_key') return t('pages.workspace.credentials.secretPrivateKey')
  if (kind.id === 'ssh_hosts') return t('pages.workspace.credentials.secretKnownHosts')
  if (kind.id === 'opencode') return t('pages.workspace.credentials.secretOpenCodeKey')
  if (kind.id === 'deepseek_harness') return t('pages.workspace.credentials.secretDeepSeekKey')
  if (kind.id === 'kimi_code') return t('pages.workspace.credentials.secretKimiCodeKey')
  return t('pages.workspace.credentials.secretApiKey')
}

async function create() {
  if (stepLock.value || creating.value || createStep.value !== 3) return
  const kind = selectedKind.value
  const alias = createAlias.value.trim()
  if ((!kind && createKind.value !== 'model_api') || !aliasKey(alias)) {
    aliasError.value = t('pages.workspace.credentials.aliasRequired')
    return
  }
  const taken = savedCredential.value ? '' : kind ? conflictingAlias(items.value, kind, alias) : items.value.find((item) => isModelCredential(item) && aliasKey(item.name) === aliasKey(alias))?.name
  if (taken) {
    aliasError.value = t('pages.workspace.credentials.aliasTaken', { alias: taken })
    return
  }
  createAttempted.value = true
  if (!secretReady.value || !connectionReady.value || !createHostValid.value) return
  aliasError.value = ''
  creating.value = true
  createError.value = ''
  try {
    const created = savedCredential.value || await api.createCredential(createKind.value === 'model_api' ? {
      purpose: 'model_api', issuer: effectiveIssuer.value, secretKind: 'api_key', name: alias, value: createSecret.value,
      ...(createIssuer.value === 'custom' ? { provider: 'custom' } : {}),
    } : {
      ...(createHasHost.value ? { host: createHost.value.trim() } : {}),
      type: kind!.type, provider: kind!.provider, name: alias, envKey: kind!.envKey, value: createSecret.value,
      purpose: kind!.type === 'ai' ? 'tool_auth' : kind!.type,
      ...(kind!.type === 'ai' ? { toolId: kind!.id } : {}),
      secretKind: kind!.secret === 'loginFile' ? 'login_file' : kind!.id === 'ssh_key' ? 'ssh_private_key' : kind!.id === 'ssh_hosts' ? 'ssh_known_hosts' : 'api_key',
    })
    savedCredential.value = created
    updateItems(created)
    clearSecrets()
    if (inlineConnection.value && !savedConnection.value) {
      savedConnection.value = await api.createModelConnection({ name: connectionName.value, providerId: createIssuer.value, protocol: connectionForm.protocol, baseUrl: connectionForm.baseUrl.trim(), credentialId: created.id, auth: { scheme: connectionForm.scheme }, models: [], state: 'active' })
    }
  } catch (e: unknown) {
    const err = e as { code?: string; alias?: string; message?: string }
    if (!savedCredential.value && err.code === 'alias_taken') {
      aliasError.value = t('pages.workspace.credentials.aliasTaken', { alias: err.alias || alias })
      return
    }
    createError.value = String(err.message || e)
    toast.error(createError.value)
    return
  } finally {
    creating.value = false
  }
  const connection = savedConnection.value
  const resumeModel = !!modelCredentialBackend.value
  modelCredentialBackend.value = ''
  showCreate.value = false
  if (savedCredential.value) emit('credentialSaved', savedCredential.value)
  emit('changed')
  toast.success(t('pages.workspace.credentials.saved'))
  // The writes have completed. A later refresh/navigation failure must never
  // turn into another key or connection POST on retry.
  try {
    if (connection) await loadServiceConnections()
    if (resumeModel) {
      await nextTick()
      await modelConfigurationManager.value?.resumeWithCredential(savedCredential.value?.id, connection?.id)
    } else if (connection) {
      activeTab.value = 'credentials'
    }
  } catch (e) {
    toast.error(`${t('pages.workspace.credentials.saved')} · ${String((e as Error).message || e)}`)
  }
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const response = await api.listCredentials()
    setItems(response.items)
    await loadServiceConnections()
  } catch (e: unknown) {
    loadError.value = String((e as { message?: string })?.message || e)
    setItems([])
  } finally {
    loading.value = false
  }
  if (props.inline && props.view === 'credentials' && !loadError.value && !orderedItems.value.length && !editorActive.value) openCreate()
}

async function save(item: CredentialItem) {
  const value = drafts[item.id] || ''
  if (!canSave(item) || saving[item.id]) return
  saving[item.id] = true
  try {
    const saved = await api.putCredential(item.id, {
      purpose: item.purpose, issuer: item.issuer, toolId: item.toolId, secretKind: item.secretKind,
      type: item.type,
      name: item.name,
      target: item.target,
      targetType: item.targetType,
      targetId: item.targetId,
      provider: item.provider,
      envKey: item.envKey,
      ...(value.trim() ? { value } : {}),
      ...(hasGitHost(item) ? { host: hostDrafts[item.id].trim() } : {}),
      metadata: item.metadata,
      enabled: item.enabled,
    })
    updateItems(saved)
    emit('credentialSaved', saved)
    emit('changed')
    toast.success(t('pages.workspace.credentials.saved'))
  } catch (e: unknown) {
    toast.error(String((e as { message?: string })?.message || e))
  } finally {
    saving[item.id] = false
  }
}

async function clear(item: CredentialItem) {
  if (clearing[item.id] || !item.configured) return
  clearing[item.id] = true
  try {
    await api.deleteCredential(item.id)
    updateItems({ ...item, configured: false, masked: '', revokedAt: undefined, secretRevision: Math.max(2, (item.secretRevision || 1) + 1) })
    toast.success(t('pages.workspace.credentials.cleared'))
  } catch (e: unknown) {
    toast.error(String((e as { message?: string })?.message || e))
  } finally {
    clearing[item.id] = false
  }
}

function canRemoveAccount(item: CredentialItem) {
  return isWorkspaceOwned(item) && isModelCredential(item)
}

async function removeAccount(item: CredentialItem) {
  if (!canRemoveAccount(item) || removing[item.id]) return
  const alias = itemLabel(item)
  if (!window.confirm(t('pages.workspace.credentials.accountDeleteConfirm', { alias }))) return
  removing[item.id] = true
  try {
    await api.removeCredential(item.id)
    items.value = items.value.filter((row) => row.id !== item.id)
    delete drafts[item.id]
    delete editing[item.id]
    delete hostDrafts[item.id]
    delete clearing[item.id]
    serviceConnections.value = serviceConnections.value.filter((service) => service.credentialId !== item.id)
    await loadServiceConnections({ keepOnError: true })
    toast.success(t('pages.workspace.credentials.accountDeleted'))
  } catch {
    toast.error(t('pages.workspace.credentials.accountDeleteFailed'))
  } finally {
    removing[item.id] = false
  }
}

async function loadServiceConnections(options?: { keepOnError?: boolean }) {
  try {
    serviceConnections.value = (await api.listModelConnections()).items || []
  } catch {
    if (!options?.keepOnError) serviceConnections.value = []
  }
}
function servicesFor(item: CredentialItem) { return serviceConnections.value.filter((service) => service.credentialId === item.id) }
async function editService(accountId: string, serviceId: string) { await connectionManager.value?.openForCredential(accountId, serviceId) }
function addService() { openCreate(); selectKind('model_api'); goNext() }
async function configureService(id: string) { await connectionManager.value?.openForCredential(id) }
function manageModelCredentials(backend?: string) {
  if (!backend) { void addService(); return }
  openCreate()
  modelCredentialBackend.value = backend
  if (backend === 'claude_code') selectIssuer('anthropic')
  else if (backend === 'codex') selectIssuer('openai')
  if (['cursor', 'trae', 'opencode', 'deepseek_harness', 'kimi_code'].includes(backend)) {
    selectKind(backend as CredentialKindId)
    goNext()
  }
}


async function configurationChanged() { await loadServiceConnections(); emit('changed') }
async function openModelCreate() {
  if (props.view === 'credentials') return
  activeTab.value = 'models'
  await nextTick()
  await modelConfigurationManager.value?.openCreate()
}
defineExpose({ openCreate, openModelCreate })

async function initialize() {
  setupStarting.value = props.setup
  try {
    await load()
    if (props.setup && !loadError.value) {
      await nextTick()
      await modelConfigurationManager.value?.openSetup(props.configurationId)
    }
  } finally { setupStarting.value = false }
}

function retry() { void initialize() }

onMounted(() => {
  void initialize()
  document.addEventListener('pointerdown', dismissAddMenu)
  document.addEventListener('keydown', escapeAddMenu)
  window.addEventListener('resize', hideAddMenu)
  window.addEventListener('scroll', hideAddMenu, true)
})

onUnmounted(() => {
  window.clearTimeout(stepTimer)
  disconnectPaneObserver()
  document.removeEventListener('pointerdown', dismissAddMenu)
  document.removeEventListener('keydown', escapeAddMenu)
  window.removeEventListener('resize', hideAddMenu)
  window.removeEventListener('scroll', hideAddMenu, true)
})

watch(showCreate, (open) => {
  if (!open) {
    clearSecrets()
    disconnectPaneObserver()
    paneHeight.value = 0
  }
})
</script>

<template>
  <div
    class="flex min-h-0 flex-1 flex-col"
    :class="inline ? 'credentials-inline' : 'credentials-page overflow-hidden'"
    data-testid="workspace-credentials-panel"
  >
    <div v-if="loading" class="flex flex-1 items-center justify-center text-[13px] text-txt3">
      {{ t('common.loading.inProgress') }}
    </div>

    <div v-else-if="loadError" class="flex flex-1 flex-col items-center justify-center gap-2 px-4 text-center">
      <p class="text-[13px] text-err">{{ loadError }}</p>
      <AppButton size="sm" variant="outline" @click="retry">{{ t('common.buttons.retry') }}</AppButton>
    </div>

    <div v-else v-show="!(inline && showCreate)" class="credentials-scroll min-h-0 flex-1" :class="inline ? '' : 'scroll-area overflow-y-auto'">
      <div class="credentials-content w-full">
        <SettingsPageHeader
          v-if="standalone"
          v-show="!setup"
          class="credentials-page-header"
          icon="key"
          :title="t('pages.workspace.credentials.title')"
          :description="t('pages.workspace.credentials.description')"
        >
          <template #actions>
            <AppButton
              ref="addTrigger"
              variant="primary"
              icon="plus"
              class="credentials-create shrink-0"
              data-testid="workspace-credential-create"
              aria-haspopup="menu"
              :aria-expanded="addMenuOpen"
              :aria-controls="addMenuOpen ? addMenuId : undefined"
              @click="addMenuOpen ? closeAddMenu() : showAddMenu()"
              @keydown="addTriggerKeydown"
            >
              {{ t('pages.workspace.credentials.add') }}
              <Icon name="chevron-down" :size="14" :interactive="false" />
            </AppButton>
          </template>
        </SettingsPageHeader>
        <header v-else v-show="!setup && (!inline || !editorActive)" class="credentials-header">
          <div class="flex min-w-0 items-center gap-3.5">
            <div class="min-w-0">
              <h2 class="m-0 text-[20px] font-semibold tracking-tight text-txt">{{ view === 'credentials' ? routingCopy.credentials : scope === 'model' || view === 'models' ? routingCopy.modelsTab : scope === 'git' ? t('pages.workspace.credentials.sections.git') : t('pages.workspace.credentials.title') }}</h2>
            </div>
          </div>
          <div class="flex flex-wrap gap-2">
            <AppButton v-if="scope !== 'git' && view !== 'credentials'" size="sm" variant="primary" icon="plus" data-testid="workspace-add-model" data-editor-trigger @click="openModelCreate">{{ t('pages.workspace.credentials.addModel') }}</AppButton>
            <AppButton v-if="view !== 'models'" size="sm" :variant="scope === 'git' || view === 'credentials' ? 'primary' : 'outline'" icon="key" data-testid="workspace-add-credential" data-editor-trigger @click="openCreate">{{ t('pages.workspace.credentials.create') }}</AppButton>
          </div>
        </header>
        <Teleport to="body"><div v-if="addMenuOpen" :id="addMenuId" ref="addMenu" role="menu" :aria-label="t('pages.workspace.credentials.add')" class="credentials-add-menu" :style="addMenuStyle" data-testid="workspace-add-menu" @keydown="addMenuKeydown"><button type="button" role="menuitem" tabindex="-1" data-testid="workspace-add-credential" @click="addFromMenu('credential')"><Icon name="key" :size="16" :interactive="false" />{{ t('pages.workspace.credentials.create') }}</button><button type="button" role="menuitem" tabindex="-1" data-testid="workspace-add-model" @click="addFromMenu('model')"><Icon name="sparkles" :size="16" :interactive="false" />{{ t('pages.workspace.credentials.addModel') }}</button></div></Teleport>

        <nav v-if="!view" v-show="(!inline || !editorActive) && tabs.length > 1" class="mb-6 mt-5 flex flex-wrap gap-1 border-b border-line" role="tablist" :aria-label="routingCopy.credentials"><button v-for="tab in tabs" :key="tab" type="button" role="tab" class="border-b-2 px-3 py-3 text-[13px] transition" :class="activeTab === tab ? 'border-accent font-semibold text-accent-2' : 'border-transparent text-txt3 hover:text-txt'" :aria-selected="activeTab === tab" :data-testid="`credentials-tab-${tab}`" @click="activeTab = tab"><span class="credentials-tab-label">{{ tab === 'credentials' ? routingCopy.credentials : routingCopy.modelsTab }}</span><span v-if="tabCount(tab) !== undefined" class="credentials-tab-count" :data-testid="`credentials-tab-count-${tab}`">{{ tabCount(tab) }}</span></button></nav>
        <div v-if="activeTab === 'credentials'">
        <div v-show="!inline || !connectionEditorOpen">
        <SettingsStatStrip v-if="!inline" class="mb-6" :items="credentialStats" data-testid="workspace-credential-stats" />
        <div v-if="!orderedItems.length" class="credentials-empty">
          <Icon name="key" :size="28" class="mb-3 text-txt3" aria-hidden="true" />
          <p class="m-0 text-[13px] text-txt3">{{ t('pages.workspace.credentials.empty') }}</p>
        </div>
        <template v-else>
          <section
            v-for="section in credentialSections"
            :key="section.id"
            class="credentials-section"
            :aria-labelledby="`credential-section-${section.id}`"
            :data-testid="`workspace-credential-section-${section.id}`"
          >
            <div class="credentials-section-heading">
              <div class="flex min-w-0 items-center gap-2.5">
                <Icon :name="section.icon" :size="16" class="shrink-0 text-txt3" aria-hidden="true" />
                <h3 :id="`credential-section-${section.id}`" class="m-0 text-[14px] font-semibold text-txt">{{ t(`pages.workspace.credentials.sections.${section.id}`) }}</h3>
                <span class="credentials-section-count">{{ section.groups.reduce((count, group) => count + group.items.length, 0) }}</span>
              </div>
            </div>
            <div class="credentials-list">
              <div
                v-for="group in section.groups"
                :key="group.id"
                class="credentials-provider-group"
                :data-testid="`workspace-credential-group-${group.id}`"
              >
                <template v-for="item in group.items" :key="item.id">
                  <article
                    class="credential-row"
                    :class="{ 'credential-row--editing': editing[item.id] }"
                    data-testid="workspace-credential-row"
                    :data-credential-kind="kindOfItem(item)?.id || 'other'"
                  >
                    <div class="credential-row-headline" data-testid="workspace-credential-headline">
                      <div class="credential-identity flex min-w-0 items-center gap-3">
                        <CredentialProviderLogo
                          :provider="isModelCredential(item) ? item.issuer || item.provider : kindOfItem(item)?.logoProvider || item.provider"
                          :icon="kindOfItem(item)?.icon"
                          match="provider"
                          :configured="item.configured"
                        />
                        <div class="min-w-0 flex-1">
                          <h4 class="m-0 text-[13px] font-semibold leading-5 text-txt" :title="itemLabel(item)" data-testid="workspace-credential-alias">{{ itemLabel(item) }}</h4>
                          <p v-if="itemLabel(item) !== typeLabel(item)" class="m-0 mt-0.5 text-[11px] text-txt3" data-testid="workspace-credential-kind">{{ typeLabel(item) }}</p>
                          <p v-if="kindOfItem(item)?.type === 'git'" class="m-0 mt-0.5 truncate font-mono text-[11px] text-txt3" :data-testid="`workspace-credential-host-label-${item.id}`">{{ displayHost(item) }}</p>
                        </div>
                      </div>
                      <div class="credential-row-value">
                        <p
                          v-if="item.configured && item.masked"
                          class="m-0 truncate font-mono text-[12px] tracking-wide text-txt2"
                          data-testid="workspace-credential-masked"
                        >{{ item.masked }}</p>
                        <p v-else class="m-0 text-[12px] text-txt3">{{ t(item.configured ? 'pages.workspace.credentials.configured' : 'pages.workspace.credentials.noValue') }}</p>
                        <p v-if="item.updatedAt && editing[item.id]" class="credential-updated mb-0 mt-1 text-[10px] leading-4 text-txt3">
                          {{ t('pages.workspace.credentials.updatedAt', { time: fmtTime(item.updatedAt) }) }}
                        </p>
                        <div v-if="isModelCredential(item) && editing[item.id]" class="mt-1"><button v-if="!servicesFor(item).length" type="button" class="credential-service-link" :data-testid="`model-credential-configure-service-${item.id}`" @click="configureService(item.id)">{{ routingCopy.configureConnectionNow }}</button><div v-for="service in servicesFor(item)" :key="service.id" class="min-w-0" :data-testid="`model-credential-service-${service.id}`"><button type="button" class="credential-service-link" :title="`${routingCopy.editService} · ${service.name}`" :data-testid="`model-credential-edit-service-${service.id}`" @click="editService(item.id, service.id)"><span class="truncate">{{ service.baseUrl || service.name }}</span><Icon name="edit" :size="11" class="shrink-0" /></button></div></div>
                      </div>
                      <div class="credential-row-controls" data-testid="workspace-credential-actions">
                        <span
                          class="credential-status"
                          :class="item.configured ? 'text-ok' : 'text-txt3'"
                          data-testid="workspace-credential-status"
                        >
                          <span class="credential-status-dot" :class="item.configured ? 'bg-ok' : 'bg-txt3/40'" aria-hidden="true" />
                          <span class="sr-only">{{ configuredText(item) }}</span>
                        </span>
                        <div class="credential-row-buttons">
                          <template v-if="isWorkspaceEditable(item) && !editing[item.id]">
                            <AppButton
                              v-if="item.configured"
                              size="sm"
                              variant="outline"
                              :data-testid="`workspace-credential-edit-${item.id}`"
                              :aria-label="t('pages.workspace.credentials.edit')"
                              :title="t('pages.workspace.credentials.edit')"
                              @click="startEdit(item)"
                            ><Icon name="edit" :size="15" /></AppButton>
                            <AppButton
                              v-else
                              size="sm"
                              variant="outline"
                              :data-testid="`workspace-credential-fill-${item.id}`"
                              @click="startEdit(item)"
                            >{{ t('pages.workspace.credentials.fillIn') }}</AppButton>
                          </template>
                          <AppButton
                            v-if="!inline && isWorkspaceOwned(item) && item.configured"
                            size="sm"
                            variant="ghost"
                            class="credential-clear"
                            :disabled="!!clearing[item.id]"
                            :loading="!!clearing[item.id]"
                            :data-testid="`workspace-credential-clear-${item.id}`"
                            :aria-label="t('pages.workspace.credentials.clear')"
                            :title="t('pages.workspace.credentials.clear')"
                            @click="clear(item)"
                          ><Icon name="trash" :size="14" /></AppButton>
                          <ModelRecordActions
                            v-if="!inline && canRemoveAccount(item)"
                            :name="itemLabel(item)"
                            prefix="api-account"
                            :record-id="item.id"
                            :show-edit="false"
                            :show-toggle="false"
                            @remove="removeAccount(item)"
                          />
                        </div>
                      </div>
                    </div>
                    <p v-if="!isWorkspaceEditable(item)" class="mb-0 mt-3 text-[11px] leading-5 text-txt3">
                      {{ t('pages.workspace.credentials.managedByAdapter', { source: item.source }) }}
                    </p>
                    <div
                      v-else-if="editing[item.id]"
                      class="credential-editor mt-3 flex min-w-0 flex-col gap-3 border-t border-line/70 pt-3"
                      :data-testid="`workspace-credential-editor-${item.id}`"
                    >
                      <label v-if="hasGitHost(item)" class="block">
                        <span class="label">Host</span>
                        <input v-model="hostDrafts[item.id]" class="input w-full font-mono" :data-testid="`workspace-credential-host-${item.id}`" :aria-invalid="!normalizeGitCredentialHost(hostDrafts[item.id] || '')" spellcheck="false" :placeholder="defaultGitCredentialHost(item.provider || '')" />
                        <p v-if="!normalizeGitCredentialHost(hostDrafts[item.id] || '')" class="mb-0 mt-1 text-xs text-err">{{ t('pages.workspace.credentials.invalidHost') }}</p>
                      </label>
                      <label class="sr-only" :for="`credential-value-${item.id}`">{{ t('pages.workspace.credentials.value') }} · {{ itemLabel(item) }}</label>
                      <textarea
                        v-if="isMultiline(item)"
                        :id="`credential-value-${item.id}`"
                        v-model="drafts[item.id]"
                        :rows="isCodexLogin(item) ? 8 : 3"
                        class="min-h-[80px] w-full min-w-0 resize-y rounded-lg border border-line bg-base px-3 py-2 font-mono text-[12px] text-txt outline-none transition placeholder:text-txt3 focus:border-accent focus:ring-2 focus:ring-accent/15"
                        :placeholder="item.configured ? t('pages.workspace.credentials.replacePlaceholder') : t('pages.workspace.credentials.valuePlaceholder')"
                        :data-testid="`workspace-credential-input-${item.id}`"
                        autocomplete="off"
                        style="-webkit-text-security: disc;"
                      />
                      <input
                        v-else
                        :id="`credential-value-${item.id}`"
                        v-model="drafts[item.id]"
                        type="password"
                        class="w-full min-w-0 rounded-lg border border-line bg-base px-3 py-2 font-mono text-[12px] text-txt outline-none transition placeholder:text-txt3 focus:border-accent focus:ring-2 focus:ring-accent/15"
                        :placeholder="item.configured ? t('pages.workspace.credentials.replacePlaceholder') : t('pages.workspace.credentials.valuePlaceholder')"
                        :data-testid="`workspace-credential-input-${item.id}`"
                        autocomplete="new-password"
                      />
                      <div class="flex flex-wrap justify-end gap-2">
                        <AppButton size="sm" variant="ghost" :disabled="!!saving[item.id]" :data-testid="`workspace-credential-cancel-${item.id}`" @click="cancelEdit(item)">
                          {{ t('common.buttons.cancel') }}
                        </AppButton>
                        <AppButton size="sm" variant="primary" :disabled="!canSave(item) || !!saving[item.id]" :loading="!!saving[item.id]" :data-testid="`workspace-credential-save-${item.id}`" @click="save(item)">
                          {{ t('pages.workspace.credentials.save') }}
                        </AppButton>
                      </div>
                    </div>
                  </article>
                </template>
              </div>
            </div>
          </section>
        </template>
        </div>
        <ModelConnectionEditor ref="connectionManager" :inline="inline" :credentials="items" @changed="configurationChanged" @editing="connectionEditorOpen = $event" @error="toast.error($event)" />
        </div>
        <ModelConfigurationManager v-if="activeTab === 'models'" ref="modelConfigurationManager" :inline="inline" :allow-credential-creation="setup || view !== 'models'" :setup="setup" hide-create-button @loaded="modelCount = $event.length" @manage-credentials="manageModelCredentials" @changed="configurationChanged" @saved="emit('modelSaved', $event)" @editing="modelEditorOpen = $event" />
      </div>
    </div>

    <CredentialEditorSurface
      :inline="inline"
      :open="showCreate"
      :title="t('pages.workspace.credentials.createTitle')"
      :width="640"
      :close-on-esc="!creating"
      trap-focus
      :close-label="t('common.buttons.close')"
      @close="closeCreate"
    >
      <ol class="credential-wizard-progress" :aria-label="t('pages.workspace.credentials.createTitle')">
        <li :class="{ 'is-active': createStep === 1 }" :aria-current="createStep === 1 ? 'step' : undefined">
          <span class="credential-step-number">1</span>
          {{ t('pages.workspace.credentials.stepKind') }}
        </li>
        <li class="credential-step-connector" aria-hidden="true" />
        <li :class="{ 'is-active': createStep === 2 }" :aria-current="createStep === 2 ? 'step' : undefined">
          <span class="credential-step-number">2</span>
          {{ t('pages.workspace.credentials.stepDetails') }}
        </li>
        <li class="credential-step-connector" aria-hidden="true" /><li :class="{ 'is-active': createStep === 3 }" :aria-current="createStep === 3 ? 'step' : undefined"><span class="credential-step-number">3</span>{{ routingCopy.review }}</li>
      </ol>
      <div
        class="credential-step-shell"
        data-testid="workspace-credential-create-form"
        :data-credential-step="createStep"
        :data-step-motion="stepMotion"
        :data-step-locked="stepLock ? 'true' : 'false'"
        :style="stepShellStyle"
      >
        <Transition
          :name="stepMotion === 'forward' ? 'step-forward' : 'step-back'"
          @enter="() => { void measurePane().then(observePane) }"
        >
          <div v-if="createStep === 1" key="kind" ref="stepPane" class="credential-step-pane">
            <button v-if="showModelKeyKind" type="button" class="mt-3 flex w-full items-center gap-3 rounded-xl border px-3 py-3 text-left" :class="createKind === 'model_api' ? 'border-accent bg-accent-dim' : 'border-line'" autofocus data-testid="credential-kind-model_api" :aria-pressed="createKind === 'model_api'" @click="selectKind('model_api')"><Icon name="key" :size="22" /><span><strong class="text-[13px] text-txt">{{ routingCopy.modelKeys }}</strong></span></button>
            <div class="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-2">
              <button
                v-for="kind in creationKinds"
                :key="kind.id"
                type="button"
                class="flex items-center gap-3 rounded-xl border px-3 py-2.5 text-left transition-[border-color,background-color] duration-[160ms] ease-[cubic-bezier(0.16,1,0.3,1)]"
                :class="createKind === kind.id ? 'border-accent bg-accent-dim' : 'border-line bg-base hover:border-line-strong hover:bg-base/80'"
                :data-testid="`credential-kind-${kind.id}`"
                :aria-pressed="createKind === kind.id"
                @click="selectKind(kind.id)"
              >
                <CredentialProviderLogo :provider="kind.logoProvider" :icon="kind.icon" match="provider" :selected="createKind === kind.id" />
                <span class="min-w-0 truncate text-[13px] font-semibold text-txt">{{ kindLabel(kind.id) }}</span>
              </button>
            </div>
          </div>
          <div v-else-if="createStep === 2" key="secret" ref="stepPane" class="credential-step-pane space-y-4">
            <div v-if="selectedKind" class="flex items-center gap-3 rounded-lg bg-elevated/60 p-3">
              <CredentialProviderLogo :provider="selectedKind.logoProvider" :icon="selectedKind.icon" match="provider" />
              <span class="text-[13px] font-medium text-txt">{{ kindLabel(selectedKind.id) }}</span>
            </div>
            <label class="block">
              <span class="label">{{ t('pages.workspace.credentials.alias') }}</span>
              <input
                v-model="createAlias"
                class="input w-full"
                data-testid="workspace-credential-create-alias"
                autofocus
                autocomplete="off"
                :disabled="!!savedCredential"
                :placeholder="t('pages.workspace.credentials.aliasPlaceholder')"
                @input="aliasError = ''"
              />
              <p v-if="aliasError" class="mb-0 mt-1 text-[12px] text-err" data-testid="workspace-credential-alias-error">{{ aliasError }}</p>
            </label>
            <label v-if="createHasHost" class="block">
              <span class="label">Host</span>
              <input v-model="createHost" class="input w-full font-mono" data-testid="workspace-credential-create-host" :aria-invalid="!createHostValid" :placeholder="defaultGitCredentialHost(createKind)" autocomplete="url" spellcheck="false" />
              <p v-if="!createHostValid" class="mb-0 mt-1 text-xs text-err">{{ t('pages.workspace.credentials.invalidHost') }}</p>
            </label>
            <template v-if="createKind === 'model_api'">
              <label class="block"><span class="label">{{ routingCopy.provider }}</span><ModelProviderSelect :model-value="createIssuer" :disabled="!!savedCredential" data-testid="model-credential-issuer" @update:model-value="selectIssuer" /></label>
              <label v-if="createIssuer === 'custom'" class="block"><span class="label">{{ routingCopy.providerLabel }}</span><input v-model="customProviderLabel" :disabled="!!savedCredential" class="input w-full" data-testid="model-credential-provider-label" /></label>
            </template>
            <label class="block">
              <span class="label flex items-center justify-between gap-2">
                <span>{{ createSecretLabel }}</span>
                <button type="button" class="text-[11px] font-medium text-accent-2" data-testid="workspace-credential-secret-toggle" @click="revealSecret = !revealSecret">
                  {{ revealSecret ? t('pages.workspace.credentials.hideSecret') : t('pages.workspace.credentials.showSecret') }}
                </button>
              </span>
              <textarea
                v-if="selectedKind && (selectedKind.secret === 'multiline' || selectedKind.secret === 'loginFile')"
                v-model="createSecret"
                :disabled="!!savedCredential"
                rows="6"
                class="input min-h-[120px] w-full resize-y font-mono"
                data-testid="workspace-credential-create-value"
                autocomplete="off"
                :placeholder="t('pages.workspace.credentials.valuePlaceholder')"
                :class="revealSecret ? undefined : 'credential-secret-mask'"
              />
              <input
                v-else
                v-model="createSecret"
                :disabled="!!savedCredential"
                :type="revealSecret ? 'text' : 'password'"
                class="input w-full font-mono"
                data-testid="workspace-credential-create-value"
                autocomplete="new-password"
                :placeholder="t('pages.workspace.credentials.valuePlaceholder')"
              />
            </label>
            <p v-if="selectedKind?.id === 'opencode'" class="m-0 text-xs leading-5 text-txt3" data-testid="workspace-credential-opencode-family">{{ t('pages.workspace.credentials.opencodeFamilyHint') }}</p>
            <template v-if="createKind === 'model_api'">
              <label v-if="!modelCredentialBackend" :for="`${addMenuId}-connection`" class="flex items-center gap-2 text-xs font-medium text-txt2"><AppSwitch :id="`${addMenuId}-connection`" v-model="createConnectionAfter" :disabled="creating" :aria-label="routingCopy.configureConnectionNow" data-testid="model-credential-create-connection" />{{ routingCopy.configureConnectionNow }}</label>
              <div v-if="createConnectionAfter" class="space-y-3">
                <label class="block"><span class="label">API Base URL</span><input v-model="connectionForm.baseUrl" type="url" class="input w-full font-mono" data-testid="model-credential-connection-base-url" placeholder="https://…" /></label>
                <details data-testid="model-credential-connection-advanced" class="pt-1"><summary class="cursor-pointer text-xs text-txt3">{{ routingCopy.advancedSettings }}</summary><div class="mt-3 space-y-3">
                  <label class="block"><span class="label">{{ routingCopy.connectionName }}</span><input v-model="connectionForm.name" class="input w-full" data-testid="model-credential-connection-name" :placeholder="connectionName" /></label>
                  <label class="block"><span class="label">{{ routingCopy.protocol }}</span><AppSelect :model-value="connectionForm.protocol" :options="protocolOptions" :aria-label="routingCopy.protocol" searchable data-testid="model-credential-connection-protocol" @update:model-value="setConnectionProtocol" /></label>
                  <label class="block"><span class="label">{{ routingCopy.auth }}</span><AppSelect :model-value="connectionForm.scheme" :options="authOptions" :aria-label="routingCopy.auth" searchable data-testid="model-credential-connection-auth" @update:model-value="setConnectionAuth" /></label>
                </div></details>
              </div>
              <p v-else class="m-0 text-xs leading-5 text-txt3">{{ routingCopy.keyOnlyHint }}</p>
            </template>
          </div>
          <div v-else key="review" ref="stepPane" class="credential-step-pane space-y-4"><dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-3 rounded-xl border border-line p-4 text-xs"><dt class="text-txt3">{{ routingCopy.credentials }}</dt><dd class="m-0 text-txt">{{ createKind === 'model_api' ? routingCopy.modelKeys : selectedKind ? t(`pages.workspace.credentials.kinds.${selectedKind.id}`) : '' }}</dd><dt class="text-txt3">{{ routingCopy.name }}</dt><dd class="m-0 break-words text-txt">{{ createAlias }}</dd><template v-if="createKind === 'model_api'"><dt class="text-txt3">{{ routingCopy.issuer }}</dt><dd class="m-0 break-words text-txt">{{ providerLabel }} · {{ effectiveIssuer }}</dd><template v-if="inlineConnection"><dt class="text-txt3">{{ routingCopy.connectionName }}</dt><dd class="m-0 break-words text-txt">{{ connectionName }}</dd><dt class="text-txt3">API Base URL</dt><dd class="m-0 break-all font-mono text-txt">{{ connectionForm.baseUrl }}</dd><dt class="text-txt3">{{ routingCopy.protocol }}</dt><dd class="m-0 break-all text-txt">{{ connectionForm.protocol }} · {{ connectionForm.scheme }}</dd></template></template><template v-if="createHasHost"><dt class="text-txt3">Host</dt><dd class="m-0 break-all font-mono text-txt" data-testid="workspace-credential-review-host">{{ createHost }}</dd></template><dt class="text-txt3">{{ createSecretLabel }}</dt><dd class="m-0 font-mono text-txt">••••••••</dd></dl></div>
        </Transition>
        <p v-if="createError" role="alert" class="mb-0 mt-4 text-xs leading-5 text-err" :data-testid="savedCredential ? 'model-credential-partial-error' : 'credential-create-error'"><template v-if="savedCredential">{{ routingCopy.keySavedConnectionFailed }}<br /></template>{{ createError }}</p>
      </div>
      <template #footer>
        <AppButton v-if="createStep === 1" variant="ghost" :disabled="creating" @click="closeCreate">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton
          v-if="createStep === 1"
          variant="primary"
          :disabled="stepLock || !createKind"
          data-testid="workspace-credential-create-next"
          @click="goNext"
        >
          {{ t('pages.workspace.credentials.next') }}
        </AppButton>
        <AppButton
          v-if="createStep > 1"
          variant="ghost"
          :disabled="creating || stepLock || (!!savedCredential && createStep === 2)"
          data-testid="workspace-credential-create-back"
          @click="goBack"
        >
          {{ t('pages.workspace.credentials.back') }}
        </AppButton>
        <AppButton v-if="createStep === 2" variant="primary" :disabled="creating || stepLock || !createAlias.trim() || !secretReady || !connectionReady || !createHostValid" data-testid="workspace-credential-review-next" @click="goNext">{{ routingCopy.next }}</AppButton>
        <AppButton
          v-if="createStep === 3"
          variant="primary"
          :disabled="creating || stepLock || !createAlias.trim() || !secretReady || !connectionReady || !createHostValid"
          :loading="creating"
          data-testid="workspace-credential-create-submit"
          @click="create"
        >
          {{ savedCredential && inlineConnection ? routingCopy.retryConnection : t('pages.workspace.credentials.create') }}
        </AppButton>
      </template>
    </CredentialEditorSurface>
  </div>
</template>

<style scoped>
.credentials-inline .credentials-content { padding: 0; }
.credentials-inline .credentials-header h2 { font-size: 15px; }
.credentials-inline .credentials-header { margin-bottom: 16px; }
.credentials-inline .credential-wizard-progress { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); }
.credentials-inline .credential-wizard-progress li { min-width: 0; flex-wrap: wrap; }
.credentials-inline .credential-wizard-progress .credential-step-connector { display: none; }
.credentials-inline .credential-step-number { flex-shrink: 0; }
.credentials-content {
  max-width: 1080px;
  margin: 0 auto;
  padding: 12px 4px 40px;
}
.credentials-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
}
.credentials-emblem {
  display: flex;
  width: 38px;
  height: 38px;
  flex: none;
  align-items: center;
  justify-content: center;
  border: 1px solid rgb(var(--c-accent) / .14);
  border-radius: 11px;
  color: rgb(var(--c-accent-2));
  background: rgb(var(--c-accent) / .07);
}
.credentials-create {
  box-shadow: none;
}
.credentials-page .credentials-content { max-width: 960px; padding-top: 0; }
.credentials-tab-count {
  margin-left: 6px;
  border-radius: 999px;
  padding: 0 6px;
  background: rgb(var(--c-elevated));
  color: rgb(var(--c-txt3));
  font-size: 11px;
  font-weight: 500;
  font-variant-numeric: tabular-nums;
  line-height: 18px;
}
.credentials-page .credentials-section {
  overflow: hidden;
  border: 1px solid rgb(var(--c-line));
  border-radius: 12px;
  background: rgb(var(--c-surface));
}
.credentials-page .credentials-section + .credentials-section { margin-top: 24px; }
.credentials-page .credentials-section-heading {
  margin-bottom: 0;
  padding: 12px 20px;
  border-bottom: 1px solid rgb(var(--c-line) / .8);
}
.credentials-page .credentials-section-count {
  border-radius: 999px;
  padding: 0 7px;
  background: rgb(var(--c-elevated));
  line-height: 18px;
}
.credentials-page .credentials-list { border-block: 0; }
.credentials-page .credential-row { padding-inline: 20px; }
.credentials-page .credentials-empty { border-style: solid; background: rgb(var(--c-surface)); }
.credentials-add-menu { position: fixed; z-index: 10000; overflow-y: auto; padding: 5px; border: 1px solid rgb(var(--c-line)); border-radius: 9px; background: rgb(var(--c-surface)); box-shadow: 0 8px 24px rgb(0 0 0 / .10); }
.credentials-add-menu button { display: flex; align-items: center; gap: 9px; width: 100%; min-height: 34px; border: 0; border-radius: 5px; padding: 7px 10px; background: transparent; color: rgb(var(--c-txt2)); text-align: left; font-size: 12px; cursor: pointer; }
.credentials-add-menu button:hover, .credentials-add-menu button:focus-visible { background: rgb(var(--c-elevated)); }
.credentials-add-menu button:focus-visible { outline: 2px solid rgb(var(--c-accent) / .5); outline-offset: -2px; }
.credentials-summary {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 24px;
  margin-bottom: 24px;
}
.credentials-stat {
  display: flex;
  align-items: center;
  gap: 6px;
}
.credentials-section + .credentials-section {
  margin-top: 28px;
}
.credentials-section-heading {
  display: flex;
  min-height: 26px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}
.credentials-section-count {
  color: rgb(var(--c-txt3));
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}
.credentials-list {
  border-top: 1px solid rgb(var(--c-line) / .7);
  border-bottom: 1px solid rgb(var(--c-line) / .7);
}
.credentials-provider-group {
  display: contents;
}
.credentials-model-manager {
  min-width: 0;
}
.credential-row {
  min-width: 0;
  padding: 14px 0;
  transition: background-color 160ms ease;
}
.credential-row + .credential-row,
.credentials-provider-group + .credentials-provider-group > .credential-row:first-child {
  border-top: 1px solid rgb(var(--c-line) / .7);
}
.credential-row:hover,
.credential-row--editing {
  background: rgb(var(--c-elevated) / .35);
}
.credential-row-headline {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 100px auto;
  align-items: center;
  gap: 24px;
}
.credential-row-controls {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
}
.credential-row-buttons {
  display: flex;
  min-width: 64px;
  justify-content: flex-end;
  gap: 4px;
}
.credential-status {
  display: inline-flex;
  flex: none;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  line-height: 18px;
  white-space: nowrap;
}
.credential-status-dot {
  width: 5px;
  height: 5px;
  flex: none;
  border-radius: 50%;
}
.credential-row-value { min-width: 0; }
.credential-service-link { display: inline-flex; max-width: 100%; align-items: center; gap: 5px; border: 0; padding: 0; background: transparent; color: rgb(var(--c-txt3)); font-size: 11px; line-height: 18px; text-align: left; cursor: pointer; }
.credential-service-link:hover { color: rgb(var(--c-accent-2)); }
.credential-service-link:focus-visible { outline: 2px solid rgb(var(--c-accent) / .5); outline-offset: 2px; }
.credential-clear { color: rgb(var(--c-txt3)); }
.credential-clear:hover { color: rgb(var(--c-err)); }
.credential-row h4 { overflow-wrap: anywhere; }
.credential-editor { margin-left: 44px; }
.credential-wizard-progress {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 0 0 24px;
  padding: 0;
  list-style: none;
}
.credential-wizard-progress li {
  display: flex;
  align-items: center;
  gap: 8px;
  color: rgb(var(--c-txt3));
  font-size: 12px;
}
.credential-wizard-progress .is-active { color: rgb(var(--c-accent-2)); }
.credential-step-number {
  display: flex;
  width: 24px;
  height: 24px;
  align-items: center;
  justify-content: center;
  border: 1px solid rgb(var(--c-line));
  border-radius: 50%;
  font-size: 11px;
}
.is-active .credential-step-number {
  border-color: rgb(var(--c-accent) / .25);
  background: rgb(var(--c-accent) / .08);
}
.credential-wizard-progress .credential-step-connector {
  width: 32px;
  height: 1px;
  background: rgb(var(--c-line));
}
.credentials-empty {
  display: flex;
  min-height: 240px;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  border: 1px dashed rgb(var(--c-line));
  border-radius: 12px;
  padding: 24px;
  text-align: center;
}
@media (max-width: 1023px) {
  .credential-row-headline { grid-template-columns: minmax(0, 1fr) auto; gap: 6px 16px; }
  .credential-row-value { grid-column: 1; padding-left: 44px; }
  .credential-row-controls { grid-column: 2; grid-row: 1 / span 2; }
}
@media (max-width: 767px) {
  .credentials-content { padding: 4px 0 32px; }
  .credentials-header { flex-wrap: wrap; gap: 16px; }
  .credentials-create { margin-left: auto; }
  .credentials-summary { gap: 6px 18px; margin-bottom: 20px; }
  .credential-row { padding: 14px 0; }
  .credentials-page .credential-row { padding-inline: 16px; }
  .credentials-page .credentials-section-heading { padding-inline: 16px; }
  .credential-row-headline { grid-template-columns: minmax(0, 1fr); gap: 10px; }
  .credential-row-value { padding-left: 44px; }
  .credential-row-controls { grid-column: 1; grid-row: auto; padding-left: 44px; gap: 8px; }
  .credential-editor { margin-left: 0; }
}
@media (prefers-reduced-motion: reduce) {
  .credential-row { transition: none; }
}
.credential-secret-mask {
  -webkit-text-security: disc;
}
.credential-step-shell {
  /* Clip leave panes during the step slide; min-height (not height) lets the
     active pane grow so overflow reaches AppModal's modal-scroll-area. */
  position: relative;
  overflow: hidden;
  transition: min-height 200ms cubic-bezier(0.16, 1, 0.3, 1);
}
.credential-step-pane {
  width: 100%;
}
.step-forward-enter-active,
.step-back-enter-active {
  transition:
    transform 200ms cubic-bezier(0.16, 1, 0.3, 1),
    opacity 200ms cubic-bezier(0.16, 1, 0.3, 1);
}
.step-forward-leave-active,
.step-back-leave-active {
  position: absolute;
  top: 0;
  left: 0;
  width: 100%;
  transition:
    transform 180ms cubic-bezier(0.16, 1, 0.3, 1),
    opacity 180ms cubic-bezier(0.16, 1, 0.3, 1);
}
.step-forward-enter-from,
.step-back-leave-to {
  opacity: 0;
  transform: translateX(28px);
}
.step-forward-leave-to,
.step-back-enter-from {
  opacity: 0;
  transform: translateX(-28px);
}
</style>
