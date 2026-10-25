/**
 * What the conversation may draw for one visit's boot.
 * The checklist follows the boot record; this only decides whether it is on screen,
 * and whether this boot's handoff and agent replies wait for「连接 Agent」.
 */
export interface BootDisplayInput {
  /** Confirm window, including the done hold, is still painted. */
  windowVisible: boolean
  /**
   * The window opened over a boot that had not connected yet.
   * New handoff / agent text stays hidden until the window actually leaves,
   * even if the record goes live during the hold.
   */
  seal: boolean
  /** User confirmed this dialog (cancel leaves this false). */
  committed: boolean
  /** Next process is automatic. Null when this is not a post-confirm choice. */
  nextAuto: boolean | null
  hasBoot: boolean
  live: boolean
}

export interface BootDisplay {
  /** In-progress「正在启动 Agent」checklist. */
  showStarting: boolean
  /** Folded「环境已就绪」. */
  showReady: boolean
  /** Hide this boot's handoff bubble and agent replies. */
  holdMessages: boolean
}

export function resolveBootDisplay(input: BootDisplayInput): BootDisplay {
  const pending = input.hasBoot && !input.live
  const ready = input.hasBoot && input.live
  if (input.windowVisible) {
    return {
      showStarting: false,
      showReady: ready && !input.seal,
      holdMessages: input.seal || pending,
    }
  }
  // Confirmed into something that is not an automatic process: no checklist,
  // and do not blank the node's ordinary waiting transcript.
  if (input.committed && input.nextAuto === false) {
    return { showStarting: false, showReady: ready, holdMessages: false }
  }
  // Confirmed, but the next process is not known to be automatic yet.
  if (input.committed && input.nextAuto !== true) {
    return { showStarting: false, showReady: ready, holdMessages: pending }
  }
  return { showStarting: pending, showReady: ready, holdMessages: pending }
}
