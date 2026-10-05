import { describe, expect, it } from 'vitest'
import { ref, nextTick } from 'vue'
import { UNNAMED_GROUP_KEY } from '@/lib/run/artifactGroups'
import { useArtifactGroupSelection } from '@/lib/run/useArtifactGroupSelection'
import type { Artifact } from '@/lib/shared/types'

function artifact(overrides: Partial<Artifact> & Pick<Artifact, 'id' | 'createdAt'>): Artifact {
  return {
    name: `${overrides.id}.json`,
    kind: 'json',
    nodeId: 'test',
    runId: 'run-1',
    workflowId: 'wf-a',
    workflowName: '工作流 A',
    sizeBytes: 100,
    content: '',
    ...overrides,
  }
}

const FIXTURE: Artifact[] = [
  artifact({ id: 'a1', createdAt: '2026-07-03T10:00:00Z', workflowId: 'wf-a', workflowName: '工作流 A' }),
  artifact({ id: 'a2', createdAt: '2026-07-02T15:00:00Z', workflowId: 'wf-b', workflowName: '工作流 B' }),
  artifact({ id: 'a3', createdAt: '2026-07-02T16:00:00Z', workflowId: 'wf-b', workflowName: '工作流 B' }),
  artifact({ id: 'a4', createdAt: '2026-07-01T12:00:00Z', workflowId: 'wf-rand', workflowName: '' }),
  artifact({ id: 'a5', createdAt: '2026-06-30T08:00:00Z', workflowId: undefined, workflowName: '' }),
]

describe('useArtifactGroupSelection', () => {
  it('defaults to the named group with most artifacts when wf is empty', async () => {
    const artifacts = ref(FIXTURE)
    const wfParam = ref('')
    const { activeGroup, shouldAutoSelectArtifact } = useArtifactGroupSelection(artifacts, wfParam)
    await nextTick()
    expect(activeGroup.value?.key).toBe('wf-b')
    expect(activeGroup.value?.isUnnamed).toBe(false)
    expect(shouldAutoSelectArtifact.value).toBe(true)
  })

  it('always exposes all groups in sidebar even when wfParam is set', async () => {
    const artifacts = ref(FIXTURE)
    const wfParam = ref('wf-a')
    const { groups, activeGroup } = useArtifactGroupSelection(artifacts, wfParam)
    await nextTick()
    expect(groups.value.length).toBeGreaterThan(1)
    expect(activeGroup.value?.key).toBe('wf-a')
  })

  it('restores activeGroup from a valid wfParam', async () => {
    const artifacts = ref(FIXTURE)
    const wfParam = ref('wf-a')
    const { activeGroup, explicitSelection } = useArtifactGroupSelection(artifacts, wfParam)
    await nextTick()
    expect(activeGroup.value?.key).toBe('wf-a')
    expect(explicitSelection.value).toBe(true)
  })

  it('falls back to the largest named group for invalid wfParam without changing wfParam', async () => {
    const artifacts = ref(FIXTURE)
    const wfParam = ref('wf-missing')
    const { activeGroup, groups, invalidWfParam } = useArtifactGroupSelection(artifacts, wfParam)
    await nextTick()
    expect(invalidWfParam.value).toBe(true)
    expect(groups.value.length).toBeGreaterThan(1)
    expect(activeGroup.value?.key).toBe('wf-b')
    expect(activeGroup.value?.isUnnamed).toBe(false)
    expect(wfParam.value).toBe('wf-missing')
  })

  it('does not default-select unnamed even when it has the most artifacts', async () => {
    const artifacts = ref([
      artifact({ id: 'a1', createdAt: '2026-07-01T00:00:00Z', workflowId: 'wf-a' }),
      artifact({ id: 'a2', createdAt: '2026-06-30T00:00:00Z', workflowId: undefined, workflowName: '' }),
      artifact({ id: 'a3', createdAt: '2026-06-29T00:00:00Z', workflowId: undefined, workflowName: '' }),
      artifact({ id: 'a4', createdAt: '2026-06-28T00:00:00Z', workflowId: undefined, workflowName: '' }),
    ])
    const wfParam = ref('')
    const { activeGroup } = useArtifactGroupSelection(artifacts, wfParam)
    await nextTick()
    expect(activeGroup.value?.key).toBe('wf-a')
    expect(activeGroup.value?.isUnnamed).toBe(false)
  })

  it('selecting a named group writes wfParam', async () => {
    const artifacts = ref(FIXTURE)
    const wfParam = ref('')
    const { selectGroup, activeGroup } = useArtifactGroupSelection(artifacts, wfParam)
    selectGroup('wf-a', false)
    await nextTick()
    expect(wfParam.value).toBe('wf-a')
    expect(activeGroup.value?.key).toBe('wf-a')
  })

  it('selecting unnamed group keeps wf empty and highlights unnamed', async () => {
    const artifacts = ref(FIXTURE)
    const wfParam = ref('')
    const { selectGroup, activeGroup, highlightUnnamed } = useArtifactGroupSelection(artifacts, wfParam)
    selectGroup(UNNAMED_GROUP_KEY, true)
    await nextTick()
    expect(wfParam.value).toBe('')
    expect(highlightUnnamed.value).toBe(true)
    expect(activeGroup.value?.key).toBe(UNNAMED_GROUP_KEY)
    expect(activeGroup.value?.artifacts).toHaveLength(1)
  })

  it('switching from named wf to unnamed group keeps highlightUnnamed', async () => {
    const artifacts = ref(FIXTURE)
    const wfParam = ref('wf-a')
    const { selectGroup, activeGroup, highlightUnnamed } = useArtifactGroupSelection(artifacts, wfParam)
    await nextTick()
    expect(activeGroup.value?.key).toBe('wf-a')

    selectGroup(UNNAMED_GROUP_KEY, true)
    await nextTick()
    expect(wfParam.value).toBe('')
    expect(highlightUnnamed.value).toBe(true)
    expect(activeGroup.value?.key).toBe(UNNAMED_GROUP_KEY)
    expect(activeGroup.value?.title).toBe('')
  })

  it('applyWorkflowFilter to a specific workflow clears unnamed highlight', async () => {
    const artifacts = ref(FIXTURE)
    const wfParam = ref('')
    const { selectGroup, applyWorkflowFilter, highlightUnnamed, activeGroup } = useArtifactGroupSelection(
      artifacts,
      wfParam,
    )
    selectGroup(UNNAMED_GROUP_KEY, true)
    await nextTick()
    expect(highlightUnnamed.value).toBe(true)

    applyWorkflowFilter('wf-a')
    await nextTick()
    expect(wfParam.value).toBe('wf-a')
    expect(highlightUnnamed.value).toBe(false)
    expect(activeGroup.value?.key).toBe('wf-a')
  })

  it('applyWorkflowFilter to all workflows resets explicit selection and restores default group', async () => {
    const artifacts = ref(FIXTURE)
    const wfParam = ref('')
    const { selectGroup, applyWorkflowFilter, highlightUnnamed, activeGroup } = useArtifactGroupSelection(
      artifacts,
      wfParam,
    )
    selectGroup(UNNAMED_GROUP_KEY, true)
    await nextTick()
    expect(highlightUnnamed.value).toBe(true)

    applyWorkflowFilter('')
    await nextTick()
    expect(wfParam.value).toBe('')
    expect(highlightUnnamed.value).toBe(false)
    expect(activeGroup.value?.key).toBe('wf-b')
  })

  it('applyWorkflowFilter from named wf to all shows all groups with default active group', async () => {
    const artifacts = ref(FIXTURE)
    const wfParam = ref('wf-a')
    const { applyWorkflowFilter, groups, activeGroup } = useArtifactGroupSelection(artifacts, wfParam)
    await nextTick()
    expect(groups.value.length).toBeGreaterThan(1)

    applyWorkflowFilter('')
    await nextTick()
    expect(groups.value.length).toBeGreaterThan(1)
    expect(activeGroup.value?.key).toBe('wf-b')
  })

  it('shouldAutoSelectArtifact is true for explicit selection but false for invalid wf fallback', async () => {
    const artifacts = ref(FIXTURE)
    const wfValid = ref('wf-a')
    const { shouldAutoSelectArtifact: autoValid } = useArtifactGroupSelection(artifacts, wfValid)
    await nextTick()
    expect(autoValid.value).toBe(true)

    const wfInvalid = ref('wf-ghost')
    const { shouldAutoSelectArtifact: autoInvalid, activeGroup } = useArtifactGroupSelection(
      artifacts,
      wfInvalid,
    )
    await nextTick()
    expect(activeGroup.value?.key).toBe('wf-b')
    expect(autoInvalid.value).toBe(false)
  })
})
