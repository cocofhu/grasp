// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))
const viewSrc = readFileSync(join(here, 'RunDetailView.vue'), 'utf8')
const canvasTag = viewSrc.match(/<WorkflowCanvas[\s\S]*?\/>/)?.[0] ?? ''

describe('RunDetailView canvas', () => {
  it('reuses the workflow canvas read-only in run mode', () => {
    expect(canvasTag).toContain('mode="run"')
    expect(canvasTag).not.toContain(':editor=')
    expect(canvasTag).toContain(':status-map="statusMap"')
    expect(canvasTag).toContain(':active-path="activePath"')
  })

  it('follows the running node by default and lets the user turn it off', () => {
    expect(viewSrc).toMatch(/const followCanvas = ref\(true\)/)
    expect(canvasTag).toContain(':follow="followCanvas"')
    expect(canvasTag).toContain(':follow-node-id="canvasFollowNodeId"')
    expect(canvasTag).toContain('@update:follow="followCanvas = $event"')
    expect(viewSrc).toMatch(/s\[n\.id\] === 'running' \|\| s\[n\.id\] === 'waiting_human'/)
  })

  it('shows iterations and failure reasons, and opens the node panel on click or reply', () => {
    expect(canvasTag).toContain(':iterations="canvasIterations"')
    expect(canvasTag).toContain(':fail-reasons="canvasFailReasons"')
    expect(canvasTag).toContain('@select-node="selectNode"')
    expect(canvasTag).toContain('@reply="selectNode"')
  })
})
