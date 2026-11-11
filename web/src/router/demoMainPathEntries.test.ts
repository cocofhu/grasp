// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))
const routerSrc = readFileSync(join(here, 'index.ts'), 'utf8')

/** Demo 主路径：三入口（Studio / Run / Gates）路由仍可切换。 */
describe('Demo main-path three entries (g6.2)', () => {
  it('keeps Agent Studio, Run detail, and Gates inbox routes', () => {
    expect(routerSrc).toMatch(/path: '\/agents'[\s\S]*AgentStudioView/)
    expect(routerSrc).toMatch(/path: '\/runs\/:id'[\s\S]*RunDetailView/)
    expect(routerSrc).toMatch(/path: '\/runs\/:runId\/nodes\/:nodeId'[\s\S]*GatesInboxView/)
  })
})
