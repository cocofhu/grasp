import { api, type GitHubDeviceLoginPoll } from '@/lib/api/api'
import { useDeviceLogin } from './deviceLogin'

export type GitHubDeviceLoginState = 'idle' | 'starting' | 'waiting' | 'authorized' | 'denied' | 'expired' | 'error'

/** GitHub's Octicon mark (16×16 viewBox). */
export const GITHUB_MARK = 'M8 0a8 8 0 0 0-2.53 15.59c.4.07.55-.17.55-.38v-1.33c-2.23.48-2.7-1.07-2.7-1.07-.36-.92-.89-1.17-.89-1.17-.73-.5.05-.49.05-.49.8.06 1.23.83 1.23.83.72 1.22 1.87.87 2.33.66.07-.52.28-.87.5-1.07-1.78-.2-3.65-.89-3.65-3.95 0-.87.31-1.59.83-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.22 2.2.82a7.6 7.6 0 0 1 4 0c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.28.82 2.15 0 3.07-1.87 3.75-3.66 3.95.29.25.54.73.54 1.48v2.2c0 .21.15.46.55.38A8 8 0 0 0 8 0Z'

/** GitHub device-flow sign-in; the device code and token stay on the server. */
export function useGitHubDeviceLogin(options: { onAuthorized?: (result: GitHubDeviceLoginPoll) => void } = {}) {
  return useDeviceLogin<GitHubDeviceLoginPoll, GitHubDeviceLoginState>({
    start: (body) => api.startGitHubDeviceLogin(body),
    poll: (id) => api.pollGitHubDeviceLogin(id),
    cancel: (id) => api.cancelGitHubDeviceLogin(id),
    // GitHub asks clients to wait at least this long between token polls.
    minIntervalSeconds: 5,
    defaultVerificationUri: 'https://github.com/login/device',
    successStatus: 'authorized',
    terminalStatuses: ['denied', 'expired'],
    onSuccess: options.onAuthorized,
  })
}
