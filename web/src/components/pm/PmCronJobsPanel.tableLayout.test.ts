// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const pmDir = dirname(fileURLToPath(import.meta.url))
const src = readFileSync(join(pmDir, 'PmCronJobsPanel.vue'), 'utf8')

function tableTagByTestId(testid: string): string {
  const idAt = src.indexOf(`data-testid="${testid}"`)
  expect(idAt, `missing ${testid}`).toBeGreaterThanOrEqual(0)
  const tagStart = src.lastIndexOf('<table', idAt)
  expect(tagStart, `missing <table> before ${testid}`).toBeGreaterThanOrEqual(0)
  return src.slice(tagStart, src.indexOf('>', tagStart) + 1)
}

function tableBlockByTestId(testid: string): string {
  const tagStart = src.lastIndexOf('<table', src.indexOf(`data-testid="${testid}"`))
  return src.slice(tagStart, src.indexOf('</table>', tagStart))
}

describe('PmCronJobsPanel table layout parity (g2.1 / g2.2)', () => {
  it('real cron table reuses the skeleton min-w-[720px] + overflow-x-auto strategy', () => {
    const real = tableTagByTestId('cron-table')
    expect(real).toMatch(/\bw-full\b/)
    expect(real).toMatch(/min-w-\[720px\]/)
    // Skeleton keeps the same min-width so rows/skeleton never jump width.
    const skeletonWrap = src.slice(src.indexOf('data-testid="cron-table-skeleton"'))
    const skeletonTag = skeletonWrap.slice(skeletonWrap.indexOf('<table'), skeletonWrap.indexOf('>', skeletonWrap.indexOf('<table')) + 1)
    expect(skeletonTag).toMatch(/min-w-\[720px\]/)
    expect(src).toMatch(/scroll-area overflow-x-auto/)
  })

  it('every real-table header is nowrap and the single action button does not wrap', () => {
    const block = tableBlockByTestId('cron-table')
    const headers = block.slice(block.indexOf('<thead'), block.indexOf('</thead>')).match(/<th\b[^>]*>/g) ?? []
    expect(headers).toHaveLength(10)
    for (const th of headers) {
      expect(th).toMatch(/whitespace-nowrap/)
    }
    const deleteBtn = block.slice(block.indexOf('data-testid="project-cron-delete"') - 200, block.indexOf('data-testid="project-cron-delete"'))
    expect(deleteBtn).toMatch(/whitespace-nowrap/)
  })
})
