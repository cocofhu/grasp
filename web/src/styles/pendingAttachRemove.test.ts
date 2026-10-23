// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const srcRoot = join(dirname(fileURLToPath(import.meta.url)), '..')

function read(rel: string): string {
  return readFileSync(join(srcRoot, rel), 'utf8')
}

function removeButton(rel: string): string {
  const src = read(rel)
  const idx = src.indexOf('pending-attach-remove')
  expect(idx, rel).toBeGreaterThan(-1)
  const start = src.lastIndexOf('<button', idx)
  const end = src.indexOf('</button>', idx)
  expect(start, rel).toBeGreaterThan(-1)
  expect(end, rel).toBeGreaterThan(idx)
  return src.slice(start, end + '</button>'.length)
}

describe('pending attachment remove button colors', () => {
  it('keeps a neutral rest state and a solid red hover or keyboard focus', () => {
    const css = read('styles/global.css')
    expect(css).toMatch(
      /\.pending-attach-remove\s*\{\s*border:\s*1px solid #36363E;\s*background-color:\s*#141417;\s*color:\s*#A1A1AA;\s*\}/,
    )
    expect(css).toMatch(
      /html\.light \.pending-attach-remove\s*\{\s*border-color:\s*#D4D4D8;\s*background-color:\s*#FFFFFF;\s*color:\s*#71717A;\s*\}/,
    )
    expect(css).toMatch(
      /\.pending-attach-remove:is\(:hover, :focus-visible\):not\(:disabled\)\s*\{\s*border-color:\s*#DC2626;\s*background-color:\s*#DC2626;\s*color:\s*#FFFFFF;\s*\}/,
    )
    expect(css).toMatch(
      /\.pending-attach-remove:disabled:is\(:hover, :focus-visible\)\s*\{\s*border-color:\s*#36363E;\s*background-color:\s*#141417;\s*color:\s*#A1A1AA;\s*\}/,
    )
    expect(css).toMatch(
      /html\.light \.pending-attach-remove:disabled:is\(:hover, :focus-visible\)\s*\{\s*border-color:\s*#D4D4D8;\s*background-color:\s*#FFFFFF;\s*color:\s*#71717A;\s*\}/,
    )
  })

  it('applies the shared control at the six pending-attachment sites only', () => {
    const sites = [
      {
        file: 'views/DashboardView.vue',
        radius: 'rounded',
        forbiddenRadius: ['rounded-full', 'rounded-none'],
        click: '@click.stop="removeAttachment(ii)"',
      },
      {
        file: 'components/run/ClarifyChat.vue',
        radius: 'rounded-full',
        forbiddenRadius: ['rounded-none'],
        click: '@click.stop="removeAttachment(ii)"',
      },
      {
        file: 'components/solo/SoloSessionPane.vue',
        radius: 'rounded-full',
        forbiddenRadius: ['rounded-none'],
        click: '@click="attach.removeAttachment(ii)"',
      },
      {
        file: 'components/pm/PmLeaderChat.vue',
        radius: null,
        forbiddenRadius: ['rounded', 'rounded-full', 'rounded-none'],
        click: '@click.stop="removeAttachment(ii)"',
        disabled: ':disabled="busy"',
      },
      {
        file: 'components/ui/ParagraphInput.vue',
        radius: 'rounded-full',
        forbiddenRadius: ['rounded-none'],
        click: '@click.stop="removeAttachment(ii)"',
        disabled: ':disabled="disabled"',
      },
      {
        file: 'components/agent/AgentChatTester.vue',
        radius: 'rounded-full',
        forbiddenRadius: ['rounded-none'],
        click: '@click.stop="removeAttachment(ii)"',
      },
    ] as const

    for (const site of sites) {
      const button = removeButton(site.file)
      expect(button, site.file).toContain('pending-attach-remove')
      expect(button, site.file).toContain('h-4 w-4')
      expect(button, site.file).toContain('-right-1.5 -top-1.5')
      expect(button, site.file).toContain('<Icon name="close" :size="9"')
      expect(button, site.file).toContain(site.click)
      expect(button, site.file).not.toMatch(/\bbg-err\b/)
      expect(button, site.file).not.toMatch(/\btext-white\b/)
      const className = button.match(/class="([^"]*)"/)?.[1] ?? ''
      const tokens = className.split(/\s+/)
      if (site.radius) expect(tokens, site.file).toContain(site.radius)
      for (const forbidden of site.forbiddenRadius) {
        expect(tokens, `${site.file} ${forbidden}`).not.toContain(forbidden)
      }
      if ('disabled' in site && site.disabled) expect(button, site.file).toContain(site.disabled)
    }

    expect(read('styles/global.css').match(/\.pending-attach-remove/g)?.length).toBe(5)
  })
})
