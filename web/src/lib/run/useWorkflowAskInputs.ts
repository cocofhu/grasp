import { computed } from 'vue'
import type { InputField } from '@/components/workflow/RunLaunchModal.vue'
import { mergeRunDraft } from '@/lib/run/runDraft'
import type { ClarifyImage, Workflow } from '@/lib/shared/types'

function askFieldsFromWorkflow(wf: Pick<Workflow, 'nodes'>): InputField[] {
  const input = (wf.nodes || []).find((n) => n.type === 'input')
  const vars = ((input?.config?.variables as any[]) || []).filter((v) => v && v.name && v.ask)
  return vars.map((v) => ({
    key: v.name,
    desc: v.desc,
    type: v.type === 'string' ? 'text' : v.type,
    required: v.required,
    default:
      v.type === 'repos'
        ? JSON.stringify(Array.isArray(v.value) ? v.value : [])
        : v.value == null
          ? ''
          : String(v.value),
    editable: v.editable,
    options: v.options,
  }))
}

function fieldOptions(f: InputField): string[] {
  return String(f.options || '')
    .split(/[,，]/)
    .map((s) => s.trim())
    .filter(Boolean)
}

export function isAskValueBlank(type: string | undefined, value: string): boolean {
  const v = String(value ?? '').trim()
  if (!v) return true
  if (type === 'repos') {
    try {
      const parsed = JSON.parse(v)
      if (Array.isArray(parsed) && parsed.length === 0) return true
    } catch {
      /* non-JSON non-empty still counts as filled */
    }
  }
  return false
}

export function missingRequiredAskField(wf: Pick<Workflow, 'nodes'>): InputField | undefined {
  return askFieldsFromWorkflow(wf).find((f) => f.required && isAskValueBlank(f.type, f.default || ''))
}

export async function seedAskLaunchFields(wf: Pick<Workflow, 'id' | 'nodes'>): Promise<{
  fields: InputField[]
  inputs: Record<string, string>
  images: Record<string, ClarifyImage[]>
  restored: boolean
}> {
  const fields = askFieldsFromWorkflow(wf)
  const seed: Record<string, string> = {}
  const imgSeed: Record<string, ClarifyImage[]> = {}
  for (const f of fields) {
    seed[f.key] = f.default || (f.type === 'select' ? fieldOptions(f)[0] || '' : '')
    imgSeed[f.key] = []
  }
  const merged = await mergeRunDraft(
    wf.id,
    seed,
    imgSeed,
    fields.map((f) => f.key),
  )
  return { fields, inputs: merged.inputs, images: merged.images, restored: merged.restored }
}

/** Extract ask=true global variables from a workflow's input node. */
export function useWorkflowAskInputs(wf: Workflow) {
  const fields = computed<InputField[]>(() => askFieldsFromWorkflow(wf))
  return { fields }
}
