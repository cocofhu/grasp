// @vitest-environment happy-dom
import { afterEach, describe, expect, it } from 'vitest'
import { createAnnotations } from './annotations'
import { strings } from './i18n'

function setup() {
  document.body.innerHTML = '<section id="hero"><button id="book">Book now</button></section><div id="host"></div>'
  const hero = document.getElementById('hero')!
  const button = document.getElementById('book')!
  hero.getBoundingClientRect = () => new DOMRect(20, 40, 400, 200)
  button.getBoundingClientRect = () => new DOMRect(60, 100, 100, 40)
  const layer = document.createElement('div')
  document.getElementById('host')!.attachShadow({ mode: 'open' }).append(layer)
  const annotations = createAnnotations(layer, strings('en'), () => {})
  annotations.setTarget(hero)
  const svg = layer.querySelector('svg')!
  const pointer = (type: string, x: number, y: number) => svg.dispatchEvent(new PointerEvent(type, { pointerId: 1, clientX: x, clientY: y, button: 0, bubbles: true }))
  return { annotations, hero, layer, svg, pointer }
}

afterEach(() => { document.body.innerHTML = '' })

describe('Live visual annotations', () => {
  it('captures a freehand circle, identifies the enclosed control and keeps source untouched', () => {
    const { annotations, hero, layer, pointer } = setup()
    const source = hero.outerHTML
    annotations.setMode('draw')
    pointer('pointerdown', 50, 90)
    for (const [x, y] of [[170, 90], [170, 150], [50, 150], [50, 90]]) pointer('pointermove', x, y)
    pointer('pointerup', 50, 90)
    const [mark] = annotations.snapshot()
    expect(mark.kind).toBe('draw')
    expect(mark.points[0]).toEqual({ x: 0.075, y: 0.25 })
    expect(mark.targets?.[0]).toEqual({ selector: 'button#book', text: 'Book now' })
    expect(layer.querySelectorAll('polyline')).toHaveLength(1)
    expect(hero.outerHTML).toBe(source)
    // A request gets a copy; subsequent edits/clear cannot alter it.
    annotations.clear()
    expect(mark.points).toHaveLength(5)
    expect(annotations.snapshot()).toEqual([])
    expect(layer.querySelectorAll('polyline')).toHaveLength(0)
  })

  it('anchors an editable comment and preserves its position on scroll/resize', () => {
    const { annotations, hero, layer, pointer } = setup()
    annotations.setMode('note')
    pointer('pointerdown', 100, 120)
    const input = layer.querySelector('textarea')!
    input.value = 'Match the suites below <script>'
    input.dispatchEvent(new Event('input'))
    expect(annotations.snapshot()[0]).toMatchObject({ kind: 'note', points: [{ x: 0.2, y: 0.4 }], text: input.value })
    expect(layer.querySelector('script')).toBeNull()
    hero.getBoundingClientRect = () => new DOMRect(40, 70, 800, 400)
    annotations.layout()
    const box = layer.querySelector<HTMLElement>('.live-marks')!
    expect(box.style.left).toBe('40px')
    expect(box.style.width).toBe('800px')
    expect(layer.querySelector<HTMLElement>('.live-mark-note')!.style.left).toBe('20%')
    annotations.undo()
    expect(annotations.snapshot()).toEqual([])
    expect(layer.querySelector('textarea')).toBeNull()
  })

  it('bounds strokes and mark counts, drops cancelled strokes, and clears on target removal', () => {
    const { annotations, layer, pointer } = setup()
    annotations.setMode('draw')
    pointer('pointerdown', 0, 0)
    for (let i = 0; i < 200; i++) pointer('pointermove', 20 + i * 2, 80 + (i % 20))
    pointer('pointerup', 1000, 1000)
    expect(annotations.snapshot()[0].points.length).toBeLessThanOrEqual(80)
    expect(annotations.snapshot()[0].points[0]).toEqual({ x: 0, y: 0 })
    pointer('pointerdown', 40, 60)
    pointer('pointermove', 50, 80)
    pointer('pointercancel', 50, 80)
    expect(annotations.count).toBe(1)
    for (let i = 0; i < 10; i++) {
      pointer('pointerdown', 40, 60)
      pointer('pointermove', 50, 80)
      pointer('pointerup', 50, 80)
    }
    expect(annotations.count).toBe(8)
    annotations.setTarget(null)
    expect(annotations.snapshot()).toEqual([])
    expect(layer.querySelector<HTMLElement>('.live-marks')!.hidden).toBe(true)
  })

  it('drops an unfinished stroke when HMR removes or collapses its target', () => {
    const { annotations, hero, pointer } = setup()
    annotations.setMode('draw')
    pointer('pointerdown', 40, 60)
    pointer('pointermove', 60, 80)
    hero.getBoundingClientRect = () => new DOMRect(0, 0, 0, 0)
    pointer('pointermove', 60, 80)
    pointer('pointerup', 60, 80)
    expect(annotations.snapshot()).toEqual([])
    hero.remove()
    pointer('pointerdown', 40, 60)
    expect(annotations.count).toBe(0)
  })
})
