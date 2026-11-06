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

  it('uses the approval component responsive preview budget', () => {
    // plan g2.1: Inbox desktop fill-preview path only
    expect(src).toMatch(/:unified-preview-budget="true"/)
    // Desktop grid stretch (g1.1) — not items-start
    expect(src).toMatch(/items-stretch/)
    expect(src).not.toMatch(/grid-cols-\[320px_1fr\]/)
    // GateApproval applies this budget only on desktop.
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

  it('binds dynamic mode and finish on the shared ReviewComposer', () => {
    expect(src).not.toMatch(/mode="clarify"/)
    const modeBindings = src.match(/:mode="composerMode"/g) || []
    expect(modeBindings.length).toBe(1)
    const finishBindings = src.match(/@finish="onClarifyFinish"/g) || []
    expect(finishBindings.length).toBe(1)
  })

  it('plan g2.2: desktop and mobile share the same confirm leave path', () => {
    const finishBindings = src.match(/@finish="onClarifyFinish"/g) || []
    const resolveBindings = src.match(/@resolve="onResolve"/g) || []
    expect(finishBindings.length).toBe(1)
    expect(resolveBindings.length).toBe(1)
    // Leave pending on confirm click (plan g1.1 / g1.2); restore on wrap-up failure (g2.2).
    expect(src).toMatch(/Leave pending at confirm click/)
    expect(src).toMatch(/inline hand-off row/)
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
    expect(seedText.length).toBe(1)
    expect(seedImages.length).toBe(1)
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

describe('GatesInboxView app-preview stage', () => {
  it('mounts the shared artifact stage with app remoteKind and pick wiring in the shared ReviewShell', () => {
    expect(src).toMatch(/inboxAppPreviewActive/)
    expect(src).toMatch(/addClarifyAnnotation/)
    expect(src).toMatch(/mergeStagedAppPreviewPick/)
    const stages = src.match(/<ReactArtifactStage[\s\S]*?\/>/g) || []
    expect(stages.length).toBe(1)
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
  it('wires annotatable + node-id in the shared ReviewShell', () => {
    const stages = src.match(/<ReactArtifactStage[\s\S]*?\/>/g) || []
    expect(stages.length).toBe(1)
    for (const block of stages) {
      expect(block).toMatch(/:node-id="active\.nodeId"/)
      expect(block).toMatch(/:node="inboxStageNode"/)
      expect(block).toMatch(/:annotatable="clarifyInputActive"/)
      expect(block).toMatch(/:preview-artifact="activeClarify\?\.previewArtifact"/)
    }
  })

  it('mounts the artifact stage for every clarify session, including visual/research review', () => {
    expect(src).toMatch(/inboxStageRemoteKind/)
    expect(src).not.toMatch(/v-else-if="inboxReactActive"/)
    const desktop = src.slice(src.indexOf('<!-- One conversation'))
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
