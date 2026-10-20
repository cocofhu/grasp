import { ref } from 'vue'
import type { InputField } from '@/components/workflow/RunLaunchModal.vue'
import { clearRunDraft, saveRunDraft } from '@/lib/run/runDraft'
import { seedAskLaunchFields } from '@/lib/run/useWorkflowAskInputs'
import { useToast } from '@/lib/composables/useToast'
import type { ClarifyImage, Workflow } from '@/lib/shared/types'
import { useI18n } from 'vue-i18n'

const open = ref(false)
const target = ref<Workflow | null>(null)
const runFields = ref<InputField[]>([])
const runInputs = ref<Record<string, string>>({})
const runImages = ref<Record<string, ClarifyImage[]>>({})
const draftRestored = ref(false)
let launchGeneration = 0

/**
 * Shell-level RunLaunchModal orchestrator so sidebar quick-launch works on any route.
 */
export function useWorkflowRunLaunch() {
  const toast = useToast()
  const { t } = useI18n()

  async function openLaunch(workflow: Workflow) {
    const generation = ++launchGeneration
    const seeded = await seedAskLaunchFields(workflow)
    if (generation !== launchGeneration) return
    target.value = workflow
    runFields.value = seeded.fields
    runInputs.value = seeded.inputs
    runImages.value = seeded.images
    draftRestored.value = seeded.restored
    open.value = true
  }

  function closeLaunch() {
    launchGeneration++
    open.value = false
    target.value = null
    runFields.value = []
    runInputs.value = {}
    runImages.value = {}
    draftRestored.value = false
  }

  async function saveRunDraftClick() {
    const wf = target.value
    if (!wf) return
    const images: Record<string, ClarifyImage[]> = {}
    for (const [k, v] of Object.entries(runImages.value)) {
      images[k] = v ? [...v] : []
    }
    const result = await saveRunDraft(wf.id, { ...runInputs.value }, images)
    if (result === 'ok') toast.success(t('common.toast.draftSaved'))
    else if (result === 'quota_exceeded' || result === 'partial') {
      // Text already on disk / fields persisted — warn per F4 (review v3), not error.
      toast.warn(t('common.toast.draftTooLarge'))
    } else toast.error(t('common.toast.draftSaveFailed'))
  }

  function onStarted() {
    if (target.value) void clearRunDraft(target.value.id)
  }

  return {
    open,
    target,
    runFields,
    runInputs,
    runImages,
    draftRestored,
    openLaunch,
    closeLaunch,
    saveRunDraftClick,
    onStarted,
  }
}
