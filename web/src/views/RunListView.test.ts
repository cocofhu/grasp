// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'RunListView.vue'), 'utf8')

describe('RunListView click UX (loading / nav / prefetch)', () => {
  it('keeps table-loading opacity without pointer-events:none blocking the list', () => {
    const blockStart = src.indexOf('.table-loading {')
    expect(blockStart).toBeGreaterThanOrEqual(0)
    const blockEnd = src.indexOf('}', blockStart)
    const block = src.slice(blockStart, blockEnd + 1)
    expect(block).toMatch(/opacity:\s*0\.55/)
    expect(block).not.toMatch(/pointer-events:\s*none/)
    // Entire source must not reintroduce list-level pointer-events:none under table-loading
    expect(src).not.toMatch(/\.table-loading\s*\{[^}]*pointer-events:\s*none/)
  })

  it('uses RouterLink / to for run rows and cards', () => {
    expect(src).toMatch(/<RouterLink/)
    expect(src).toMatch(/:to="runHref\(r\.id\)"/)
    expect(src).toMatch(/function runHref\(id: string\)/)
    expect(src).toMatch(/return '\/runs\/' \+ id/)
    // Both mobile cards and desktop rows use custom + navigate (no bare <a> wrapping ops)
    expect(src.match(/custom\s*\n\s*v-slot="\{ navigate, href \}"/g)?.length).toBeGreaterThanOrEqual(2)
    expect(src.match(/@click="navigate"/g)?.length).toBeGreaterThanOrEqual(2)
    expect(src).toMatch(/role="link"/)
  })

  it('prefetches RunDetail chunk on hover / touchstart / pointerdown', () => {
    expect(src).toMatch(/function prefetchRunDetail\(/)
    expect(src).toMatch(/import\('@\/views\/RunDetailView\.vue'\)/)
    expect(src).toMatch(/@mouseenter="prefetchRunDetail"/)
    expect(src).toMatch(/@touchstart\.passive="prefetchRunDetail"/)
    expect(src).toMatch(/@pointerdown="prefetchRunDetail"/)
  })

  it('skips poll assignment when list fingerprint is unchanged', () => {
    expect(src).toMatch(/function runsListUnchanged\(/)
    expect(src).toMatch(/function runListFingerprint\(/)
    expect(src).toMatch(/if \(!runsListUnchanged\(/)
    expect(src).toMatch(/setInterval\(load, 3000\)/)
  })
})

describe('RunListView cancel run', () => {
  it('exposes inline cancel for queued, running, and waiting_human', () => {
    expect(src).toMatch(/data-testid="cancel-run-btn"/)
    expect(src).toMatch(/data-testid="confirm-cancel-run-btn"/)
    expect(src).toMatch(/function canCancelRun\(/)
    expect(src).toMatch(/r\.status === 'queued' \|\| r\.status === 'running' \|\| r\.status === 'waiting_human'/)
    expect(src).toMatch(/common\.table\.actions/)
    expect(src).toMatch(/v-if="canCancelRun\(r\)"/)
  })

  it('stops row navigation when clicking cancel and confirms before POST', () => {
    expect(src).toMatch(/@click\.stop/)
    expect(src).toMatch(/openCancelConfirm\(r\)/)
    const openStart = src.indexOf('function openCancelConfirm(r: Run)')
    const openEnd = src.indexOf('\nfunction closeCancelConfirm(', openStart)
    const openFn = src.slice(openStart, openEnd)
    expect(openFn).toContain('cancelTarget.value = r')
    expect(openFn).not.toContain('api.cancelRun')

    const confirmStart = src.indexOf('async function confirmCancelRun()')
    const confirmEnd = src.indexOf('\nfunction openDeleteConfirm(', confirmStart)
    const confirmFn = src.slice(confirmStart, confirmEnd > 0 ? confirmEnd : undefined)
    expect(confirmFn).toContain('api.cancelRun(target.id)')
    expect(confirmFn).toContain("toast.success(t('pages.runDetail.cancelSuccess'))")
    expect(confirmFn).toContain('await load()')
  })

  it('keeps cancel confirm copy distinct from delete and supports mobile cards', () => {
    expect(src).toMatch(/pages\.runDetail\.cancelWarning/)
    expect(src).toMatch(/pages\.runList\.cancelConfirm/)
    // Mobile cards: RouterLink custom + role=link (not bare <a> / role=button + openRun)
    expect(src).toMatch(/<!-- Mobile card list -->[\s\S]*?<RouterLink/)
    const mobileBlock = src.slice(src.indexOf('<!-- Mobile card list -->'), src.indexOf('<!-- Desktop table -->'))
    expect(mobileBlock).toMatch(/custom/)
    expect(mobileBlock).toMatch(/v-slot="\{ navigate, href \}"/)
    expect(mobileBlock).toMatch(/role="link"/)
    expect(mobileBlock).not.toMatch(/<RouterLink[^>]*\n[^>]*class="/)
    expect(mobileBlock).toMatch(/@click\.stop @keydown\.stop/)
  })
})

describe('RunListView delete run and ops column', () => {
  it('adds canDeleteRun mutually exclusive with canCancelRun', () => {
    expect(src).toMatch(/function canDeleteRun\(/)
    expect(src).toMatch(
      /r\.status === 'completed' \|\| r\.status === 'failed' \|\| r\.status === 'cancelled'/,
    )
    expect(src).toMatch(/v-else-if="canDeleteRun\(r\)"/)
    expect(src).toMatch(/data-testid="delete-run-btn"/)
    expect(src).toMatch(/data-testid="run-ops-placeholder"/)
    // cancel and delete sets must not overlap in predicates
    const cancelFn = src.slice(
      src.indexOf('function canCancelRun(r: Run)'),
      src.indexOf('function canDeleteRun(r: Run)'),
    )
    const deleteFn = src.slice(
      src.indexOf('function canDeleteRun(r: Run)'),
      src.indexOf('function prefetchRunDetail('),
    )
    expect(cancelFn).not.toMatch(/completed|failed/)
    expect(deleteFn).not.toMatch(/queued|running|waiting_human/)
  })

  it('renders text danger buttons without icons and grey dash placeholder', () => {
    // List ops cancel/delete must be text-only; confirm modals may still use icons.
    const cancelBtnBlocks = [...src.matchAll(/data-testid="cancel-run-btn"[\s\S]*?<\/AppButton>/g)].map((m) => m[0])
    const deleteBtnBlocks = [...src.matchAll(/data-testid="delete-run-btn"[\s\S]*?<\/AppButton>/g)].map((m) => m[0])
    expect(cancelBtnBlocks.length).toBeGreaterThanOrEqual(2)
    expect(deleteBtnBlocks.length).toBeGreaterThanOrEqual(2)
    for (const block of [...cancelBtnBlocks, ...deleteBtnBlocks]) {
      expect(block).not.toMatch(/\bicon=/)
    }
    expect(src).toMatch(/data-testid="run-ops-placeholder"/)
    expect(src).toContain('—')
    expect(src).toMatch(/common\.buttons\.delete/)
  })

  it('opens delete confirm before DELETE and stays on list after success', () => {
    expect(src).toMatch(/openDeleteConfirm\(r\)/)
    expect(src).toMatch(/data-testid="confirm-delete-run-btn"/)
    expect(src).toMatch(/pages\.runDetail\.deleteWarning/)
    expect(src).toMatch(/pages\.runList\.deleteConfirm/)
    expect(src).toMatch(/pages\.runList\.deleteSuccess/)

    const openStart = src.indexOf('function openDeleteConfirm(r: Run)')
    const openEnd = src.indexOf('\nfunction closeDeleteConfirm(', openStart)
    const openFn = src.slice(openStart, openEnd)
    expect(openFn).toContain('deleteTarget.value = r')
    expect(openFn).not.toContain('api.deleteRun')

    const confirmStart = src.indexOf('async function confirmDeleteRun()')
    const confirmEnd = src.indexOf('\nwatch(statusFilterOpen', confirmStart)
    const confirmFn = src.slice(confirmStart, confirmEnd > 0 ? confirmEnd : undefined)
    expect(confirmFn).toContain('api.deleteRun(target.id)')
    expect(confirmFn).toContain("toast.success(t('pages.runList.deleteSuccess'))")
    expect(confirmFn).toContain('await load()')
    expect(confirmFn).not.toContain('router.push')
    expect(confirmFn).toContain('deletingRun.value = true')
    expect(confirmFn).toContain('mapDeleteRunError')
  })

  it('stops propagation on ops area and placeholder so row does not open detail', () => {
    expect(src).toMatch(/data-testid="run-ops"/)
    expect(src).toMatch(/data-testid="run-ops-placeholder"/)
    // desktop ops cell and mobile ops wrapper both stop click
    expect(src.match(/data-testid="run-ops"[^>]*@click\.stop/g)?.length).toBeGreaterThanOrEqual(2)
    // Regression: mobile must use custom+navigate so ops @click.stop does not enable native <a> href
    const mobileBlock = src.slice(src.indexOf('<!-- Mobile card list -->'), src.indexOf('<!-- Desktop table -->'))
    expect(mobileBlock).toMatch(/custom/)
    expect(mobileBlock).toMatch(/@click="navigate"/)
    expect(mobileBlock).toMatch(/role="link"/)
  })

  it('keeps cancel confirm flow and does not drop cancel modal', () => {
    expect(src).toMatch(/openCancelConfirm\(r\)/)
    expect(src).toMatch(/confirmCancelRun/)
    expect(src).toMatch(/data-testid="confirm-cancel-run-btn"/)
    expect(src).toMatch(/pages\.runDetail\.cancelTitle/)
  })
})

describe('RunListView duration column layout (g1.1 / g1.2 / g2.1)', () => {
  const desktopBlock = src.slice(src.indexOf('<!-- Desktop table (loading skeleton or rows) -->'))
  const mobileBlock = src.slice(
    src.indexOf('<!-- Mobile card list -->'),
    src.indexOf('<!-- Desktop table -->'),
  )

  const durationTd = () =>
    desktopBlock.match(/<td class="[^"]*">\{\{ r\.status === 'queued' \? '—' : fmtDuration\(r\.durationSec\) \}\}<\/td>/)?.[0]

  it('uses tabular-nums, whitespace-nowrap and right alignment on the desktop duration td', () => {
    const td = durationTd()
    expect(td).toBeTruthy()
    expect(td).toMatch(/\btabular-nums\b/)
    expect(td).toMatch(/\bwhitespace-nowrap\b/)
    expect(td).toMatch(/\btext-right\b/)
    const th = desktopBlock.match(/<th class="[^"]*">\{\{ t\('pages\.runList\.colDuration'\) \}\}<\/th>/)?.[0]
    expect(th).toMatch(/whitespace-nowrap/)
    expect(th).toMatch(/text-right/)
  })

  it('reserves fixed column widths (incl. hh:mm:ss duration) via colgroup and the skeleton', () => {
    const colgroup = desktopBlock.match(/<colgroup>[\s\S]*?<\/colgroup>/)?.[0]
    expect(colgroup).toBeTruthy()
    // 运行 flexes; 状态 / 开始 / 耗时 / ⋯ are fixed so short columns never wrap.
    expect(colgroup!.match(/<col\b/g)).toHaveLength(5)
    expect(colgroup).toMatch(/<col \/>\s*<col style="width: 216px" \/>\s*<col style="width: 112px" \/>\s*<col style="width: 88px" \/>\s*<col style="width: 44px" \/>/)
    const skelStart = desktopBlock.indexOf('v-if="initialLoading"')
    const skelEnd = desktopBlock.indexOf('<template v-else>', skelStart)
    const skeleton = desktopBlock.slice(skelStart, skelEnd)
    expect(skeleton).toMatch(/w-\[8ch\]/)
    expect(skeleton.match(/<td\b/g)).toHaveLength(5)
  })

  it('switches the desktop table to fixed layout with a scrollable minimum width', () => {
    expect(desktopBlock).toMatch(/<table class="run-table w-full text-sm">/)
    expect(src).toMatch(/\.run-table \{ table-layout: fixed; min-width: 640px; \}/)
    // the table scroller already scrolls horizontally on narrow widths
    expect(src).toMatch(/<div class="min-h-0 flex-1 overflow-auto">/)
  })

  it('does not add duration display to mobile cards', () => {
    expect(mobileBlock).not.toMatch(/fmtDuration/)
    expect(mobileBlock).not.toMatch(/common\.table\.duration/)
  })
})

describe('RunListView sorting', () => {
  it('exposes a desktop sortable header for start time only (priority column removed)', () => {
    expect(src).toMatch(/applySortClick\('started_at'\)/)
    expect(src).not.toMatch(/applySortClick\('priority'\)/)
    // ?sort=priority links stay valid: priority is still on the API whitelist.
    expect(src).toMatch(/ALLOWED_SORT: Record<RunSortKey, true> = \{ started_at: true, priority: true \}/)
    expect(src).toMatch(/role="columnheader"/)
    expect(src).toMatch(/aria-sort/)
    expect(src).toMatch(/th\.sortable/)
    expect(src).toMatch(/sort-icon/)
    // No product "restore default" control; sort UI is desktop-table only.
    expect(src).not.toMatch(/恢复默认|restoreDefault|clearSort/)
    expect(src).toMatch(/<!-- Desktop table -->[\s\S]*applySortClick\('started_at'\)/)
    expect(src).toMatch(/<!-- Mobile card list -->[\s\S]*?<!-- Desktop table -->/)
    const mobileBlock = src.slice(
      src.indexOf('<!-- Mobile card list -->'),
      src.indexOf('<!-- Desktop table -->'),
    )
    expect(mobileBlock).not.toContain('applySortClick')
    expect(mobileBlock).not.toContain('sort-icon')
  })

  it('syncs whitelist sort to URL and listParams; strips illegal pairs', () => {
    expect(src).toMatch(/parseRunSort/)
    expect(src).toMatch(/sort: sort\.sort, order: sort\.order/)
    expect(src).toMatch(/delete query\.sort/)
    expect(src).toMatch(/delete query\.order/)
    expect(src).toMatch(/router\.replace\(\{ query \}\)/)
    expect(src).toMatch(/started_at: 'desc'/)
    expect(src).toMatch(/priority: 'desc'/)
  })
})
