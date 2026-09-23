// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const viewsDir = dirname(fileURLToPath(import.meta.url))
const detailSrc = readFileSync(join(viewsDir, 'ProjectDetailView.vue'), 'utf8')
const detailLogicSrc = readFileSync(join(viewsDir, '../lib/project/useProjectDetail.ts'), 'utf8')
const detailFullSrc = `${detailSrc}\n${detailLogicSrc}`
const shellSrc = readFileSync(join(viewsDir, '../components/shell/AppShell.vue'), 'utf8')

function metaTabBlock(): string {
  const start = detailSrc.indexOf("tab === 'meta'")
  expect(start, 'missing meta tab block').toBeGreaterThanOrEqual(0)
  const end = detailSrc.indexOf('ref="fileInput"', start)
  expect(end, 'missing end marker after meta tab').toBeGreaterThan(start)
  return detailSrc.slice(start, end)
}

describe('ProjectDetailView meta tab fill-height chain (g1.1 / g1.2 / g1.3)', () => {
  it('outer wrapper uses min-h-0 flex-1 instead of 420px hard floor (g1.1)', () => {
    const meta = metaTabBlock()
    expect(meta).toMatch(/class="flex min-h-0 flex-1 flex-col"/)
    expect(meta).not.toMatch(/min-h-\[420px\]/)
    expect(meta).not.toMatch(/\bmin-h-\[[0-9]+vh\]/)
    expect(meta).not.toMatch(/\bh-\[[0-9]+vh\]/)
    expect(meta).toMatch(/flex flex-1 flex-col overflow-hidden border border-line bg-surface shadow-\[var\(--shadow-card\)\]/)
  })

  it('head/footer shrink-0 and fields scroll inside remaining height (g1.2)', () => {
    const meta = metaTabBlock()
    expect(meta).toMatch(/shrink-0 border-b border-line bg-elevated\/55/)
    expect(meta).toMatch(
      /scroll-area flex min-h-0 flex-1 flex-col gap-3\.5 overflow-y-auto p-4/,
    )
    expect(meta).toMatch(
      /flex shrink-0 flex-wrap items-center justify-between[\s\S]*data-testid="project-meta-footer"/,
    )
  })

  it('textarea stays min-h-[120px] without flex-1 stretch (g1.3)', () => {
    const meta = metaTabBlock()
    const ta = meta.match(/<textarea[\s\S]*?\/>/)
    expect(ta?.[0], 'missing meta textarea').toBeTruthy()
    expect(ta![0]).toMatch(/min-h-\[120px\]/)
    expect(ta![0]).toMatch(/rows="5"/)
    expect(ta![0]).toMatch(/resize-y/)
    expect(ta![0]).not.toMatch(/flex-1/)
  })
})

describe('ProjectDetailView meta tab keeps existing chrome and save semantics (g1.4 / g2.1 / g2.2)', () => {
  it('keeps near-full-width card chrome and live tokens, not Demo skin (g1.4)', () => {
    const meta = metaTabBlock()
    expect(meta).toMatch(/border border-line bg-surface shadow-\[var\(--shadow-card\)\]/)
    expect(meta).toMatch(/bg-elevated\/55/)
    expect(meta).not.toMatch(/max-w-\[/)
    expect(meta).not.toMatch(/mx-auto/)
    expect(meta).not.toMatch(/border-radius:\s*0/)
    expect(meta).not.toMatch(/#7B61FF/)
  })

  it('save stays bound to savingMeta, not Demo dirty-disable (g2.1)', () => {
    const meta = metaTabBlock()
    expect(meta).toMatch(/:disabled="savingMeta"/)
    expect(meta).not.toMatch(/!dirty/)
    expect(detailFullSrc).toMatch(/savingMeta\.value = true/)
    expect(detailFullSrc).toMatch(/async function saveMeta\(/)
  })

  it('does not change AppShell height chain; short tabs use fill scroll (plan g2.1)', () => {
    expect(shellSrc).toMatch(/class="[^"]*relative flex h-screen w-screen overflow-hidden text-txt/)
    expect(shellSrc).toMatch(/<main[\s\S]*class="relative min-h-0 flex-1 overflow-hidden"/)
    expect(detailSrc).toMatch(/tab === 'cronJobs'[\s\S]*?class="scroll-area flex min-h-0 flex-1 flex-col overflow-y-auto"/)
    expect(detailSrc).toMatch(/tab === 'notify' && project[\s\S]*?class="scroll-area min-h-0 flex-1 overflow-y-auto"/)
    expect(detailSrc).not.toMatch(/tab === 'cronJobs'" class="flex min-h-\[420px\] flex-col"/)
    expect(detailSrc).not.toMatch(/tab === 'notify' && project" class="min-h-\[420px\]"/)
    // variables / agents / sharedAgent 已接入铺满链（对齐 meta），不再锁定 420 硬底
    expect(detailSrc).toMatch(/tab === 'agents'" class="flex min-h-0 flex-1 flex-col"/)
    expect(detailSrc).toMatch(/tab === 'sharedAgent'" class="flex min-h-0 flex-1 flex-col"/)
    expect(detailSrc).toMatch(/tab === 'variables'" class="flex min-h-0 flex-1 flex-col"/)
    expect(detailSrc).toMatch(/tab === 'audit'" class="flex min-h-0 flex-1 flex-col"/)
  })

  it('variables / sharedAgent shells drop bottom border and use three-zone flex (fill-height)', () => {
    const agentsStart = detailSrc.indexOf("tab === 'agents'")
    const sharedStart = detailSrc.indexOf("tab === 'sharedAgent'")
    const variablesStart = detailSrc.indexOf("tab === 'variables'")
    const auditStart = detailSrc.indexOf("tab === 'audit'")
    expect(agentsStart).toBeGreaterThanOrEqual(0)
    expect(sharedStart).toBeGreaterThan(agentsStart)
    expect(variablesStart).toBeGreaterThan(sharedStart)
    expect(auditStart).toBeGreaterThan(variablesStart)
    const agents = detailSrc.slice(agentsStart, sharedStart)
    const shared = detailSrc.slice(sharedStart, variablesStart)
    const variables = detailSrc.slice(variablesStart, auditStart)

    expect(agents).toMatch(/ProjectAgentsPanel/)
    expect(agents).toMatch(/data-testid="project-agents-tab"/)
    expect(agents).toMatch(/class="flex min-h-0 flex-1 flex-col"/)

    expect(shared).toMatch(/ProjectSharedAgentPanel/)
    expect(shared).toMatch(/class="flex min-h-0 flex-1 flex-col"/)
    expect(shared).not.toMatch(/min-h-\[420px\]/)
    expect(shared).not.toMatch(/sandbox-env/)

    expect(variables).toMatch(/border border-b-0 border-line bg-surface/)
    expect(variables).toMatch(/scroll-area flex min-h-0 flex-1 flex-col overflow-y-auto/)
    expect(variables).toMatch(/flex shrink-0 flex-wrap gap-2 border-t border-line/)
    expect(variables).not.toMatch(/min-h-\[420px\]/)
    // g1.1 / g1.2: Tabs 下直接 empty/data shell，无 hintRow（varsHint + 合并规则）
    expect(variables).not.toMatch(/varsHint/)
    expect(variables).not.toMatch(/project-detail-merge-rules/)
  })
})
