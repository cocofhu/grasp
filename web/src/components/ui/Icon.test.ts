// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import Icon from './Icon.vue'

describe('Icon', () => {
  it('renders svg for known icon name', () => {
    const wrapper = mount(Icon, { props: { name: 'check', size: 16 } })
    expect(wrapper.find('svg').exists()).toBe(true)
    wrapper.unmount()
  })

  it('falls back for unknown icon', () => {
    const wrapper = mount(Icon, { props: { name: 'unknown-icon-xyz' } })
    expect(wrapper.find('svg').exists()).toBe(true)
    wrapper.unmount()
  })

  it('renders lock glyph used by 403 states', () => {
    const wrapper = mount(Icon, { props: { name: 'lock', size: 18 } })
    expect(wrapper.find('svg').exists()).toBe(true)
    expect(wrapper.html()).toMatch(/rect|M8 11V8/)
    wrapper.unmount()
  })

  it('renders the crossed-out eye used by hide actions', () => {
    const wrapper = mount(Icon, { props: { name: 'eye-off', size: 14 } })
    expect(wrapper.findAll('path')).toHaveLength(4)
    expect(wrapper.html()).toContain('M1 1l22 22')
    wrapper.unmount()
  })

  it('renders panel-left glyph used to hide desktop nav (g2.1 / g5.1)', () => {
    const wrapper = mount(Icon, { props: { name: 'panel-left', size: 18 } })
    expect(wrapper.find('svg').exists()).toBe(true)
    expect(wrapper.html()).toMatch(/rx="2"|M9 3v18|m16 15-3-3 3-3/)
    expect(wrapper.html()).not.toMatch(/M9 4v16/)
    wrapper.unmount()
  })

  it('spinner is a dual-tone ring (track circle + 90° arc), not the old bubble-like 270° arc (plan g1.1)', () => {
    const wrapper = mount(Icon, { props: { name: 'spinner', size: 18 } })
    const html = wrapper.html()
    expect(html).toMatch(/<circle[^>]*cx="12"[^>]*cy="12"[^>]*r="9"/)
    expect(html).toMatch(/M21 12a9 9 0 0 1-9 9/)
    expect(html).not.toMatch(/M12 3a9 9 0 1 0 9 9/)
    // Must not look like chat bubble (no tail path).
    expect(html).not.toMatch(/M21 12a8 8 0 0 1-11\.3 7\.3/)
    expect(html).not.toMatch(/L4 20/)
    expect(wrapper.find('svg').classes()).toContain('icon-spinner')
    wrapper.unmount()
  })

  it('keeps shared doc, terminal, and globe paths while adding stage glyphs', () => {
    const doc = mount(Icon, { props: { name: 'doc' } })
    expect(doc.html()).toContain('M14 3H7')
    expect(doc.html()).toContain('M14 3v5h5')
    doc.unmount()

    const terminal = mount(Icon, { props: { name: 'terminal' } })
    expect(terminal.html()).toContain('M7 9l3 3-3 3M13 15h4')
    terminal.unmount()

    const globe = mount(Icon, { props: { name: 'globe' } })
    expect(globe.html()).toContain('r="10"')
    globe.unmount()

    const ide = mount(Icon, { props: { name: 'stage-ide' } })
    expect(ide.html()).toContain('M8.2 8v12.4')
    expect(ide.html()).toContain('M11 12.2h6')
    expect(ide.html()).toContain('M11 15.4h4')
    expect(ide.find('svg').attributes('fill')).toBe('none')
    ide.unmount()

    const stageTerminal = mount(Icon, { props: { name: 'stage-terminal' } })
    expect(stageTerminal.html()).toContain('M7.1 9.3 10.3 12 7.1 14.7')
    expect(stageTerminal.html()).toContain('M12.2 14.8h4.6')
    stageTerminal.unmount()

    const browser = mount(Icon, { props: { name: 'stage-browser' } })
    expect(browser.html()).toMatch(/<circle[^>]*\sr="8"/)
    expect(browser.html()).toContain('M4 12h16')
    browser.unmount()

    const log = mount(Icon, { props: { name: 'stage-log' } })
    const dots = log.findAll('circle')
    expect(dots).toHaveLength(3)
    for (const dot of dots) {
      expect(dot.attributes('r')).toBe('1.15')
      expect(dot.attributes('fill')).toBe('currentColor')
      expect(dot.attributes('stroke')).toBe('none')
    }
    expect(log.find('svg').attributes('fill')).toBe('none')
    expect(log.html()).toContain('M9.3 12h6.4')
    log.unmount()
  })

  it('spinner root SVG spins around center without inline wobble (plan g1.2)', () => {
    const wrapper = mount(Icon, { props: { name: 'spinner', size: 18 } })
    const svg = wrapper.find('svg')
    expect(svg.classes()).toContain('icon-spinner')
    expect(svg.attributes('style')).toMatch(/display:\s*block/)
    expect(svg.attributes('style')).toMatch(/transform-origin:\s*center/)
    wrapper.unmount()
  })

  it('updates a mounted chat icon between send, solid stop, artifacts, and spinner', async () => {
    const wrapper = mount(Icon, { props: { name: 'chat-send', size: 18 }, attrs: { class: 'text-accent-2' } })
    const svg = wrapper.find('svg')
    const element = svg.element
    const sendPath = wrapper.find('path').attributes('d')
    expect(svg.classes()).toEqual(expect.arrayContaining(['app-icon', 'text-accent-2']))
    expect(svg.attributes('data-icon')).toBe('chat-send')
    expect(svg.attributes('fill')).toBe('none')
    expect(svg.attributes('stroke-width')).toBe('1.7')
    expect(svg.attributes('stroke-linecap')).toBe('round')

    await wrapper.setProps({ name: 'chat-stop' })
    expect(wrapper.find('svg').element).toBe(element)
    expect(svg.attributes('data-icon')).toBe('chat-stop')
    expect(svg.attributes('fill')).toBe('currentColor')
    expect(wrapper.find('path').exists()).toBe(false)
    expect(wrapper.find('rect').attributes('rx')).toBe('2')

    await wrapper.setProps({ name: 'chat-artifacts' })
    expect(svg.attributes('data-icon')).toBe('chat-artifacts')
    expect(svg.attributes('fill')).toBe('none')
    expect(svg.attributes('stroke-width')).toBe('1.7')
    expect(wrapper.find('rect').exists()).toBe(true)
    expect(wrapper.find('path').exists()).toBe(true)
    expect(wrapper.find('path').attributes('d')).not.toBe(sendPath)

    await wrapper.setProps({ name: 'spinner' })
    expect(svg.classes()).toEqual(expect.arrayContaining(['app-icon', 'icon-spinner']))
    expect(wrapper.find('circle').exists()).toBe(true)
    expect(svg.attributes('style')).toMatch(/transform-origin:\s*center/)

    await wrapper.setProps({ name: 'chat-send' })
    expect(svg.classes()).toContain('app-icon')
    expect(svg.classes()).not.toContain('icon-spinner')
    expect(svg.attributes('style') || '').not.toMatch(/transform-origin/)
    expect(wrapper.find('path').attributes('d')).toBe(sendPath)
    expect(wrapper.find('circle').exists()).toBe(false)
    wrapper.unmount()
  })

  it('updates numeric and CSS sizes without remounting and restores the default', async () => {
    const wrapper = mount(Icon, { props: { name: 'check' } })
    const svg = wrapper.find('svg')
    const element = svg.element
    expect(svg.attributes('width')).toBe('18')
    expect(svg.attributes('height')).toBe('18')

    await wrapper.setProps({ size: 24 })
    expect(svg.element).toBe(element)
    expect(svg.attributes('width')).toBe('24')
    expect(svg.attributes('height')).toBe('24')

    await wrapper.setProps({ size: '1.25em' })
    expect(svg.attributes('width')).toBe('1.25em')
    expect(svg.attributes('height')).toBe('1.25em')

    await wrapper.setProps({ size: undefined })
    expect(svg.attributes('width')).toBe('18')
    expect(svg.attributes('height')).toBe('18')
    wrapper.unmount()
  })

  it('renders pin as a filled PushpinOutlined with no stroke (plan g2.1)', () => {
    const wrapper = mount(Icon, { props: { name: 'pin', size: 12 } })
    const html = wrapper.html()
    const svg = wrapper.find('svg')
    const path = wrapper.find('path')
    expect(html).not.toContain('M14.4 2.8')
    expect(html).toContain('M878.3 392.1')
    expect(html).toContain('scale(0.0234375)')
    expect(svg.attributes('viewBox')).toBe('0 0 24 24')
    expect(svg.attributes('width')).toBe('12')
    expect(svg.attributes('fill')).toBe('currentColor')
    expect(svg.attributes('stroke')).toBe('none')
    expect(svg.attributes('stroke-width')).toBeUndefined()
    expect(path.attributes('fill')).toBe('currentColor')
    expect(path.attributes('stroke')).toBe('none')
    wrapper.unmount()

    const filled = mount(Icon, { props: { name: 'star-filled', size: 12 } })
    expect(filled.find('svg').attributes('stroke')).toBe('currentColor')
    expect(filled.find('svg').attributes('stroke-width')).toBe('1.2')
    filled.unmount()
  })

  it('replaces a known solid icon with the unknown-name fallback', async () => {
    const fallback = mount(Icon, { props: { name: 'unknown-icon-xyz' } })
    const wrapper = mount(Icon, { props: { name: 'chat-stop' } })
    await wrapper.setProps({ name: 'unknown-icon-xyz' })
    expect(wrapper.find('svg').element.innerHTML).toBe(fallback.find('svg').element.innerHTML)
    expect(wrapper.find('svg').attributes('data-icon')).toBe('unknown-icon-xyz')
    expect(wrapper.find('svg').attributes('fill')).toBe('none')
    wrapper.unmount()
    fallback.unmount()
  })
})
