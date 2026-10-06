import { ref } from 'vue'
import { api } from '@/lib/api/api'
import {
  DEFAULT_PROJECT_ID,
  isOnboardingSuppressed,
  shouldAutoOpenOnboarding,
  type OnboardingMode,
} from '@/lib/pm/onboardingWizard'

/**
 * First install / new-project / retry share one App-level wizard mount.
 */
export const onboardingOpen = ref(false)
export const onboardingMode = ref<OnboardingMode>('firstInstall')
export const onboardingProjectId = ref(DEFAULT_PROJECT_ID)

/** Bumped after a successful bootstrap so open views can refetch. */
export const firstInstallCompletedAt = ref(0)

export function openFirstInstall(): void {
  onboardingMode.value = 'firstInstall'
  onboardingProjectId.value = DEFAULT_PROJECT_ID
  onboardingOpen.value = true
}

export function openCreateProjectOnboarding(): void {
  onboardingMode.value = 'createProject'
  onboardingProjectId.value = ''
  onboardingOpen.value = true
}

export function openRetryOnboarding(projectId: string): void {
  const id = projectId.trim()
  if (!id) return
  onboardingMode.value = id === DEFAULT_PROJECT_ID ? 'firstInstall' : 'retry'
  onboardingProjectId.value = id
  onboardingOpen.value = true
}

export function closeFirstInstall(): void {
  onboardingOpen.value = false
}

export function closeOnboarding(): void {
  onboardingOpen.value = false
}

export function markFirstInstallCompleted(): void {
  firstInstallCompletedAt.value = Date.now()
}

let probe: Promise<void> | null = null

/** Probes once per session; later route changes must not re-open the wizard. */
export function probeFirstInstall(): Promise<void> {
  if (!probe) probe = runProbe()
  return probe
}

export function resetFirstInstallProbe(): void {
  probe = null
  onboardingOpen.value = false
  onboardingMode.value = 'firstInstall'
  onboardingProjectId.value = DEFAULT_PROJECT_ID
}

async function runProbe(): Promise<void> {
  if (isOnboardingSuppressed(DEFAULT_PROJECT_ID)) return
  try {
    // getProject rejects when the default project is absent — nothing to bootstrap into.
    const [, workflows, agents] = await Promise.all([
      api.getProject(DEFAULT_PROJECT_ID),
      api.listWorkflows({ projectId: DEFAULT_PROJECT_ID }),
      api.listAgents(),
    ])
    const named = agents.map((a) => ({ name: a.name, projectId: a.projectId }))
    if (shouldAutoOpenOnboarding(DEFAULT_PROJECT_ID, workflows, named)) {
      onboardingMode.value = 'firstInstall'
      onboardingProjectId.value = DEFAULT_PROJECT_ID
      onboardingOpen.value = true
    }
  } catch {
    /* offline or no default project: stay silent, CTA still opens it manually */
  }
}
