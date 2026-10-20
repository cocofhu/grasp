import { ref } from 'vue'
import { api } from '@/lib/api/api'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
import {
  shouldAutoOpenOnboarding,
  isOnboardingSuppressed,
  type OnboardingMode,
} from '@/lib/pm/onboardingWizard'

/**
 * First install and retry share one App-level wizard mount.
 */
export const onboardingOpen = ref(false)
export const onboardingMode = ref<OnboardingMode>('firstInstall')
export const onboardingProjectId = ref('')

/** Bumped after a successful bootstrap so open views can refetch. */
export const firstInstallCompletedAt = ref(0)

export function openFirstInstall(): void {
  onboardingMode.value = 'firstInstall'
  onboardingProjectId.value = ''
  onboardingOpen.value = true
}

export function openRetryOnboarding(projectId: string): void {
  const id = projectId.trim()
  if (!id) return
  onboardingMode.value = 'retry'
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
let probeGeneration = 0

/** Probes once per session; later route changes must not re-open the wizard. */
export function probeFirstInstall(): Promise<void> {
  if (!probe) probe = runProbe(probeGeneration)
  return probe
}

export function resetFirstInstallProbe(): void {
  probeGeneration++
  probe = null
  onboardingOpen.value = false
  onboardingMode.value = 'firstInstall'
  onboardingProjectId.value = ''
}

async function runProbe(generation: number): Promise<void> {
  try {
    const workspace = await useWorkspaceContext().ensureLoaded()
    if (generation !== probeGeneration || !workspace || isOnboardingSuppressed(workspace.id)) return
    const [workflows, agents] = await Promise.all([api.listWorkflows(), api.listAgents()])
    if (generation !== probeGeneration) return
    if (shouldAutoOpenOnboarding(workspace.id, workflows, agents)) {
      onboardingMode.value = 'firstInstall'
      onboardingProjectId.value = workspace.id
      onboardingOpen.value = true
    }
  } catch { /* Optional first-install prompt stays closed while offline. */ }
}
