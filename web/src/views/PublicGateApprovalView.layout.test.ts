// @vitest-environment node
/**
 * PublicGateApproval viewport lock + sidebar/chat scroll chain (g1 / g2 / g3.1).
 * Source-structure assertions prevent the long-chat whole-page scroll regression.
 */
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const viewsDir = dirname(fileURLToPath(import.meta.url))
const viewSrc = readFileSync(join(viewsDir, 'PublicGateApprovalView.vue'), 'utf8')
const shellSrc = readFileSync(join(viewsDir, '../components/run/ReviewShell.vue'), 'utf8')
const chatSrc = readFileSync(join(viewsDir, '../components/run/ClarifyChat.vue'), 'utf8')
const composerSrc = readFileSync(join(viewsDir, '../components/run/ReviewComposer.vue'), 'utf8')

function publicGateRootClass(): string {
  const rootIdx = viewSrc.indexOf('data-testid="public-gate-root"')
  expect(rootIdx, 'missing public-gate-root').toBeGreaterThanOrEqual(0)
  const block = viewSrc.slice(Math.max(0, rootIdx - 160), rootIdx + 80)
  const cls = block.match(/class="([^"]+)"/)
  expect(cls?.[1], 'missing public-gate-root class').toBeTruthy()
  return cls![1]
}

describe('PublicGateApproval root viewport lock (g1.1)', () => {
  it('locks root to viewport height with overflow-hidden (not min-h-screen alone)', () => {
    const cls = publicGateRootClass()
    expect(cls).toMatch(/\bh-screen\b|\bh-dvh\b|\bh-\[100dvh\]\b/)
    expect(cls).toMatch(/\boverflow-hidden\b/)
    expect(cls).toMatch(/\bflex\b/)
    expect(cls).toMatch(/\bflex-col\b/)
    expect(cls).not.toMatch(/\bmin-h-screen\b/)
    expect(viewSrc).not.toMatch(/class="flex min-h-screen flex-col/)
  })
})

describe('PublicGateApproval height chain to clarify-scroller (g1.2 / g2)', () => {
  it('workbench and sidebar keep min-h-0 flex chain', () => {
    expect(viewSrc).toMatch(/data-testid="public-gate-workbench"/)
    expect(viewSrc).toMatch(/class="flex min-h-0 flex-1 flex-col"[^>]*data-testid="public-gate-workbench"/)
    // ref= may sit between tag and class (shellRef for confirm ceremony).
    expect(viewSrc).toMatch(/<ReviewShell[\s\S]*?class="min-h-0 flex-1"/)
    expect(viewSrc).toMatch(/data-testid="public-gate-sidebar"/)
    expect(viewSrc).toMatch(/class="flex h-full min-h-0 flex-col"[^>]*data-testid="public-gate-sidebar"/)
    // Chat host wrapper (ClarifyChat is multi-root; fallthrough class is ignored)
    expect(viewSrc).toMatch(/data-testid="public-gate-chat-host"/)
    expect(viewSrc).toMatch(/class="flex min-h-0 flex-1 flex-col"[^>]*data-testid="public-gate-chat-host"/)
  })

  it('confirm stays in the bounded ReviewComposer instead of the page footer', () => {
    const footerIdx = viewSrc.indexOf('data-testid="public-gate-footer"')
    expect(footerIdx).toBeGreaterThanOrEqual(0)
    const footerOpen = viewSrc.slice(Math.max(0, footerIdx - 200), footerIdx + 40)
    expect(footerOpen).toMatch(/\bshrink-0\b/)
    expect(viewSrc).toMatch(/<ReviewComposer/)
    expect(viewSrc).toMatch(/@finish="onComposerFinish"/)
    expect(viewSrc).not.toMatch(/data-testid="public-gate-confirm"/)
    expect(viewSrc).not.toMatch(/\bhide-finish\b/)
    expect(composerSrc).toMatch(/data-testid="review-composer-shell"/)
    expect(chatSrc).toMatch(/data-testid="clarify-confirm-flow"/)
  })

  it('stage keeps bounded height with internal scroll for tall content (g2.3)', () => {
    expect(viewSrc).toMatch(/data-testid="public-gate-stage"/)
    expect(viewSrc).toMatch(/class="flex min-h-0 flex-1 flex-col"[^>]*data-testid="public-gate-stage"/)
    expect(viewSrc).toMatch(/class="min-h-0 flex-1 overflow-hidden"/)
    const structuredIdx = viewSrc.indexOf('data-testid="public-gate-structured"')
    expect(structuredIdx).toBeGreaterThanOrEqual(0)
    const structuredBlock = viewSrc.slice(Math.max(0, structuredIdx - 160), structuredIdx + 40)
    expect(structuredBlock).toMatch(/\boverflow-y-auto\b/)
  })

  it('ReviewShell sidebar/stage preserve min-h-0 overflow chain (g1.2 / g2.2)', () => {
    // relative + overflow-hidden required so ConfirmFlowOverlay clips to desk (not app chrome).
    expect(shellSrc).toMatch(/class="relative flex h-full min-h-0 overflow-hidden"/)
    expect(shellSrc).toMatch(/data-testid="review-shell-stage"/)
    expect(shellSrc).toMatch(/flex min-h-0 flex-1 flex-col overflow-hidden/)
    expect(shellSrc).toMatch(/data-testid="review-shell-sidebar"/)
    expect(shellSrc).toMatch(/class="flex min-h-0 flex-1 flex-col overflow-hidden"/)
  })

  it('ClarifyChat clarify-scroller remains the overflow-y-auto list (g2.2 / g3.1)', () => {
    const scrollerIdx = chatSrc.indexOf('data-testid="clarify-scroller"')
    expect(scrollerIdx).toBeGreaterThanOrEqual(0)
    const scrollerBlock = chatSrc.slice(Math.max(0, scrollerIdx - 160), scrollerIdx + 40)
    expect(scrollerBlock).toMatch(/\boverflow-y-auto\b/)
    expect(chatSrc).toMatch(/border-t border-line p-3/)
  })
})

describe('PublicGateApproval react artifact stage', () => {
  it('uses ReactArtifactStage for ReAct review/clarify/app_preview workbench', () => {
    expect(viewSrc).toMatch(/const usePublicArtifactStage = computed\(\(\) => isReview\.value\)/)
    expect(viewSrc).toMatch(/data-testid="public-gate-react-stage"/)
    expect(viewSrc).toMatch(/v-if="usePublicArtifactStage"/)
    expect(viewSrc).toMatch(/:annotatable="inspectable"/)
    expect(viewSrc).toMatch(/:preview-artifact="publicPreviewName"/)
    expect(viewSrc).toMatch(/loadPublicArtifacts/)
    expect(viewSrc).toMatch(/publicGateApi\.artifacts/)
    expect(viewSrc).toMatch(
      /:remote-kind="productKind === 'app_preview' \|\| appPreviewPorts.length \? 'public' : 'off'"/,
    )
    expect(viewSrc).toMatch(/data-testid="public-gate-stage"/)
  })
})
