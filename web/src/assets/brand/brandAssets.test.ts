// @vitest-environment node
import { existsSync, readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))
const web = join(here, '../../..')
const media = join(web, '../media')
const read = (p: string) => readFileSync(p, 'utf8')

const characterVue = read(join(web, 'src/components/shell/BrandCharacterMark.vue'))
const catBody = characterVue.match(/<path\s+d="(M58 160[^"]+)"/)?.[1] ?? ''

const LOGOS = ['logo-mark.svg', 'logo-light.svg', 'logo-dark.svg'] as const
const MEDIA_SVGS = [...LOGOS, 'grasp-logo-banner.svg'] as const

describe('brand assets: ghost cat + Fredoka wordmark', () => {
  it('reads the cat body path from BrandCharacterMark.vue', () => {
    expect(catBody).toMatch(/^M58 160 .*Z$/)
  })

  it.each(MEDIA_SVGS)('media/%s draws the same cat and no legacy diamond mark', (name) => {
    const svg = read(join(media, name))
    expect(svg).toContain(`d="${catBody}"`)
    for (const legacy of ['#3b2f4a', '#ff9db5', '#9fe3ff', 'coreGrad']) {
      expect(svg).not.toContain(legacy)
    }
    expect(svg).not.toMatch(/<text\b|@font-face|font-family|<image\b|https?:\/\/(?!www\.w3\.org\/2000\/svg)/)
  })

  it.each(['logo-light.svg', 'logo-dark.svg', 'grasp-logo-banner.svg'])(
    'media/%s outlines the wordmark as a path',
    (name) => {
      const svg = read(join(media, name))
      expect(svg).toMatch(/viewBox="6 -216 798 288"[\s\S]*<path d="M117 5/)
    },
  )

  it('uses ink #18181b on light and #ededf0 on dark lockups', () => {
    expect(read(join(media, 'logo-light.svg'))).toMatch(/fill="#18181b"/)
    expect(read(join(media, 'logo-dark.svg'))).toMatch(/fill="#ededf0"/)
    expect(read(join(media, 'logo-mark.svg'))).not.toMatch(/viewBox="6 -216 798 288"/)
  })

  it.each(LOGOS)('web/src/assets/brand/%s matches media/', (name) => {
    expect(read(join(here, name))).toBe(read(join(media, name)))
  })

  it('keeps the three favicon.svg copies in sync', () => {
    const src = read(join(media, 'favicon.svg'))
    expect(read(join(here, 'favicon.svg'))).toBe(src)
    expect(read(join(web, 'public/favicon.svg'))).toBe(src)
  })

  it('declares self-hosted Fredoka 600 in fonts.css', () => {
    const css = read(join(web, 'src/styles/fonts.css'))
    const face = css.match(/@font-face\s*\{[^}]*font-family:\s*'Fredoka'[^}]*\}/)?.[0] ?? ''
    expect(face).toMatch(/font-weight:\s*600/)
    expect(face).toMatch(/font-display:\s*swap/)
    expect(face).toMatch(/url\('\/fonts\/fredoka-latin-600-normal\.woff2'\) format\('woff2'\)/)
    expect(existsSync(join(web, 'public/fonts/fredoka-latin-600-normal.woff2'))).toBe(true)
    expect(read(join(web, 'public/fonts/NOTICE.md'))).toMatch(/\| Fredoka \|.*Open Font License 1\.1/)
  })
})
