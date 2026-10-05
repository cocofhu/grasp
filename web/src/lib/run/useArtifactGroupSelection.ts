import { computed, ref, watch, type Ref } from 'vue'
import {
  buildGroups,
  resolveDefaultGroup,
  type WorkflowMap,
} from '@/lib/run/artifactGroups'
import type { Artifact } from '@/lib/shared/types'

export function useArtifactGroupSelection(
  artifacts: Ref<Artifact[]>,
  wfParam: Ref<string>,
  workflows?: Ref<WorkflowMap | undefined>,
) {
  const selectedGroupKey = ref<string | null>(null)
  const highlightUnnamed = ref(false)
  const explicitSelection = ref(false)

  const allGroups = computed(() =>
    buildGroups(artifacts.value, workflows?.value),
  )

  const namedGroups = computed(() => allGroups.value.filter((g) => !g.isUnnamed))

  const invalidWfParam = computed(() => {
    if (!wfParam.value) return false
    return !allGroups.value.some((g) => g.workflowId === wfParam.value)
  })

  /** Sidebar always shows all groups; wfParam only drives highlight and URL sync. */
  const groups = computed(() => allGroups.value)

  const activeGroup = computed(() => {
    const list = allGroups.value
    if (!list.length) return null

    if (wfParam.value && !invalidWfParam.value) {
      const fromUrl = list.find((g) => g.workflowId === wfParam.value)
      if (fromUrl) return fromUrl
    }

    if (invalidWfParam.value) {
      return resolveDefaultGroup(namedGroups.value)
    }

    if (highlightUnnamed.value) {
      return list.find((g) => g.isUnnamed) ?? resolveDefaultGroup(list, { highlightUnnamed: true })
    }

    if (explicitSelection.value && selectedGroupKey.value) {
      const byKey = list.find((g) => g.key === selectedGroupKey.value)
      if (byKey) return byKey
    }

    return resolveDefaultGroup(namedGroups.value)
  })

  const activeGroupArtifacts = computed(() => activeGroup.value?.artifacts ?? [])

  const shouldAutoSelectArtifact = computed(() => {
    if (activeGroup.value === null || invalidWfParam.value) return false
    if (explicitSelection.value || highlightUnnamed.value) return true
    return !wfParam.value
  })

  function selectGroup(key: string, isUnnamed: boolean) {
    explicitSelection.value = true
    selectedGroupKey.value = key
    highlightUnnamed.value = isUnnamed
    if (isUnnamed) {
      wfParam.value = ''
    } else {
      const g = allGroups.value.find((x) => x.key === key)
      if (g?.workflowId) wfParam.value = g.workflowId
    }
  }

  /** WorkflowFilter changed; always clears sidebar unnamed highlight (even when wf stays empty). */
  function applyWorkflowFilter(wf: string) {
    highlightUnnamed.value = false
    selectedGroupKey.value = null
    explicitSelection.value = false
    wfParam.value = wf
  }

  watch(
    [wfParam, allGroups],
    ([wf]) => {
      if (wf && !invalidWfParam.value) {
        explicitSelection.value = true
        const g = allGroups.value.find((x) => x.workflowId === wf)
        if (g) selectedGroupKey.value = g.key
      }
    },
    { immediate: true },
  )

  return {
    selectedGroupKey,
    highlightUnnamed,
    explicitSelection,
    invalidWfParam,
    allGroups,
    groups,
    activeGroup,
    activeGroupArtifacts,
    shouldAutoSelectArtifact,
    selectGroup,
    applyWorkflowFilter,
  }
}
