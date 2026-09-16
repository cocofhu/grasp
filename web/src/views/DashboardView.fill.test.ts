// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const dir = dirname(fileURLToPath(import.meta.url))
const src = readFileSync(join(dir, 'DashboardView.vue'), 'utf8')
const particleBgSrc = readFileSync(
  join(dir, '../components/dashboard/HomeParticleMeshBackground.vue'),
  'utf8',
)
const pipelineSelectSrc = readFileSync(
  join(dir, '../components/dashboard/HomePipelineSelect.vue'),
  'utf8',
)
const shellSrc = readFileSync(join(dir, '../components/shell/AppShell.vue'), 'utf8')
const globalCss = readFileSync(join(dir, '../styles/global.css'), 'utf8')

describe('DashboardView home chat layout', () => {
  it('root uses flex fill without overflow-hidden (plan g1.4 mobile + desktop)', () => {
    expect(src).toMatch(/data-testid="dashboard-view"[^>]*class="[^"]*flex h-full min-h-0 flex-col/)
    expect(src).not.toMatch(/data-testid="dashboard-view"[^>]*overflow-hidden/)
    expect(src).not.toMatch(/calc\(100vh/)
    expect(src).toMatch(/home-shell__content[^>]*overflow-y-auto/)
  })

  it('centers composer, pipeline cards, and empty states', () => {
    expect(src).toMatch(/data-testid="home-composer"/)
    expect(src).toMatch(/data-testid="home-pipeline-cards"/)
    expect(src).toMatch(/data-testid="home-composer-input"/)
    expect(src).not.toMatch(/data-testid="home-no-project"/)
    expect(src).not.toMatch(/data-testid="dashboard-select-project"/)
    expect(src).toMatch(/data-testid="home-pipelines-empty"/)
    expect(src).toMatch(/data-testid="home-go-projects"/)
    expect(src).toMatch(/data-testid="home-new-workflow"/)
    expect(src).toMatch(/HomeCreateBaselineModal/)
    expect(src).toMatch(/@created="onBaselineCreated"/)
    expect(src).toMatch(/reloadAfterCreate/)
    expect(src).not.toMatch(/dashboard-kpi-/)
    expect(src).not.toMatch(/dashboard-board-empty/)
    expect(src).not.toMatch(/RunBoardColumn/)
  })

  // plan g1.1 — no purple stage atmosphere; particle mesh background instead
  it('does not include full-bleed purple stage layers', () => {
    expect(src).not.toMatch(/home-stage-bg/)
    expect(src).not.toMatch(/home-stage__wash/)
    expect(src).not.toMatch(/home-stage__grid/)
    expect(src).not.toMatch(/home-stage__glow/)
    expect(src).not.toMatch(/rgba\(91,\s*66,\s*180/)
    expect(src).toMatch(/HomeParticleMeshBackground/)
    expect(particleBgSrc).toMatch(/data-testid="home-particle-mesh-bg"/)
    expect(particleBgSrc).toMatch(/pointer-events:\s*none/)
  })

  // plan g1.2 / g1.3 — monospace Grasp, no gradient shimmer / staggered / serif accent
  it('uses local monospace brand without banned brand effects', () => {
    expect(src).toMatch(/data-testid="home-brand"/)
    expect(src).toMatch(/ui-monospace/)
    expect(src).toMatch(/home-brand__cursor/)
    expect(src).not.toMatch(/var\(--grad-logo\)/)
    expect(src).not.toMatch(/background-clip:\s*text/)
    expect(src).not.toMatch(/shimmer/)
    expect(src).not.toMatch(/stagger/)
    expect(src).not.toMatch(/serif/)
    expect(src).not.toMatch(/fonts\.googleapis|fonts\.gstatic|cdn\.jsdelivr/)
  })

  // plan g1 — shell composer 16px + control toolbar 8px (no right-angle Open Design)
  it('uses shell-radius Open Design composer with toolbar partition', () => {
    expect(src).toMatch(/home-composer/)
    expect(src).toMatch(/home-composer__toolbar/)
    expect(src).toMatch(/home-composer__plus/)
    expect(src).toMatch(/home-composer__send/)
    expect(src).toMatch(/data-testid="home-composer"/)
    expect(src).toMatch(/data-testid="home-composer-plus"/)
    expect(src).toMatch(/HomePipelineSelect/)
    expect(src).toMatch(/HomePrioritySelect/)
    expect(src).toMatch(/:initial-priority="launchPriority"/)
    expect(pipelineSelectSrc).toMatch(/data-testid="home-pipeline-select"/)
    expect(src).not.toMatch(/<select[^>]*home-pipeline-select/)
    expect(src).toMatch(/data-testid="home-composer-send"/)
    expect(src).toMatch(/<textarea/)
    expect(src).toMatch(/\.home-composer\s*\{[^}]*border-radius:\s*16px/s)
    expect(src).toMatch(/\.home-composer__plus\s*\{[^}]*border-radius:\s*8px/s)
    expect(src).toMatch(/\.home-composer__send\s*\{[^}]*border-radius:\s*8px/s)
  })

  // review — 无 subtitle；流水线卡片脱离全局 .card；圆角 Token 12px
  it('omits home-subtitle and uses rounded pipeline cards', () => {
    expect(src).not.toMatch(/data-testid="home-subtitle"/)
    expect(src).toMatch(/class="home-shell__card[^"]*border border-line/)
    expect(src).not.toMatch(/class="[^"]*\bcard\b[^"]*home-shell__card|class="home-shell__card[^"]*\bcard\b/)
    expect(src).toMatch(/\.home-shell__card\s*\{[^}]*border-radius:\s*12px/s)
    expect(src).toMatch(/data-testid="home-pipeline-card-name"/)
    expect(src).toMatch(/home-pipeline-card-project/)
    expect(src).toMatch(/p\.projectName/)
    expect(src).toMatch(/rounded bg-err[\s\S]{0,80}data-testid="home-attach-remove"/)
    expect(src).not.toMatch(/rounded-none bg-err[\s\S]{0,80}data-testid="home-attach-remove"/)
    expect(src).toMatch(/thumb-class="rounded"/)
    expect(src).not.toMatch(/thumb-class="rounded-none"/)
  })

  // plan g2 / g3 — no filter hint; caret opacity settle; placeholder typewriter
  // plan g1.1 / g2.2 — Ctrl/Meta+Enter send (no shiftKey-only Enter-to-send)
  it('omits filter hint and keeps caret settle + placeholder typewriter', () => {
    expect(src).not.toMatch(/data-testid="home-filter-hint"/)
    expect(src).not.toMatch(/filterHint/)
    expect(src).toMatch(/home-brand__cursor--gone/)
    expect(src).toMatch(/data-testid="home-composer-placeholder"/)
    expect(src).toMatch(/ctrlKey\s*\|\|\s*e\.metaKey|e\.metaKey\s*\|\|\s*e\.ctrlKey/)
    expect(src).toMatch(/sendShortcut/)
    expect(src).not.toMatch(/if \(e\.key !== 'Enter' \|\| e\.shiftKey\) return/)
    expect(src).toMatch(/prefers-reduced-motion/)
  })

  // plan g1 — pipeline rail hides scrollbar and adds edge nav aligned to page.html demo
  it('hides pipeline horizontal scrollbar and adds edge scroll arrows', () => {
    expect(src).toMatch(/data-testid="home-pipeline-rail-wrap"/)
    expect(src).toMatch(/data-testid="home-pipeline-scroll-prev"/)
    expect(src).toMatch(/data-testid="home-pipeline-scroll-next"/)
    expect(src).toMatch(/home-pipeline-rail/)
    expect(src).toMatch(/home-pipeline-rail--overflow/)
    expect(src).toMatch(/justify-content:\s*center/)
    expect(src).toMatch(/home-pipeline-rail--overflow[\s\S]*justify-content:\s*flex-start/)
    expect(src).not.toMatch(
      /data-testid="home-pipeline-cards"[^>]*justify-center/,
    )
    expect(src).toMatch(/scrollbar-width:\s*none/)
    expect(src).toMatch(/::-webkit-scrollbar/)
    expect(src).toMatch(/home-pipeline-nav/)
    expect(src).toMatch(/home-pipeline-fade/)
    expect(src).toMatch(/syncPipelineNav/)
    expect(src).toMatch(/scrollPipelineByDir/)
    expect(src).not.toMatch(
      /data-testid="home-pipeline-cards"[^>]*overflow-x-auto/,
    )
  })

  // plan g2.5 / g3.2 — scoped only; no external fonts
  it('keeps styles scoped to DashboardView and does not load external fonts', () => {
    expect(src).toMatch(/<style scoped>/)
    expect(src).not.toMatch(/@import|googleapis|gstatic|jsdelivr.*font/)
    expect(globalCss).toMatch(/:root|html/)
    expect(src).not.toContain(globalCss.slice(0, 40))
  })

  it('does not alter AppShell height chain', () => {
    expect(shellSrc).toMatch(/h-screen/)
    expect(shellSrc).toMatch(/min-h-0 flex-1/)
  })

  // plan g1.1 / g2.1 — mobile top spacing: ~4–4.5rem padding-top, keep flex-start
  it('uses moderate mobile top padding without vertical centering', () => {
    expect(src).toMatch(
      /@media \(max-width: 520px\)[\s\S]*\.home-shell__content[\s\S]*padding-top:\s*4\.25rem/,
    )
    expect(src).not.toMatch(
      /@media \(max-width: 520px\)[\s\S]*\.home-shell__content[\s\S]*padding-top:\s*2rem/,
    )
    expect(src).toMatch(
      /@media \(max-width: 520px\)[\s\S]*\.home-shell__content[\s\S]*justify-content:\s*flex-start/,
    )
    expect(src).not.toMatch(
      /@media \(max-width: 520px\)[\s\S]*\.home-shell__content[\s\S]*justify-content:\s*center/,
    )
    expect(src).toMatch(/justify-center/)
  })

  // plan g1.2 — keep overflow-hidden on composer; priority panel teleports instead
  it('keeps .home-composer overflow-hidden and does not raise other z-index layers', () => {
    expect(src).toMatch(/\.home-composer\s*\{[^}]*overflow:\s*hidden/s)
    expect(src).toMatch(/z-\[9999\]/)
    const prioritySrc = readFileSync(
      join(dir, '../components/dashboard/HomePrioritySelect.vue'),
      'utf8',
    )
    expect(prioritySrc).toMatch(/Teleport to="body"/)
    expect(prioritySrc).toMatch(/z-index:\s*60/)
    expect(prioritySrc).toMatch(/zIndex:\s*'60'/)
    expect(prioritySrc).toMatch(/addEventListener\('scroll', onScrollOrResize, true\)/)
  })

  // plan g1 / g2 — whole-rail enter: no loading copy; immediate 420ms group rise; reduced-motion
  it('reveals pipeline rail as one enter group without loading copy or post-success wait', () => {
    expect(src).not.toMatch(/data-testid="home-pipelines-loading"/)
    expect(src).not.toMatch(/v-else-if="loading"/)
    expect(src).toMatch(/data-testid="home-pipeline-enter"/)
    expect(src).toMatch(/home-pipeline-enter--ready/)
    expect(src).toMatch(/pipelineRailRevealed/)
    expect(src).toMatch(/prev === true && now === false && !loadError/)
    expect(src).toMatch(/animation:\s*home-pipeline-rail-enter 420ms cubic-bezier\(0\.16,\s*1,\s*0\.3,\s*1\)/)
    expect(src).not.toMatch(/nth-child\([^)]+\)[^{]*animation-delay|animation-delay:[^;]+nth-child/)
    expect(src).not.toMatch(/DEFAULT_MIN_VISIBLE|SHOW_AFTER|minVisible|min-visible|show-after/)
    expect(src).toMatch(/pipelineRailRevealed\.value = true/)
    expect(src).not.toMatch(/pipelineRailRevealed\.value = false/)
    expect(src).toMatch(
      /@media \(prefers-reduced-motion: reduce\)[\s\S]*\.home-pipeline-enter[\s\S]*animation:\s*none/,
    )
  })
})
