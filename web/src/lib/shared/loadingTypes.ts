/** Shared loading mode: first paint vs background refresh vs button submit. */
export type LoadingMode = 'initial' | 'refresh' | 'submit'

/**
 * Refresh trigger intent.
 * Only `user_initiated` may show top bar / Toast / dim / aria-live.
 * `silent_poll` is peek/interval refresh: aria-busy only.
 */
export type RefreshIntent = 'user_initiated' | 'silent_poll'

/**
 * Terminal states after a load attempt. These must not impersonate each other:
 * empty ≠ error, cancelled ≠ error/empty.
 */
export type LoadingTerminalState = 'success' | 'empty' | 'error' | 'cancelled'

export const DEFAULT_SHOW_AFTER_MS: Record<LoadingMode, number> = {
  initial: 200,
  refresh: 200,
  submit: 0,
}

export const DEFAULT_MIN_VISIBLE_MS: Record<LoadingMode, number> = {
  initial: 300,
  refresh: 300,
  submit: 0,
}

/** Default GET timeout for the shared loading layer only (not global api). */
export const DEFAULT_LOADING_TIMEOUT_MS = 15_000
