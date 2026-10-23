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
const workflowSelectSrc = readFileSync(
  join(dir, '../components/dashboard/HomeWorkflowSelect.vue'),
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

  it('centers composer, workflow cards, and unpublished guidance without empty-state navigation', () => {
    expect(src).toMatch(/data-testid="home-composer"/)
    expect(src).toMatch(/data-testid="home-workflow-cards"/)
    expect(src).toMatch(/data-testid="home-composer-input"/)
    expect(src).toMatch(/data-testid="home-workflows-empty"/)
    expect(src).not.toMatch(/data-testid="home-go-workflows"/)
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

  // fill LOCKED — cropped yume + copy (no wordmark / logo-mark / old hint)
  it('uses fill hero with yume crop, copy, and no banned effects', () => {
    expect(src).toMatch(/data-testid="home-brand-block"/)
    expect(src).toMatch(/HomeYumeCharacter/)
    expect(src).toMatch(/data-testid="home-hero-copy"/)
    expect(src).toMatch(/home-hero-copy/)
    expect(src).not.toMatch(/class="em"/)
    expect(src).not.toContain('pages.dashboard.heroEm')
    expect(src).not.toContain('pages.dashboard.heroRest')
    expect(src).toContain('useBrandSettings')
    expect(src).toContain('homeSubtitle')
    expect(src).toContain("t('pages.dashboard.heroSubtitle')")
    expect(src).not.toContain('先说清')
    expect(src).not.toMatch(/data-testid="home-brand"/)
    expect(src).not.toMatch(/data-testid="home-brand-row"/)
    expect(src).not.toMatch(/data-testid="home-brand-copy"/)
    expect(src).not.toMatch(/data-testid="home-logo-mark"/)
    expect(src).not.toMatch(/data-testid="home-title"/)
    expect(src).not.toMatch(/class="home-hint"/)
    expect(src).not.toMatch(/logo-mark\.svg/)
    expect(src).not.toMatch(/HomeLogoEasterEgg|homeLogoJumpGame|home-logo-easter-egg/)
    expect(src).not.toMatch(/home-brand__cursor/)
    expect(src).not.toMatch(/ui-monospace/)
    expect(src).toMatch(/--yume-w:\s*240px/)
    expect(src).toMatch(/--yume-h:\s*200px/)
    expect(src).not.toMatch(/--yume-size/)
    expect(src).toMatch(/margin-top:\s*6px/)
    expect(src).toMatch(/\.home-composer-wrap\s*\{[^}]*margin-top:\s*22px/s)
    expect(src).toMatch(/justify-start/)
    expect(src).not.toMatch(/var\(--grad-logo\)/)
    expect(src).not.toMatch(/background-clip:\s*text/)
    expect(src).not.toMatch(/shimmer/)
    expect(src).not.toMatch(/stagger/)
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
    expect(src).toMatch(/HomeWorkflowSelect/)
    expect(src).toMatch(/HomePrioritySelect/)
    expect(src).toMatch(/:initial-priority="launchPriority"/)
    expect(workflowSelectSrc).toMatch(/data-testid="home-workflow-select"/)
    expect(src).not.toMatch(/<select[^>]*home-workflow-select/)
    expect(src).toMatch(/data-testid="home-composer-send"/)
    expect(src).toMatch(/<textarea/)
    expect(src).toMatch(/\.home-composer\s*\{[^}]*border-radius:\s*16px/s)
    expect(src).toMatch(/\.home-composer__plus\s*\{[^}]*border-radius:\s*8px/s)
    expect(src).toMatch(/\.home-composer__send\s*\{[^}]*border-radius:\s*8px/s)
  })

  // review — 无 subtitle；工作流卡片脱离全局 .card；圆角 Token 12px
  it('omits home-subtitle/hint and uses rounded workflow cards', () => {
    expect(src).not.toMatch(/data-testid="home-subtitle"/)
    expect(src).not.toMatch(/data-testid="home-title"/)
    expect(src).not.toMatch(/home-hint/)
    expect(src).toMatch(/class="home-shell__card[^"]*border border-line/)
    expect(src).not.toMatch(/class="[^"]*\bcard\b[^"]*home-shell__card|class="home-shell__card[^"]*\bcard\b/)
    expect(src).toMatch(/\.home-shell__card\s*\{[^}]*border-radius:\s*12px/s)
    expect(src).toMatch(/data-testid="home-workflow-card-name"/)
    expect(src).toMatch(/rounded pending-attach-remove[\s\S]{0,80}data-testid="home-attach-remove"/)
    expect(src).not.toMatch(/rounded-full[\s\S]{0,80}data-testid="home-attach-remove"/)
    expect(src).not.toMatch(/rounded-none[\s\S]{0,80}data-testid="home-attach-remove"/)
    expect(src).toMatch(/thumb-class="rounded"/)
    expect(src).not.toMatch(/thumb-class="rounded-none"/)
  })

  // plan g2 / g3 — no filter hint; caret opacity settle; placeholder typewriter
  // plan g1.1 / g2.2 — Ctrl/Meta+Enter send (no shiftKey-only Enter-to-send)
  it('omits filter hint and keeps placeholder typewriter', () => {
    expect(src).not.toMatch(/data-testid="home-filter-hint"/)
    expect(src).not.toMatch(/filterHint/)
    expect(src).not.toMatch(/home-brand__cursor/)
    expect(src).not.toMatch(/brandCursorGone/)
    expect(src).toMatch(/data-testid="home-composer-placeholder"/)
    expect(src).toMatch(/ctrlKey\s*\|\|\s*e\.metaKey|e\.metaKey\s*\|\|\s*e\.ctrlKey/)
    expect(src).toMatch(/sendShortcut/)
    expect(src).not.toMatch(/if \(e\.key !== 'Enter' \|\| e\.shiftKey\) return/)
    expect(src).toMatch(/prefers-reduced-motion/)
  })

  // plan g1 — workflow rail hides scrollbar and adds edge nav aligned to page.html demo
  it('hides workflow horizontal scrollbar and adds edge scroll arrows', () => {
    expect(src).toMatch(/data-testid="home-workflow-rail-wrap"/)
    expect(src).toMatch(/data-testid="home-workflow-scroll-prev"/)
    expect(src).toMatch(/data-testid="home-workflow-scroll-next"/)
    expect(src).toMatch(/home-workflow-rail/)
    expect(src).toMatch(/home-workflow-rail--overflow/)
    expect(src).toMatch(/justify-content:\s*center/)
    expect(src).toMatch(/home-workflow-rail--overflow[\s\S]*justify-content:\s*flex-start/)
    expect(src).not.toMatch(
      /data-testid="home-workflow-cards"[^>]*justify-center/,
    )
    expect(src).toMatch(/scrollbar-width:\s*none/)
    expect(src).toMatch(/::-webkit-scrollbar/)
    expect(src).toMatch(/home-workflow-nav/)
    expect(src).toMatch(/home-workflow-fade/)
    expect(src).toMatch(/syncWorkflowNav/)
    expect(src).toMatch(/scrollWorkflowByDir/)
    expect(src).not.toMatch(
      /data-testid="home-workflow-cards"[^>]*overflow-x-auto/,
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

  // fill LOCKED — top-down shell (justify-start); desktop inset basis is 120px
  // and shrinks (floor 48px) before the column scrolls. Mobile basis is 4.25rem.
  // Mobile floor is 36px so a float-high plus jump-high ear stays in the column.
  it('uses a shrinkable 120px desktop top inset and keeps moderate mobile padding', () => {
    expect(src).toMatch(/home-shell__content[^"]*justify-start/)
    expect(src).toMatch(/home-shell__content[^"]*overflow-x-hidden/)
    expect(src).toMatch(/\.home-shell__content::before\s*\{[^}]*flex:\s*0 1 120px/)
    expect(src).toMatch(/\.home-shell__content::before\s*\{[^}]*min-height:\s*48px/)
    expect(src).toMatch(/\.home-shell__content\s*\{[^}]*padding-bottom:\s*40px/)
    expect(src).not.toMatch(/\.home-shell__content\s*\{[^}]*padding-top:\s*120px/)
    expect(src).not.toMatch(/padding-top:\s*72px/)
    expect(src).not.toMatch(/padding-top:\s*40px/)
    expect(src).not.toMatch(/home-shell__content[^"]*\bpy-10\b/)
    expect(src).toMatch(
      /w-full min-w-0 flex-wrap gap-2 pr-1\.5 pt-1\.5"[\s\S]{0,40}data-testid="home-attach-chips"/,
    )
    expect(src).toMatch(
      /@media \(max-width: 520px\)[\s\S]*\.home-shell__content::before\s*\{[^}]*flex-basis:\s*4\.25rem[^}]*min-height:\s*36px/,
    )
    expect(src).not.toMatch(
      /@media \(max-width: 520px\)[\s\S]*padding-top:\s*2rem/,
    )
    expect(src).not.toMatch(
      /@media \(max-width: 520px\)[\s\S]*\.home-shell__content[\s\S]*justify-content:\s*center/,
    )
  })

  it('anchors and animates the workflow/Solo mode switch', () => {
    expect(src).toMatch(/home-shell__content--mode-locked/)
    expect(src).toMatch(/lockHomeModeTopInset/)
    expect(src).toMatch(/<Transition name="home-mode-controls" mode="out-in">/)
    expect(src).toMatch(/<Transition name="home-mode-tail" mode="out-in">/)
    expect(src).toMatch(/\.home-mode-controls-enter-active[\s\S]*transition:/)
    expect(src).toMatch(/\.home-mode-tail-enter-active[\s\S]*transition:/)
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
  it('reveals workflow rail as one enter group without loading copy or post-success wait', () => {
    expect(src).not.toMatch(/data-testid="home-workflows-loading"/)
    expect(src).not.toMatch(/v-else-if="loading"/)
    expect(src).toMatch(/data-testid="home-workflow-enter"/)
    expect(src).toMatch(/home-workflow-enter--ready/)
    expect(src).toMatch(/workflowRailRevealed/)
    expect(src).toMatch(/prev === true && now === false && !loadError/)
    expect(src).toMatch(/animation:\s*home-workflow-rail-enter 420ms cubic-bezier\(0\.16,\s*1,\s*0\.3,\s*1\)/)
    expect(src).not.toMatch(/nth-child\([^)]+\)[^{]*animation-delay|animation-delay:[^;]+nth-child/)
    expect(src).not.toMatch(/DEFAULT_MIN_VISIBLE|SHOW_AFTER|minVisible|min-visible|show-after/)
    expect(src).toMatch(/workflowRailRevealed\.value = true/)
    expect(src).not.toMatch(/workflowRailRevealed\.value = false/)
    expect(src).toMatch(
      /@media \(prefers-reduced-motion: reduce\)[\s\S]*\.home-workflow-enter[\s\S]*animation:\s*none/,
    )
  })

  it('loads slogan copy from locale files instead of a hardcoded Chinese line', () => {
    const zh = JSON.parse(
      readFileSync(join(dir, '../locales/zh-CN/pages.json'), 'utf8'),
    ) as { pages: { dashboard: { heroSubtitle: string } } }
    const en = JSON.parse(
      readFileSync(join(dir, '../locales/en/pages.json'), 'utf8'),
    ) as { pages: { dashboard: { heroSubtitle: string } } }
    expect(zh.pages.dashboard.heroSubtitle).toBe('先说清，再动手')
    expect(en.pages.dashboard.heroSubtitle).toBe('Clarify first, then act')
    expect(src).toMatch(/\.home-composer:focus-within\s*\{[^}]*rgb\(var\(--c-accent\)/)
    expect(src).toMatch(/0 0 0 3px rgb\(var\(--c-accent\) \/ 0\.07\)/)
    expect(src).toMatch(/\.home-composer__send:hover:not\(:disabled\)\s*\{[^}]*translateY\(-1px\)/)
    expect(src).toMatch(/prefers-reduced-motion: reduce[\s\S]*\.home-composer__send[\s\S]*transform:\s*none/)
    expect(src).toMatch(/\.home-composer\s*\{[^}]*background:\s*rgb\(var\(--c-surface\)\)/)
    const lightComposer = src.match(/html\.light\s+\.home-composer\s*\{([^}]+)\}/)?.[1] ?? ''
    expect(lightComposer).not.toMatch(/border-color/)
  })
})
