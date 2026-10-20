import { onScopeDispose, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import { agentProfileIssues } from '@/lib/run/workflowIO'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
import { useToast } from '@/lib/composables/useToast'
import { useAuth } from '@/lib/composables/useAuth'
import type { Workflow } from '@/lib/shared/types'

export function useWorkflowImport(opts?: {
  dirty?: () => boolean
  onConfirmDiscard?: () => Promise<boolean>
  projectId?: () => string | undefined
  /** When set, called after a successful import instead of navigating to the editor. */
  onImported?: (wf: Workflow) => void | Promise<void>
}) {
  const router = useRouter()
  const { t } = useI18n()
  const toast = useToast()
  const { workspaceId, ensureLoaded } = useWorkspaceContext()
  const fileInput = ref<HTMLInputElement | null>(null)
  const showDiscardConfirm = ref(false)
  const { user } = useAuth()
  let importGeneration = 0
  let disposed = false
  watch(() => user.value?.username || '', () => {
    importGeneration++
    showDiscardConfirm.value = false
    if (fileInput.value) fileInput.value.value = ''
  }, { flush: 'sync' })
  onScopeDispose(() => { disposed = true; importGeneration++ })

  function triggerImport() {
    if (opts?.dirty?.()) {
      showDiscardConfirm.value = true
      return
    }
    fileInput.value?.click()
  }

  function onDiscardCancel() {
    showDiscardConfirm.value = false
  }

  function onDiscardConfirm() {
    showDiscardConfirm.value = false
    fileInput.value?.click()
  }

  async function handleFileChange(e: Event) {
    const input = e.target as HTMLInputElement
    const file = input.files?.[0]
    input.value = ''
    if (!file) return
    const generation = ++importGeneration
    const username = user.value?.username || ''
    const current = () => !disposed && generation === importGeneration && username === (user.value?.username || '')
    try {
      const text = await file.text()
      if (!current()) return
      await ensureLoaded()
      if (!current()) return
      const projectId = workspaceId.value
      if (!projectId) throw new Error(t('pages.workflowEditor.projectRequired'))
      const wf = await api.importWorkflow(text, projectId)
      if (!current()) return
      await warnMissingAgents(wf, current)
      if (!current()) return
      toast.success(t('pages.workflowIO.import.success', { name: wf.name }))
      if (opts?.onImported) {
        await opts.onImported(wf)
      } else {
        router.push('/workflows/' + wf.id + '/edit')
      }
    } catch (err: any) {
      if (!current()) return
      toast.error(String(err?.message || err))
    }
  }

  async function warnMissingAgents(wf: Workflow, current: () => boolean) {
    try {
      const agents = await api.listAgents()
      if (!current()) return
      const issues = agentProfileIssues(wf.nodes, agents)
      if (issues.length) {
        const list = issues
          .map((issue) => `${issue.name}（${t('pages.workflowIO.import.issueMissing')}）`)
          .join('、')
        toast.warn(t('pages.workflowIO.import.missingAgentProfiles', { list }))
      }
    } catch {
      // non-blocking
    }
  }

  return {
    fileInput,
    showDiscardConfirm,
    triggerImport,
    onDiscardCancel,
    onDiscardConfirm,
    handleFileChange,
  }
}
