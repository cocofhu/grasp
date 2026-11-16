import { api, type CodexDeviceLoginPoll } from '@/lib/api/api'
import { useDeviceLogin } from './deviceLogin'

export type CodexDeviceLoginState = 'idle' | 'starting' | 'waiting' | 'success' | 'denied' | 'expired' | 'not_enabled' | 'error'

export const CODEX_DEVICE_URL = 'https://auth.openai.com/codex/device'

/** Codex ChatGPT device-code sign-in; the device auth ID and tokens stay on the server. */
export function useCodexDeviceLogin(options: { onSuccess?: (result: CodexDeviceLoginPoll) => void } = {}) {
  return useDeviceLogin<CodexDeviceLoginPoll, CodexDeviceLoginState>({
    start: (body) => api.startCodexDeviceLogin(body),
    poll: (id) => api.pollCodexDeviceLogin(id),
    cancel: (id) => api.cancelCodexDeviceLogin(id),
    // The server asks OpenAI at most this often; polling faster only returns pending.
    minIntervalSeconds: 3,
    defaultVerificationUri: CODEX_DEVICE_URL,
    successStatus: 'success',
    terminalStatuses: ['denied', 'expired', 'not_enabled'],
    startFailureState: (fail) => fail.code === 'not_enabled' ? 'not_enabled' : 'error',
    onSuccess: options.onSuccess,
  })
}
