import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { InputField } from '@/components/workflow/RunLaunchModal.vue'
import { api } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import { useImageAttachments } from '@/lib/composables/useImageAttachments'
import { readStoredProjectId } from '@/lib/composables/useProjectContext'
import { graspFirstNodeId, isPublishedGraspFirst } from '@/lib/run/graspFirstPipeline'
import {
  clearHomeComposerDraft,
  loadHomeComposerDraft,
  saveHomeComposerDraft,
} from '@/lib/run/homeComposerDraft'
import { setHomeApproveHandoff } from '@/lib/run/homeApproveHandoff'
import { clipRunTitle } from '@/lib/run/runTitle'
import { missingRequiredAskField, seedAskLaunchFields } from '@/lib/run/useWorkflowAskInputs'
import { attachmentDisplayName } from '@/lib/shared/attachments'
import type { ClarifyImage, Project, Workflow } from '@/lib/shared/types'
import type { RunPriority } from '@/components/ui/PrioritySegmented.vue'

import {
  GRASP_STORAGE_KEYS,
  LEGACY_STORAGE_KEYS,
  migrateLocalStorageKey,
} from '@/lib/shared/migrateBrandStorage'

/** Remember last selected home pipeline across visits (plan g2.4). */
export const HOME_PIPELINE_MEMORY_KEY = GRASP_STORAGE_KEYS.homeLastPipelineId

/** Remember last home Composer priority (plan g1.4). Not stored in IndexedDB draft. */
export const HOME_PRIORITY_MEMORY_KEY = GRASP_STORAGE_KEYS.homeLastPriority

migrateLocalStorageKey(LEGACY_STORAGE_KEYS.homeLastPipelineId, HOME_PIPELINE_MEMORY_KEY)
migrateLocalStorageKey(LEGACY_STORAGE_KEYS.homeLastPriority, HOME_PRIORITY_MEMORY_KEY)

/** Debounce for auto-save (plan g2.2; NFR ~300–800ms). */
export const HOME_COMPOSER_DRAFT_DEBOUNCE_MS = 400

function titleFromDraft(text: string, images: ClarifyImage[]): string {
  const clipped = clipRunTitle(text)
  if (clipped) return clipped
  const name = images[0] ? attachmentDisplayName(images[0], 0) : ''
  return clipRunTitle(name)
}

function readLastPipelineId(): string {
  try {
    return localStorage.getItem(HOME_PIPELINE_MEMORY_KEY)?.trim() || ''
  } catch {
    return ''
  }
}

function writeLastPipelineId(id: string) {
  try {
    if (id) localStorage.setItem(HOME_PIPELINE_MEMORY_KEY, id)
    else localStorage.removeItem(HOME_PIPELINE_MEMORY_KEY)
  } catch {
    /* ignore quota / private mode */
  }
}

function pickDefaultPipelineId(list: Workflow[], preferred: string): string {
  if (preferred && list.some((w) => w.id === preferred)) return preferred
  return list[0]?.id || ''
}

/** Map projectId → name; missing list entry falls back to id; no projectId → empty. */
export function resolveHomeProjectName(
  projectId: string | undefined,
  namesById: Map<string, string>,
): string {
  const id = (projectId || '').trim()
  if (!id) return ''
  const name = namesById.get(id)?.trim()
  return name || id
}

function projectNameMap(projects: Project[] | null | undefined): Map<string, string> {
  const map = new Map<string, string>()
  for (const p of Array.isArray(projects) ? projects : []) {
    if (p?.id) map.set(p.id, p.name || '')
  }
  return map
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
  const projectNamesById = ref<Map<string, string>>(new Map())
  const loading = ref(false)
  const loadError = ref<string | null>(null)
  const selectedId = ref('')
  const launchPriority = ref<RunPriority>(readLastPriority())
  const draft = ref('')
  const sending = ref(false)
  const hidingPipelineId = ref<string | null>(null)
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
  /** Draft pipeline id preferred over HOME_PIPELINE_MEMORY_KEY (plan g2.4). */
  let preferredDraftPipelineId = ''
  /** Toast once after a non-empty restore. */
  let restoreToastShown = false
  /** Auto-save quota / partial toast once per session (plan g2.2 / F4). */
  let quotaToastShown = false

  const pipelines = computed(() =>
    workflows.value
      .filter((w) => isPublishedGraspFirst(w) && !!w.showOnHome)
      .map((w) => ({
        ...w,
        projectName: resolveHomeProjectName(w.projectId, projectNamesById.value),
      })),
  )
  const selected = computed(
    () => pipelines.value.find((w) => w.id === selectedId.value) || pipelines.value[0] || null,
  )
  /** Project context from the selected pipeline (not a home project gate). */
  const projectId = computed(
    () => selected.value?.projectId || launchTarget.value?.projectId || readStoredProjectId() || '',
  )
  const launchTitle = computed(() => titleFromDraft(pendingText.value, pendingImages.value))
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
      selectedId.value || preferredDraftPipelineId || '',
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
    preferredDraftPipelineId = ''
  }

  /**
   * Restore from IndexedDB (with legacy localStorage migration) (plan g2.1).
   * Pipeline selection: draft pipeline > lastPipeline memory > list default (g2.4).
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
      if (stored.pipelineId) {
        const list = pipelines.value
        if (list.length === 0) {
          // List not loaded yet — keep preference for pipelines watch (plan g2.4).
          preferredDraftPipelineId = stored.pipelineId
          selectedId.value = stored.pipelineId
        } else if (list.some((w) => w.id === stored.pipelineId)) {
          preferredDraftPipelineId = stored.pipelineId
          selectedId.value = stored.pipelineId
        } else {
          // Stale draft pipeline after list is known — do not clobber current selection.
          preferredDraftPipelineId = ''
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

  watch(
    pipelines,
    (list) => {
      // While the list is still empty (initial mount / load in flight), keep any
      // draft pipeline preference from hydrate — do not treat "not in empty list"
      // as unavailable (plan g2.4).
      if (list.length === 0) return
      const draftPreferred =
        preferredDraftPipelineId && list.some((w) => w.id === preferredDraftPipelineId)
          ? preferredDraftPipelineId
          : ''
      // Drop stale draft preference only once we know the pipeline is gone.
      if (preferredDraftPipelineId && !draftPreferred) {
        preferredDraftPipelineId = ''
      }
      const selectedPreferred =
        selectedId.value && list.some((w) => w.id === selectedId.value) ? selectedId.value : ''
      const preferred = draftPreferred || selectedPreferred || readLastPipelineId()
      const next = pickDefaultPipelineId(list, preferred)
      if (next !== selectedId.value) selectedId.value = next
      if (next) writeLastPipelineId(next)
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
      // Cross-project: omit projectId so the API returns all visible workflows.
      // Parallel listProjects so home cards can show names without a flash of UUID (g1.1).
      const [list, projects] = await Promise.all([
        api.listWorkflows({ signal: ac.signal }),
        api.listProjects({ signal: ac.signal }).catch(() => [] as Project[]),
      ])
      if (ac.signal.aborted) return
      workflows.value = Array.isArray(list) ? list : []
      projectNamesById.value = projectNameMap(projects)
    } catch (e: any) {
      if (ac.signal.aborted || e?.name === 'AbortError') return
      loadError.value = String(e?.message || e)
      workflows.value = []
      projectNamesById.value = new Map()
    } finally {
      if (!ac.signal.aborted) loading.value = false
    }
  }

  /** Reload home cards after from-baseline create (plan g2.1). Keep prior list if reload fails. */
  async function reloadAfterCreate(preferredId?: string) {
    const previousWorkflows = workflows.value
    const previousNames = projectNamesById.value
    await load()
    if (loadError.value) {
      workflows.value = previousWorkflows
      projectNamesById.value = previousNames
      return
    }
    const id = (preferredId || '').trim()
    if (id && pipelines.value.some((w) => w.id === id)) {
      selectPipeline(id)
    }
  }

  function selectPipeline(id: string) {
    if (!id) return
    selectedId.value = id
    preferredDraftPipelineId = id
    writeLastPipelineId(id)
  }

  function selectPriority(value: RunPriority) {
    launchPriority.value = parseRunPriority(value)
    writeLastPriority(launchPriority.value)
  }

  async function hidePipelineFromHome(wf: Workflow) {
    if (hidingPipelineId.value) return
    const previousWorkflows = workflows.value
    const previousSelectedId = selectedId.value
    hidingPipelineId.value = wf.id
    workflows.value = workflows.value.map((item) =>
      item.id === wf.id ? { ...item, showOnHome: false } : item,
    )
    const nextId = pipelines.value[0]?.id || ''
    selectedId.value = nextId
    preferredDraftPipelineId = nextId
    writeLastPipelineId(nextId)
    try {
      const saved = await api.patchWorkflowHomeVisibility(wf.id, false)
      workflows.value = workflows.value.map((item) =>
        item.id === wf.id ? { ...item, ...saved, showOnHome: false } : item,
      )
      toast.success(t('pages.projectDetail.homeVisibility.updated'))
    } catch (e: any) {
      workflows.value = previousWorkflows
      selectedId.value = previousSelectedId
      preferredDraftPipelineId = previousSelectedId
      writeLastPipelineId(previousSelectedId)
      toast.error(String(e?.message || e) || t('pages.projectDetail.homeVisibility.updateFailed'))
    } finally {
      hidingPipelineId.value = null
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

  function goGates(runId: string, nodeId?: string) {
    return router.push({
      path: '/gates',
      query: nodeId ? { run: runId, node: nodeId } : { run: runId },
    })
  }

  /**
   * The message travels with startRun and is delivered into the sandbox by the
   * engine once the approve node parks, so all we do here is hand the optimistic
   * bubble to the inbox and navigate.
   */
  async function afterStart(runId: string, text: string, images: ClarifyImage[]) {
    const wf = selected.value || launchTarget.value
    const knownNodeId = wf ? graspFirstNodeId(wf) || '' : ''
    setHomeApproveHandoff({ runId, nodeId: knownNodeId, text, images })
    await goGates(runId, knownNodeId || undefined)
  }

  async function send() {
    const wf = selected.value
    const text = draft.value.trim()
    const images = attach.attachments.value.map((im) => ({ ...im }))
    if (!wf) {
      toast.warn(t('pages.dashboard.pickPipeline'))
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
    writeLastPipelineId(wf.id)
    try {
      const missing = missingRequiredAskField(wf)
      if (missing) {
        toast.warn(t('pages.dashboard.missingRequired'))
        await seedLaunch(wf)
        return
      }
      const res = await api.startRun(wf.id, {}, 'manual', launchPriority.value, [], {
        title: titleFromDraft(text, images),
        firstMessage: { text, images },
      })
      // Success path: clear composer + local draft (plan g2.3).
      suppressSave = true
      draft.value = ''
      attach.clearAttachments()
      clearComposerDraftNow()
      suppressSave = false
      await afterStart(res.id, text, images)
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
      await afterStart(runId, text, images)
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
    pipelines,
    selected,
    selectedId,
    launchPriority,
    draft,
    sending,
    hidingPipelineId,
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
    selectPipeline,
    selectPriority,
    hidePipelineFromHome,
    send,
    closeLaunch,
    onLaunchStarted,
  }
}
