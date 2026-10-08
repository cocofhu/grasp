// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, describe, expect, it } from 'vitest'
import CredentialProviderLogo from './CredentialProviderLogo.vue'
import { setTheme } from '@/lib/shared/theme'

afterEach(() => {
  setTheme('dark')
})

describe('CredentialProviderLogo', () => {
  it.each([
    ['Claude Code', { envKey: 'GRASP_CLAUDE_API_KEY' }, 'claude'],
    ['CodeBuddy', { provider: 'codebuddy' }, 'codebuddy'],
    ['Codex', { envKey: 'GRASP_CODEX_AUTH_JSON' }, 'codex'],
    ['Cursor', { provider: 'cursor' }, 'cursor'],
    ['Trae', { provider: 'trae' }, 'trae'],
    ['OpenCode', { provider: 'opencode' }, 'opencode'],
    ['GitHub', { provider: 'github' }, 'github'],
    ['GitLab', { provider: 'gitlab' }, 'gitlab'],
  ])('maps %s to a local logo', (_name, props, key) => {
    const wrapper = mount(CredentialProviderLogo, { props })
    const logo = wrapper.get('[data-provider-logo]')
    expect(logo.attributes('data-provider-logo')).toBe(key)
    expect(logo.attributes('role')).toBe('img')
    expect(logo.attributes('aria-label')).toContain('logo')
    expect(logo.get('img').attributes('data-logo-source')).toBe('bundled-brand-asset')
    expect(logo.get('img').attributes('src')).toMatch(/^data:image\/svg\+xml|\.svg(?:$|\?)/)
  })

  it('uses a neutral accessible logo for unknown providers', () => {
    const wrapper = mount(CredentialProviderLogo, {
      props: { provider: 'my-private-service', name: 'Staging key', type: 'custom' },
    })
    const logo = wrapper.get('[data-provider-logo="neutral"]')
    expect(logo.attributes('aria-label')).toBe('Credential provider logo')
    expect(logo.find('svg').exists()).toBe(true)
    expect(logo.find('[data-logo-source]').exists()).toBe(false)
  })

  it('uses the official OpenAI mark for Codex and model vendors', () => {
    const codex = mount(CredentialProviderLogo, { props: { provider: 'codex' } })
    const openai = mount(CredentialProviderLogo, { props: { provider: 'openai' } })

    expect(codex.get('img').attributes('data-logo-asset')).toBe('codex')
    expect(openai.get('img').attributes('data-logo-asset')).toBe('openai')
  })

  it('uses an opaque surface for configured Cursor and GitHub logos', () => {
    for (const provider of ['cursor', 'github']) {
      const wrapper = mount(CredentialProviderLogo, { props: { provider, configured: true } })
      const logo = wrapper.get('[data-provider-logo]')
      const classes = logo.classes().join(' ')
      expect(logo.classes()).toEqual(expect.arrayContaining(['bg-surface', 'border-line']))
      expect(classes).not.toMatch(/bg-ok|border-ok/)
      wrapper.unmount()
    }
  })

  it('keeps the surface fill and only emphasizes the border when selected', () => {
    const wrapper = mount(CredentialProviderLogo, {
      props: { provider: 'cursor', configured: true, selected: true },
    })
    const logo = wrapper.get('[data-provider-logo]')
    const classes = logo.classes().join(' ')
    expect(logo.classes()).toEqual(expect.arrayContaining(['bg-surface', 'border-accent']))
    expect(classes).not.toMatch(/bg-ok|border-ok/)
    wrapper.unmount()
  })

  it('still inverts a configured monochrome mark only in the dark theme', async () => {
    setTheme('dark')
    const wrapper = mount(CredentialProviderLogo, { props: { provider: 'github', configured: true } })
    expect(wrapper.get('img').classes()).toContain('provider-logo-image--invert')
    setTheme('light')
    await nextTick()
    expect(wrapper.get('img').classes()).not.toContain('provider-logo-image--invert')
    wrapper.unmount()
  })

  it('inverts every bundled monochrome mark only in the dark theme', async () => {
    setTheme('dark')
    const wrappers = [
      'claude',
      'codebuddy',
      'codex',
      'cursor',
      'trae',
      'opencode',
      'github',
      'gitlab',
      'openai',
      'deepseek',
      'openrouter',
      'xai',
    ].map((provider) => mount(CredentialProviderLogo, { props: { provider } }))

    expect(wrappers.every((wrapper) => wrapper.get('img').classes().includes('provider-logo-image--invert'))).toBe(true)

    setTheme('light')
    await nextTick()
    expect(wrappers.every((wrapper) => !wrapper.get('img').classes().includes('provider-logo-image--invert'))).toBe(true)

    setTheme('dark')
    await nextTick()
    expect(wrappers.every((wrapper) => wrapper.get('img').classes().includes('provider-logo-image--invert'))).toBe(true)
    wrappers.forEach((wrapper) => wrapper.unmount())
  })
})
