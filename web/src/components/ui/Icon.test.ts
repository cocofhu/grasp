// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { iconDefinitions, iconNames } from '@/lib/icons/catalog'
import Icon from './Icon.vue'

const wrappers: VueWrapper[] = []
function render(name: string, props: Record<string, unknown> = {}, host?: HTMLElement) {
  const wrapper = mount(Icon, { props: { name, ...props }, attachTo: host })
  wrappers.push(wrapper)
  return wrapper
}
function button(parent: HTMLElement = document.body) {
  const element = document.createElement('button')
  parent.appendChild(element)
  return element
}
async function enter(host: HTMLElement, target: HTMLElement | SVGElement = host) {
  target.dispatchEvent(new PointerEvent('pointerover', { bubbles: true }))
  await nextTick()
  await nextTick()
}
afterEach(() => {
  for (const wrapper of wrappers.splice(0)) wrapper.unmount()
  document.body.replaceChildren()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('Icon geometry and compatibility', () => {
  it('renders every registered local SVG without scripts or unresolved names', () => {
    expect(iconNames.length).toBeGreaterThanOrEqual(103)
    for (const name of iconNames) {
      const wrapper = render(name)
      expect(wrapper.find('svg').attributes('data-icon')).toBe(name)
      expect(wrapper.find('svg').element.children.length).toBeGreaterThan(0)
      expect(wrapper.find('script').exists()).toBe(false)
      expect(wrapper.find('svg').attributes('width')).toBe('18')
      expect(wrapper.find('svg').attributes('aria-hidden')).toBe('true')
    }
  })

  it('safely falls back and clears solid fill for an unknown dynamic name', async () => {
    const warning = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const wrapper = render('chat-stop')
    const svg = wrapper.get('svg')
    expect(svg.attributes('fill')).toBe('currentColor')
    await wrapper.setProps({ name: 'unknown-icon-xyz' })
    expect(wrapper.get('circle').attributes('r')).toBe('4')
    expect(svg.attributes('fill')).toBe('none')
    expect(svg.attributes('data-icon')).toBe('unknown-icon-xyz')
    expect(warning).not.toHaveBeenCalled()
  })

  it('updates names, numeric/CSS sizes and inherited classes without replacing the SVG', async () => {
    const wrapper = render('chat-send', { size: 18 })
    const svg = wrapper.get('svg')
    const element = svg.element
    await wrapper.setProps({ name: 'chat-stop', size: 24 })
    expect(svg.element).toBe(element)
    expect(svg.attributes('fill')).toBe('currentColor')
    expect(svg.attributes('stroke')).toBe('none')
    expect(svg.attributes('width')).toBe('24')
    expect(wrapper.get('rect').attributes('width')).toBe('16.5')
    expect(wrapper.get('rect').attributes('height')).toBe('16.5')
    expect(wrapper.get('rect').attributes('rx')).toBe('3')
    await wrapper.setProps({ name: 'chat-artifacts', size: '1.25em' })
    expect(svg.attributes('fill')).toBe('none')
    expect(svg.attributes('stroke-width')).toBe('1.7')
    expect(svg.attributes('height')).toBe('1.25em')
    await wrapper.setProps({ size: undefined })
    expect(svg.attributes('width')).toBe('18')
  })

  it('preserves filled pin scaling and the thinner filled-star stroke', () => {
    const pin = render('pin', { size: 12 })
    expect(pin.get('g').attributes('transform')).toBe('scale(0.0234375)')
    expect(pin.html()).toContain('M878.3 392.1')
    expect(pin.get('svg').attributes('stroke')).toBe('none')
    expect(pin.get('svg').attributes('stroke-width')).toBeUndefined()
    expect(render('star-filled').get('svg').attributes('stroke-width')).toBe('1.2')
  })

  it('draws completed checks from the left through the joint without a start dot', () => {
    const completed = render('check-draw')
    expect(completed.find('circle').exists()).toBe(false)
    expect(completed.get('path').attributes('pathLength')).toBe('1')
    expect(completed.get('path').attributes('d')).toBe('M7.6 12.4l3.6 3.6L18.2 7.4')
    expect(render('check').get('path').attributes('d')).toBe('M5 12l5 5 9-11')
  })

  it('draws equal rounded board columns and a smaller solid workflow', () => {
    const board = render('board')
    const columns = board.findAll('rect')
    expect(columns).toHaveLength(3)
    expect(columns.map((column) => column.attributes('x'))).toEqual(['3.1', '9.5', '15.9'])
    expect(columns.map((column) => column.attributes('width'))).toEqual(['5', '5', '5'])
    expect(columns.map((column) => column.attributes('y'))).toEqual(['3.6', '3.6', '3.6'])
    expect(columns.map((column) => column.attributes('height'))).toEqual(['16.8', '16.8', '16.8'])
    expect(columns.map((column) => column.attributes('rx'))).toEqual(['1.6', '1.6', '1.6'])
    const boardSvg = board.get('svg')
    expect(boardSvg.attributes('viewBox')).toBe('0 0 24 24')
    expect(boardSvg.attributes('fill')).toBe('none')
    expect(boardSvg.attributes('stroke')).toBe('currentColor')
    expect(boardSvg.attributes('stroke-width')).toBe('1.7')
    expect(boardSvg.attributes('stroke-linecap')).toBe('round')
    expect(board.html()).not.toContain('width="4"')

    const flow = render('workflow')
    expect(flow.get('.node-a').attributes()).toMatchObject({ cx: '6', cy: '6', r: '2.2', 'fill-opacity': '0' })
    expect(flow.get('.node-b').attributes()).toMatchObject({ cx: '6', cy: '18', r: '2.2', 'fill-opacity': '0' })
    expect(flow.get('.node-c').attributes()).toMatchObject({ cx: '18', cy: '12', r: '2.2', 'fill-opacity': '0' })
    for (const node of ['.node-a', '.node-b', '.node-c']) {
      expect(Number(flow.get(node).attributes('r'))).toBeLessThan(3)
    }
    const line = 'M8.2 6C12 6 14 8.2 16.45 10.45M8.2 18C12 18 14 15.8 16.45 13.55'
    const staticPath = flow.findAll('path').find((path) => !path.classes().includes('signal'))
    expect(staticPath?.attributes('d')).toBe(line)
    expect(staticPath?.attributes('opacity')).toBeUndefined()
    expect(flow.html()).not.toMatch(/opacity=["']\.55["']/)
    const signal = flow.get('.signal')
    expect(signal.attributes('d')).toBe(line)
    expect(signal.attributes('opacity')).toBe('0')
    expect(signal.attributes('stroke-width')).toBe('2.5')
    expect(signal.attributes('pathLength')).toBe('1')
    const flowSvg = flow.get('svg')
    expect(flowSvg.attributes('viewBox')).toBe('0 0 24 24')
    expect(flowSvg.attributes('fill')).toBe('none')
    expect(flowSvg.attributes('stroke')).toBe('currentColor')
    expect(flowSvg.attributes('stroke-width')).toBe('1.7')
    expect(iconDefinitions.board.duration).toBe(570)
    expect(iconDefinitions.workflow.duration).toBe(1250)

    const css = readFileSync(join(process.cwd(), 'src/styles/icon-motion.css'), 'utf8')
    expect(css).toContain('.app-icon[data-icon="board"][data-icon-motion="once"] rect:nth-of-type(1)')
    expect(css).toContain('.app-icon[data-icon="board"][data-icon-motion="once"] rect:nth-of-type(2)')
    expect(css).toContain('.app-icon[data-icon="board"][data-icon-motion="once"] rect:nth-of-type(3)')
    expect(css).toContain('.app-icon[data-icon="workflow"][data-icon-motion="once"] .node-a')
    expect(css).toContain('.app-icon[data-icon="workflow"][data-icon-motion="once"] .node-b')
    expect(css).toContain('.app-icon[data-icon="workflow"][data-icon-motion="once"] .node-c')
    expect(css).toContain('.app-icon[data-icon="workflow"][data-icon-motion="once"] .signal')
    expect(css).toContain('@media (prefers-reduced-motion: reduce)')
  })

  it('keeps generated icon copies on the tightened board and workflow geometry', () => {
    const repo = join(process.cwd(), '..')
    const copies = [
      'web/public/preview-pick.js',
      'server/internal/handlers/preview-pick.js',
      'sandbox-gateway/sandbox/internal/previewinject/preview-pick.js',
      'sandbox-gateway/sandbox/web/static/js/ui/icons.js',
      'web/public/live-overlay.js',
      'server/internal/handlers/live-overlay.js',
      'sandbox-gateway/sandbox/internal/previewinject/live-overlay.js',
    ]
    for (const file of copies) {
      const src = readFileSync(join(repo, file), 'utf8')
      expect(src, file).toContain(iconDefinitions.board.body)
      expect(src, file).toContain(iconDefinitions.workflow.body)
      expect(src, file).not.toContain('cx="6" cy="6" r="3"')
      expect(src, file).not.toContain('opacity=".55"')
      expect(src, file).not.toContain('width="4" height="16"')
    }
  })

  it('uses a punched tag, aligned logout arrow, complete puzzle, and actual flow minimap', () => {
    const tag = render('tag')
    expect(tag.get('circle').attributes('cx')).toBe('7.5')
    expect(tag.html()).not.toContain('12 22l-8.6')
    const logout = render('logout')
    expect(logout.get('.logout-arrow').html()).toContain('M10 12h10')
    expect(logout.get('.logout-arrow').html()).toContain('m16 8 4 4-4 4')
    expect(render('skills').get('.skill-piece').attributes('d')).toMatch(/Z$/)
    expect(render('minimap').find('.minimap-viewport').exists()).toBe(true)
    expect(render('review').find('.review-lens').exists()).toBe(true)
    expect(render('queue').find('.queue-hand').exists()).toBe(true)
    expect(render('image-off').get('.image-slash').attributes('d')).toBe('M3 3l18 18')
  })
})

describe('Icon motion policy', () => {
  it('keeps noninteractive metadata static and plays once per host entry', async () => {
    vi.useFakeTimers()
    const metadata = render('copy')
    await metadata.trigger('pointerover')
    expect(metadata.attributes('data-icon-motion')).toBe('idle')
    const host = button()
    const icon = render('copy', {}, host)
    expect(icon.attributes('data-icon-motion')).toBe('idle')
    await enter(host)
    expect(icon.attributes('data-icon-motion')).toBe('once')
    await vi.advanceTimersByTimeAsync(iconDefinitions.copy.duration + 40)
    expect(icon.attributes('data-icon-motion')).toBe('idle')
    await enter(host)
    expect(icon.attributes('data-icon-motion')).toBe('idle')
    host.dispatchEvent(new PointerEvent('pointerout', { bubbles: true }))
    await enter(host)
    expect(icon.attributes('data-icon-motion')).toBe('once')
  })

  it('lets the nearest nested control own feedback', async () => {
    const outer = button()
    const outerIcon = render('copy', {}, outer)
    const inner = button(outer)
    const innerIcon = render('trash', {}, inner)
    await enter(inner, innerIcon.get('svg').element)
    expect(innerIcon.attributes('data-icon-motion')).toBe('once')
    expect(outerIcon.attributes('data-icon-motion')).toBe('idle')
  })

  it('suppresses disabled decoration while allowing a real loader inside a disabled button', async () => {
    const host = button()
    host.disabled = true
    const copy = render('copy', {}, host)
    await enter(host)
    expect(copy.attributes('data-icon-motion')).toBe('idle')
    const spinner = render('spinner', {}, host)
    await nextTick()
    expect(spinner.attributes('data-icon-motion')).toBe('busy')
    expect(spinner.classes()).toContain('icon-spinner')
    await spinner.setProps({ disabled: true })
    expect(spinner.attributes('data-icon-motion')).toBe('idle')
  })

  it('cancels decoration when any ancestor becomes disabled, inert or busy', async () => {
    const ancestor = document.createElement('fieldset')
    document.body.appendChild(ancestor)
    const host = button(ancestor)
    const icon = render('copy', {}, host)
    const spinner = render('spinner', {}, host)
    for (const [attribute, value] of [['aria-disabled', 'true'], ['aria-busy', 'true'], ['inert', ''], ['disabled', '']]) {
      await enter(host)
      expect(icon.attributes('data-icon-motion')).toBe('once')
      ancestor.setAttribute(attribute, value)
      await vi.waitFor(() => expect(icon.attributes('data-icon-motion')).toBe('idle'))
      expect(spinner.attributes('data-icon-motion')).toBe('busy')
      ancestor.removeAttribute(attribute)
      host.dispatchEvent(new PointerEvent('pointerout', { bubbles: true }))
      await nextTick()
    }
  })

  it('stops decorative motion during canvas dragging while preserving a real loader', async () => {
    const node = document.createElement('div')
    node.className = 'vue-flow__node'
    document.body.appendChild(node)
    const host = button(node)
    const icon = render('copy', {}, host)
    const spinner = render('spinner', {}, host)
    await enter(host)
    expect(icon.attributes('data-icon-motion')).toBe('once')
    node.classList.add('dragging')
    await vi.waitFor(() => expect(icon.attributes('data-icon-motion')).toBe('idle'))
    expect(spinner.attributes('data-icon-motion')).toBe('busy')
    node.classList.remove('dragging')
    host.dispatchEvent(new PointerEvent('pointerout', { bubbles: true }))
    await enter(host)
    expect(icon.attributes('data-icon-motion')).toBe('once')
  })

  it('requires explicit busy for status indicators and stops when busy ends', async () => {
    const arc = render('arc-spinner')
    const breath = render('breath-dot')
    expect(arc.attributes('data-icon-motion')).toBe('idle')
    expect(breath.attributes('data-icon-motion')).toBe('idle')
    await arc.setProps({ busy: true })
    expect(arc.attributes('data-icon-motion')).toBe('busy')
    await arc.setProps({ busy: false })
    expect(arc.attributes('data-icon-motion')).toBe('idle')
  })

  it('plays changed event keys once while completed checks ignore event and mount triggers', async () => {
    const bell = render('bell', { motionKey: 1, interactive: false })
    expect(bell.attributes('data-icon-motion')).toBe('idle')
    await bell.setProps({ motionKey: 2 })
    await nextTick()
    await nextTick()
    expect(bell.attributes('data-icon-motion')).toBe('once')
    const completed = render('check-draw', { motionKey: 1 })
    await completed.setProps({ motionKey: 2 })
    await nextTick()
    expect(completed.attributes('data-icon-motion')).toBe('idle')
  })

  it('honors focus-visible, reduced motion and interactive=false', async () => {
    const host = button()
    const icon = render('copy', {}, host)
    const matches = host.matches.bind(host)
    vi.spyOn(host, 'matches').mockImplementation((selector) => selector === ':focus-visible' || matches(selector))
    host.dispatchEvent(new FocusEvent('focusin', { bubbles: true }))
    await nextTick()
    await nextTick()
    expect(icon.attributes('data-icon-motion')).toBe('once')
    await icon.setProps({ interactive: false })
    expect(icon.attributes('data-icon-motion')).toBe('idle')
    const media = { matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() } as unknown as MediaQueryList
    vi.spyOn(window, 'matchMedia').mockReturnValue(media)
    const reduced = render('spinner')
    expect(reduced.attributes('data-icon-motion')).toBe('idle')
    const css = readFileSync(join(process.cwd(), 'src/styles/icon-motion.css'), 'utf8')
    expect(css).toContain('@media (prefers-reduced-motion: reduce)')
    expect(css).not.toContain('stroke-width: 2.35')
    expect(css).not.toContain('.grasp-status-host:hover')
  })
})
