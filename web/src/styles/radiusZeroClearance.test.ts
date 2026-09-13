// @vitest-environment node
/**
 * Radius zero-clearance gate (plan g4): no rounded-none leftovers in product
 * sources, plus hard contracts for home / switch / demo / analytics / login.
 */
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))
const srcRoot = join(here, '..')

const SCAN_EXTS = new Set(['.vue', '.ts', '.css'])

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === 'dist') continue
    const full = join(dir, name)
    const st = statSync(full)
    if (st.isDirectory()) {
      walk(full, out)
      continue
    }
    const lower = name.toLowerCase()
    // Skip *.test.ts / *.spec.ts; keep product .ts/.vue/.css
    if (/\.(test|spec)\.ts$/.test(lower)) continue
    const dot = lower.lastIndexOf('.')
    const ext = dot >= 0 ? lower.slice(dot) : ''
    if (!SCAN_EXTS.has(ext)) continue
    out.push(full)
  }
  return out
}

function read(relFromSrc: string): string {
  return readFileSync(join(srcRoot, relFromSrc), 'utf8')
}

describe('radius zero clearance', () => {
  it('has no rounded-none in web/src product .vue / non-test .ts / .css', () => {
    const hits: string[] = []
    for (const file of walk(srcRoot)) {
      const text = readFileSync(file, 'utf8')
      if (!text.includes('rounded-none')) continue
      // Line-level report for actionable failures
      text.split(/\r?\n/).forEach((line, i) => {
        if (line.includes('rounded-none')) {
          hits.push(`${relative(srcRoot, file)}:${i + 1}:${line.trim().slice(0, 160)}`)
        }
      })
    }
    expect(hits).toEqual([])
  })

  it('DashboardView home composer uses shell 16px and toolbar controls 8px', () => {
    const src = read('views/DashboardView.vue')
    expect(src).toMatch(/\.home-composer\s*\{[^}]*border-radius:\s*16px/s)
    expect(src).toMatch(/\.home-composer__plus\s*\{[^}]*border-radius:\s*8px/s)
    expect(src).toMatch(/\.home-composer__send\s*\{[^}]*border-radius:\s*8px/s)
    expect(src).not.toMatch(/home-composer__plus[^"]*rounded-none/)
    expect(src).not.toMatch(/thumb-class="rounded-none"/)
  })

  it('AppSwitch uses capsule rounded-full (not rounded-none)', () => {
    const src = read('components/ui/AppSwitch.vue')
    expect(src).toMatch(/role="switch"[^>]*class="[^"]*\brounded-full\b/)
    expect(src).not.toMatch(/rounded-none/)
  })

  it('ClarifyDemoFrame root uses card radius rounded-lg', () => {
    const src = read('components/run/ClarifyDemoFrame.vue')
    expect(src).toMatch(/class="[^"]*\brounded-lg\b[^"]*border/)
  })

  it('TokenAnalyticsView filter selects and clear use control rounded', () => {
    const src = read('views/TokenAnalyticsView.vue')
    expect(src).toMatch(
      /v-model="projectSel"\s+class="[^"]*\brounded\b[^"]*\bborder border-line\b/,
    )
    expect(src).toMatch(
      /v-model="modelSel"\s+class="[^"]*\brounded\b[^"]*\bborder border-line\b/,
    )
    expect(src).toMatch(
      /<button[^>]*class="[^"]*\brounded\b[^"]*"[^>]*@click="clearFilters"/,
    )
  })

  it('LoginView login card uses shell radius rounded-xl', () => {
    const src = read('views/LoginView.vue')
    expect(src).toMatch(/class="[^"]*\brounded-xl\b[^"]*border border-line bg-surface/)
  })

  it('HomePipelineSelect trigger 8px and panel 12px', () => {
    const src = read('components/dashboard/HomePipelineSelect.vue')
    expect(src).toMatch(/\.home-pipeline-select__trigger\s*\{[^}]*border-radius:\s*8px/s)
    expect(src).toMatch(/\.home-pipeline-select__panel\s*\{[^}]*border-radius:\s*12px/s)
    expect(src).toMatch(/\.home-pipeline-select__search\s*\{[^}]*border-radius:\s*8px/s)
  })

  it('HomePrioritySelect trigger 8px and panel 12px (plan g1.1)', () => {
    const src = read('components/dashboard/HomePrioritySelect.vue')
    expect(src).toMatch(/\.home-priority-select__trigger\s*\{[^}]*border-radius:\s*8px/s)
    expect(src).toMatch(/\.home-priority-select__panel\s*\{[^}]*border-radius:\s*12px/s)
    expect(src).toMatch(/height:\s*32px/)
  })

  function appButtonOpenTag(src: string, testid: string): string | undefined {
    return src.match(new RegExp(`<AppButton\\b[^>]*data-testid="${testid}"[^>]*>`))?.[0]
  }

  function handwrittenButtonOpenTag(src: string, testid: string): string | undefined {
    return src.match(new RegExp(`<button\\b[^>]*data-testid="${testid}"[^>]*>`))?.[0]
  }

  // plan g1.1 / g1.2 / g3.1 — AppButton outline sm expand; list still clips corners
  it('OutputResultCards enlarge is AppButton outline sm expand and list clips corners', () => {
    const src = read('components/run/OutputResultCards.vue')
    const enlarge = appButtonOpenTag(src, 'output-result-enlarge')
    expect(enlarge).toBeTruthy()
    expect(enlarge!).toMatch(/variant="outline"/)
    expect(enlarge!).toMatch(/size="sm"/)
    expect(enlarge!).toMatch(/icon="expand"/)
    expect(handwrittenButtonOpenTag(src, 'output-result-enlarge')).toBeUndefined()
    expect(enlarge!).not.toMatch(/\bbg-accent\b/)
    expect(enlarge!).not.toMatch(/\brounded-lg\b/)
    const list = src.match(/<div[\s\S]*?data-testid="output-result-list"[\s\S]*?>/)?.[0]
    expect(list).toBeTruthy()
    expect(list!).toMatch(/\brounded-lg\b/)
    expect(list!).toMatch(/\boverflow-hidden\b/)
  })

  // plan g2.1 / g2.2 / g3.1 — RunOutputPptModal footer + empty exits are AppButton
  it('RunOutputPptModal mark-read / close / empty exits are AppButton with required variants', () => {
    const src = read('components/shell/RunOutputPptModal.vue')
    const mark = appButtonOpenTag(src, 'run-output-mark-read')
    expect(mark).toBeTruthy()
    expect(mark!).toMatch(/variant="primary"/)
    expect(mark!).toMatch(/icon="check"/)
    expect(handwrittenButtonOpenTag(src, 'run-output-mark-read')).toBeUndefined()
    expect(mark!).not.toMatch(/\brounded-lg\b/)
    expect(mark!).not.toMatch(/hover:brightness-110/)

    const close = appButtonOpenTag(src, 'run-output-close')
    expect(close).toBeTruthy()
    expect(close!).toMatch(/variant="ghost"/)

    const openRun = appButtonOpenTag(src, 'run-output-empty-open-run')
    expect(openRun).toBeTruthy()
    expect(openRun!).toMatch(/variant="primary"/)
    expect(handwrittenButtonOpenTag(src, 'run-output-empty-open-run')).toBeUndefined()
    expect(openRun!).not.toMatch(/\brounded-lg\b/)
    expect(openRun!).not.toMatch(/hover:brightness-110/)

    const artifacts = appButtonOpenTag(src, 'run-output-empty-open-artifacts')
    expect(artifacts).toBeTruthy()
    expect(artifacts!).toMatch(/variant="outline"/)
    expect(handwrittenButtonOpenTag(src, 'run-output-empty-open-artifacts')).toBeUndefined()
  })

  // plan g2.1 / g3.1 — screenshot: mobile nav drawer 16px floating card
  it('AppShell mobile-nav-drawer is app-sidebar-card with inset (not flush inset-y-0)', () => {
    const src = read('components/shell/AppShell.vue')
    const aside = src.match(/<aside[\s\S]*?data-testid="mobile-nav-drawer"[\s\S]*?>/)?.[0]
    expect(aside).toBeTruthy()
    expect(aside!).toMatch(/\bapp-sidebar-card\b/)
    expect(aside!).not.toMatch(/\binset-y-0\b/)
    expect(aside!).toMatch(/\bleft-3\.5\b/)
    const close = src.match(/<button[\s\S]*?data-testid="mobile-nav-close"[\s\S]*?>/)?.[0]
    expect(close).toBeTruthy()
    expect(close!).toMatch(/\brounded-md\b/)
  })

  it('StatusMetrics compact strip uses control 8px rounded-md', () => {
    const src = read('components/shell/StatusMetrics.vue')
    // g1.1: compact is a container (not a single button); bar surface keeps 8px
    const compact = src.match(/<(?:div|button)[\s\S]*?data-testid="status-metrics-compact"[\s\S]*?>/)?.[0]
    expect(compact).toBeTruthy()
    expect(compact!).toMatch(/\brounded-md\b/)
  })

  it('AppDrawer sheet follows shell 16px via app-sidebar-card', () => {
    const src = read('components/ui/AppDrawer.vue')
    expect(src).toMatch(/\bapp-sidebar-card\b/)
  })

  it('ProjectAuditPanel scoped boxed controls use role radii (plan g3.2)', () => {
    const src = read('components/project/ProjectAuditPanel.vue')
    expect(src).toMatch(/\.btn\s*\{[^}]*border-radius:\s*8px/s)
    expect(src).toMatch(/\.search\s*\{[^}]*border-radius:\s*8px/s)
    expect(src).toMatch(/\.seg\s*\{[^}]*border-radius:\s*8px/s)
    expect(src).toMatch(/\.event-card\s*\{[^}]*border-radius:\s*12px/s)
  })

  it('RequirementDraftsPanel .seg track is control 8px (plan g3.2)', () => {
    const src = read('components/project/RequirementDraftsPanel.vue')
    expect(src).toMatch(/\.seg\s*\{[^}]*border-radius:\s*8px/s)
  })

  it('LangSelect ghost trigger uses control rounded-md (plan g3.2)', () => {
    const src = read('components/ui/LangSelect.vue')
    expect(src).toMatch(/variant === 'ghost'[\s\S]*?\brounded-md\b/)
  })

  it('同源 bg-accent solid keys carry rounded (plan g1.3)', () => {
    const upstream = read('components/run/UpstreamRequirementContext.vue')
    const enlarge = upstream.match(/<button[\s\S]*?data-testid="upstream-enlarge"[\s\S]*?>/g)
    expect(enlarge?.length).toBeGreaterThanOrEqual(1)
    for (const btn of enlarge!) {
      if (btn.includes('bg-accent')) expect(btn).toMatch(/\brounded-md\b/)
    }
    const share = read('components/run/GateShareLinkPanel.vue')
    for (const id of ['gate-share-create', 'gate-share-copy', 'gate-share-confirm-ok']) {
      const btn = share.match(new RegExp(`<button[\\s\\S]*?data-testid="${id}"[\\s\\S]*?>`))?.[0]
      expect(btn).toBeTruthy()
      expect(btn!).toMatch(/\brounded-md\b/)
    }
    const pub = read('views/PublicGateApprovalView.vue')
    for (const id of ['public-gate-upstream-enlarge', 'public-gate-upstream-retry']) {
      const btn = pub.match(new RegExp(`<button[\\s\\S]*?data-testid="${id}"[\\s\\S]*?>`))?.[0]
      expect(btn).toBeTruthy()
      expect(btn!).toMatch(/\brounded-md\b/)
    }
  })
})
