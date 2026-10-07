// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import CredentialProviderLogo from './CredentialProviderLogo.vue'

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
  })

  it('uses a neutral accessible logo for unknown providers', () => {
    const wrapper = mount(CredentialProviderLogo, {
      props: { provider: 'my-private-service', name: 'Staging key', type: 'custom' },
    })
    const logo = wrapper.get('[data-provider-logo="neutral"]')
    expect(logo.attributes('aria-label')).toBe('Credential provider logo')
    expect(logo.find('svg').exists()).toBe(true)
  })
})
