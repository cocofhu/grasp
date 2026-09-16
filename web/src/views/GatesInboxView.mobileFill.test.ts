// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const vueSrc = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'GatesInboxView.vue'), 'utf8')
const logicSrc = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../lib/inbox/useGatesInbox.ts'), 'utf8')
const src = `${vueSrc}\n${logicSrc}`

describe('GatesInboxView GateApproval isolation', () => {
  it('keeps fill-preview but does not enable mobile-fill-remaining', () => {
    expect(src).toMatch(/:fill-preview="true"/)
    expect(src).not.toMatch(/mobile-fill-remaining/)
    expect(src).not.toMatch(/mobileFillRemaining/)
  })

  it('desktop Inbox enables unified-preview-budget; mobile detail does not', () => {
    // plan g2.1: Inbox desktop fill-preview path only
    expect(src).toMatch(/:unified-preview-budget="true"/)
    // Desktop grid stretch (g1.1) — not items-start
    expect(src).toMatch(/items-stretch/)
    expect(src).not.toMatch(/grid-cols-\[320px_1fr\] items-start/)
    // Mobile GateApproval block omits unified budget (only one binding, on desktop).
    const unifiedBindings = src.match(/:unified-preview-budget="true"/g) || []
    expect(unifiedBindings.length).toBe(1)
  })

  it('clarify stage uses loading pane, product retry, and react stage for all ReAct sessions', () => {
    expect(src).toMatch(/ArtifactLoadingPane/)
    expect(src).toMatch(/ClarifyProductStage/)
    expect(src).toMatch(/ReactArtifactStage/)
    expect(src).not.toMatch(/inboxReactActive/)
    expect(src).toMatch(/retryActiveRun/)
    expect(src).toMatch(/inboxClarifyStageKind/)
    expect(src).toMatch(/:stage-kind="inboxClarifyStageKind"/)
  })
})

describe('GatesInboxView review/clarify composer mode', () => {
  it('derives reviewActive via inboxReviewMode helpers', () => {
    expect(src).toMatch(/resolveInboxReviewState/)
    expect(src).toMatch(/pickInboxClarifySession/)
    expect(src).toMatch(/inboxComposerMode/)
    expect(src).toMatch(/const reviewActive = computed/)
    expect(src).toMatch(/composerMode/)
  })

  it('binds dynamic mode and finish on both ReviewComposers', () => {
    expect(src).not.toMatch(/mode="clarify"/)
    const modeBindings = src.match(/:mode="composerMode"/g) || []
    expect(modeBindings.length).toBe(2)
    const finishBindings = src.match(/@finish="onClarifyFinish"/g) || []
    expect(finishBindings.length).toBe(2)
  })

  it('plan g2.2: desktop and mobile share the same confirm leave path', () => {
    const finishBindings = src.match(/@finish="onClarifyFinish"/g) || []
    const resolveBindings = src.match(/@resolve="onResolve"/g) || []
    expect(finishBindings.length).toBe(2)
    expect(resolveBindings.length).toBe(2)
    // Leave pending on confirm click (plan g1.1 / g1.2); restore on wrap-up failure (g2.2).
    expect(src).toMatch(/Leave pending at confirm click/)
    expect(src).toMatch(/play overlay \+ leave pending before wrap-up HTTP/)
    expect(src).toMatch(/restoreListItemLocally/)
  })

  it('selects by run/node query and seeds the first human bubble', () => {
    expect(src).toMatch(/findInboxItemForQuery/)
    expect(src).toMatch(/consumeHomeApproveHandoff/)
    expect(src).toMatch(/findInboxItemForHandoff/)
    expect(src).toMatch(/waitForQueryItem/)
    expect(src).toMatch(/incomingGhost/)
    expect(src).toMatch(/mergeIncomingGhost/)
    expect(src).toMatch(/homeApproveHandoffMatchesRun/)
    const seedText = src.match(/:seed-human-text="activeHomeSeed\?\.text"/g) || []
    const seedImages = src.match(/:seed-human-images="activeHomeSeed\?\.images \?\? \[\]"/g) || []
    expect(seedText.length).toBe(2)
    expect(seedImages.length).toBe(2)
    expect(src).toMatch(/showClarifyReviewShell/)
  })

  it('finish uses confirmFlowPrompt with force=true and success/error toasts', () => {
    expect(src).toMatch(/function onClarifyFinish/)
    expect(src).toMatch(/pages\.clarify\.confirmFlowPrompt/)
    expect(src).toMatch(/onClarifySend\(prompt, \[\], \[\], true\)/)
    expect(src).toMatch(/pages\.gatesInbox\.reviewFinished/)
    expect(src).toMatch(/pages\.gatesInbox\.reviewFinishedPending/)
    expect(src).toMatch(/pages\.gatesInbox\.reviewFinishFailed/)
  })
})

const EMPTY_CARD_CLASS =
  'card flex min-h-0 flex-1 flex-col items-center justify-center overflow-auto'

describe('GatesInboxView list-card-lift hover padding (plan g2.1)', () => {
  it('mobile and desktop list scroll areas keep py-0.5 so hover top border is not clipped', () => {
    expect(vueSrc).toMatch(/ref="listEl" class="scroll-area flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto py-0\.5"/)
    const scrollMatches = vueSrc.match(
      /scroll-area flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto py-0\.5/g,
    )
    expect(scrollMatches?.length).toBeGreaterThanOrEqual(2)
  })
})

describe('GatesInboxView empty inbox fill (plan g1 / g2.1 / g1.3)', () => {
  it('mobile + desktop empty wrappers both include flex-1 and vertical centering (g1.1 g1.2 g2.1)', () => {
    const escaped = EMPTY_CARD_CLASS.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const matches = src.match(new RegExp(`class="${escaped}"`, 'g')) || []
    expect(matches.length).toBe(2)
    expect(src).toMatch(/items-center justify-center overflow-auto/)
    expect(src).not.toMatch(/<div v-else class="card">/)
  })

  it('pipeline-filter empty and global empty share the same fill wrappers (g1.3)', () => {
    const blocks = [
      ...src.matchAll(
        /<div v-else(?:-if="!isMobile")? class="card flex min-h-0 flex-1 flex-col items-center justify-center overflow-auto">[\s\S]*?<\/div>/g,
      ),
    ]
    expect(blocks.length).toBe(2)
    for (const block of blocks) {
      expect(block[0]).toMatch(/listTotal \? t\('common\.empty\.noPendingGatesForPipeline'\)/)
      expect(block[0]).toMatch(/listTotal\s+\?\s+t\('common\.empty\.noPendingGatesPipelineDesc'\)/)
      expect(block[0]).toMatch(/t\('common\.empty\.noPendingGates'\)/)
      expect(block[0]).toMatch(/t\('common\.empty\.noPendingGatesDesc'\)/)
    }
  })

  it('empty wrappers keep .card skin tokens and do not zero-radius (g3.1)', () => {
    const matches = src.match(/class="card flex min-h-0 flex-1 flex-col items-center justify-center overflow-auto"/g) || []
    expect(matches.length).toBe(2)
    expect(src).not.toMatch(/inbox-empty.*rounded-none/)
    expect(src).not.toMatch(/empty-card.*!rounded-none/)
  })
})

describe('GatesInboxView list first-load tri-state (plan g1 / g2 / g3.2)', () => {
  it('listLoading starts true and template consumes listLoading + listLoadError', () => {
    expect(src).toMatch(/const listLoading = ref\(true\)/)
    expect(src).toMatch(/const listLoadError = ref<string \| null>\(null\)/)
    expect(src).toMatch(/showListSkeleton/)
    expect(src).toMatch(/showListError/)
    expect(src).toMatch(/listLoadError\.value = null/)
    expect(src).toMatch(/listLoadError\.value =/)
    expect(src).toMatch(/retryListLoad/)
    expect(src).toMatch(/t\('common\.asyncState\.loadFailedTitle'\)/)
    expect(src).toMatch(/t\('common\.asyncState\.loadFailedDesc'\)/)
    expect(src).toMatch(/data-testid="inbox-list-skeleton"/)
    expect(src).toMatch(/data-testid="inbox-list-failed"/)
    expect(src).toMatch(/data-testid="inbox-pending-card-skeleton"/)
    expect(src).toMatch(/retry-testid="inbox-list-retry"/)
    expect(src).toMatch(/t\('pages\.gatesInbox\.listLoadingHint'\)/)
    expect(src).toMatch(/aria-busy="listPanelBusy \? 'true' : 'false'"/)
    expect(src).toMatch(/SKELETON_CARDS = 6/)
    expect(src).toMatch(/h-9 w-9 shrink-0 bg-elevated animate-pulse/)
    expect(src).not.toMatch(/<AppSkeleton/)
    expect(src).not.toMatch(/InboxPendingCardSkeleton/)
  })

  it('branch order is loading∧empty → error∧empty → EmptyState; empty wrappers stay two', () => {
    const mobileList = src.slice(src.indexOf('<!-- Mobile list view -->'), src.indexOf('<!-- Mobile detail view -->'))
    expect(mobileList.indexOf('v-if="showListSkeleton"')).toBeGreaterThan(-1)
    expect(mobileList.indexOf('v-else-if="showListError"')).toBeGreaterThan(
      mobileList.indexOf('v-if="showListSkeleton"'),
    )
    expect(mobileList.indexOf('v-else class="card flex min-h-0 flex-1 flex-col items-center justify-center overflow-auto"')).toBeGreaterThan(
      mobileList.indexOf('v-else-if="showListError"'),
    )

    const desktop = src.slice(src.indexOf('<!-- Desktop three-zone'))
    // Content with rows first; empty-path order is skeleton → error → EmptyState.
    expect(desktop.indexOf('v-else-if="!isMobile && (listItems.length || confirmFlowDeskHold)"')).toBeGreaterThan(-1)
    expect(desktop.indexOf('v-else-if="!isMobile && showListSkeleton"')).toBeGreaterThan(
      desktop.indexOf('v-else-if="!isMobile && (listItems.length || confirmFlowDeskHold)"'),
    )
    expect(desktop.indexOf('v-else-if="!isMobile && showListError"')).toBeGreaterThan(
      desktop.indexOf('v-else-if="!isMobile && showListSkeleton"'),
    )
    expect(desktop.indexOf('v-else-if="!isMobile" class="card flex min-h-0 flex-1 flex-col items-center justify-center overflow-auto"')).toBeGreaterThan(
      desktop.indexOf('v-else-if="!isMobile && showListError"'),
    )
    expect(desktop).toMatch(/grid-cols-\[320px_1fr\] items-stretch/)
    expect(src).toMatch(/loadList\(\{ showLoading: listItems\.value\.length === 0 \}\)/)
    expect(src).toMatch(/void loadList\(\{ showLoading: true \}\)/)
  })
})

describe('GatesInboxView app_preview stage (g2.2)', () => {
  it('mounts the shared artifact stage with app remoteKind and pick wiring on both ReviewShell stages', () => {
    expect(src).toMatch(/inboxAppPreviewActive/)
    expect(src).toMatch(/addClarifyAnnotation/)
    expect(src).toMatch(/mergeStagedAppPreviewPick/)
    const stages = src.match(/<ReactArtifactStage[\s\S]*?\/>/g) || []
    expect(stages.length).toBe(2)
    for (const block of stages) {
      expect(block).toMatch(/:remote-kind="inboxRemoteKind"/)
      expect(block).not.toMatch(/:share-enabled=/)
      expect(block).not.toMatch(/@open-share=/)
      expect(block).toMatch(/:run="activeRun \|\| undefined"/)
    }
    expect(src).not.toMatch(/<AppPreviewPanel/)
  })
})

describe('GatesInboxView react artifact stage', () => {
  it('wires annotatable + node-id on both ReviewShell stages', () => {
    const stages = src.match(/<ReactArtifactStage[\s\S]*?\/>/g) || []
    expect(stages.length).toBe(2)
    for (const block of stages) {
      expect(block).toMatch(/:node-id="active\.nodeId"/)
      expect(block).toMatch(/:node-type="inboxStageNodeType"/)
      expect(block).toMatch(/:annotatable="clarifyInputActive"/)
      expect(block).toMatch(/:preview-artifact="activeClarify\?\.previewArtifact"/)
    }
  })

  it('mounts the artifact stage for every clarify session, including visual/research review', () => {
    expect(src).toMatch(/inboxStageRemoteKind/)
    expect(src).not.toMatch(/v-else-if="inboxReactActive"/)
    const desktop = src.slice(src.indexOf('<!-- Desktop three-zone'))
    const shell = desktop.slice(
      desktop.indexOf('<ReviewShell'),
      desktop.indexOf('</ReviewShell>') + '</ReviewShell>'.length,
    )
    expect(shell).toMatch(/showClarifyReviewShell/)
    expect(shell).toMatch(/<ReactArtifactStage/)
    expect(shell).not.toMatch(/stage-kind="panel"/)
    expect(src).toMatch(/GateApproval/)
  })
})
