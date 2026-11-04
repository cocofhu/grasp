import type { Router } from 'vue-router'
import { authApi } from '@/lib/api/api'
import { authRedirectPath, getAuthState, markAuthReady, useAuth } from '@/lib/composables/useAuth'

/**
 * True when this document is framed and may render only preview routes.
 * The server marks workspace pages that configured origins may embed.
 */
export function isFramed(): boolean {
  if (typeof window === 'undefined' || window.self === window.top) return false
  return document.querySelector('meta[name="grasp-workspace-framing"][content="allowed"]') === null
}

/**
 * Auth navigation guard. Protected `/me` is non-blocking so the shell can paint;
 * unauthenticated redirect semantics stay the same. Login remains non-blocking.
 */
export function installAuthGuard(router: Router) {
  router.beforeEach(async (to) => {
    if (isFramed() && !to.meta.previewFrame) return false
    if (to.meta.public) {
      if (to.name === 'login') {
        const { user, setUser } = useAuth()
        if (user.value) {
          // Return a string so Vue Router parses ?query (object `{ path }` drops it).
          const redirect = typeof to.query.redirect === 'string' ? to.query.redirect : '/'
          return authRedirectPath(redirect)
        }
        // Do not await /me before painting login — brand LCP must not wait on auth RTT.
        void authApi
          .me()
          .then((me) => {
            setUser({ username: me.username, expiresAt: me.expires_at, isAdmin: !!me.is_admin })
            const redirect = typeof to.query.redirect === 'string' ? to.query.redirect : '/'
            return router.replace(authRedirectPath(redirect))
          })
          .catch(() => {
            markAuthReady()
          })
        return true
      }
      return true
    }

    const state = getAuthState()
    const { setUser } = useAuth()
    if (!state.user) {
      void authApi
        .me()
        .then((me) => {
          setUser({ username: me.username, expiresAt: me.expires_at, isAdmin: !!me.is_admin })
        })
        .catch(() => {
          markAuthReady()
          return router.replace({ path: '/login', query: { redirect: to.fullPath } })
        })
      return true
    }
    markAuthReady()
    return true
  })
}
