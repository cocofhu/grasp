export { apiState, blobContentUrl } from './httpCore'
export type * from './apiTypes'

import { authRedirectPath } from '../composables/useAuth'
import { req } from './httpCore'
import type { AuthLoginResponse, AuthMeResponse } from './apiTypes'
import { projectsClient } from './clients/projectsClient'
import { pmClient } from './clients/pmClient'
import { workflowsClient } from './clients/workflowsClient'
import { runsClient } from './clients/runsClient'
import { agentsClient } from './clients/agentsClient'
import { sandboxesClient } from './clients/sandboxesClient'
import { artifactsClient } from './clients/artifactsClient'
import { settingsClient } from './clients/settingsClient'
import { statsClient } from './clients/statsClient'
import { notificationsClient } from './clients/notificationsClient'

export const api = {
  ...projectsClient,
  ...pmClient,
  ...workflowsClient,
  ...runsClient,
  ...agentsClient,
  ...sandboxesClient,
  ...artifactsClient,
  ...settingsClient,
  ...statsClient,
  ...notificationsClient,
}

export const authApi = {
  login: (username: string, password: string, redirect = '/') =>
    req<AuthLoginResponse>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password, redirect: authRedirectPath(redirect) }),
    }),
  logout: () => req<{ status: string }>('/auth/logout', { method: 'POST' }),
  me: () => req<AuthMeResponse>('/auth/me'),
}
