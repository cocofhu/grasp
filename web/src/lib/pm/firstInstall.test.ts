// @vitest-environment happy-dom
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/api/api', () => ({
  api: {
    getProject: vi.fn(),
    listWorkflows: vi.fn(),
    listAgents: vi.fn(),
  },
}))

import { api } from '@/lib/api/api'
import { DEFAULT_PROJECT_ID, ONBOARDING_WORKFLOW_NAMES, suppressOnboarding } from './onboardingWizard'
import {
  closeFirstInstall,
  firstInstallCompletedAt,
  firstInstallOpen,
  markFirstInstallCompleted,
  openFirstInstall,
  probeFirstInstall,
  resetFirstInstallProbe,
} from './firstInstall'

const mocked = {
  getProject: vi.mocked(api.getProject),
  listWorkflows: vi.mocked(api.listWorkflows),
  listAgents: vi.mocked(api.listAgents),
}

function stubEmptyDefaultProject() {
  mocked.getProject.mockResolvedValue({ id: DEFAULT_PROJECT_ID } as never)
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
    expect(firstInstallOpen.value).toBe(true)
  })

  it('stays closed while suppressed and never calls the API', async () => {
    stubEmptyDefaultProject()
    suppressOnboarding(DEFAULT_PROJECT_ID)
    await probeFirstInstall()
    expect(firstInstallOpen.value).toBe(false)
    expect(mocked.getProject).not.toHaveBeenCalled()
  })

  it('stays closed once the default workflow exists', async () => {
    stubEmptyDefaultProject()
    mocked.listWorkflows.mockResolvedValue([
      { id: 'wf-1', name: ONBOARDING_WORKFLOW_NAMES[0] },
    ] as never)
    await probeFirstInstall()
    expect(firstInstallOpen.value).toBe(false)
  })

  it('still opens when other workflows exist but the default one is missing', async () => {
    stubEmptyDefaultProject()
    mocked.listWorkflows.mockResolvedValue([{ id: 'wf-1', name: '我的流程' }] as never)
    await probeFirstInstall()
    expect(firstInstallOpen.value).toBe(true)
  })

  it('probes once per session so route changes do not reopen it', async () => {
    stubEmptyDefaultProject()
    await probeFirstInstall()
    closeFirstInstall()
    await probeFirstInstall()
    expect(firstInstallOpen.value).toBe(false)
    expect(mocked.getProject).toHaveBeenCalledTimes(1)
  })

  it('stays closed when listing agents fails', async () => {
    stubEmptyDefaultProject()
    mocked.listAgents.mockRejectedValue(new Error('network'))
    await expect(probeFirstInstall()).resolves.toBeUndefined()
    expect(firstInstallOpen.value).toBe(false)
  })

  it('stays silent when the default project is missing', async () => {
    mocked.getProject.mockRejectedValue(new Error('404'))
    mocked.listWorkflows.mockResolvedValue([] as never)
    mocked.listAgents.mockResolvedValue([] as never)
    await expect(probeFirstInstall()).resolves.toBeUndefined()
    expect(firstInstallOpen.value).toBe(false)
  })

  it('open/close and completion signal are manual controls', () => {
    openFirstInstall()
    expect(firstInstallOpen.value).toBe(true)
    closeFirstInstall()
    expect(firstInstallOpen.value).toBe(false)

    const before = firstInstallCompletedAt.value
    markFirstInstallCompleted()
    expect(firstInstallCompletedAt.value).toBeGreaterThanOrEqual(before)
    expect(firstInstallCompletedAt.value).toBeGreaterThan(0)
  })
})
