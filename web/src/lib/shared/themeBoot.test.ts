// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let loaded: typeof import('./theme') | undefined
beforeEach(() => {
  vi.resetModules()
  localStorage.clear()
  sessionStorage.clear()
  history.replaceState(null, '', '/embed/runs/first/nodes/solo/shared-chat')
  document.documentElement.removeAttribute('data-grasp-embed-theme')
})
afterEach(() => {
  loaded?.setThemeOverride(null)
  loaded = undefined
  history.replaceState(null, '', '/')
  document.documentElement.removeAttribute('data-grasp-embed-theme')
  vi.restoreAllMocks()
})

describe('theme handoff from the HTML boot shell', () => {
  it.each(['light', 'dark'] as const)('keeps the boot %s override while retaining the account preference', async theme => {
    const preference = theme === 'light' ? 'dark' : 'light'
    localStorage.setItem('grasp-theme', preference)
    document.documentElement.setAttribute('data-grasp-embed-theme', theme)
    loaded = await import('./theme')
    expect(loaded.theme.value).toBe(preference)
    expect(loaded.renderedTheme.value).toBe(theme)
    expect(document.documentElement.style.colorScheme).toBe(theme)
    loaded.setThemeOverride(preference)
    expect(sessionStorage.getItem('grasp-embed-theme:/embed/runs/first/nodes/solo/shared-chat')).toBe(preference)
    history.replaceState(null, '', '/embed/runs/second/nodes/solo/shared-chat')
    loaded.setThemeOverride(theme)
    expect(sessionStorage.getItem('grasp-embed-theme:/embed/runs/first/nodes/solo/shared-chat')).toBe(preference)
    expect(sessionStorage.getItem('grasp-embed-theme:/embed/runs/second/nodes/solo/shared-chat')).toBe(theme)
    expect(localStorage.getItem('grasp-theme')).toBe(preference)
  })

  it('ignores an embed override outside an embedded page', async () => {
    history.replaceState(null, '', '/')
    localStorage.setItem('grasp-theme', 'dark')
    document.documentElement.setAttribute('data-grasp-embed-theme', 'light')
    loaded = await import('./theme')
    expect(loaded.renderedTheme.value).toBe('dark')
    expect(document.documentElement.classList.contains('light')).toBe(false)
  })

  it('still applies the host theme when per-preview storage is blocked', async () => {
    loaded = await import('./theme')
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    expect(() => loaded!.setThemeOverride('light')).not.toThrow()
    expect(loaded.renderedTheme.value).toBe('light')
    expect(document.documentElement.style.colorScheme).toBe('light')
  })
})
