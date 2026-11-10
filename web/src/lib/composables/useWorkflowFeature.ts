import { computed, reactive } from 'vue'

export interface WorkflowFeature { enabled: boolean; locked: boolean; source: string }
const state = reactive<WorkflowFeature>({ enabled: true, locked: false, source: 'config' })

export function setWorkflowFeature(value?: WorkflowFeature) {
  if (value) Object.assign(state, value)
}

export function useWorkflowFeature() {
  return { enabled: computed(() => state.enabled), locked: computed(() => state.locked) }
}
