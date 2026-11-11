import { computed, reactive } from 'vue'

interface AuthUser {
  username: string
  expiresAt: string
  isAdmin?: boolean
}

const state = reactive<{ user: AuthUser | null; ready: boolean }>({
  user: null,
  ready: false,
})

export function authRedirectPath(raw: string): string {
  if (!raw || !raw.startsWith('/') || raw.startsWith('//')) return '/'
  return raw
}

export function useAuth() {
  const user = computed(() => state.user)
  const isLoggedIn = computed(() => !!state.user)
  const ready = computed(() => state.ready)

  function setUser(u: AuthUser | null) {
    state.user = u
    state.ready = true
  }

  function clearUser() {
    state.user = null
    state.ready = true
  }

  return { user, isLoggedIn, ready, setUser, clearUser }
}

export function markAuthReady() {
  state.ready = true
}

export function getAuthState() {
  return state
}
