import { computed, reactive } from 'vue'
import { api, type BrandSettings } from '@/lib/api/api'

export const DEFAULT_PRODUCT_NAME = 'Grasp'

export type ExecutionMode = 'worktree' | 'local' | 'sandbox'

const brand = reactive<BrandSettings>({
  product_name: '',
  home_subtitle: '',
})
const deployment = reactive<{
  mode: ExecutionMode
  installedTools: string[]
  hostGitUser: { name: string; email: string }
}>({
  mode: 'sandbox',
  installedTools: [],
  hostGitUser: { name: '', email: '' },
})
let request: Promise<void> | null = null
let loaded = false

export function setBrandSettings(value?: Partial<BrandSettings> | null) {
  brand.product_name = (value?.product_name || '').trim()
  brand.home_subtitle = value?.home_subtitle?.trim() || ''
}

export function setExecutionMode(mode: string | null | undefined) {
  deployment.mode = mode === 'local' ? 'local' : mode === 'worktree' ? 'worktree' : 'sandbox'
}

export function executionMode(): ExecutionMode {
  return deployment.mode
}

/** Worktree / local has no sandbox containers, so the sandbox pages are not reachable. */
export function hideSandboxPath(path: string): boolean {
  const mode = executionMode()
  return (mode === 'worktree' || mode === 'local') && (path === '/sandboxes' || path.startsWith('/sandboxes/'))
}

export async function loadBrandSettings() {
  if (loaded) return
  if (request) return request
  request = api.getSettings()
    .then((response) => {
      setBrandSettings(response.brand)
      setExecutionMode(response.execution_mode)
      deployment.installedTools = Array.isArray(response.installed_tools) ? response.installed_tools : []
      deployment.hostGitUser = {
        name: (response.host_git_user?.name || '').trim(),
        email: (response.host_git_user?.email || '').trim(),
      }
      loaded = true
    })
    .catch(() => {
      setBrandSettings(null)
    })
    .finally(() => {
      request = null
    })
  return request
}

export function resetBrandSettingsForTest() {
  loaded = false
  request = null
  setBrandSettings(null)
  setExecutionMode('sandbox')
  deployment.installedTools = []
  deployment.hostGitUser = { name: '', email: '' }
}

export function useBrandSettings() {
  const productName = computed(() => brand.product_name || DEFAULT_PRODUCT_NAME)
  const homeSubtitle = computed(() => brand.home_subtitle)
  return { productName, homeSubtitle, loadBrandSettings, setBrandSettings }
}

export function useExecutionMode() {
  const mode = computed(() => deployment.mode)
  const worktreeMode = computed(() => mode.value === 'worktree' || mode.value === 'local')
  const localMode = computed(() => mode.value === 'local')
  const installedTools = computed(() => deployment.installedTools)
  const hostGitUser = computed(() => deployment.hostGitUser)
  return { mode, worktreeMode, localMode, installedTools, hostGitUser }
}
