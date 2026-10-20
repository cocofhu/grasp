// @vitest-environment happy-dom
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/api/api', () => ({
  api: {
    getWorkspace: vi.fn(),
    listWorkflows: vi.fn(),
    listAgents: vi.fn(),
  },
}))

vi.mock('@/lib/composables/useWorkspaceContext', () => ({ useWorkspaceContext: () => ({ ensureLoaded: () => import('@/lib/api/api').then(({ api }) => api.getWorkspace()) }) }))

import { api } from '@/lib/api/api'
import { DEFAULT_PROJECT_ID, ONBOARDING_WORKFLOW_NAMES, suppressOnboarding } from './onboardingWizard'
import {
  closeFirstInstall,
  firstInstallCompletedAt,
  onboardingOpen,
  markFirstInstallCompleted,
  openFirstInstall,
  probeFirstInstall,
  resetFirstInstallProbe,
} from './firstInstall'

const mocked = {
  getWorkspace: vi.mocked(api.getWorkspace),
  listWorkflows: vi.mocked(api.listWorkflows),
  listAgents: vi.mocked(api.listAgents),
}

function stubEmptyDefaultProject() {
  mocked.getWorkspace.mockResolvedValue({ id: DEFAULT_PROJECT_ID } as never)
  mocked.listWorkflows.mockResolvedValue([] as never)
  mocked.listAgents.mockResolvedValue([] as never)
}

describe('firstInstall', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    resetFirstInstallProbe()
  })

  it('opens on entry when the default project is still empty', async () => {
    stubEmptyDefaultProject()
    await probeFirstInstall()
    expect(onboardingOpen.value).toBe(true)
  })

  it('ignores a previous account probe that completes after reset', async () => {
    stubEmptyDefaultProject()
    let release!: (value: never[]) => void
    mocked.listWorkflows.mockReturnValueOnce(new Promise((resolve) => { release = resolve }))
    const oldProbe = probeFirstInstall()
    await vi.waitFor(() => expect(mocked.listWorkflows).toHaveBeenCalled())
    resetFirstInstallProbe()
    release([])
    await oldProbe
    expect(onboardingOpen.value).toBe(false)
  })

  it('stays closed while suppressed and never calls the API', async () => {
    stubEmptyDefaultProject()
    suppressOnboarding(DEFAULT_PROJECT_ID)
    await probeFirstInstall()
    expect(onboardingOpen.value).toBe(false)
    expect(mocked.listWorkflows).not.toHaveBeenCalled()
  })

  it('stays closed once the default workflow exists', async () => {
    stubEmptyDefaultProject()
    mocked.listWorkflows.mockResolvedValue([
      { id: 'wf-1', name: ONBOARDING_WORKFLOW_NAMES[0] },
    ] as never)
    await probeFirstInstall()
    expect(onboardingOpen.value).toBe(false)
  })

  it('does not reopen setup for an existing workspace without the default workflow', async () => {
    stubEmptyDefaultProject()
    mocked.listWorkflows.mockResolvedValue([{ id: 'wf-1', name: '我的流程' }] as never)
    await probeFirstInstall()
    expect(onboardingOpen.value).toBe(false)
  })

  it('probes once per session so route changes do not reopen it', async () => {
    stubEmptyDefaultProject()
    await probeFirstInstall()
    closeFirstInstall()
    await probeFirstInstall()
    expect(onboardingOpen.value).toBe(false)
    expect(mocked.getWorkspace).toHaveBeenCalledTimes(1)
  })

  it('stays closed when listing agents fails', async () => {
    stubEmptyDefaultProject()
    mocked.listAgents.mockRejectedValue(new Error('network'))
    await expect(probeFirstInstall()).resolves.toBeUndefined()
    expect(onboardingOpen.value).toBe(false)
  })

  it('stays silent when the default project is missing', async () => {
    mocked.getWorkspace.mockRejectedValue(new Error('404'))
    mocked.listWorkflows.mockResolvedValue([] as never)
    mocked.listAgents.mockResolvedValue([] as never)
    await expect(probeFirstInstall()).resolves.toBeUndefined()
    expect(onboardingOpen.value).toBe(false)
  })

  it('open/close and completion signal are manual controls', () => {
    openFirstInstall()
    expect(onboardingOpen.value).toBe(true)
    closeFirstInstall()
    expect(onboardingOpen.value).toBe(false)

    const before = firstInstallCompletedAt.value
    markFirstInstallCompleted()
    expect(firstInstallCompletedAt.value).toBeGreaterThanOrEqual(before)
    expect(firstInstallCompletedAt.value).toBeGreaterThan(0)
  })
})
