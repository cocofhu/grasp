import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { InputField } from '@/components/workflow/RunLaunchModal.vue'
import { api } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import { useImageAttachments } from '@/lib/composables/useImageAttachments'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
import {
  clarifyFirstNodeId,
  isClarifyFirstWorkflow,
  isPublishedClarifyFirst,
  withPublishedSnapshot,
} from '@/lib/run/clarifyFirstWorkflow'
import {
  clearHomeComposerDraft,
  loadHomeComposerDraft,
  saveHomeComposerDraft,
} from '@/lib/run/homeComposerDraft'
import { setHomeApproveHandoff } from '@/lib/run/homeApproveHandoff'
import { runTitleFromDraft } from '@/lib/run/runTitle'
import { missingRequiredAskField, seedAskLaunchFields } from '@/lib/run/useWorkflowAskInputs'
import type { ClarifyImage, Workflow } from '@/lib/shared/types'
import type { Agent } from '@/lib/api/apiTypes'
import type { RunPriority } from '@/components/ui/PrioritySegmented.vue'

/** Remember last selected home workflow across visits (plan g2.4). */
export const HOME_WORKFLOW_MEMORY_KEY = 'grasp.home.lastPipelineId'

/** Remember last home Composer priority (plan g1.4). Not stored in IndexedDB draft. */
export const HOME_PRIORITY_MEMORY_KEY = 'grasp.home.lastPriority'

/** Debounce for auto-save (plan g2.2; NFR ~300–800ms). */
export const HOME_COMPOSER_DRAFT_DEBOUNCE_MS = 400

function readLastWorkflowId(): string {
  try {
    return localStorage.getItem(HOME_WORKFLOW_MEMORY_KEY)?.trim() || ''
  } catch {
    return ''
  }
}

function writeLastWorkflowId(id: string) {
  try {
    if (id) localStorage.setItem(HOME_WORKFLOW_MEMORY_KEY, id)
    else localStorage.removeItem(HOME_WORKFLOW_MEMORY_KEY)
  } catch {
    /* ignore quota / private mode */
  }
}

function pickDefaultWorkflowId(list: Workflow[], preferred: string): string {
  if (preferred && list.some((w) => w.id === preferred)) return preferred
  return list[0]?.id || ''
}

export function parseRunPriority(raw: string | null | undefined): RunPriority {
  if (raw === 'high' || raw === 'normal' || raw === 'low') return raw
  return 'normal'
}

function readLastPriority(): RunPriority {
  try {
    return parseRunPriority(localStorage.getItem(HOME_PRIORITY_MEMORY_KEY))
  } catch {
    return 'normal'
  }
}

function writeLastPriority(value: RunPriority) {
  try {
    localStorage.setItem(HOME_PRIORITY_MEMORY_KEY, value)
  } catch {
    /* ignore quota / private mode */
  }
}

export function useHomeApproveChat() {
  const router = useRouter()
  const toast = useToast()
  const { t } = useI18n()
  const attach = useImageAttachments()

  const workflows = ref<Workflow[]>([])
  const workflowsLoaded = ref(false)
  const { workspaceId, ensureLoaded: loadWorkspace } = useWorkspaceContext()
  const agentsByName = ref<Map<string, Agent>>(new Map())
  const loading = ref(false)
  const loadError = ref<string | null>(null)
  const selectedId = ref('')
  const launchPriority = ref<RunPriority>(readLastPriority())
  const draft = ref('')
  const sending = ref(false)
  const hidingWorkflowId = ref<string | null>(null)
  const pendingText = ref('')
  const pendingImages = ref<ClarifyImage[]>([])

  const launchOpen = ref(false)
  const launchTarget = ref<Workflow | null>(null)
  const runFields = ref<InputField[]>([])
  const runInputs = ref<Record<string, string>>({})
  const runImages = ref<Record<string, ClarifyImage[]>>({})
  const draftRestored = ref(false)

  let loadAbort: AbortController | null = null
  let saveTimer: ReturnType<typeof setTimeout> | null = null
  /** Skip auto-save while applying a restored draft. */
  let suppressSave = false
  /** Draft workflow id preferred over HOME_WORKFLOW_MEMORY_KEY (plan g2.4). */
  let preferredDraftWorkflowId = ''
  /** Toast once after a non-empty restore. */
  let restoreToastShown = false
  /** Auto-save quota / partial toast once per session (plan g2.2 / F4). */
  let quotaToastShown = false

  const homeWorkflows = computed(() =>
    workflows.value
      .filter((w) => isPublishedClarifyFirst(w, agentsByName.value) && !!w.showOnHome)
      .map((w) => ({
        ...withPublishedSnapshot(w),

      })),
  )
  /**
   * Home-visible clarify-first heads that are not startable: never published,
   * or the published snapshot itself is not clarify-first. Shown only when
   * homeWorkflows is empty.
   */
  const unpublishedHomeWorkflows = computed(() =>
    workflows.value.filter(
      (w) =>
        !!w.showOnHome &&
        !isPublishedClarifyFirst(w, agentsByName.value) &&
        isClarifyFirstWorkflow(w, agentsByName.value),
    ),
  )
  const selected = computed(() => homeWorkflows.value.find((w) => w.id === selectedId.value) || homeWorkflows.value[0] || null)
  const projectId = workspaceId
  const launchTitle = computed(() => runTitleFromDraft(pendingText.value, pendingImages.value))
  /** Opening message carried through the launch modal's own startRun call. */
  const launchFirstMessage = computed(() => ({
    text: pendingText.value,
    images: pendingImages.value,
  }))
  const canSend = computed(() => !!draft.value.trim() || attach.attachments.value.length > 0)

  async function flushComposerDraftSave() {
    if (suppressSave) return
    const result = await saveHomeComposerDraft(
      draft.value,
      attach.attachments.value,
      selectedId.value || preferredDraftWorkflowId || '',
    )
    if (result === 'ok') return
    if (result === 'quota_exceeded' || result === 'partial') {
      if (!quotaToastShown) {
        quotaToastShown = true
        toast.warn(t('common.toast.draftTooLarge'))
      }
    } else if (result === 'error') {
      toast.error(t('common.toast.draftSaveFailed'))
    }
  }

  function scheduleComposerDraftSave() {
    if (suppressSave) return
    if (saveTimer) clearTimeout(saveTimer)
    saveTimer = setTimeout(() => {
      saveTimer = null
      void flushComposerDraftSave()
    }, HOME_COMPOSER_DRAFT_DEBOUNCE_MS)
  }

  function clearComposerDraftNow() {
    if (saveTimer) {
      clearTimeout(saveTimer)
      saveTimer = null
    }
    void clearHomeComposerDraft()
    preferredDraftWorkflowId = ''
  }

  /**
   * Restore from IndexedDB (or the localStorage text fallback) (plan g2.1).
   * Workflow selection: draft workflow > lastWorkflow memory > list default (g2.4).
   */
  async function hydrateComposerDraft() {
    const stored = await loadHomeComposerDraft()
    if (!stored) return
    const hasContent = !!stored.text.trim() || stored.attachments.length > 0
    if (!hasContent) {
      await clearHomeComposerDraft()
      return
    }

    suppressSave = true
    try {
      draft.value = stored.text
      attach.attachments.value = stored.attachments.map((im) => ({ ...im }))
      // A project compose link supplies explicit context even when IDB resolves
      // after workflows load. Restore content without replacing that selection.
      if (stored.pipelineId) {
        const list = homeWorkflows.value
        if (list.length === 0) {
          // List not loaded yet — keep preference for workflows watch (plan g2.4).
          preferredDraftWorkflowId = stored.pipelineId
          selectedId.value = stored.pipelineId
        } else if (list.some((w) => w.id === stored.pipelineId)) {
          preferredDraftWorkflowId = stored.pipelineId
          selectedId.value = stored.pipelineId
        } else {
          // Stale draft workflow after list is known — do not clobber current selection.
          preferredDraftWorkflowId = ''
        }
      }
      if (!restoreToastShown) {
        restoreToastShown = true
        toast.success(t('pages.dashboard.draftRestored'))
      }
    } finally {
      suppressSave = false
    }
  }

  watch(homeWorkflows, (list) => {
      // While the list is still empty (initial mount / load in flight), keep any
      // draft workflow preference from hydrate — do not treat "not in empty list"
      // as unavailable (plan g2.4).
      if (list.length === 0) return
      const draftPreferred =
        preferredDraftWorkflowId && list.some((w) => w.id === preferredDraftWorkflowId)
          ? preferredDraftWorkflowId
          : ''
      // Drop stale draft preference only once we know the workflow is gone.
      if (preferredDraftWorkflowId && !draftPreferred) {
        preferredDraftWorkflowId = ''
      }
      const selectedPreferred =
        selectedId.value && list.some((w) => w.id === selectedId.value) ? selectedId.value : ''
      const preferred = draftPreferred || selectedPreferred || readLastWorkflowId()
      const next = pickDefaultWorkflowId(list, preferred)
      if (next !== selectedId.value) selectedId.value = next
      if (next) writeLastWorkflowId(next)
    },
    { immediate: true },
  )

  watch([draft, () => attach.attachments.value, selectedId], () => {
    scheduleComposerDraftSave()
  }, { deep: true })

  async function load() {
    loadAbort?.abort()
    const ac = new AbortController()
    loadAbort = ac
    loading.value = true
    loadError.value = null
    try {
      // Load the visible workflow catalog and workspace metadata together.
      // Agents supply the caps that decide which workflows open with a clarify Agent.
      const [list, , agents] = await Promise.all([
        api.listWorkflows({ signal: ac.signal }),
        loadWorkspace(),
        api.listAgents().catch(() => [] as Agent[]),
      ])
      if (ac.signal.aborted) return
      agentsByName.value = new Map((Array.isArray(agents) ? agents : []).map((a) => [a.name, a]))
      workflows.value = Array.isArray(list) ? list : []
      workflowsLoaded.value = true
    } catch (e: any) {
      if (ac.signal.aborted || e?.name === 'AbortError') return
      loadError.value = String(e?.message || e)
      workflows.value = []
    } finally {
      if (!ac.signal.aborted) loading.value = false
    }
  }

  /** Reload home cards after from-baseline create (plan g2.1). Keep prior list if reload fails. */
  async function reloadAfterCreate(preferredId?: string) {
    const previousWorkflows = workflows.value
    await load()
    if (loadError.value) {
      workflows.value = previousWorkflows
      return
    }
    const id = (preferredId || '').trim()
    if (id && homeWorkflows.value.some((w) => w.id === id)) {
      selectWorkflow(id)
    }
  }

  function selectWorkflow(id: string) {
    if (!id) return
    selectedId.value = id
    preferredDraftWorkflowId = id
    writeLastWorkflowId(id)
  }

  function selectPriority(value: RunPriority) {
    launchPriority.value = parseRunPriority(value)
    writeLastPriority(launchPriority.value)
  }

  async function hideWorkflowFromHome(wf: Workflow) {
    if (hidingWorkflowId.value) return
    const previousWorkflows = workflows.value
    const previousSelectedId = selectedId.value
    hidingWorkflowId.value = wf.id
    workflows.value = workflows.value.map((item) =>
      item.id === wf.id ? { ...item, showOnHome: false } : item,
    )
    const candidates = homeWorkflows.value
    const nextId = candidates[0]?.id || ''
    selectedId.value = nextId
    preferredDraftWorkflowId = nextId
    writeLastWorkflowId(nextId)
    try {
      const saved = await api.patchWorkflowHomeVisibility(wf.id, false)
      workflows.value = workflows.value.map((item) =>
        item.id === wf.id ? { ...item, ...saved, showOnHome: false } : item,
      )
      toast.success(t('pages.projectDetail.homeVisibility.updated'))
    } catch (e: any) {
      workflows.value = previousWorkflows
      selectedId.value = previousSelectedId
      preferredDraftWorkflowId = previousSelectedId
      writeLastWorkflowId(previousSelectedId)
      toast.error(String(e?.message || e) || t('pages.projectDetail.homeVisibility.updateFailed'))
    } finally {
      hidingWorkflowId.value = null
    }
  }

  async function seedLaunch(wf: Workflow) {
    const seeded = await seedAskLaunchFields(wf)
    launchTarget.value = wf
    runFields.value = seeded.fields
    runInputs.value = seeded.inputs
    runImages.value = seeded.images
    draftRestored.value = seeded.restored
    launchOpen.value = true
  }

  function closeLaunch() {
    launchOpen.value = false
  }

  function goGates(runId: string, _wf: Workflow | null, nodeId?: string) {
    // Align inbox filters to the workflow being started (plan g1.1). Use the
    // workflow object's projectId only — never a hard-coded project name/UUID.
    const query: Record<string, string> = { run: runId }
    if (nodeId) query.node = nodeId
    return router.push({ path: '/gates', query })
  }

  /**
   * The message travels with startRun and is delivered into the sandbox by the
   * engine once the approve node parks, so all we do here is hand the optimistic
   * bubble to the inbox and navigate.
   */
  async function afterStart(runId: string, text: string, images: ClarifyImage[], wf: Workflow | null) {
    const knownNodeId = wf ? clarifyFirstNodeId(wf, agentsByName.value) || '' : ''
    setHomeApproveHandoff({ runId, nodeId: knownNodeId, text, images })
    await goGates(runId, wf, knownNodeId || undefined)
  }

  async function send() {
    const wf = selected.value
    const text = draft.value.trim()
    const images = attach.attachments.value.map((im) => ({ ...im }))
    if (!wf) {
      toast.warn(t('pages.dashboard.pickWorkflow'))
      return
    }
    if (!text && images.length === 0) {
      toast.warn(t('pages.dashboard.needText'))
      return
    }
    if (attach.blockSendIfOversized(images)) return
    if (sending.value) return
    sending.value = true
    pendingText.value = text
    pendingImages.value = images
    writeLastWorkflowId(wf.id)
    try {
      const missing = missingRequiredAskField(wf)
      if (missing) {
        toast.warn(t('pages.dashboard.missingRequired'))
        await seedLaunch(wf)
        return
      }
      const res = await api.startRun(wf.id, {}, 'manual', launchPriority.value, [], {
        title: runTitleFromDraft(text, images),
        firstMessage: { text, images },
        publishedSnapshot: true,
      })
      // Success path: clear composer + local draft (plan g2.3).
      suppressSave = true
      draft.value = ''
      attach.clearAttachments()
      clearComposerDraftNow()
      suppressSave = false
      await afterStart(res.id, text, images, wf)
    } catch (e: any) {
      const msg = String(e?.message || e)
      if (msg.includes('缺少必填项')) {
        toast.warn(t('pages.dashboard.missingRequired'))
        await seedLaunch(wf)
        return
      }
      toast.error(msg)
    } finally {
      sending.value = false
    }
  }

  async function onLaunchStarted(runId: string) {
    const wf = launchTarget.value || selected.value
    launchOpen.value = false
    const text = pendingText.value
    const images = pendingImages.value.map((im) => ({ ...im }))
    sending.value = true
    try {
      // RunLaunch completed startRun — clear composer + draft (plan g2.3).
      suppressSave = true
      draft.value = ''
      attach.clearAttachments()
      clearComposerDraftNow()
      suppressSave = false
      await afterStart(runId, text, images, wf)
    } finally {
      sending.value = false
    }
  }

  onMounted(() => {
    // Hydrate first so draft preference is set before the workflow list settles (plan g2.4).
    void (async () => {
      await hydrateComposerDraft()
      await load()
    })()
  })
  onUnmounted(() => {
    loadAbort?.abort()
    // Flush pending debounced save so a quick leave still persists (plan g2.2).
    if (saveTimer) {
      clearTimeout(saveTimer)
      saveTimer = null
      void flushComposerDraftSave()
    }
  })

  return {
    projectId,
    homeWorkflows,
    unpublishedHomeWorkflows,
    selected,
    selectedId,
    launchPriority,
    draft,
    sending,
    hidingWorkflowId,
    canSend,
    loading,
    loadError,
    launchOpen,
    launchTarget,
    launchTitle,
    launchFirstMessage,
    runFields,
    runInputs,
    runImages,
    draftRestored,
    attachments: attach.attachments,
    fileInput: attach.fileInput,
    attachNotice: attach.notice,
    onPickFiles: attach.onPickFiles,
    onPaste: attach.onPaste,
    removeAttachment: attach.removeAttachment,
    load,
    reloadAfterCreate,
    selectWorkflow,
    selectPriority,
    hideWorkflowFromHome,
    send,
    closeLaunch,
    onLaunchStarted,
  }
}
