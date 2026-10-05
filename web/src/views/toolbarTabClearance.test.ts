// @vitest-environment node
/**
 * Plan coverage locks for「操作按钮与 Tab 底线错开」:
 * g1.1 project tab-panel · g1.2 Studio action row · g1.3 inline rows · g1.4 shared tokens
 */
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const read = (rel: string) => readFileSync(join(root, rel), 'utf8')

const globalCss = read('styles/global.css')
const detailSrc = read('views/ProjectDetailView.vue')
const studioSrc = read('views/AgentStudioView.vue')
const sharedSrc = read('components/project/ProjectSharedAgentPanel.vue')
const mcpSrc = read('components/agent/AgentMcpPanel.vue')
const envSrc = read('components/agent/AgentEnvPanel.vue')
const capsSrc = read('components/agent/AgentCapabilitiesPanel.vue')
const chatSrc = read('components/agent/AgentChatTester.vue')

describe('toolbar Tab clearance tokens (g1.4)', () => {
  it('defines shared toolbar-below-tabs and toolbar-inline-row (8–12px band)', () => {
    expect(globalCss).toMatch(/\.toolbar-below-tabs\s*\{[\s\S]*?@apply pt-2\.5/)
    expect(globalCss).toMatch(/\.toolbar-inline-row\s*\{[\s\S]*?@apply py-2\.5/)
  })
})

describe('project page tab-panel clearance (g1.1)', () => {
  it('applies toolbar-below-tabs on project-detail-tab-panel', () => {
    expect(detailSrc).toMatch(
      /class="toolbar-below-tabs flex min-h-0 flex-1 flex-col overflow-hidden"\s+data-testid="project-detail-tab-panel"/,
    )
  })
})

describe('Agent Studio action / name bars (g1.2 / g1.3)', () => {
  it('no longer renders independent import/new action row under tabs', () => {
    expect(studioSrc).not.toMatch(/data-testid="agent-studio-action-row"/)
    expect(studioSrc).not.toMatch(/toolbar-below-tabs mb-5 flex shrink-0 gap-4/)
  })

  it('desktop and mobile name bars use toolbar-inline-row', () => {
    const bars = studioSrc.match(/data-test="studio-name-bar"[\s\S]*?class="([^"]+)"/g) ?? []
    expect(bars.length).toBeGreaterThanOrEqual(2)
    for (const bar of bars) {
      expect(bar).toMatch(/toolbar-inline-row/)
      expect(bar).not.toMatch(/\bpy-1\b/)
      expect(bar).not.toMatch(/\bpy-2\b/)
    }
  })
})

describe('same-row toolbars leave the border-b (g1.3)', () => {
  it('shared Agent save row uses toolbar-inline-row instead of py-1', () => {
    expect(sharedSrc).toMatch(
      /class="toolbar-inline-row flex shrink-0 items-center gap-2 border-b border-line px-2"/,
    )
    expect(sharedSrc).not.toMatch(/border-b border-line px-2 py-1/)
  })

  it('MCP / Env / capabilities / chat tester headers use toolbar-inline-row', () => {
    expect(mcpSrc).toMatch(/class="toolbar-inline-row flex items-center gap-2 border-b border-line px-4"/)
    expect(envSrc).toMatch(/class="toolbar-inline-row flex items-center gap-2 border-b border-line px-4"/)
    expect(capsSrc).toMatch(
      /class="toolbar-inline-row flex items-center justify-between gap-2 border-b border-line px-4"/,
    )
    expect(chatSrc).toMatch(
      /v-if="!embedded" class="toolbar-inline-row flex items-center gap-2 border-b border-line px-4"/,
    )
  })
})
