// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const dir = dirname(fileURLToPath(import.meta.url))
const src = readFileSync(join(dir, 'RunListView.vue'), 'utf8')
const shellSrc = readFileSync(join(dir, '../components/shell/AppShell.vue'), 'utf8')
const routerSrc = readFileSync(join(dir, '../router/index.ts'), 'utf8')
const sandboxSrc = readFileSync(join(dir, 'SandboxListView.vue'), 'utf8')
const projectSrc = readFileSync(join(dir, 'ProjectListView.vue'), 'utf8')
const zhCommon = readFileSync(join(dir, '../locales/zh-CN/common.json'), 'utf8')
const zhPages = readFileSync(join(dir, '../locales/zh-CN/pages.json'), 'utf8')

const EMPTY_CARD_CLASS =
  'card flex min-h-0 flex-1 flex-col items-center justify-center overflow-auto'
const FILL_CARD_CLASS = 'card flex min-h-0 flex-1 flex-col overflow-hidden'

describe('RunListView fill height chain (g1)', () => {
  it('root uses flex h-full min-h-0 flex-col without calc(100vh) (g1.1)', () => {
    expect(src).toMatch(/<div class="flex h-full min-h-0 flex-col">/)
    expect(src).not.toMatch(/calc\(100vh/)
  })

  it('keeps title and filter bar shrink-0 (g1.2)', () => {
    expect(src).toMatch(
      /mb-5 flex shrink-0 flex-col gap-2\.5 md:flex-row md:items-start md:justify-between/,
    )
  })

  it('desktop and mobile main cards flex-1 min-h-0; empty/fail may overflow-auto (g1.3)', () => {
    const emptyCards = src.match(new RegExp(`class="${EMPTY_CARD_CLASS.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}[^"]*"`, 'g')) || []
    expect(emptyCards.length).toBeGreaterThanOrEqual(4)
    expect(src).toMatch(new RegExp(FILL_CARD_CLASS.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')))
    expect(src).toMatch(/class="flex min-h-0 flex-1 flex-col overflow-hidden"/)
  })

  it('loading skeleton still uses SKELETON_ROWS=6 at top, not centered empty (g1.4)', () => {
    expect(src).toMatch(/const SKELETON_ROWS = 6/)
    expect(src).toMatch(/v-for="n in SKELETON_ROWS"/)
    expect(src).toMatch(/:key="'skel-card-' \+ n"/)
    expect(src).toMatch(/:key="'skel-' \+ n"/)
    const mobile = src.slice(src.indexOf('<!-- Mobile card list -->'), src.indexOf('<!-- Desktop table -->'))
    expect(mobile).toMatch(/initialLoading[\s\S]*overflow-y-auto[\s\S]*SKELETON_ROWS/)
    expect(mobile).not.toMatch(/initialLoading[\s\S]*items-center justify-center[\s\S]*SKELETON_ROWS/)
  })
})

describe('RunListView empty / fail split out of table (g2)', () => {
  it('desktop empty/no-match is a centered fill card, not thead+td[colspan] (g2.1)', () => {
    expect(src).not.toMatch(/colspan/)
    expect(src).toMatch(/emptyMessage/)
    expect(src).toMatch(/common\.empty\.noRuns/)
    expect(src).toMatch(/common\.empty\.noMatchingRuns/)
    expect(src).not.toMatch(/EmptyState/)
    const desktop = src.slice(src.indexOf('<!-- Desktop table -->'))
    expect(desktop).toMatch(new RegExp(EMPTY_CARD_CLASS.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')))
    expect(desktop).toMatch(/\{\{\s*emptyMessage\s*\}\}/)
  })

  it('desktop fail is centered fill card without alert icon box (g2.2)', () => {
    expect(src).not.toMatch(/inline-flex h-10 w-10 items-center justify-center border border-err\/30/)
    expect(src).not.toMatch(/<Icon name="alert" :size="18"/)
    expect(src).toMatch(/pages\.runList\.loadFailedTitle/)
    expect(src).toMatch(/pages\.runList\.loadFailedDesc/)
    const desktopFail = src.slice(
      src.indexOf('<!-- Desktop table -->'),
      src.indexOf('<!-- Desktop table (loading skeleton or rows) -->'),
    )
    expect(desktopFail).toMatch(/loadFailed/)
    expect(desktopFail).toMatch(new RegExp(EMPTY_CARD_CLASS.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')))
    expect(desktopFail).not.toMatch(/<table/)
    expect(desktopFail).not.toMatch(/Icon name="alert"/)
  })

  it('user-driven reload failures show the error instead of a stale/empty list', () => {
    const loadFn = src.slice(src.indexOf('async function load('), src.indexOf('async function validateQueryParams('))
    expect(loadFn).toMatch(/isFirstLoad \|\| showLoading/)
    expect(loadFn).toMatch(/loadErrorMessage\.value = e instanceof Error \? e\.message/)
    expect(src.match(/\{\{ loadErrorMessage \}\}/g)?.length).toBe(2)
  })

  it('mobile empty/fail use the same flex-1 centered fill card (g2.3)', () => {
    const mobile = src.slice(src.indexOf('<!-- Mobile card list -->'), src.indexOf('<!-- Desktop table -->'))
    const emptyCards = mobile.match(new RegExp(`class="${EMPTY_CARD_CLASS.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}[^"]*"`, 'g')) || []
    expect(emptyCards.length).toBe(2)
    expect(mobile).toMatch(/loadFailed/)
    expect(mobile).toMatch(/emptyMessage/)
    expect(mobile).not.toMatch(/inline-flex h-10 w-10/)
    expect(mobile).not.toMatch(/Icon name="alert"/)
    expect(mobile).not.toMatch(/applySortClick/)
  })
})

describe('RunListView sticky table + pager pin (g3)', () => {
  it('desktop table wrap is overflow-auto with sticky elevated thead (g3.1)', () => {
    expect(src).toMatch(/class="min-h-0 flex-1 overflow-auto"/)
    expect(src).toMatch(/thead th\s*\{[^}]*position:\s*sticky/s)
    expect(src).toMatch(/thead th\s*\{[^}]*top:\s*0/s)
    expect(src).toMatch(/thead th\s*\{[^}]*background:\s*rgb\(var\(--c-elevated\)\)/s)
  })

  it('Pagination shrink-0 still gates on total > PAGE_SIZE and binds page (g3.2)', () => {
    expect(src).toMatch(/const PAGE_SIZE = 20/)
    const pagers = src.match(/<Pagination v-if="total > PAGE_SIZE" v-model:page="page"/g) || []
    expect(pagers.length).toBe(2)
    const shrinkPagers = src.match(/<Pagination v-if="total > PAGE_SIZE" v-model:page="page" class="shrink-0"/g) || []
    expect(shrinkPagers.length).toBe(2)
  })

  it('mobile data list uses overflow-y-auto and pins pager (g3.3)', () => {
    const mobile = src.slice(src.indexOf('<!-- Mobile card list -->'), src.indexOf('<!-- Desktop table -->'))
    expect(mobile).toMatch(/min-h-0 flex-1 flex-col gap-2 overflow-y-auto/)
    expect(mobile).toMatch(/<Pagination v-if="total > PAGE_SIZE" v-model:page="page" class="shrink-0"/)
    expect(mobile).not.toMatch(/applySortClick/)
    expect(mobile).not.toMatch(/sort-icon/)
  })

  it('mobile data list uses overflow-y-auto p-2 so list-card-lift hover is not clipped (g2.3 regress)', () => {
    const mobile = src.slice(src.indexOf('<!-- Mobile card list -->'), src.indexOf('<!-- Desktop table -->'))
    expect(mobile).toMatch(/min-h-0 flex-1 flex-col gap-2 overflow-y-auto p-2/)
    expect(mobile).toMatch(/list-card-lift/)
  })
})

describe('RunListView fill scope lock (g4.3)', () => {
  it('does not alter AppShell height chain or /runs meta.full', () => {
    expect(shellSrc).toMatch(/h-screen/)
    expect(shellSrc).toMatch(/min-h-0 flex-1/)
    expect(routerSrc).toMatch(
      /path: '\/runs', name: 'runs', component: \(\) => import\('@\/views\/RunListView\.vue'\), meta: \{ titleKey: 'route\.runs' \}/,
    )
    expect(routerSrc).not.toMatch(/path: '\/runs'[^}]*full:\s*true/)
  })

  it('does not change EmptyState or empty/fail i18n strings (list A pages now use fill scroll — plan g1)', () => {
    expect(sandboxSrc).toMatch(/flex h-full min-h-0 flex-col/)
    expect(projectSrc).toMatch(/flex h-full min-h-0 flex-col/)
    expect(src).not.toMatch(/EmptyState/)
    expect(zhCommon).toMatch(/"noRuns": "暂无运行记录"/)
    expect(zhCommon).toMatch(/"noMatchingRuns": "无匹配运行记录"/)
    expect(zhPages).toMatch(/"loadFailedTitle": "加载失败"/)
    expect(zhPages).toMatch(/"loadFailedDesc": "无法加载运行记录，后台轮询将自动重试。筛选与翻页仍可使用。"/)
  })
})
