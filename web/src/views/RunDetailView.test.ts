// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))
const webSrc = join(here, '..')

function read(...parts: string[]) {
  return readFileSync(join(webSrc, ...parts), 'utf8')
}

/** Entry + panel shells + orchestration composables (Demo「入口只装配」拆分后的源码图). */
const viewSrc = read('views/RunDetailView.vue')
const viewModeSwitcherSrc = read('components/run/RunViewModeSwitcher.vue')
const gatePanelSrc = read('components/run/RunGatePanel.vue')
const logPanelSrc = read('components/run/RunLogPanel.vue')
const sandboxPanelSrc = read('components/run/RunSandboxPanel.vue')
const reviewPanelSrc = read('components/run/RunReviewPanel.vue')
const clarifyPanelSrc = read('components/run/RunClarifyPanel.vue')
const liveLogSrc = read('lib/run/useRunDetailLiveLog.ts')
const wsSrc = read('lib/run/useRunDetailWs.ts')
const selectionSrc = read('lib/run/useRunDetailSelection.ts')
const detailOrchestrationSrc = read('lib/run/useRunDetail.ts')

const src = [
  viewSrc,
  viewModeSwitcherSrc,
  detailOrchestrationSrc,
  gatePanelSrc,
  logPanelSrc,
  sandboxPanelSrc,
  reviewPanelSrc,
  clarifyPanelSrc,
  liveLogSrc,
  wsSrc,
  selectionSrc,
].join('\n')

/** View shell + extracted orchestration (post structure-sink). */
const viewOrchestrationSrc = viewSrc + '\n' + detailOrchestrationSrc + '\n' + viewModeSwitcherSrc

describe('RunDetailView delete run', () => {
  it('exposes delete button, disabled hint, and confirm modal wiring', () => {
    expect(src).toMatch(/data-testid="delete-run-btn"/)
    expect(src).toMatch(/data-testid="delete-run-hint"/)
    expect(src).toMatch(/data-testid="confirm-delete-run-btn"/)
    expect(src).toMatch(/canDeleteRun/)
    expect(src).toMatch(/api\.deleteRun/)
    expect(src).toMatch(/pages\.runDetail\.deleteWarning/)
    expect(src).not.toMatch(/pages\.runDetail\.deleteHintCancelled/)
    expect(src).toMatch(/pages\.runDetail\.deleteHintActive/)
    expect(src).toMatch(/s === 'completed' \|\| s === 'failed' \|\| s === 'cancelled'/)
    expect(src).toMatch(/router\.push\('\/runs' \+ qs\)/)
  })
})

describe('RunDetailView cancel run', () => {
  it('shows cancel for queued, running, and waiting_human with AppModal confirm', () => {
    expect(src).toMatch(/data-testid="cancel-run-btn"/)
    expect(src).toMatch(/data-testid="confirm-cancel-run-btn"/)
    expect(src).toMatch(/canCancelRun/)
    expect(src).toMatch(/s === 'queued' \|\| s === 'running' \|\| s === 'waiting_human'/)
    expect(src).toMatch(/openCancelConfirm/)
    expect(src).toMatch(/confirmCancelRun/)
    expect(src).toMatch(/api\.cancelRun/)
    expect(src).toMatch(/pages\.runDetail\.cancelTitle/)
    expect(src).toMatch(/pages\.runDetail\.cancelWarning/)
    expect(src).toMatch(/pages\.runDetail\.cancelSuccess/)
    expect(src).toMatch(/pages\.runDetail\.cancelErrorNotCancellable/)
  })

  it('confirms before POST and stays on detail after success', () => {
    // Button opens modal; only confirmCancelRun calls the API.
    expect(src).toMatch(/@click="openCancelConfirm"/)
    const openStart = src.indexOf('function openCancelConfirm()')
    const openEnd = src.indexOf('\nfunction closeCancelConfirm(', openStart)
    const openFn = src.slice(openStart, openEnd)
    expect(openFn).toContain("showCancelConfirm.value = true")
    expect(openFn).not.toContain('api.cancelRun')

    const confirmStart = src.indexOf('async function confirmCancelRun()')
    const confirmEnd = src.indexOf('\nconst canDeleteRun', confirmStart)
    const confirmFn = src.slice(confirmStart, confirmEnd)
    expect(confirmFn).toContain('api.cancelRun(runId.value)')
    expect(confirmFn).toContain('cancellingRun.value = true')
    expect(confirmFn).toContain("toast.success(t('pages.runDetail.cancelSuccess'))")
    expect(confirmFn).toContain('await loadRun(false)')
    expect(confirmFn).not.toContain('router.push')
  })
})

describe('RunDetailView GateApproval fill-preview', () => {
  it('always passes fill-preview=true (desktop content-fit), not isMobile', () => {
    expect(gatePanelSrc).toMatch(/<GateApproval[\s\S]*?:fill-preview="true"/)
    expect(src).not.toMatch(/:fill-preview="isMobile"/)
    expect(src).not.toMatch(/fit-visual-preview/)
    expect(src).not.toMatch(/fitVisualPreview/)
  })

  it('passes mobile-fill-remaining for Run-detail visual layout isolation', () => {
    expect(gatePanelSrc).toMatch(/<GateApproval[\s\S]*?:mobile-fill-remaining="true"/)
    expect(viewSrc).toMatch(/data-testid="run-detail-right-panel"/)
    expect(viewSrc).toMatch(/min-h-0 flex-1/)
  })
})

describe('RunDetailView priority badge popover', () => {
  it('does not render a default-expanded editor under the progress bar', () => {
    // Progress bar block must not be followed by an always-mounted priority editor card.
    expect(src).not.toMatch(
      /progressFrac \* 100 \+ '%'[\s\S]{0,400}data-testid="run-priority-editor"/,
    )
    // Editor lives in a Teleport popover gated by priorityPopoverOpen.
    expect(src).toMatch(/priorityPopoverOpen/)
    expect(src).toMatch(/<Teleport to="body">/)
    expect(src).toMatch(/data-testid="run-priority-editor"/)
    expect(src).toMatch(/data-testid="priority-popover-backdrop"/)
  })

  it('uses an editable badge trigger with aria switch attributes', () => {
    expect(src).toMatch(/data-testid="priority-badge"/)
    expect(src).toMatch(/aria-haspopup="dialog"/)
    expect(src).toMatch(/:aria-expanded="priorityPopoverOpen"/)
    expect(src).toMatch(/aria-controls="run-priority-editor"/)
    expect(src).toMatch(/priorityEditTrigger/)
    expect(src).toMatch(/priorityEditAria/)
    // Inner badge must not override the trigger hover title.
    expect(src).toMatch(/<PriorityBadge :priority="run\.priority" hide-title/)
  })

  it('shows chevron only for queued and supports Esc / backdrop close without cancel', () => {
    expect(src).toMatch(/showPriorityChevron/)
    expect(src).toMatch(/run\.value\.status === 'queued'/)
    expect(src).toMatch(/name="chevron-down"/)
    expect(src).toMatch(/priorityChevronClass/)
    expect(src).toMatch(/function closePriorityPopover\(/)
    expect(src).toMatch(/e\.key === 'Escape'/)
    expect(src).toMatch(/@click="closePriorityPopover\(true\)"/)
    // No explicit cancel button in the editor layer.
    expect(src).not.toMatch(/run-priority-editor[\s\S]{0,800}cancel/i)
  })

  it('moves focus into the dialog on open and returns it to the trigger on close', () => {
    expect(src).toMatch(/ref="priorityEditorRef"/)
    expect(src).toMatch(/tabindex="-1"/)
    expect(src).toMatch(/priorityEditorRef\.value\?\.focus\(\)/)
    expect(src).toMatch(/priorityBadgeRef\.value\?\.focus\(\)/)
  })

  it('keeps success tip independent from draft sync and auto-closes after save', () => {
    expect(src).toMatch(/function showPrioritySaved\(/)
    expect(src).toMatch(/priorityOkTimer = setTimeout/)
    expect(src).toMatch(/closePriorityPopover\(false\)/)
    // syncPriorityDraft must not clear the success tip (would flash it away on save).
    const syncFn = src.match(/function syncPriorityDraft\(\) \{[\s\S]*?\n\}/)
    expect(syncFn?.[0]).toBeTruthy()
    expect(syncFn?.[0]).not.toMatch(/priorityOk\.value = false/)
  })

  it('discards draft on close and blocks save when draft equals committed', () => {
    expect(src).toMatch(/function closePriorityPopover\(discard = true\)/)
    expect(src).toMatch(/if \(discard\) syncPriorityDraft\(\)/)
    expect(src).toMatch(/if \(priorityDraft\.value === committedPriority\.value\) return/)
    expect(src).toMatch(/:disabled="prioritySaving \|\| priorityDraft === committedPriority"/)
  })
})

describe('RunDetailView priority draft vs poll', () => {
  it('watches scalar priority/status and preserves dirty draft while editable', () => {
    // Must watch scalar getters — `() => [priority, status]` returns a new array
    // each evaluation and re-fires on every poll that replaces run.value.
    expect(src).toMatch(
      /watch\(\s*\[\s*\(\)\s*=>\s*run\.value\.priority\s*,\s*\(\)\s*=>\s*run\.value\.status\s*\]/,
    )
    expect(src).not.toMatch(
      /watch\(\s*\(\)\s*=>\s*\[\s*run\.value\.priority\s*,\s*run\.value\.status\s*\]/,
    )
    expect(src).toMatch(
      /if \(priorityEditable\.value && priorityDraft\.value !== saved\) return/,
    )
  })
})

describe('RunDetailView live-log boot timeout persistence', () => {
  it('keeps log/sandbox panels mounted with v-show and persists boot session', () => {
    // log ↔ sandbox must not use exclusive v-else-if on LiveLogPanel alone
    // (that unmounted the panel and reset the ~120s dwell clock).
    expect(src).toMatch(/nodeTab === 'log' \|\| nodeTab === 'sandbox'/)
    expect(src).toMatch(/v-show="nodeTab === 'log'"/)
    expect(src).toMatch(/v-show="nodeTab === 'sandbox'"/)
    expect(src).toMatch(/liveLogBootSessions/)
    expect(src).toMatch(/:boot-session="currentLiveLogBootSession"/)
    expect(src).toMatch(/@boot-session="onLiveLogBootSession"/)
    // Remount on node/execution switch so ratchet state cannot leak across sessions.
    expect(src).toMatch(/:key="`\$\{selected\}:\$\{selExecIdx\}`"/)
    expect(src).toMatch(/function goSandboxLogTab\(\)/)
    expect(src).toMatch(/nodeTab\.value = 'sandbox'/)
  })

  it('polls sandbox signals for boot while on log or sandbox tab', () => {
    expect(src).toMatch(/nodeTab\.value !== 'log' && nodeTab\.value !== 'sandbox'/)
  })

  it('uses RefreshStrip/HardLoadLayer dual-track and sandbox log gen+Abort', () => {
    expect(src).toMatch(/RefreshStrip/)
    expect(src).toMatch(/HardLoadLayer/)
    expect(src).toMatch(/sandboxLogGen/)
    expect(src).toMatch(/sandboxLogAbort/)
    expect(src).toMatch(/attemptGen !== sandboxLogGen/)
    expect(src).toMatch(/pages\.sandboxConsole\.logRefreshing/)
    expect(src).toMatch(/common\.buttons\.cancelling/)
    expect(src).toMatch(/cancellingRun/)
    expect(src).toMatch(/gateSubmitting/)
    expect(src).toMatch(/panelSwitching && !\(nodeTab === 'gate' && run\.gate\)/)
  })

  it('maps sandbox-log six states (empty / live / live-empty / archived / error)', () => {
    // Always persist API result (including found=false / empty content / error).
    expect(src).toMatch(/sbxLogs\[nodeId\] = \{/)
    expect(src).toMatch(/error: r\.error \|\| undefined/)
    expect(src).toMatch(/catch \(e\)/)
    expect(src).toMatch(/data-testid="sandbox-log-error"/)
    expect(src).toMatch(/data-testid="sandbox-log-live-empty"/)
    expect(src).toMatch(/sandboxLog\.errorTitle/)
    expect(src).toMatch(/sandboxLog\.liveEmpty/)
    // Empty content must not fall through to the "暂无沙箱日志" empty state when found+live.
    expect(src).toMatch(/sbxLog\?\.found && sbxLog\.live/)
  })
})

describe('RunDetailView ACP log rehydrate state machine', () => {
  it('wires rehydrate loading/error/retry separate from Boot and default tab', () => {
    expect(src).toMatch(/RehydrateOrchestrator/)
    expect(src).toMatch(/async function rehydrateNodeEvents/)
    expect(src).toMatch(/function retryRehydrate/)
    expect(src).toMatch(/:rehydrate-status="selRehydrateStatus"/)
    expect(src).toMatch(/@retry-rehydrate="retryRehydrate"/)
    // Rehydrate runs on enter (init + paint-then-work selection path); default tab logic unchanged.
    expect(src).toMatch(/void rehydrateNodeEvents\(selected\.value\)/)
    expect(src).toMatch(/async function runSelectionSideEffects/)
    expect(src).toMatch(/panelSwitching/)
    expect(src).toMatch(/afterNextPaint/)
    expect(src).toMatch(/data-testid="run-detail-panel-switching"/)
    expect(src).toMatch(/else if \(hasLog\.value\) nodeTab\.value = 'log'/)
    // Failed rehydrate must not auto-recover via the 2s poll.
    expect(src).toMatch(/if \(rh === 'error' \|\| rh === 'loading'\)/)
    // Timeout/retry abort in-flight REST via orchestrator (generation + AbortSignal).
    expect(src).toMatch(/signal: opts\?\.signal/)
    expect(src).toMatch(/force: !!opts\?\.force/)
  })

  it('keeps session snapshot cache across hard remount and WS does not clear error', () => {
    expect(src).toMatch(/from '@\/lib\/run\/liveLogSnapshotCache'/)
    expect(src).toMatch(/from '@\/lib\/run\/applyLiveWsAcpPage'/)
    expect(src).toMatch(/from '@\/lib\/run\/pendingAcpBuffer'/)
    expect(src).toMatch(/restoreEventPagesFromCache/)
    expect(src).toMatch(/clearLiveLogSnapshotsExceptRun/)
    expect(src).toMatch(/syncEventPageToCache/)
    expect(src).toMatch(/selMcpCalls/)
    // Hard remount restores cache after clearing reactive pages.
    expect(src).toMatch(/clearReactiveRecord\(eventPages\)[\s\S]*restoreEventPagesFromCache\(id\)/)
    // Empty WS frames must not rewrite timeline / sync empty cache.
    expect(src).toMatch(/applyLiveWsAcpPage\(eventPages\[m\.nodeId\], wsEvents\)|applyLiveWsAcpPage\(eventPages\[nodeId\], wsEvents\)/)
    expect(src).toMatch(/if \(mergedPage\)/)
    // WS merge must not promote rehydrate / clear soft warn.
    expect(src).toMatch(/WS only merges into the snapshot/)
    expect(src).not.toMatch(/rehydrateByNode\[m\.nodeId\]\s*=\s*['"]ready['"]/)
    // Seed-then-live: busy restore seeds dialogue ACP; hard-load buffers when chat gone.
    expect(src).toMatch(/seedDialogueAcpAfterRestore/)
    expect(src).toMatch(/applyOrBufferDialogueAcp/)
    expect(src).toMatch(/pendingDialogueAcp/)
    expect(src).toMatch(/projectDialogueAfterLoad/)
    expect(src).toMatch(/deliverOrBufferDialogueAcp/)
    // Real delivery: applyAcpEvents returning false must buffer (ReviewComposer nest).
    expect(src).toMatch(/if \(!reviewChatRef\.value\?\.applyAcpEvents\) return false/)
    // Gate apply must honor boolean return (not constant true) — g1.4.
    expect(src).toMatch(/return gateApprovalRef\.value\.applyAcpEvents\(evs\) !== false/)
    // Busy seed retry + WS reconnect re-seed (g3/g4).
    expect(src).toMatch(/runBusySeedRetry/)
    expect(src).toMatch(/createWsReconnectController/)
    expect(src).toMatch(/fromReconnect/)
    expect(src).toMatch(/projectDialogueAfterLoad/)
  })
})


describe('RunDetailView desktop outer sash layout (all node tabs)', () => {
  it('enables outer sash on desktop for every node tab with bound width', () => {
    expect(src).toMatch(/from '@\/lib\/inbox\/reviewLayoutBudget'/)
    expect(src).toMatch(/initOuterSashFromMemory/)
    expect(src).toMatch(
      /desktopOuterSashLayout = computed\(\(\) => !isMobile\.value\)/,
    )
    expect(src).toMatch(/:style="reviewRightPanelStyle"/)
    expect(src).toMatch(/:style="leftPaneStyle"/)
    expect(src).toMatch(/data-testid="run-detail-outer-sash"/)
    // Desktop right panel uses bound width — no fixed md:w-[520px] fallback.
    expect(viewSrc).not.toMatch(/md:w-\[520px\]/)
    expect(viewOrchestrationSrc).toMatch(/v-if="desktopOuterSashLayout"/)
    expect(viewOrchestrationSrc).toMatch(/readSharedOuterSashMem/)
    expect(viewOrchestrationSrc).toMatch(/writeSharedOuterSashMem/)
    expect(viewOrchestrationSrc).toMatch(/initOuterSashFromMemory/)
    expect(viewOrchestrationSrc).toMatch(/outerSashBooting/)
  })

  it('sync-inits width from memory and hides split until apply (no default flash)', () => {
    expect(detailOrchestrationSrc).toMatch(/initOuterSashFromMemory\(initialOuterWs\)/)
    expect(detailOrchestrationSrc).toMatch(/outerRightPx = ref\(initialOuterLayout\.width\)/)
    expect(detailOrchestrationSrc).not.toMatch(/reviewRightPanelCssWidth/)
    expect(viewSrc).toMatch(/run-detail-outer-sash-booting/)
    expect(viewSrc).toMatch(/outerSashBooting && desktopOuterSashLayout/)
    expect(detailOrchestrationSrc).toMatch(/revealOuterSashLayout/)
    expect(detailOrchestrationSrc).toMatch(
      /watch\(\s*\(\) => runLoading\.value,/,
    )
  })

  it('keeps in-memory outer width across tab switches (no per-tab re-read)', () => {
    expect(viewOrchestrationSrc).toMatch(
      /watch\(\s*\(\) => desktopOuterSashLayout\.value,/,
    )
    expect(viewOrchestrationSrc).not.toMatch(
      /watch\(\s*\(\) => \[desktopOuterSashLayout\.value, nodeTab\.value\]/,
    )
  })

  it('passes sidebar-width=REVIEW_SIDEBAR only on Run Detail review ReviewShell', () => {
    expect(reviewPanelSrc).toMatch(
      /<ReviewShell[\s\S]*?:sidebar-width="REVIEW_SIDEBAR"[\s\S]*?:storage-key="REVIEW_SHELL_WIDTH_KEY_REVIEW"/,
    )
    // Default ReviewShell width stays 400 elsewhere — this file must not change the default.
    expect(src).not.toMatch(/sidebarWidth:\s*300/)
  })

  it('does not bind REVIEW_CANVAS_MIN as drag canvas minWidth; canvas may go to 0', () => {
    expect(viewOrchestrationSrc).not.toMatch(/minWidth: `\$\{REVIEW_CANVAS_MIN\}px`/)
    expect(viewOrchestrationSrc).not.toMatch(/canvasPaneStyle/)
    expect(viewOrchestrationSrc).toMatch(/minWidth: '0px'/)
    expect(viewOrchestrationSrc).toMatch(/width: '0px'/)
    expect(viewOrchestrationSrc).toMatch(/flexBasis: '0px'/)
    expect(viewOrchestrationSrc).toMatch(/overflow: 'hidden'/)
  })

  it('outer sash uses pointer capture, isolated dragging class, clamp, snap, dblclick reset', () => {
    expect(viewOrchestrationSrc).toMatch(/setPointerCapture/)
    expect(viewOrchestrationSrc).toMatch(/e\.stopPropagation\(\)/)
    expect(viewOrchestrationSrc).toMatch(/run-detail-outer-sash-dragging/)
    expect(viewOrchestrationSrc).not.toMatch(/review-shell-sash-dragging/)
    expect(viewOrchestrationSrc).toMatch(/clampOuterRight/)
    expect(viewOrchestrationSrc).toMatch(/reviewDefaultRightPx/)
    expect(viewOrchestrationSrc).toMatch(/onOuterSashDblClick/)
    expect(viewOrchestrationSrc).toMatch(/role="separator"/)
    expect(viewOrchestrationSrc).toMatch(/aria-orientation="vertical"/)
    expect(viewOrchestrationSrc).toMatch(/readSharedOuterSashMem/)
    expect(viewOrchestrationSrc).toMatch(/fullOpen/)
  })

  it('keeps view-mode switcher inside left pane on default split and above node tabs when full-open (g1)', () => {
    // plan g1.1 / g1.3: default split mounts switcher inside left canvas/timeline with overflow clip
    expect(viewSrc).toMatch(/data-testid="run-detail-left-pane"/)
    expect(viewSrc).toMatch(/overflow-hidden border-r border-line md:block/)
    // timeline relies on leftPaneStyle.overflow=hidden (desktop); class must stay free of Tailwind `hidden`
    expect(viewSrc).toMatch(/data-testid="run-timeline-pane"/)
    expect(viewOrchestrationSrc).toMatch(/overflow: 'hidden'/)
    expect(viewSrc).toMatch(/RunViewModeSwitcher/)
    // plan g1.2: full-open mounts switcher in right panel document flow above AppTabs
    expect(viewSrc).toMatch(/v-if="!isMobile && outerFullOpen"/)
    expect(viewSrc).toMatch(/data-testid="run-detail-right-panel"[\s\S]*?run-detail-view-mode-switcher[\s\S]*?AppTabs/)
    // Must not float over the whole split with left-5 (old overlay that covered node tabs)
    expect(viewSrc).not.toMatch(/md:left-5 md:z-\[1\]/)
    expect(viewSrc).not.toMatch(/md:absolute md:top-3 md:border-0 md:bg-transparent md:p-0/)
  })

  it('renders only one view-mode switcher mount at a time (g1.3)', () => {
    // Mutually exclusive branches: mobile | stats | left(!fullOpen) | right(fullOpen)
    expect(viewSrc).toMatch(/v-if="isMobile"[\s\S]*?run-detail-view-mode-switcher/)
    expect(viewSrc).toMatch(/v-else-if="viewMode === 'stats'"[\s\S]*?run-detail-view-mode-switcher/)
    expect(viewSrc).toMatch(/v-if="!outerFullOpen"[\s\S]*?run-detail-view-mode-switcher/)
    expect(viewSrc).toMatch(/v-if="!isMobile && outerFullOpen"[\s\S]*?run-detail-view-mode-switcher/)
  })

  it('hides outer sash on mobile (no horizontal drag)', () => {
    expect(viewSrc).toMatch(/v-if="desktopOuterSashLayout"/)
    expect(viewSrc).toMatch(/hidden shrink-0 cursor-col-resize bg-line md:block/)
  })
})

describe('RunDetailView mobile timeline view contract', () => {
  it('does not hide the main-path timeline pane behind hidden md:block', () => {
    expect(src).toMatch(/data-testid="run-timeline-pane"/)
    // Timeline pane itself must not use desktop-only hidden.
    const pane = src.match(/data-testid="run-timeline-pane"[\s\S]{0,280}class="[^"]*"/)
    expect(pane?.[0]).toBeTruthy()
    expect(pane?.[0]).not.toMatch(/\bhidden\b/)
  })

  it('uses mobile single-panel mutual exclusion instead of dual-stack min-h', () => {
    // Page-level tabs + back bar for ≤767 list-detail.
    expect(src).toMatch(/data-testid="mobile-main-panel-tabs"/)
    expect(src).toMatch(/data-testid="mobile-panel-timeline"/)
    expect(src).toMatch(/data-testid="mobile-panel-detail"/)
    expect(src).toMatch(/data-testid="mobile-back-to-timeline"/)
    expect(src).toMatch(/mobileMainPanel/)
    expect(src).toMatch(/v-show="!isMobile \|\| mobileMainPanel === 'timeline'"/)
    expect(src).toMatch(/ensure-visible-token="timelineScrollToken"/)
    // Timeline main path must not keep the dual rigid min-heights.
    const timelinePane = src.match(
      /data-testid="run-timeline-pane"[\s\S]{0,320}class="[^"]*"/,
    )
    expect(timelinePane?.[0]).toBeTruthy()
    expect(timelinePane?.[0]).not.toMatch(/min-h-\[240px\]/)
    // Detail panel mobile path uses min-h-0 flex-1, not stacked min-h-[320px].
    expect(src).not.toMatch(
      /viewMode === 'timeline' &&\s*!\(isMobile && run\.status === 'waiting_human' && nodeTab === 'gate'\)/,
    )
    const detailPanelBlock = src.slice(src.indexOf('data-testid="run-detail-right-panel"'))
    const detailClass = detailPanelBlock.match(/class="[^"]*"[\s\S]{0,200}:class="\[[\s\S]*?\]"/)
    expect(detailClass?.[0] || detailPanelBlock.slice(0, 500)).not.toMatch(/min-h-\[320px\] md:min-h-0/)
  })

  it('hides canvas entry on mobile and silently normalizes canvas → timeline', () => {
    expect(src).toMatch(/v-if="!isMobile"/)
    expect(src).toMatch(/data-testid="view-mode-canvas"/)
    expect(src).toMatch(/if \(mobile && viewMode\.value === 'canvas'\) viewMode\.value = 'timeline'/)
    expect(src).toMatch(
      /viewMode = ref<RunViewMode>\(isMobile\.value \? 'timeline' : 'canvas'\)/,
    )
  })

  it('delegates default node pick to pickDefaultTimelineNodeId', () => {
    expect(src).toMatch(/pickDefaultTimelineNodeId/)
    expect(src).toMatch(/const defaultNode = computed\(\(\) => pickDefaultTimelineNodeId\(run\.value\)\)/)
    // Must not keep the old Object.values().find first-running shortcut alone.
    expect(src).not.toMatch(
      /Object\.values\(run\.value\.nodeRuns\)\s*\n\s*return \(entries\.find/,
    )
  })

  it('defaults waiting_human→detail on mobile; live/deep-link completed→detail', () => {
    expect(src).toMatch(/mobileMainPanel\.value = 'timeline'/)
    expect(src).toMatch(/mobileMainPanel\.value = 'detail'/)
    expect(src).toMatch(/outputFocusLock/)
    expect(src).toMatch(/applyOutputDeepLinkFocus/)
    expect(src).toMatch(/applyDetailArtifactsDeepLink/)
    expect(src).toMatch(/detail === 'artifacts'|detail'\) !== 'artifacts'/)
    expect(src).toMatch(/resolveOutputFocusNodeId/)
    expect(src).toMatch(/queryParam\('tab'\) === 'output'/)
    expect(src).toMatch(/if \(isMobile\.value\) mobileMainPanel\.value = 'detail'/)
    expect(src).toMatch(/prev !== 'running' && prev !== 'waiting_human'/)
    expect(src).toMatch(/if \(runLoading\.value\) return/)
    expect(src).toMatch(/hasProduct\.value && nodeCompleted\.value\) nodeTab\.value = 'product'/)
    expect(src).toMatch(/:mobile-fill-remaining="true"/)
  })

  it('does not toast on run completed (g5.3)', () => {
    expect(src).not.toMatch(/运行已完成/)
    expect(src).not.toMatch(/toast\.(success|info|warn)\([^)]*complet/i)
  })
})

describe('RunDetailView load failure split', () => {
  it('classifies not_found vs network_or_server and gates retry', () => {
    expect(src).toMatch(/type RunLoadErrorKind = 'not_found' \| 'network_or_server'/)
    expect(src).toMatch(/isClearlyInvalidRunRouteId/)
    expect(src).toMatch(/classifyRunLoadError/)
    expect(src).toMatch(/pages\.runDetail\.notFoundTitle/)
    expect(src).toMatch(/pages\.runDetail\.notFoundDesc/)
    expect(src).toMatch(/pages\.runDetail\.retryUnavailable/)
    expect(src).toMatch(/data-testid="run-retry-unavailable"/)
    expect(src).toMatch(/data-testid="run-retry"/)
    // Must not keep the old single network-leaning copy as the only failure path.
    expect(src).not.toMatch(/请检查网络或确认 Run 是否存在/)
  })
})

describe('RunDetailView clarify session narrow update (g3.2)', () => {
  it('skips react/status/trace/artifact_edit/focus loadRun while clarify session busy', () => {
    expect(src).toMatch(/function isClarifySessionBusy\(/)
    expect(src).toMatch(/function patchRunChrome\(/)
    // All four event types share one busy gate — chrome patch, not hard load.
    expect(src).toMatch(
      /m\.type === 'trace'[\s\S]*?m\.type === 'status'[\s\S]*?m\.type === 'react'[\s\S]*?m\.type === 'artifact_edit'[\s\S]*?if \(isClarifySessionBusy\(\)/,
    )
    expect(src).toMatch(/if \(m\.type === 'artifact_edit'\) refreshArtifactPreview/)
    expect(src).toMatch(/void patchRunChrome/)
    expect(src).toMatch(/loadRun\(false, true\)/)
    expect(src).toMatch(/liveBusy\[m\.nodeId\] = true/)
    expect(src).toMatch(/m\.event === 'turn_begin'/)
  })
})

describe('RunDetailView react artifact stage', () => {
  it('keeps clarify annotations on the panel and stage annotatable while input is active', () => {
    expect(viewSrc).toMatch(/v-model:annotations="clarifyAnnotations"/)
    expect(clarifyPanelSrc).toMatch(/:annotatable="inputActive"/)
    expect(clarifyPanelSrc).toMatch(/:node-id="nodeId"/)
    expect(clarifyPanelSrc).toMatch(/:node="graphNode"/)
    expect(clarifyPanelSrc).toMatch(/annotate-enabled/)
    expect(clarifyPanelSrc).toMatch(/v-model:annotations="annotations"/)
    expect(clarifyPanelSrc).toMatch(/ReactArtifactStage/)
    expect(clarifyPanelSrc).toMatch(/@pick="onRemotePick"/)
  })

  it('uses the same artifact stage for review, not StructuredProductPanel', () => {
    expect(reviewPanelSrc).toMatch(/ReactArtifactStage/)
    expect(reviewPanelSrc).toMatch(/:remote-kind="remoteKind"/)
    expect(reviewPanelSrc).toMatch(/:run="run"/)
    expect(reviewPanelSrc).toMatch(/:node-id="node.id"/)
    expect(reviewPanelSrc).toMatch(/:node="node"/)
    expect(reviewPanelSrc).not.toMatch(/StructuredProductPanel/)
    expect(reviewPanelSrc).not.toMatch(/AppPreviewPanel/)
    expect(gatePanelSrc).toMatch(/GateApproval/)
  })
})

describe('RunDetailView entry assembly (g4)', () => {
  it('assembles major panels via Run*Panel shells and useRunDetail* orchestration', () => {
    expect(viewSrc).toMatch(/RunGatePanel/)
    expect(viewSrc).toMatch(/RunOutputPanel/)
    expect(viewSrc).toMatch(/RunLogPanel/)
    expect(viewSrc).toMatch(/RunSandboxPanel/)
    expect(viewOrchestrationSrc).toMatch(/useRunDetailLiveLog/)
    expect(viewOrchestrationSrc).toMatch(/useRunDetailWs/)
    expect(viewOrchestrationSrc).toMatch(/useRunDetailSelection/)
    expect(viewSrc).toMatch(/<AppTabs :tabs="nodeTabs" v-model="nodeTab"/)
  })

  it('keeps Demo main-path nodeTab switching for gate/log/sandbox/output', () => {
    expect(selectionSrc).toMatch(/nodeTabs/)
    expect(selectionSrc).toMatch(/nodeTab/)
    expect(viewSrc).toMatch(/nodeTab === 'gate'/)
    expect(viewSrc).toMatch(/nodeTab === 'log'/)
    expect(viewSrc).toMatch(/nodeTab === 'sandbox'/)
    expect(viewSrc).toMatch(/RunOutputPanel/)
    expect(gatePanelSrc).toMatch(/GateApproval/)
  })
})
