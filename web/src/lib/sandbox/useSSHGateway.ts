import { readonly, ref } from 'vue'
import { api } from '@/lib/api/api'
import type { SSHGatewayInfo } from '@/lib/api/apiTypes'

const info = ref<SSHGatewayInfo | null>(null)
let inflight: Promise<SSHGatewayInfo> | null = null

/** Shared SSH gateway status. A failed lookup reads as disabled. */
export function useSSHGateway() {
  function load(force = false): Promise<SSHGatewayInfo> {
    if (inflight) return inflight
    if (info.value && !force) return Promise.resolve(info.value)
    inflight = api.sshGateway()
      .catch((): SSHGatewayInfo => ({ enabled: false }))
      .then((value) => {
        info.value = value
        return value
      })
      .finally(() => {
        inflight = null
      })
    return inflight
  }
  return { info: readonly(info), load }
}

/** Test hook: forget the cached status. */
export function resetSSHGatewayForTest() {
  info.value = null
  inflight = null
}
