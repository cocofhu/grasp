// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { compileStyle } from '@vue/compiler-sfc'
import { describe, expect, it } from 'vitest'

const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'DashboardView.vue'), 'utf8')
const style = src.match(/<style scoped>([\s\S]*)<\/style>/)?.[1] ?? ''

type CssRule = { selector: string; body: string }

function collectRules(css: string): CssRule[] {
  const rules: CssRule[] = []
  let i = 0
  while (i < css.length) {
    const open = css.indexOf('{', i)
    if (open < 0) break
    const selector = css.slice(i, open).trim()
    let depth = 1
    let j = open + 1
    while (j < css.length && depth > 0) {
      if (css[j] === '{') depth += 1
      else if (css[j] === '}') depth -= 1
      j += 1
    }
    const body = css.slice(open + 1, j - 1)
    if (selector.startsWith('@media')) collectRules(body).forEach((rule) => rules.push(rule))
    else if (selector && !selector.startsWith('@')) rules.push({ selector, body })
    i = j
  }
  return rules
}

const compiled = compileStyle({
  source: style,
  filename: 'DashboardView.vue',
  id: 'lightfix',
  scoped: true,
})

function hasClass(selector: string, className: string): boolean {
  return selector.split(',').some((part) =>
    part.split(/[\s>+~]+/).some((compound) => compound.split(/[:[]/)[0] === className),
  )
}

describe('DashboardView light selectors survive scoped compile', () => {
  it('compiles scoped light rules onto their target classes', () => {
    expect(compiled.errors).toEqual([])
    const rules = collectRules(compiled.code)

    const lightCopy = rules.filter(
      (rule) =>
        rule.selector.includes('html.light') &&
        hasClass(rule.selector, '.home-hero-copy') &&
        !hasClass(rule.selector, '.em') &&
        rule.body.includes('#6b6b6b'),
    )
    expect(lightCopy.length).toBeGreaterThan(0)
    for (const rule of lightCopy) {
      expect(rule.selector.replace(/\s+/g, ' ').trim()).not.toBe('html.light')
      expect(rule.selector).not.toMatch(/html\.light\[data-v-/)
      expect(rule.selector).toMatch(/\.home-hero-copy\[data-v-/)
      expect(hasClass(rule.selector, '.home-hero-copy')).toBe(true)
    }

    const lightEm = rules.filter(
      (rule) =>
        rule.selector.includes('html.light') &&
        hasClass(rule.selector, '.home-hero-copy') &&
        hasClass(rule.selector, '.em') &&
        rule.body.includes('#1a1a1a'),
    )
    expect(lightEm.length).toBeGreaterThan(0)
    for (const rule of lightEm) {
      expect(rule.selector.replace(/\s+/g, ' ').trim()).not.toBe('html.light')
    }

    const darkCopy = rules.filter(
      (rule) => hasClass(rule.selector, '.home-hero-copy') && rule.body.includes('#9a9a9a'),
    )
    const darkEm = rules.filter(
      (rule) => hasClass(rule.selector, '.em') && rule.body.includes('#f2f2f2'),
    )
    expect(darkCopy.length).toBeGreaterThan(0)
    expect(darkEm.length).toBeGreaterThan(0)
    expect(darkCopy.every((rule) => !rule.selector.includes('html.light'))).toBe(true)
    expect(darkEm.every((rule) => !rule.selector.includes('html.light'))).toBe(true)

    const targets: { className: string; body: RegExp }[] = [
      { className: '.home-composer', body: /--c-elevated/ },
      { className: '.home-composer__send', body: /#fff/ },
      { className: '.home-shell__card', body: /--c-elevated/ },
      { className: '.home-shell__card-top', body: /244,\s*244,\s*245/ },
    ]
    for (const target of targets) {
      const matched = rules.filter(
        (rule) =>
          rule.selector.includes('html.light') &&
          hasClass(rule.selector, target.className) &&
          target.body.test(rule.body),
      )
      expect(matched.length, target.className).toBeGreaterThan(0)
      for (const rule of matched) {
        expect(rule.selector.replace(/\s+/g, ' ').trim()).not.toBe('html.light')
        expect(hasClass(rule.selector, target.className)).toBe(true)
      }
    }

    const darkSurfaces = rules.filter(
      (rule) =>
        !rule.selector.includes('html.light') &&
        (hasClass(rule.selector, '.home-composer') ||
          hasClass(rule.selector, '.home-composer__send') ||
          hasClass(rule.selector, '.home-shell__card') ||
          hasClass(rule.selector, '.home-shell__card-top')) &&
        /--c-surface|--c-txt|--c-elevated/.test(rule.body),
    )
    expect(darkSurfaces.length).toBeGreaterThan(0)
  })
})
