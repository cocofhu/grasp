import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))
const github = readFileSync(join(here, 'GitHubDeviceLoginCard.vue'), 'utf8')
const codex = readFileSync(join(here, 'CodexDeviceLoginCard.vue'), 'utf8')
const tabs = readFileSync(join(here, '../ui/SlidingTabs.vue'), 'utf8')
const css = readFileSync(join(here, '../../styles/global.css'), 'utf8')

describe('device-code card motion and copy', () => {
  it('reuses the Codex timings and the shared press token', () => {
    for (const source of [github, codex]) {
      expect(source).toMatch(/animation: \w+-device-breathe 1\.6s ease-in-out infinite/)
      expect(source).toMatch(/transition: transform 1s linear/)
      expect(source).toContain('ui-pressable')
      expect(source).toMatch(/prefers-reduced-motion: reduce/)
      expect(source).toContain("from '@/lib/shared/copyToClipboard'")
      expect(source).not.toContain('navigator.clipboard.writeText')
    }
    expect(tabs).toMatch(/transform 420ms var\(--sliding-tabs-ease\)/)
    expect(css).toMatch(/--dur-press:\s*90ms/)
    expect(css).toMatch(/\.ui-pressable:not\(:disabled\):active[\s\S]*scale\(0\.97\) translateY\(1px\)/)
  })

  it('does not list requested scopes on the GitHub card', () => {
    expect(github).not.toContain('申请权限')
    expect(github).not.toContain('gh-device__chip')
    expect(github).not.toContain("['repo'")
  })
})
