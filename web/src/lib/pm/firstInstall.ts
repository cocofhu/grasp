import { ref } from 'vue'
import { api } from '@/lib/api/api'
import { invalidateWorkflowCatalog } from '@/lib/api/workflowCatalogEvents'
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

/**
 * Bumped after a successful bootstrap. Open workflow surfaces refetch via
 * workflowCatalogRevision (advanced in markFirstInstallCompleted), not this stamp.
 */
export const firstInstallCompletedAt = ref(0)

export function openFirstInstall(): void {
  onboardingMode.value = 'firstInstall'
  onboardingOpen.value = true
}

export function openRetryOnboarding(): void {
  onboardingMode.value = 'retry'
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
  // First install and retry share this success event. Failure and cancel do not call it.
  invalidateWorkflowCatalog()
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
}

async function runProbe(generation: number): Promise<void> {
  try {
    const workspace = await useWorkspaceContext().ensureLoaded()
    if (generation !== probeGeneration || !workspace || isOnboardingSuppressed()) return
    const [workflows, agents] = await Promise.all([api.listWorkflows(), api.listAgents()])
    if (generation !== probeGeneration) return
    if (shouldAutoOpenOnboarding(workflows, agents)) {
      onboardingMode.value = 'firstInstall'
      onboardingOpen.value = true
    }
  } catch { /* Optional first-install prompt stays closed while offline. */ }
}
