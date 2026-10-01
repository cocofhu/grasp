// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { applyPublicLightChrome, reapplyThemeChrome, setTheme, theme, toggleTheme } from './theme'
import { migrateLocalStorageKey } from './migrateBrandStorage'

describe('theme', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.classList.remove('light')
  })

  it('sets and toggles theme with persistence', () => {
    setTheme('light')
    expect(theme.value).toBe('light')
    expect(localStorage.getItem('grasp-theme')).toBe('light')
    expect(document.documentElement.classList.contains('light')).toBe(true)

    toggleTheme()
    expect(theme.value).toBe('dark')
    expect(document.documentElement.classList.contains('light')).toBe(false)
  })

  it('applyPublicLightChrome forces html.light without persisting theme', () => {
    setTheme('dark')
    applyPublicLightChrome()
    expect(document.documentElement.classList.contains('light')).toBe(true)
    expect(document.documentElement.style.colorScheme).toBe('light')
    expect(theme.value).toBe('dark')
    expect(localStorage.getItem('grasp-theme')).toBe('dark')

    reapplyThemeChrome()
    expect(document.documentElement.classList.contains('light')).toBe(false)
    expect(document.documentElement.style.colorScheme).toBe('dark')
    expect(localStorage.getItem('grasp-theme')).toBe('dark')
  })

  it('migrates approving-theme to grasp-theme (g1.1 evidence)', () => {
    localStorage.setItem('approving-theme', 'light')
    migrateLocalStorageKey('approving-theme', 'grasp-theme')
    expect(localStorage.getItem('grasp-theme')).toBe('light')
    expect(localStorage.getItem('approving-theme')).toBeNull()
    setTheme('dark')
    expect(localStorage.getItem('grasp-theme')).toBe('dark')
    expect(localStorage.getItem('approving-theme')).toBeNull()
  })
})

describe('toggleTheme shell motion (plan g1.2 g1.3 g2.1 g2.2)', () => {
  const originalMatchMedia = window.matchMedia.bind(window)

  function matchMediaReduced(reduced: boolean) {
    window.matchMedia = ((query: string) => ({
      matches: reduced && query.includes('prefers-reduced-motion'),
      media: query,
      onchange: null,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
      dispatchEvent() {
        return false
      },
    })) as typeof window.matchMedia
  }

  beforeEach(() => {
    localStorage.clear()
    document.documentElement.classList.remove('light', 'theme-vt-capture', 'theme-color-motion')
    matchMediaReduced(false)
    delete document.startViewTransition
    setTheme('dark')
  })

  afterEach(() => {
    window.matchMedia = originalMatchMedia
    delete document.startViewTransition
    document.documentElement.classList.remove('theme-vt-capture', 'theme-color-motion')
  })

  it('persists the toggled theme (plan g2.1)', () => {
    toggleTheme()
    expect(theme.value).toBe('light')
    expect(localStorage.getItem('grasp-theme')).toBe('light')
    expect(document.documentElement.classList.contains('light')).toBe(true)
  })

  it('cross-fades with the language-menu duration and keeps the last click (plan g1.2 g2.2)', () => {
    const seen: string[] = []
    document.startViewTransition = (cb) => {
      cb()
      seen.push(document.documentElement.classList.contains('light') ? 'light' : 'dark')
      return {
        ready: Promise.resolve(undefined),
        finished: Promise.resolve(undefined),
        updateCallbackDone: Promise.resolve(undefined),
        skipTransition() {},
        types: new Set<string>(),
      }
    }
    toggleTheme()
    toggleTheme()
    toggleTheme()
    expect(seen).toEqual(['light', 'dark', 'light'])
    expect(theme.value).toBe('light')
    expect(localStorage.getItem('grasp-theme')).toBe('light')
    expect(document.documentElement.classList.contains('light')).toBe(true)
    expect(document.documentElement.classList.contains('theme-vt-capture')).toBe(true)
  })

  it('does not animate setTheme, embed override, or reduced motion (plan g1.3)', () => {
    const start = vi.fn()
    document.startViewTransition = start as unknown as typeof document.startViewTransition
    setTheme('light')
    expect(start).not.toHaveBeenCalled()
    expect(localStorage.getItem('grasp-theme')).toBe('light')

    matchMediaReduced(true)
    toggleTheme()
    expect(start).not.toHaveBeenCalled()
    expect(theme.value).toBe('dark')
    expect(localStorage.getItem('grasp-theme')).toBe('dark')
    expect(document.documentElement.classList.contains('light')).toBe(false)
    expect(document.documentElement.classList.contains('theme-vt-capture')).toBe(false)
    expect(document.documentElement.classList.contains('theme-color-motion')).toBe(false)
  })

  it('page cross-fade matches overlay tokens and does not translate the page (plan g1.2 g1.3)', () => {
    const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../../styles/global.css'), 'utf8')
    const rootBlock = css.match(/::view-transition-group\(root\)[\s\S]*?\}/)?.[0] ?? ''
    expect(rootBlock).toMatch(/animation-duration:\s*var\(--dur-overlay\)/)
    expect(rootBlock).toMatch(/var\(--ease-out-expo\)/)
    expect(rootBlock).not.toMatch(/translate|scale\(/)
    expect(css).toMatch(/@keyframes theme-icon-pop-in[\s\S]*translateY\(-4px\) scale\(0\.98\)/)
    expect(css).toMatch(/html\.theme-vt-capture \.shell-theme-icon/)
    expect(css).toMatch(/html\.theme-color-motion/)
    expect(css).toMatch(/::view-transition-old\(\*\)/)
    expect(css).toMatch(/--dur-overlay:\s*200ms/)
  })
})
