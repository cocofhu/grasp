// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { compileStyle } from '@vue/compiler-sfc'
import { describe, expect, it } from 'vitest'

const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'DashboardView.vue'), 'utf8')
const style = src.match(/<style scoped>([\s\S]*)<\/style>/)?.[1] ?? ''

type CssRule = { selector: string; body: string; media: string }

function collectRules(css: string, media = ''): CssRule[] {
  const rules: CssRule[] = []
  let i = 0
  while (i < css.length) {
    const open = css.indexOf('{', i)
    if (open < 0) break
    const selector = css.slice(i, open).replace(/\/\*[\s\S]*?\*\//g, '').trim()
    let depth = 1
    let j = open + 1
    while (j < css.length && depth > 0) {
      if (css[j] === '{') depth += 1
      else if (css[j] === '}') depth -= 1
      j += 1
    }
    const body = css.slice(open + 1, j - 1)
    if (selector.startsWith('@media')) collectRules(body, selector).forEach((rule) => rules.push(rule))
    else if (selector && !selector.startsWith('@')) rules.push({ selector, body, media })
    i = j
  }
  return rules
}

/** a-b-c specificity. :not/:is add the argument; :where adds nothing. */
function specificity(selector: string): [number, number, number] {
  let a = 0
  let b = 0
  let c = 0
  let rest = selector.replace(/:(not|is)\(([^()]*)\)/g, (_, _kind, inner: string) => {
    const [ia, ib, ic] = specificity(inner)
    a += ia
    b += ib
    c += ic
    return ''
  })
  rest = rest.replace(/:where\([^()]*\)/g, '')
  a += rest.match(/#[\w-]+/g)?.length ?? 0
  b += rest.match(/\.[\w-]+/g)?.length ?? 0
  b += rest.match(/\[[^\]]+\]/g)?.length ?? 0
  b += rest.match(/:(?!not\b|is\b|where\b|has\b)[\w-]+(?:\([^)]*\))?/g)?.length ?? 0
  const elements = rest
    .replace(/#[\w-]+/g, ' ')
    .replace(/\.[\w-]+/g, ' ')
    .replace(/\[[^\]]+\]/g, ' ')
    .replace(/::?[\w-]+(?:\([^)]*\))?/g, ' ')
  c += elements.match(/[a-zA-Z*][\w-]*/g)?.length ?? 0
  return [a, b, c]
}

function beatsOrTies(winner: [number, number, number], other: [number, number, number]): boolean {
  for (let i = 0; i < 3; i += 1) {
    if (winner[i] !== other[i]) return winner[i] > other[i]
  }
  return true
}

function partMatching(selector: string, needle: RegExp): string {
  const part = selector.split(',').map((item) => item.trim()).find((item) => needle.test(item))
  if (!part) throw new Error(`no selector part matched ${needle} in ${selector}`)
  return part
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

    // plan g1.3 — hero copy unified via the --c-txt token; no .em split highlight
    const heroCopyRules = rules.filter(
      (rule) =>
        hasClass(rule.selector, '.home-hero-copy') && /var\(--c-txt\)/.test(rule.body),
    )
    expect(heroCopyRules.length).toBeGreaterThan(0)
    for (const rule of heroCopyRules) {
      expect(rule.selector.replace(/\s+/g, ' ').trim()).not.toBe('html.light')
      expect(rule.selector).not.toMatch(/html\.light\[data-v-/)
      expect(rule.selector).toMatch(/\.home-hero-copy\[data-v-/)
      expect(rule.selector).not.toMatch(/html\.light/)
    }
    const heroEmRules = rules.filter((rule) => hasClass(rule.selector, '.em'))
    expect(heroEmRules.length).toBe(0)

    const targets: { className: string; body: RegExp }[] = [
      { className: '.home-composer__send', body: /#fff/ },
      { className: '.home-shell__card-top', body: /^\s*background:\s*rgb\(var\(--c-surface\)\);?\s*$/ },
      { className: '.home-composer__toolbar', body: /^\s*background:\s*rgb\(var\(--c-surface\)\);?\s*$/ },
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

    const lightCardTops = rules.filter(
      (rule) => rule.selector.includes('html.light') && hasClass(rule.selector, '.home-shell__card-top'),
    )
    for (const rule of lightCardTops) expect(rule.body).not.toMatch(/244,\s*244,\s*245/)
    expect(
      lightCardTops.some(
        (rule) => rule.selector.includes('.home-shell__card--selected') && /--c-accent\)\s*\/\s*0\.055/.test(rule.body),
      ),
    ).toBe(true)

    const toolbarBase = rules.find(
      (rule) => !rule.selector.includes('html.light') && hasClass(rule.selector, '.home-composer__toolbar'),
    )
    expect(toolbarBase?.body).toMatch(/border-color:\s*rgb\(var\(--c-line\)\s*\/\s*0\.55\)/)
    expect(toolbarBase?.body).toMatch(/color-mix\(in srgb, rgb\(var\(--c-base\)\) 35%, rgb\(var\(--c-surface\)\)\)/)

    const sharedSurfaces = rules.filter(
      (rule) =>
        !rule.selector.includes('html.light') &&
        (hasClass(rule.selector, '.home-composer') ||
          hasClass(rule.selector, '.home-composer__send') ||
          hasClass(rule.selector, '.home-shell__card') ||
          hasClass(rule.selector, '.home-shell__card-top')) &&
        /--c-surface|--c-txt|--c-elevated/.test(rule.body),
    )
    expect(sharedSurfaces.length).toBeGreaterThan(0)
  })

  it('reduced-motion send button none beats the hover lift and press scale', () => {
    expect(compiled.errors).toEqual([])
    const rules = collectRules(compiled.code)
    const lift = rules.find(
      (rule) =>
        !rule.media.includes('prefers-reduced-motion') &&
        /translateY\(-1px\)/.test(rule.body) &&
        rule.selector.includes('.home-composer__send') &&
        rule.selector.includes(':hover'),
    )
    const press = rules.find(
      (rule) =>
        !rule.media.includes('prefers-reduced-motion') &&
        /scale\(0\.96\)/.test(rule.body) &&
        rule.selector.includes('.home-composer__send') &&
        rule.selector.includes(':active'),
    )
    const reduced = rules.filter(
      (rule) =>
        rule.media.includes('prefers-reduced-motion') &&
        /transform:\s*none/.test(rule.body) &&
        rule.selector.includes('.home-composer__send'),
    )
    expect(lift).toBeTruthy()
    expect(press).toBeTruthy()
    expect(reduced.length).toBeGreaterThan(0)

    const reducedHover = reduced
      .map((rule) => partMatching(rule.selector, /\.home-composer__send[^{]*:hover/))
      .find((selector) => selector.includes(':not(') && selector.includes(':disabled'))
    const reducedActive = reduced
      .map((rule) => partMatching(rule.selector, /\.home-composer__send[^{]*:active/))
      .find((selector) => selector.includes(':not(') && selector.includes(':disabled'))
    expect(reducedHover, 'reduced hover selector').toBeTruthy()
    expect(reducedActive, 'reduced active selector').toBeTruthy()
    expect(beatsOrTies(specificity(reducedHover!), specificity(lift!.selector))).toBe(true)
    expect(beatsOrTies(specificity(reducedActive!), specificity(press!.selector))).toBe(true)

    const reducedBase = reduced.some((rule) =>
      rule.selector.split(',').some((part) => {
        const trimmed = part.trim()
        return (
          trimmed.includes('.home-composer__send') &&
          !trimmed.includes(':hover') &&
          !trimmed.includes(':active') &&
          /transition:\s*none/.test(rule.body)
        )
      }),
    )
    expect(reducedBase).toBe(true)
  })
})
