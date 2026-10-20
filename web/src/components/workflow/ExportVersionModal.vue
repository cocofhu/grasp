<script setup lang="ts">
import { ref, watch, computed, onBeforeUnmount } from 'vue'
import { isAbortError } from '@/lib/run/liveLogRehydrate'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import { api } from '@/lib/api/api'
import { buildEnvelope, downloadJson, sanitizeFilename, type WorkflowGraphPayload } from '@/lib/run/workflowIO'
import { useToast } from '@/lib/composables/useToast'
import type { Workflow, WorkflowVersion } from '@/lib/shared/types'

const props = defineProps<{
  open: boolean
  workflowId: string
  workflowName: string
  description: string
  needsRepo: boolean
  status: Workflow['status']
  /** Current runtime defaults apply to every graph version; editor may include unsaved overrides. */
  defaults?: Workflow['defaults']
  /** When set (editor), draft export uses this graph including unsaved edits. */
  localDraft?: WorkflowGraphPayload | null
}>()

const emit = defineEmits<{ (e: 'close'): void }>()

const { t } = useI18n()
const toast = useToast()

const versions = ref<WorkflowVersion[]>([])
const loading = ref(false)
const exporting = ref(false)
const exportError = ref('')
const selected = ref<string>('draft')
let listAbort: AbortController | null = null
let exportAbort: AbortController | null = null
let listGen = 0
let exportGen = 0

function abortPending() {
  listAbort?.abort()
  exportAbort?.abort()
  listAbort = null
  exportAbort = null
  listGen++
  exportGen++
  loading.value = false
  exporting.value = false
}

watch(
  () => props.open,
  async (open) => {
    if (!open) {
      abortPending()
      exportError.value = ''
      return
    }
    selected.value = 'draft'
    versions.value = []
    exportError.value = ''
    if (props.status === 'published' && props.workflowId) {
      listAbort?.abort()
      const gen = ++listGen
      listAbort = new AbortController()
      loading.value = true
      try {
        versions.value = await api.listWorkflowVersions(props.workflowId, { signal: listAbort.signal })
        if (gen !== listGen) return
      } catch (e) {
        if (gen !== listGen || isAbortError(e)) return
        versions.value = []
      } finally {
        if (gen === listGen) loading.value = false
      }
    }
  },
  { immediate: true },
)

onBeforeUnmount(abortPending)

const options = computed(() => {
  const opts: { id: string; label: string; desc?: string }[] = [
    {
      id: 'draft',
      label: t('pages.workflowIO.export.currentDraft'),
      desc: props.localDraft
        ? t('pages.workflowIO.export.currentDraftEditorDesc')
        : t('pages.workflowIO.export.currentDraftDesc'),
    },
  ]
  for (const v of versions.value) {
    opts.push({
      id: `v${v.version}`,
      label: t('pages.workflowIO.export.publishedVersion', { n: v.version }),
      desc: t('pages.workflowIO.export.publishedVersionDesc'),
    })
  }
  return opts
})

async function confirmExport() {
  if (exporting.value) return
  exportAbort?.abort()
  const gen = ++exportGen
  exportAbort = new AbortController()
  exporting.value = true
  exportError.value = ''
  try {
    const meta = { name: props.workflowName, description: props.description, needsRepo: props.needsRepo, defaults: props.defaults }
    let graph: WorkflowGraphPayload

    if (selected.value === 'draft') {
      if (props.localDraft) {
        graph = props.localDraft
      } else {
        const wf = await api.getWorkflow(props.workflowId, { signal: exportAbort.signal })
        meta.defaults = wf.defaults
        graph = { nodes: wf.nodes, edges: wf.edges, variables: (wf as any).variables }
      }
    } else {
      const version = parseInt(selected.value.slice(1), 10)
      graph = await api.getWorkflowVersionGraph(props.workflowId, version, { signal: exportAbort.signal })
    }

    if (gen !== exportGen) return
    const envelope = buildEnvelope(meta, graph)
    downloadJson(sanitizeFilename(meta.name), envelope)
    toast.success(t('pages.workflowIO.export.success', { name: meta.name }))
    emit('close')
  } catch (e: any) {
    if (gen !== exportGen || isAbortError(e) || exportAbort.signal.aborted) return
    exportError.value = t('pages.workflowIO.export.failed')
    toast.error(exportError.value)
  } finally {
    if (gen === exportGen) exporting.value = false
  }
}
</script>

<template>
  <AppModal :open="open" :title="t('pages.workflowIO.export.title', { name: workflowName })" :width="440" @close="emit('close')">
    <div class="space-y-3">
      <p class="text-[12px] leading-relaxed text-txt3">{{ t('pages.workflowIO.export.intro') }}</p>
      <div v-if="loading" class="py-6 text-center text-sm text-txt3" role="status" aria-busy="true">{{ t('common.buttons.loading') }}</div>
      <div v-else class="space-y-2">
        <label
          v-for="opt in options"
          :key="opt.id"
          class="flex cursor-pointer items-start gap-3 rounded-md border px-3 py-2.5 transition"
          :class="selected === opt.id ? 'border-accent/50 bg-accent-dim/40' : 'border-line bg-base/40 hover:border-line-strong'"
        >
          <input v-model="selected" type="radio" :value="opt.id" class="mt-1 accent-accent" />
          <div class="min-w-0 flex-1">
            <div class="text-[13px] font-medium text-txt">{{ opt.label }}</div>
            <div v-if="opt.desc" class="mt-0.5 text-[11px] text-txt3">{{ opt.desc }}</div>
          </div>
        </label>
      </div>
      <p v-if="exportError" class="text-[12px] text-err" role="alert">{{ exportError }}</p>
    </div>
    <template #footer>
      <AppButton variant="ghost" :disabled="exporting" @click="emit('close')">{{ t('common.buttons.cancel') }}</AppButton>
      <AppButton variant="primary" icon="download" :disabled="exporting || loading" :aria-busy="exporting ? 'true' : undefined" @click="confirmExport">
        {{ exporting ? t('common.buttons.submitting') : t('pages.workflowIO.export.confirm') }}
      </AppButton>
    </template>
  </AppModal>
</template>
