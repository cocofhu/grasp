// @vitest-environment happy-dom
import { h } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SettingsPageHeader from './SettingsPageHeader.vue'
import SettingsStatStrip from './SettingsStatStrip.vue'
import SettingsSection from './SettingsSection.vue'
import SettingsRow from './SettingsRow.vue'

describe('SettingsPageHeader', () => {
  it('renders a static emblem icon, title, description and actions', () => {
    const wrapper = mount(SettingsPageHeader, {
      props: { icon: 'key', title: 'Credentials', description: 'Manage keys' },
      slots: { actions: '<button data-testid="action">Add</button>' },
    })
    const icon = wrapper.get('[data-testid="settings-page-header-emblem"] svg.app-icon')
    expect(icon.attributes('data-icon')).toBe('key')
    expect(icon.attributes('data-icon-motion')).toBe('idle')
    expect(wrapper.get('[data-testid="settings-page-header-emblem"]').attributes('aria-hidden')).toBe('true')
    expect(wrapper.get('h1').text()).toBe('Credentials')
    expect(wrapper.text()).toContain('Manage keys')
    expect(wrapper.get('.settings-page-header-actions [data-testid="action"]').text()).toBe('Add')
  })

  it('omits the description and actions when absent and supports a second-level heading', () => {
    const wrapper = mount(SettingsPageHeader, { props: { icon: 'settings', title: 'General', level: 2 } })
    expect(wrapper.find('h1').exists()).toBe(false)
    expect(wrapper.get('h2').text()).toBe('General')
    expect(wrapper.find('.settings-page-header-desc').exists()).toBe(false)
    expect(wrapper.find('.settings-page-header-actions').exists()).toBe(false)
  })
})

describe('SettingsStatStrip', () => {
  const items = [
    { key: 'total', icon: 'key', label: 'Total', value: 7, tone: 'accent' as const },
    { key: 'ready', icon: 'check', label: 'Ready', value: 6, total: 7, tone: 'ok' as const },
    { key: 'memory', icon: 'server', label: 'Memory', value: 8192, unit: 'MiB' },
    { key: 'pending', icon: 'warning', label: 'Pending', value: 1, tone: 'warn' as const },
    { key: 'missing', icon: 'chart', label: 'Missing' },
  ]

  it('renders a description list, hides items without data and applies tones', () => {
    const wrapper = mount(SettingsStatStrip, { props: { items } })
    expect(wrapper.element.tagName).toBe('DL')
    expect(wrapper.findAll('dt').map((dt) => dt.text())).toEqual(['Total', 'Ready', 'Memory', 'Pending'])
    expect(wrapper.find('[data-testid="settings-stat-missing"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="settings-stat-ready"]').text()).toContain('/ 7')
    expect(wrapper.get('[data-testid="settings-stat-memory"]').text()).toContain('MiB')
    expect(wrapper.get('[data-testid="settings-stat-total"]').classes()).toContain('settings-stat--accent')
    expect(wrapper.get('[data-testid="settings-stat-ready"]').classes()).toContain('settings-stat--ok')
    expect(wrapper.get('[data-testid="settings-stat-pending"]').classes()).toContain('settings-stat--warn')
    expect(wrapper.get('[data-testid="settings-stat-memory"]').classes()).toContain('settings-stat--neutral')
    const icons = wrapper.findAll('svg.app-icon')
    expect(icons).toHaveLength(4)
    expect(icons.every((icon) => icon.attributes('data-icon-motion') === 'idle')).toBe(true)
    expect(wrapper.find('[tabindex]').exists()).toBe(false)
    expect(wrapper.find('[role="button"]').exists()).toBe(false)
  })

  it('shows skeleton cells instead of zeros while loading', () => {
    const wrapper = mount(SettingsStatStrip, { props: { items, loading: true } })
    expect(wrapper.attributes('aria-hidden')).toBe('true')
    expect(wrapper.findAll('[data-testid="settings-stat-skeleton"]')).toHaveLength(4)
    expect(wrapper.find('dl').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('7')
  })

  it('renders nothing when no item has data and marks text values', () => {
    expect(mount(SettingsStatStrip, { props: { items: [items[4]!] } }).find('[data-testid="settings-stat-strip"]').exists()).toBe(false)
    const text = mount(SettingsStatStrip, { props: { items: [{ key: 'mode', icon: 'terminal', label: 'Mode', value: 'Sandbox' }] } })
    expect(text.get('.settings-stat-value').classes()).toContain('settings-stat-value--text')
  })
})

describe('SettingsSection', () => {
  it('renders a card header with an optional static icon and count', () => {
    const wrapper = mount(SettingsSection, {
      props: { title: 'Packs', description: 'Open capabilities', icon: 'layers', count: '3 / 5' },
      slots: { default: () => [h(SettingsRow, { label: 'A' }), h(SettingsRow, { label: 'B' })], footer: 'Read only' },
    })
    expect(wrapper.get('h2').text()).toBe('Packs')
    expect(wrapper.get('[data-testid="settings-section-count"]').text()).toBe('3 / 5')
    expect(wrapper.get('[data-testid="settings-section-icon"]').attributes('data-icon')).toBe('layers')
    expect(wrapper.get('[data-testid="settings-section-icon"]').element.tagName.toLowerCase()).toBe('svg')
    expect(wrapper.findAll('.settings-row')).toHaveLength(2)
    expect(wrapper.get('.settings-section-footer').text()).toBe('Read only')
  })

  it('keeps the plain header when no icon or count is passed', () => {
    const wrapper = mount(SettingsSection, { props: { title: 'Plain' } })
    expect(wrapper.find('[data-testid="settings-section-icon"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="settings-section-count"]').exists()).toBe(false)
    expect(wrapper.find('.settings-section-footer').exists()).toBe(false)
  })
})
