// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const viewsDir = dirname(fileURLToPath(import.meta.url))
const detailSrc = readFileSync(join(viewsDir, 'ProjectDetailView.vue'), 'utf8')
const panelSrc = readFileSync(join(viewsDir, '../components/project/ProjectAuditPanel.vue'), 'utf8')
const filterSrc = readFileSync(join(viewsDir, '../components/project/AuditFilterDropdown.vue'), 'utf8')
const runListSrc = readFileSync(join(viewsDir, 'RunListView.vue'), 'utf8')
const gatesSrc = readFileSync(join(viewsDir, 'GatesInboxView.vue'), 'utf8')
const artifactsSrc = readFileSync(join(viewsDir, 'ArtifactsView.vue'), 'utf8')

function cssBlock(src: string, selector: string): string {
  const start = src.indexOf(`${selector} {`)
  expect(start, `missing CSS block ${selector}`).toBeGreaterThanOrEqual(0)
  const end = src.indexOf('}', start)
  return src.slice(start, end + 1)
}

describe('ProjectDetailView audit tab fill-height chain (g1.1 / g5.1)', () => {
  it('audit tab uses min-h-0 flex-1 instead of 420px hard floor', () => {
    const auditBlock = detailSrc.slice(detailSrc.indexOf("tab === 'audit'"), detailSrc.indexOf("tab === 'meta'"))
    expect(auditBlock).toMatch(/class="flex min-h-0 flex-1 flex-col"/)
    expect(auditBlock).toMatch(/data-testid="project-audit-tab"/)
    expect(auditBlock).not.toMatch(/min-h-\[420px\]/)
  })
})

describe('ProjectAuditPanel fill layout + sticky header + tokens (g1.2–g4.2 / g5.1)', () => {
  it('panel consumes remaining height without 420/520 hard floors', () => {
    const panel = cssBlock(panelSrc, '.audit-panel')
    expect(panel).toMatch(/flex:\s*1/)
    expect(panel).toMatch(/min-height:\s*0/)
    expect(panel).toMatch(/overflow:\s*hidden/)
    expect(panel).not.toMatch(/min-height:\s*420px/)
    expect(panelSrc).not.toMatch(/max-height:\s*520px/)
    expect(panelSrc).not.toMatch(/min-height:\s*420px/)

    const wrap = cssBlock(panelSrc, '.table-wrap')
    expect(wrap).toMatch(/flex:\s*1/)
    expect(wrap).toMatch(/min-height:\s*0/)
    expect(wrap).toMatch(/overflow:\s*auto/)

    const cards = cssBlock(panelSrc, '.event-cards')
    expect(cards).toMatch(/flex:\s*1/)
    expect(cards).toMatch(/min-height:\s*0/)
    expect(cards).toMatch(/overflow:\s*auto/)

    expect(cssBlock(panelSrc, '.filters')).toMatch(/flex-shrink:\s*0/)
    expect(cssBlock(panelSrc, '.chips')).toMatch(/flex-shrink:\s*0/)
    expect(cssBlock(panelSrc, '.groups-wrap')).toMatch(/flex:\s*1/)
    expect(cssBlock(panelSrc, '.groups-wrap')).toMatch(/min-height:\s*0/)
    expect(cssBlock(panelSrc, '.groups-wrap')).toMatch(/overflow:\s*auto/)

    expect(panelSrc).toMatch(/<Pagination[\s\S]*?class="shrink-0"/)
    expect(panelSrc).toMatch(/v-if="mode === 'all' && !noRuns"/)
    expect(panelSrc).toMatch(/AUDIT_PAGE_SIZE_OPTIONS = \[5, 10, 20\]/)
    expect(panelSrc).toMatch(/data-layout="groups"/)
    expect(panelSrc).toMatch(/class="toolbar-end"/)
    expect(panelSrc).toMatch(/class="toolbar-stats"/)
    expect(panelSrc).toMatch(/project-audit-run-count/)
    expect(panelSrc).not.toMatch(/<h4>/)
    expect(panelSrc).not.toMatch(/class="meta/)
    expect(panelSrc).not.toMatch(/class="run-count/)
    expect(panelSrc).not.toMatch(/class="panel-hd/)
    expect(panelSrc).not.toMatch(/class="filters-actions"/)
  })

  it('desktop thead sticks with elevated bg and txt2 (not Demo txt3 / #fafafa)', () => {
    const th = cssBlock(panelSrc, 'thead th')
    expect(th).toMatch(/position:\s*sticky/)
    expect(th).toMatch(/top:\s*0/)
    expect(th).toMatch(/background:\s*rgb\(var\(--c-elevated\)\)/)
    expect(th).toMatch(/color:\s*rgb\(var\(--c-txt2\)\)/)
    expect(th).not.toMatch(/#fafafa/)
    expect(th).not.toMatch(/--c-txt3/)
    expect(cssBlock(panelSrc, 'table')).toMatch(/border-collapse:\s*separate/)
    expect(cssBlock(panelSrc, 'table')).toMatch(/border-spacing:\s*0/)
  })

  it('toolbar / stats / filter trigger use global --c-* tokens', () => {
    expect(cssBlock(panelSrc, '.filters')).toMatch(/border-bottom:\s*1px solid rgb\(var\(--c-line\)\)/)
    expect(cssBlock(panelSrc, '.search')).toMatch(/background:\s*rgb\(var\(--c-surface\)\)/)
    expect(cssBlock(panelSrc, '.search')).toMatch(/border:\s*1px solid rgb\(var\(--c-line\)\)/)
    expect(cssBlock(panelSrc, '.btn')).toMatch(/background:\s*rgb\(var\(--c-surface\)\)/)
    expect(cssBlock(panelSrc, '.chip')).toMatch(/background:\s*rgb\(var\(--c-elevated\)\)/)
    expect(cssBlock(panelSrc, '.audit-panel')).toMatch(/background:\s*rgb\(var\(--c-surface\)\)/)
    expect(cssBlock(panelSrc, '.audit-panel')).toMatch(/border:\s*1px solid rgb\(var\(--c-line\)\)/)
    expect(cssBlock(panelSrc, '.seg')).toMatch(/background:\s*rgb\(var\(--c-elevated\)\)/)
    expect(panelSrc).not.toMatch(/var\(--txt/)
    expect(panelSrc).not.toMatch(/var\(--card/)
    expect(panelSrc).not.toMatch(/var\(--line/)
    expect(panelSrc).not.toMatch(/var\(--accent/)

    const trig = cssBlock(filterSrc, '.audit-dd-trig')
    expect(trig).toMatch(/color:\s*rgb\(var\(--c-txt\)\)/)
    expect(trig).toMatch(/background:\s*rgb\(var\(--c-surface\)\)/)
    expect(trig).toMatch(/border:\s*1px solid rgb\(var\(--c-line\)\)/)
    expect(cssBlock(filterSrc, '.audit-dd-trig .k')).toMatch(/color:\s*rgb\(var\(--c-txt2\)\)/)
    expect(filterSrc).not.toMatch(/var\(--txt/)
    expect(filterSrc).not.toMatch(/var\(--card/)
    expect(filterSrc).not.toMatch(/var\(--line/)
    expect(filterSrc).not.toMatch(/var\(--accent/)
    expect(filterSrc).not.toMatch(/background:\s*#fff/)
    expect(filterSrc).not.toMatch(/#e4e4e7/)
    expect(filterSrc).not.toMatch(/#fafafa/)
  })

  it('loading / noRuns / empty / denied occupy remaining slot and center', () => {
    const slot = cssBlock(panelSrc, '.list-placeholder')
    expect(slot).toMatch(/flex:\s*1/)
    expect(slot).toMatch(/min-height:\s*0/)
    expect(slot).toMatch(/align-items:\s*center/)
    expect(slot).toMatch(/justify-content:\s*center/)
    expect(panelSrc).toMatch(/class="list-placeholder text-\[13px\] text-txt2"/)
    expect(panelSrc).toMatch(/class="empty list-placeholder"/)
    expect(panelSrc).toMatch(/data-testid="project-audit-empty"/)
    expect(panelSrc).toMatch(/data-testid="project-audit-empty-runs"/)
    expect(panelSrc).toMatch(/data-testid="project-audit-denied"/)
    expect(panelSrc).toMatch(/flex min-h-0 flex-1 flex-col items-center justify-center/)
  })

  it('keeps audit e2e testids and dual-mode slots', () => {
    for (const id of [
      'project-audit-panel',
      'project-audit-list',
      'project-audit-mode-run',
      'project-audit-mode-all',
      'project-audit-empty-runs',
    ]) {
      expect(panelSrc).toContain(`data-testid="${id}"`)
    }
    expect(panelSrc).toContain('summary-test-id="project-audit-pager-info"')
    expect(panelSrc).toContain('page-size-test-id="project-audit-page-size"')
  })
})

describe('shared Pagination consumers keep existing page interaction (g5.2)', () => {
  it('Run list still gates pager on total > PAGE_SIZE and binds page', () => {
    expect(runListSrc).toMatch(/import Pagination from '@\/components\/ui\/Pagination\.vue'/)
    expect(runListSrc).toMatch(/<Pagination v-if="total > PAGE_SIZE" v-model:page="page"/)
  })

  it('Gates inbox still gates pager on listTotal > PAGE_SIZE and binds listPage', () => {
    expect(gatesSrc).toMatch(/import Pagination from '@\/components\/ui\/Pagination\.vue'/)
    expect(gatesSrc).toMatch(/<Pagination v-if="listTotal > PAGE_SIZE" v-model:page="listPage"/)
  })

  it('Artifacts list still gates pager on pageTotal > PAGE_SIZE with shrink-0', () => {
    expect(artifactsSrc).toMatch(/import Pagination from '@\/components\/ui\/Pagination\.vue'/)
    expect(artifactsSrc).toMatch(/v-if="pageTotal > PAGE_SIZE"/)
    expect(artifactsSrc).toMatch(/v-model:page="page"/)
    expect(artifactsSrc).toMatch(/class="shrink-0"/)
  })

  it('Artifacts L2 listArtifacts uses groupBy=run (pageTotal is Run count)', () => {
    expect(artifactsSrc).toMatch(/groupBy:\s*['"]run['"]/)
    expect(artifactsSrc).toMatch(/const PAGE_SIZE = 20/)
    expect(artifactsSrc).toMatch(/:group-total="selectionInfo\?\.count/)
  })
})
