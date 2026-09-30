import { selectorPath, visibleText } from './describe'
import type { Strings } from './i18n'

export type LivePoint = { x: number; y: number }
export type LiveMark = {
  kind: 'draw' | 'note'
  points: LivePoint[]
  text?: string
  targets?: Array<{ selector: string; text?: string }>
}

const MAX_MARKS = 8
const SVG_NS = 'http://www.w3.org/2000/svg'
const CSS = '.live-marks{position:fixed;z-index:2147483644;pointer-events:none;outline:2px solid #0f766e;border-radius:5px}' +
  '.live-marks svg{position:absolute;inset:0;width:100%;height:100%;overflow:visible}' +
  '.live-marks[data-editing] svg{pointer-events:auto;touch-action:none;cursor:crosshair}' +
  '.live-mark-note{position:absolute;display:flex;align-items:start;gap:5px;pointer-events:auto;max-width:240px}' +
  '.live-mark-note b{background:#f59e0b;color:#18181b;border:2px solid #fff;border-radius:50%;min-width:22px;height:22px;text-align:center}' +
  '.live-mark-note textarea{font:12px/1.4 system-ui;color:#fafafa;background:#18181b;border:1px solid #52525b;border-radius:6px;padding:6px;width:180px;max-width:35vw;resize:vertical;min-height:38px}'

/** Marks live only in the shadow overlay; source/preview DOM is never painted. */
export function createAnnotations(layer: HTMLElement, T: Strings, changed: () => void) {
  let target: Element | null = null
  let marks: LiveMark[] = []
  let mode: 'draw' | 'note' | null = null
  let active: LiveMark | null = null
  let pointerID: number | null = null
  layer.innerHTML = `<style>${CSS}</style><div class="live-marks" hidden><svg viewBox="0 0 1000 1000" preserveAspectRatio="none" aria-label="${T.markCanvas}"></svg><div data-mark-notes></div></div>`
  const box = layer.querySelector<HTMLElement>('.live-marks')!
  const svg = layer.querySelector<SVGSVGElement>('svg')!
  const notes = layer.querySelector<HTMLElement>('[data-mark-notes]')!

  function layout() {
    const rect = target?.getBoundingClientRect()
    box.hidden = !target?.isConnected || !rect?.width || !rect?.height
    if (!rect || box.hidden) return
    Object.assign(box.style, { left: `${rect.left}px`, top: `${rect.top}px`, width: `${rect.width}px`, height: `${rect.height}px` })
  }

  function point(e: PointerEvent): LivePoint | null {
    const rect = target?.getBoundingClientRect()
    if (!target?.isConnected || !rect?.width || !rect?.height) return null
    const clamp = (n: number) => Math.round(Math.max(0, Math.min(1, n)) * 1000) / 1000
    return { x: clamp((e.clientX - rect.left) / rect.width), y: clamp((e.clientY - rect.top) / rect.height) }
  }

  function targetsFor(mark: LiveMark): NonNullable<LiveMark['targets']> {
    if (!target) return []
    const rect = target.getBoundingClientRect()
    const xs = mark.points.map((p) => rect.left + p.x * rect.width)
    const ys = mark.points.map((p) => rect.top + p.y * rect.height)
    const left = Math.min(...xs), right = Math.max(...xs), top = Math.min(...ys), bottom = Math.max(...ys)
    // Smallest intersecting descendants identify the circled control, even
    // when the pointer itself traces the empty space around that control.
    return [target, ...Array.from(target.querySelectorAll('*')).slice(0, 300)]
      .map((el) => ({ el, rect: el.getBoundingClientRect() }))
      .filter(({ rect: r }) => r.width > 0 && r.height > 0 && r.right >= left && r.left <= right && r.bottom >= top && r.top <= bottom)
      .sort((a, b) => a.rect.width * a.rect.height - b.rect.width * b.rect.height)
      .slice(0, 4)
      .map(({ el }) => ({ selector: selectorPath(el), text: visibleText(el) }))
  }

  function paintPaths() {
    svg.replaceChildren()
    for (const mark of marks) {
      if (mark.kind !== 'draw') continue
      const path = document.createElementNS(SVG_NS, 'polyline')
      path.setAttribute('points', mark.points.map((p) => `${p.x * 1000},${p.y * 1000}`).join(' '))
      path.setAttribute('fill', 'none')
      path.setAttribute('stroke', '#0f766e')
      path.setAttribute('stroke-width', '4')
      path.setAttribute('stroke-linecap', 'round')
      path.setAttribute('stroke-linejoin', 'round')
      path.setAttribute('vector-effect', 'non-scaling-stroke')
      svg.append(path)
    }
  }

  function render(focus?: LiveMark) {
    box.toggleAttribute('data-editing', !!mode && marks.length < MAX_MARKS)
    paintPaths()
    notes.replaceChildren()
    marks.forEach((mark, i) => {
      if (mark.kind !== 'note') return
      const pin = document.createElement('div')
      pin.className = 'live-mark-note'
      pin.style.left = `${mark.points[0].x * 100}%`
      pin.style.top = `${mark.points[0].y * 100}%`
      if (mark.points[0].x > 0.6) pin.style.transform = 'translateX(-100%)'
      const label = document.createElement('b')
      label.textContent = String(i + 1)
      const input = document.createElement('textarea')
      input.dataset.markNote = String(i)
      input.setAttribute('aria-label', `${T.markNote} ${i + 1}`)
      input.placeholder = T.markNotePrompt
      input.maxLength = 300
      input.value = mark.text || ''
      input.addEventListener('input', () => { mark.text = input.value })
      pin.append(label, input)
      notes.append(pin)
      if (mark === focus) input.focus()
    })
    layout()
  }

  svg.addEventListener('pointerdown', (e) => {
    if (!target || !mode || marks.length >= MAX_MARKS || e.button !== 0 || pointerID !== null) return
    e.preventDefault()
    e.stopPropagation()
    const start = point(e)
    if (!start) return
    const mark: LiveMark = { kind: mode, points: [start] }
    marks.push(mark)
    if (mode === 'note') {
      mark.targets = targetsFor(mark)
      mode = null
      render(mark)
      changed()
    } else {
      active = mark
      pointerID = e.pointerId
      svg.setPointerCapture?.(e.pointerId)
      paintPaths()
    }
  })
  svg.addEventListener('pointermove', (e) => {
    if (!active || e.pointerId !== pointerID) return
    e.preventDefault()
    const p = point(e), last = active.points.at(-1)!
    if (!p) {
      marks = marks.filter((m) => m !== active)
      active = null
      pointerID = null
      render()
      changed()
      return
    }
    if (Math.hypot(p.x - last.x, p.y - last.y) < 0.002) return
    if (active.points.length >= 80) active.points = active.points.filter((_, i) => i % 2 === 0)
    active.points.push(p)
    paintPaths()
  })
  function finish(e: PointerEvent) {
    if (!active || e.pointerId !== pointerID) return
    if (e.type === 'pointercancel' || !point(e) || active.points.length < 2) marks = marks.filter((m) => m !== active)
    else active.targets = targetsFor(active)
    active = null
    pointerID = null
    render()
    changed()
  }
  svg.addEventListener('pointerup', finish)
  svg.addEventListener('pointercancel', finish)
  svg.addEventListener('lostpointercapture', finish)

  return {
    layout,
    get mode() { return mode },
    get count() { return marks.length },
    setMode(next: 'draw' | 'note') { mode = mode === next ? null : next; render() },
    setTarget(el: Element | null) {
      if (target === el) { layout(); return }
      target = el
      marks = []
      mode = null
      active = null
      pointerID = null
      render()
    },
    undo() { marks.pop(); active = null; pointerID = null; render() },
    clear() { marks = []; active = null; pointerID = null; render() },
    snapshot(): LiveMark[] { return JSON.parse(JSON.stringify(marks.filter((m) => m.kind === 'draw' ? m.points.length >= 2 : !!m.text?.trim()))) as LiveMark[] },
  }
}
