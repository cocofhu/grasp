// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { applyParam, paramDefault, parseParams, readWrapper, scanWrappers, setVariantVisible, showVariant } from './scan'
import { describeElement, selectorPath, visibleText } from './describe'
import { dropView, getView, putView } from './viewStore'

afterEach(() => {
  document.body.innerHTML = ''
  sessionStorage.clear()
  vi.unstubAllGlobals()
})

describe('scan', () => {
  it('reads wrappers, variants, labels and params', () => {
    document.body.innerHTML =
      '<div data-grasp-live="sid001" style="display:contents">' +
      '<section data-grasp-variant="0" hidden>orig</section>' +
      '<section data-grasp-variant="2" data-grasp-variant-label="紧凑" hidden>b</section>' +
      `<section data-grasp-variant="1" data-grasp-variant-label="层级" data-grasp-params='[{"id":"gap","kind":"range","min":8,"max":48,"unit":"px","default":24}]'>a</section>` +
      '<p>not a variant</p><i data-grasp-variant="x">bad</i></div>' +
      '<div data-grasp-live="bad sid"></div>'
    const ws = scanWrappers()
    expect(ws).toHaveLength(1)
    const w = ws[0]
    expect(w.sid).toBe('sid001')
    expect(w.original?.textContent).toBe('orig')
    expect(w.variants.map((v) => [v.n, v.label])).toEqual([
      [1, '层级'],
      [2, '紧凑'],
    ])
    expect(w.variants[0].params[0]).toMatchObject({ id: 'gap', kind: 'range', unit: 'px' })
    showVariant(w, 2)
    expect(w.variants[0].el.hidden).toBe(true)
    expect(w.variants[1].el.hidden).toBe(false)
    expect(w.original?.hidden).toBe(true)
    showVariant(w, 0)
    expect(w.original?.hidden).toBe(false)
    expect(readWrapper(document.createElement('div'))).toBeNull()
  })

  it('parses and applies params defensively', () => {
    expect(parseParams(null)).toEqual([])
    expect(parseParams('{')).toEqual([])
    expect(parseParams('{}')).toEqual([])
    const ps = parseParams(
      JSON.stringify([
        { id: 'gap', kind: 'range', min: 0, max: 10, step: 2, unit: 'rem', label: '间距' },
        { id: 'tone', kind: 'steps', options: [{ value: 'soft', label: '柔和' }, { value: 'strong' }, { bad: 1 }] },
        { id: 'shadow', kind: 'toggle' },
        { id: 'unit', kind: 'range', min: 0, max: 1, unit: 'px;color:red' },
        { id: 'Bad Id', kind: 'range', min: 0, max: 1 },
        { id: 'nomin', kind: 'range' },
        { id: 'noopts', kind: 'steps' },
        null,
      ]),
    )
    expect(ps.map((p) => p.id)).toEqual(['gap', 'tone', 'shadow', 'unit'])
    expect(ps[3].unit).toBeUndefined()
    const el = document.createElement('div')
    applyParam(el, ps[0], 4)
    expect(el.style.getPropertyValue('--gp-gap')).toBe('4rem')
    applyParam(el, ps[1], 'strong')
    expect(el.getAttribute('data-gp-tone')).toBe('strong')
    expect(paramDefault(ps[0])).toBe(0)
    expect(paramDefault(ps[1])).toBe('soft')
    expect(paramDefault(ps[2])).toBe('off')
    expect(paramDefault({ id: 'x', kind: 'steps' })).toBe('')
    expect(paramDefault({ id: 'x', kind: 'range', default: 3 })).toBe(3)
  })

  it('hides roots with author and inline display rules and restores their display', () => {
    document.body.innerHTML =
      '<style>.flex{display:flex}.grid{display:grid}</style>' +
      '<div data-grasp-live="sid001"><section class="flex" data-grasp-variant="0" hidden>original</section>' +
      '<section class="grid" data-grasp-variant="1" style="display:grid!important">one</section>' +
      '<section class="flex" data-grasp-variant="2" style="display:flex!important" hidden>two</section></div>'
    const w = scanWrappers()[0]
    showVariant(w, 1)
    expect(getComputedStyle(w.original!).display).toBe('none')
    expect(getComputedStyle(w.variants[1].el).display).toBe('none')
    showVariant(w, 2)
    expect(getComputedStyle(w.variants[0].el).display).toBe('none')
    expect(w.variants[1].el.style.getPropertyValue('display')).toBe('flex')
    expect(w.variants[1].el.style.getPropertyPriority('display')).toBe('important')
    // HMR supplies a newer inline display to a hidden root.
    w.variants[0].el.style.setProperty('display', 'inline-grid')
    showVariant(w, 1)
    expect(w.variants[0].el.style.getPropertyValue('display')).toBe('inline-grid')
    showVariant(w, 0)
    expect(w.original!.style.getPropertyValue('display')).toBe('')
    expect(getComputedStyle(w.original!).display).toBe('flex')
  })

  it('fades only the selected candidate while rapid switches keep the others hidden', () => {
    document.body.innerHTML = '<div data-grasp-live="sid001"><section data-grasp-variant="0" hidden>original</section><section data-grasp-variant="1">one</section><section data-grasp-variant="2" style="opacity:.75" hidden>two</section></div>'
    vi.stubGlobal('matchMedia', () => ({ matches: false }))
    const w = scanWrappers()[0]
    const firstAnimation = { cancel: vi.fn(), onfinish: null } as unknown as Animation
    const secondAnimation = { cancel: vi.fn(), onfinish: null } as unknown as Animation
    const firstAnimate = vi.fn(() => firstAnimation)
    const secondAnimate = vi.fn(() => secondAnimation)
    Object.defineProperty(w.variants[0].el, 'animate', { value: firstAnimate })
    Object.defineProperty(w.variants[1].el, 'animate', { value: secondAnimate })
    showVariant(w, 1)
    expect(firstAnimate).not.toHaveBeenCalled()
    showVariant(w, 2)
    expect(secondAnimate).toHaveBeenCalledWith([{ opacity: 0 }, { opacity: '.75' }], { duration: 160, easing: 'ease-out' })
    expect(w.variants[0].el.hidden).toBe(true)
    expect(getComputedStyle(w.variants[0].el).display).toBe('none')
    expect(w.variants[1].el.hidden).toBe(false)
    showVariant(w, 1)
    expect(secondAnimation.cancel).toHaveBeenCalledOnce()
    expect(firstAnimate).toHaveBeenCalledOnce()
    expect(w.variants[1].el.hidden).toBe(true)
    // Entering compare cancels the transient opacity and reveals all roots normally.
    for (const el of [w.original!, ...w.variants.map((v) => v.el)]) setVariantVisible(el, true)
    expect(firstAnimation.cancel).toHaveBeenCalledOnce()
    showVariant(w, 2)
    expect(secondAnimate).toHaveBeenCalledOnce()
    showVariant(w, 0)
    showVariant(w, 1)
    expect(firstAnimate).toHaveBeenCalledOnce()
  })

  it('switches immediately when reduced motion is requested', () => {
    document.body.innerHTML = '<div data-grasp-live="sid001"><section data-grasp-variant="1">one</section><section data-grasp-variant="2" hidden>two</section></div>'
    vi.stubGlobal('matchMedia', () => ({ matches: true }))
    const w = scanWrappers()[0]
    const animate = vi.fn()
    Object.defineProperty(w.variants[1].el, 'animate', { value: animate })
    showVariant(w, 2)
    expect(animate).not.toHaveBeenCalled()
    expect(w.variants[0].el.hidden).toBe(true)
    expect(w.variants[1].el.hidden).toBe(false)
  })
})

describe('describe', () => {
  it('builds a selector path and description', () => {
    document.body.innerHTML = '<main id="app"><section class="card dark"><h2>A</h2><h2>Design   notes</h2></section></main>'
    const h = document.querySelectorAll('h2')[1]
    expect(selectorPath(h)).toBe('main#app > section > h2:nth-of-type(2)')
    expect(visibleText(h)).toBe('Design notes')
    const d = describeElement(document.querySelector('section')!)
    expect(d).toMatchObject({ selector: 'main#app > section', tagName: 'section', classes: ['card', 'dark'] })
    expect(d.outerHTML?.startsWith('<section')).toBe(true)
    document.body.innerHTML = '<div><p>x</p></div>'
    expect(selectorPath(document.querySelector('p')!)).toBe('body > div > p')
  })
})

describe('viewStore', () => {
  it('saves, reads and drops views', () => {
    expect(getView('sid001')).toBeNull()
    putView('sid001', { current: 2, mode: 'compare', params: { '2': { gap: 4 } } })
    expect(getView('sid001')).toEqual({ current: 2, mode: 'compare', params: { '2': { gap: 4 } } })
    dropView('sid001')
    dropView('sid001')
    expect(getView('sid001')).toBeNull()
    sessionStorage.setItem('__grasp_live', '[1]')
    expect(getView('x')).toBeNull()
    sessionStorage.setItem('__grasp_live', 'nope')
    expect(getView('x')).toBeNull()
    for (let i = 0; i < 25; i++) putView(`sid${String(i).padStart(4, '0')}`, { current: 1, mode: 'inplace', params: {} })
    expect(Object.keys(JSON.parse(sessionStorage.getItem('__grasp_live') || '{}')).length).toBe(20)
  })
})
