import { ref } from 'vue'

/** One-shot route flag. It is not a share credential. */
export const INBOX_SHARE_INTENT_QUERY = 'share'
export const INBOX_SHARE_INTENT_VALUE = '1'

/**
 * Published by the inbox page while a confirm is in flight.
 * False when that page is not mounted, so the sidebar does not invent a lock.
 */
export const knownInboxProcessingLock = ref(false)
