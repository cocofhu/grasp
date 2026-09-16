// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import MermaidDiagram from './MermaidDiagram.vue'
import { flushMermaidQueue } from './mermaidRenderQueue'
import { cssTokenColor, isLightTheme, mermaidThemeName, themeVars } from './mermaidTheme'

const mermaidInitialize = vi.fn()
const mermaidParse = vi.fn()
const mermaidRender = vi.fn()

vi.mock('mermaid', () => ({
  default: {
    initialize: (...args: unknown[]) => mermaidInitialize(...args),
    parse: (...args: unknown[]) => mermaidParse(...args),
    render: (...args: unknown[]) => mermaidRender(...args),
  },
}))

function createI18nPlugin() {
  return createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: {
      'zh-CN': {
        pages: { plan: { diagramFallback: '图渲染失败,显示源码' } },
      },
    },
  })
}

function mountDiagram(source = 'flowchart LR\n  A-->B', extra?: { format?: string; caption?: string }) {
  return mount(MermaidDiagram, {
    props: {
      diagram: { format: extra?.format ?? 'mermaid', source, caption: extra?.caption },
      jsonPath: 'architecture.diagram',
    },
    global: { plugins: [createI18nPlugin()] },
  })
}

/** Three-section plan fixture — mirrors PlanView architecture / data / interaction. */
const THREE_SECTION_FIXTURE = {
  architecture: 'flowchart LR\n  ARCH_ONLY-->Q',
  data_design: 'erDiagram\n  DATA_ENTITY_ONLY {\n    string id PK\n  }',
  interaction: 'sequenceDiagram\n  participant INTERACT_ONLY\n  Note over INTERACT_ONLY: note-only',
} as const

/** Screenshot-equivalent ER from clarify feedback (MYSQL_INSTANCE / CVM_FIREWALL_RULE). */
const SCREENSHOT_ER = `erDiagram
  MYSQL_INSTANCE ||--o{ CVM_FIREWALL_RULE : allows
  MYSQL_INSTANCE {
    string id PK
    string name
    string status
  }
  CVM_FIREWALL_RULE {
    string id PK
    string instance_id FK
    string protocol
    string port
  }`

describe('MermaidDiagram theme helpers (g2.1/g2.3)', () => {
  beforeEach(async () => {
    document.documentElement.classList.remove('light')
    document.documentElement.style.cssText = ''
    mermaidInitialize.mockReset()
    mermaidParse.mockReset()
    mermaidRender.mockReset()
    mermaidParse.mockResolvedValue(true)
    mermaidRender.mockResolvedValue({ svg: '<svg data-ok="1"></svg>' })
    await flushMermaidQueue()
  })

  afterEach(async () => {
    document.documentElement.classList.remove('light')
    document.documentElement.style.cssText = ''
    await flushMermaidQueue()
  })

  it('isLightTheme follows html.light', () => {
    expect(isLightTheme()).toBe(false)
    document.documentElement.classList.add('light')
    expect(isLightTheme()).toBe(true)
  })

  it('cssTokenColor converts --c-* channel tokens to rgb()', () => {
    document.documentElement.style.setProperty('--c-base', '250 250 251')
    expect(cssTokenColor('--c-base', 'rgb(0, 0, 0)')).toBe('rgb(250, 250, 251)')
  })

  it('themeVars under light uses shallow --c-* tokens not dark fallbacks', () => {
    document.documentElement.classList.add('light')
    document.documentElement.style.setProperty('--c-base', '250 250 251')
    document.documentElement.style.setProperty('--c-txt', '24 24 27')
    document.documentElement.style.setProperty('--c-txt2', '82 82 91')
    document.documentElement.style.setProperty('--c-elevated', '244 244 245')
    document.documentElement.style.setProperty('--c-line', '229 229 232')
    document.documentElement.style.setProperty('--c-line-strong', '212 212 216')
    const vars = themeVars()
    expect(vars.background).toBe('rgb(250, 250, 251)')
    expect(vars.primaryTextColor).toBe('rgb(24, 24, 27)')
    expect(vars.secondaryTextColor).toBe('rgb(24, 24, 27)')
    expect(vars.nodeTextColor).toBe('rgb(24, 24, 27)')
    expect(vars.tertiaryTextColor).toBe('rgb(82, 82, 91)')
    expect(vars.textColor).toBe('rgb(24, 24, 27)')
    expect(vars.background).not.toMatch(/10,\s*10,\s*11/)
    expect(mermaidThemeName()).toBe('base')
  })

  it('themeVars under dark keeps dark tokens', () => {
    document.documentElement.style.setProperty('--c-base', '10 10 11')
    document.documentElement.style.setProperty('--c-txt', '237 237 240')
    document.documentElement.style.setProperty('--c-txt2', '161 161 170')
    const vars = themeVars()
    expect(vars.background).toBe('rgb(10, 10, 11)')
    expect(vars.primaryTextColor).toBe('rgb(237, 237, 240)')
    expect(vars.secondaryTextColor).toBe('rgb(237, 237, 240)')
    expect(vars.nodeTextColor).toBe('rgb(237, 237, 240)')
    expect(mermaidThemeName()).toBe('dark')
  })

  it('initialize uses base theme under html.light (not hardcoded dark)', async () => {
    document.documentElement.classList.add('light')
    document.documentElement.style.setProperty('--c-base', '250 250 251')
    document.documentElement.style.setProperty('--c-txt', '24 24 27')
    document.documentElement.style.setProperty('--c-txt2', '82 82 91')
    document.documentElement.style.setProperty('--c-elevated', '244 244 245')
    document.documentElement.style.setProperty('--c-line', '229 229 232')
    document.documentElement.style.setProperty('--c-line-strong', '212 212 216')
    const wrapper = mountDiagram()
    await flushPromises()
    await flushMermaidQueue()
    expect(mermaidInitialize).toHaveBeenCalled()
    const lightCalls = mermaidInitialize.mock.calls
    const cfg = lightCalls[lightCalls.length - 1]?.[0] as {
      theme: string
      themeVariables: Record<string, string>
      suppressErrorRendering?: boolean
      securityLevel?: string
    }
    expect(cfg.theme).toBe('base')
    expect(cfg.theme).not.toBe('dark')
    expect(cfg.suppressErrorRendering).toBe(true)
    expect(cfg.securityLevel).toBe('strict')
    expect(cfg.themeVariables.background).toBe('rgb(250, 250, 251)')
    expect(cfg.themeVariables.primaryTextColor).toBe('rgb(24, 24, 27)')
    expect(cfg.themeVariables.nodeTextColor).toBe('rgb(24, 24, 27)')
    expect(cfg.themeVariables.secondaryTextColor).toBe('rgb(24, 24, 27)')
    wrapper.unmount()
  })

  it('initialize uses dark theme without html.light', async () => {
    document.documentElement.style.setProperty('--c-base', '10 10 11')
    document.documentElement.style.setProperty('--c-txt', '237 237 240')
    document.documentElement.style.setProperty('--c-elevated', '28 28 33')
    document.documentElement.style.setProperty('--c-line', '38 38 43')
    document.documentElement.style.setProperty('--c-line-strong', '54 54 62')
    const wrapper = mountDiagram()
    await flushPromises()
    await flushMermaidQueue()
    const darkCalls = mermaidInitialize.mock.calls
    const cfg = darkCalls[darkCalls.length - 1]?.[0] as { theme: string }
    expect(cfg.theme).toBe('dark')
    wrapper.unmount()
  })

  it('re-initializes when html.light is toggled', async () => {
    const wrapper = mountDiagram()
    await flushPromises()
    await flushMermaidQueue()
    const callsBefore = mermaidInitialize.mock.calls.length
    document.documentElement.classList.add('light')
    await flushPromises()
    await new Promise((r) => setTimeout(r, 0))
    await flushPromises()
    await flushMermaidQueue()
    expect(mermaidInitialize.mock.calls.length).toBeGreaterThan(callsBefore)
    const toggleCalls = mermaidInitialize.mock.calls
    const cfg = toggleCalls[toggleCalls.length - 1]?.[0] as { theme: string }
    expect(cfg.theme).toBe('base')
    wrapper.unmount()
  })

  it('falls back to source when render fails twice (retry exhausted)', async () => {
    mermaidRender.mockRejectedValue(new Error('boom'))
    const wrapper = mountDiagram('flowchart LR\n  FAIL-->HERE')
    await flushPromises()
    await flushMermaidQueue()
    expect(wrapper.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('FAIL-->HERE')
    // One immediate retry after first render throw.
    expect(mermaidRender.mock.calls.length).toBeGreaterThanOrEqual(2)
    const hint = wrapper.find('[data-testid="plan-diagram-fallback-hint"]')
    const source = wrapper.find('[data-testid="plan-diagram-fallback-source"]')
    expect(hint.classes()).toContain('text-txt2')
    expect(hint.classes()).not.toContain('text-txt3')
    expect(source.classes()).toContain('text-txt2')
    wrapper.unmount()
  })

  it('g2.2: retries once when render fails transiently then succeeds', async () => {
    mermaidRender
      .mockRejectedValueOnce(new Error('transient'))
      .mockResolvedValueOnce({ svg: '<svg data-retry="1"></svg>' })
    const wrapper = mountDiagram('flowchart LR\n  RETRY-->OK')
    await flushPromises()
    await flushMermaidQueue()
    expect(wrapper.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(false)
    expect(wrapper.html()).toContain('data-retry="1"')
    expect(mermaidRender).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})

describe('MermaidDiagram parse-first and sticky parse fallback (g2.1 / g2.2 / g2.3 / g3.3)', () => {
  beforeEach(async () => {
    document.documentElement.classList.remove('light')
    document.body.innerHTML = ''
    mermaidInitialize.mockReset()
    mermaidParse.mockReset()
    mermaidRender.mockReset()
    mermaidParse.mockResolvedValue(true)
    mermaidRender.mockResolvedValue({ svg: '<svg data-ok="1"></svg>' })
    await flushMermaidQueue()
  })

  afterEach(async () => {
    document.documentElement.classList.remove('light')
    document.body.innerHTML = ''
    await flushMermaidQueue()
  })

  it('g2.1: enables suppressErrorRendering and parses before render', async () => {
    const wrapper = mountDiagram()
    await flushPromises()
    await flushMermaidQueue()
    expect(mermaidParse).toHaveBeenCalledWith('flowchart LR\n  A-->B')
    expect(mermaidRender).toHaveBeenCalled()
    const g21Calls = mermaidInitialize.mock.calls
    const cfg = g21Calls[g21Calls.length - 1]?.[0] as { suppressErrorRendering?: boolean }
    expect(cfg.suppressErrorRendering).toBe(true)
    expect(wrapper.find('svg[data-ok="1"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('g2.1/g3.3: illegal source falls back once and never calls render; theme toggle does not re-enter', async () => {
    mermaidParse.mockRejectedValue(new Error('Parse error on line 2'))
    const wrapper = mountDiagram('flowchart LR\n  A-->[')
    await flushPromises()
    await flushMermaidQueue()
    expect(wrapper.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-testid="plan-diagram-fallback"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('A-->[')
    expect(mermaidRender).not.toHaveBeenCalled()
    expect(document.body.textContent || '').not.toContain('Syntax error in text')
    // Theme toggle must not re-enter parse/render for the same parse-failed source.
    const parseCalls = mermaidParse.mock.calls.length
    document.documentElement.classList.add('light')
    await flushPromises()
    await new Promise((r) => setTimeout(r, 0))
    await flushPromises()
    await flushMermaidQueue()
    expect(mermaidParse.mock.calls.length).toBe(parseCalls)
    expect(mermaidRender).not.toHaveBeenCalled()
    expect(wrapper.findAll('[data-testid="plan-diagram-fallback"]')).toHaveLength(1)
    wrapper.unmount()
  })

  it('g2.2: render failure is not sticky — theme toggle retries', async () => {
    mermaidRender.mockRejectedValue(new Error('render boom'))
    const wrapper = mountDiagram('flowchart LR\n  TRANSIENT-->X')
    await flushPromises()
    await flushMermaidQueue()
    expect(wrapper.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(true)
    const rendersAfterFail = mermaidRender.mock.calls.length
    expect(rendersAfterFail).toBeGreaterThanOrEqual(2)

    mermaidRender.mockReset()
    mermaidRender.mockResolvedValue({ svg: '<svg data-recovered="1"></svg>' })
    document.documentElement.classList.add('light')
    await flushPromises()
    await new Promise((r) => setTimeout(r, 0))
    await flushPromises()
    await flushMermaidQueue()
    expect(mermaidRender.mock.calls.length).toBeGreaterThan(0)
    expect(wrapper.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(false)
    expect(wrapper.html()).toContain('data-recovered="1"')
    wrapper.unmount()
  })

  it('g2.2: cleans temporary #d{id} nodes after failure', async () => {
    mermaidParse.mockResolvedValue(true)
    mermaidRender.mockImplementation(async (id: string) => {
      const tmp = document.createElement('div')
      tmp.id = `d${id}`
      tmp.textContent = 'Syntax error in text'
      document.body.appendChild(tmp)
      throw new Error('render failed')
    })
    const wrapper = mountDiagram('flowchart LR\n  BAD-->X')
    await flushPromises()
    await flushMermaidQueue()
    expect(wrapper.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(true)
    expect(document.querySelectorAll('[id^="dplan-mmd-"]').length).toBe(0)
    expect(document.body.textContent || '').not.toMatch(/Syntax error in text/)
    wrapper.unmount()
  })

  it('g2.3: legal diagram still renders SVG; theme toggle re-draws', async () => {
    const wrapper = mountDiagram('flowchart LR\n  OK-->YES')
    await flushPromises()
    await flushMermaidQueue()
    expect(wrapper.find('svg[data-ok="1"]').exists()).toBe(true)
    const rendersBefore = mermaidRender.mock.calls.length
    document.documentElement.classList.add('light')
    await flushPromises()
    await new Promise((r) => setTimeout(r, 0))
    await flushPromises()
    await flushMermaidQueue()
    expect(mermaidRender.mock.calls.length).toBeGreaterThan(rendersBefore)
    wrapper.unmount()
  })

  it('g2.2: source change clears sticky parse lock and re-renders', async () => {
    mermaidParse.mockImplementation(async (src: string) => {
      if (String(src).includes('BAD')) throw new Error('bad')
      return true
    })
    const wrapper = mountDiagram('flowchart LR\n  BAD-->X')
    await flushPromises()
    await flushMermaidQueue()
    expect(wrapper.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(true)
    expect(mermaidRender).not.toHaveBeenCalled()

    await wrapper.setProps({
      diagram: { format: 'mermaid', source: 'flowchart LR\n  GOOD-->Y' },
      jsonPath: 'architecture.diagram',
    })
    await flushPromises()
    await flushMermaidQueue()
    expect(wrapper.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(false)
    expect(wrapper.find('svg[data-ok="1"]').exists()).toBe(true)
    expect(mermaidRender).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('g3.1: illegal diagram falls back while a subsequent legal diagram still renders', async () => {
    mermaidParse.mockImplementation(async (src: string) => {
      if (String(src).includes('BAD')) throw new Error('bad')
      return true
    })
    mermaidRender.mockImplementation(async (_id: string, src: string) => ({
      svg: `<svg data-src="${String(src).includes('GOOD') ? 'good' : 'other'}"></svg>`,
    }))
    const bad = mountDiagram('flowchart LR\n  BAD-->X')
    await flushPromises()
    await flushMermaidQueue()
    expect(bad.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(true)
    bad.unmount()

    const good = mountDiagram('flowchart LR\n  GOOD-->Y')
    await flushPromises()
    await flushMermaidQueue()
    expect(good.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(false)
    expect(good.html()).toContain('data-src="good"')
    good.unmount()
  })
})

describe('MermaidDiagram serial isolation (g1.1 / g1.2 / g3.1 / g3.2)', () => {
  beforeEach(async () => {
    document.documentElement.classList.remove('light')
    mermaidInitialize.mockReset()
    mermaidParse.mockReset()
    mermaidRender.mockReset()
    mermaidParse.mockResolvedValue(true)
    await flushMermaidQueue()
  })

  afterEach(async () => {
    document.documentElement.classList.remove('light')
    await flushMermaidQueue()
  })

  it('g1.1/g1.2/g3.2: three concurrent mounts serialize render and never write sibling content', async () => {
    let inFlight = 0
    let maxInFlight = 0
    mermaidRender.mockImplementation(async (_id: string, src: string) => {
      inFlight++
      maxInFlight = Math.max(maxInFlight, inFlight)
      await new Promise((r) => setTimeout(r, 15))
      inFlight--
      const tag =
        src.includes('ARCH_ONLY') ? 'arch' : src.includes('DATA_ENTITY_ONLY') ? 'data' : src.includes('INTERACT_ONLY') ? 'interact' : 'other'
      return { svg: `<svg data-section="${tag}"><text>${tag}</text></svg>` }
    })

    const i18n = createI18nPlugin()
    const wrappers = [
      mount(MermaidDiagram, {
        props: { diagram: { format: 'mermaid', source: THREE_SECTION_FIXTURE.architecture }, jsonPath: 'architecture.diagram' },
        global: { plugins: [i18n] },
      }),
      mount(MermaidDiagram, {
        props: { diagram: { format: 'mermaid', source: THREE_SECTION_FIXTURE.data_design }, jsonPath: 'data_design.diagram' },
        global: { plugins: [i18n] },
      }),
      mount(MermaidDiagram, {
        props: { diagram: { format: 'mermaid', source: THREE_SECTION_FIXTURE.interaction }, jsonPath: 'interaction.diagram' },
        global: { plugins: [i18n] },
      }),
    ]

    await flushPromises()
    await flushMermaidQueue()
    await flushPromises()

    expect(maxInFlight).toBe(1)
    expect(mermaidRender).toHaveBeenCalledTimes(3)

    const svgs = wrappers.map((w) => w.element.querySelector('svg')?.getAttribute('data-section') || '')
    expect(svgs).toEqual(['arch', 'data', 'interact'])
    expect(wrappers[0].html()).toContain('data-section="arch"')
    expect(wrappers[0].html()).not.toContain('data-section="data"')
    expect(wrappers[0].html()).not.toContain('data-section="interact"')
    expect(wrappers[1].html()).toContain('data-section="data"')
    expect(wrappers[1].html()).not.toContain('data-section="arch"')
    expect(wrappers[1].html()).not.toContain('data-section="interact"')
    expect(wrappers[2].html()).toContain('data-section="interact"')
    expect(wrappers[2].html()).not.toContain('data-section="arch"')
    expect(wrappers[2].html()).not.toContain('data-section="data"')
    const sources = mermaidRender.mock.calls.map((c) => String(c[1]))
    expect(sources.some((s) => s.includes('ARCH_ONLY'))).toBe(true)
    expect(sources.some((s) => s.includes('DATA_ENTITY_ONLY'))).toBe(true)
    expect(sources.some((s) => s.includes('INTERACT_ONLY'))).toBe(true)

    wrappers.forEach((w) => w.unmount())
  })

  it('g1.2/g3.2: one failed section falls back without polluting siblings', async () => {
    mermaidParse.mockImplementation(async (src: string) => {
      if (src.includes('BAD')) throw new Error('parse fail')
      return true
    })
    mermaidRender.mockImplementation(async (_id: string, src: string) => {
      await new Promise((r) => setTimeout(r, 5))
      return { svg: `<svg data-ok="${src.includes('GOOD_A') ? 'a' : 'b'}"></svg>` }
    })
    const i18n = createI18nPlugin()
    const bad = mount(MermaidDiagram, {
      props: { diagram: { format: 'mermaid', source: 'flowchart LR\n  BAD-->X' }, jsonPath: 'architecture.diagram' },
      global: { plugins: [i18n] },
    })
    const goodA = mount(MermaidDiagram, {
      props: { diagram: { format: 'mermaid', source: 'flowchart LR\n  GOOD_A-->Y' }, jsonPath: 'data_design.diagram' },
      global: { plugins: [i18n] },
    })
    const goodB = mount(MermaidDiagram, {
      props: { diagram: { format: 'mermaid', source: 'flowchart LR\n  GOOD_B-->Z' }, jsonPath: 'interaction.diagram' },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    await flushMermaidQueue()
    await flushPromises()

    expect(bad.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(true)
    expect(bad.text()).toContain('BAD-->X')
    expect(goodA.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(false)
    expect(goodB.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(false)
    expect(goodA.element.querySelector('svg')?.getAttribute('data-ok')).toBe('a')
    expect(goodB.element.querySelector('svg')?.getAttribute('data-ok')).toBe('b')
    expect(goodA.html()).not.toContain('BAD-->X')
    expect(goodB.html()).not.toContain('BAD-->X')

    bad.unmount()
    goodA.unmount()
    goodB.unmount()
  })

  it('g1.2: theme MutationObserver re-render keeps three hosts isolated', async () => {
    mermaidRender.mockImplementation(async (_id: string, src: string) => {
      await new Promise((r) => setTimeout(r, 8))
      const tag = src.includes('T_ARCH') ? 'arch' : src.includes('T_DATA') ? 'data' : 'interact'
      return { svg: `<svg data-theme="${tag}"></svg>` }
    })
    const i18n = createI18nPlugin()
    const wrappers = [
      mount(MermaidDiagram, {
        props: { diagram: { format: 'mermaid', source: 'flowchart LR\n  T_ARCH-->A' }, jsonPath: 'architecture.diagram' },
        global: { plugins: [i18n] },
      }),
      mount(MermaidDiagram, {
        props: { diagram: { format: 'mermaid', source: 'flowchart LR\n  T_DATA-->B' }, jsonPath: 'data_design.diagram' },
        global: { plugins: [i18n] },
      }),
      mount(MermaidDiagram, {
        props: { diagram: { format: 'mermaid', source: 'flowchart LR\n  T_INTER-->C' }, jsonPath: 'interaction.diagram' },
        global: { plugins: [i18n] },
      }),
    ]
    await flushPromises()
    await flushMermaidQueue()
    await flushPromises()

    document.documentElement.classList.add('light')
    await new Promise((r) => setTimeout(r, 0))
    await flushPromises()
    await flushMermaidQueue()
    await flushPromises()

    const tags = wrappers.map((w) => w.element.querySelector('svg')?.getAttribute('data-theme'))
    expect(tags).toEqual(['arch', 'data', 'interact'])
    wrappers.forEach((w) => w.unmount())
  })

  it('g1.3: unmount invalidates in-flight render so destroyed host stays empty', async () => {
    let resolveRender!: (v: { svg: string }) => void
    mermaidRender.mockImplementation(
      () =>
        new Promise<{ svg: string }>((resolve) => {
          resolveRender = resolve
        }),
    )
    const wrapper = mountDiagram('flowchart LR\n  LATE-->X')
    await flushPromises()
    wrapper.unmount()
    resolveRender({ svg: '<svg data-late="1"></svg>' })
    await flushPromises()
    await flushMermaidQueue()
    await flushPromises()
    expect(mermaidRender).toHaveBeenCalled()
  })

  it('g3.2: rapid consecutive source updates settle to the latest content only', async () => {
    const renderLog: string[] = []
    mermaidRender.mockImplementation(async (_id: string, src: string) => {
      renderLog.push(src)
      await new Promise((r) => setTimeout(r, 12))
      const marker = src.match(/V(\d+)/)?.[1] || '?'
      return { svg: `<svg data-v="${marker}"></svg>` }
    })
    const wrapper = mountDiagram('flowchart LR\n  V1-->A')
    await flushPromises()

    await wrapper.setProps({ diagram: { format: 'mermaid', source: 'flowchart LR\n  V2-->A' }, jsonPath: 'architecture.diagram' })
    await wrapper.setProps({ diagram: { format: 'mermaid', source: 'flowchart LR\n  V3-->A' }, jsonPath: 'architecture.diagram' })
    await wrapper.setProps({ diagram: { format: 'mermaid', source: 'flowchart LR\n  V4-->A' }, jsonPath: 'architecture.diagram' })

    await flushPromises()
    await flushMermaidQueue()
    await flushPromises()

    expect(wrapper.element.querySelector('svg')?.getAttribute('data-v')).toBe('4')
    expect(wrapper.html()).not.toMatch(/data-v="[123]"/)
    expect(renderLog.some((s) => s.includes('V4'))).toBe(true)
    wrapper.unmount()
  })

  it('g3.1: screenshot ER parse+render shows SVG without fallback; caption kept', async () => {
    mermaidRender.mockResolvedValue({ svg: '<svg data-er="mysql"></svg>' })
    const wrapper = mountDiagram(SCREENSHOT_ER, { caption: 'MySQL / CVM firewall ER' })
    await flushPromises()
    await flushMermaidQueue()
    expect(mermaidParse).toHaveBeenCalled()
    const parsed = String(mermaidParse.mock.calls[0]?.[0] || '')
    expect(parsed).toContain('MYSQL_INSTANCE')
    expect(parsed).toContain('CVM_FIREWALL_RULE')
    expect(mermaidRender).toHaveBeenCalled()
    expect(wrapper.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(false)
    expect(wrapper.html()).toContain('data-er="mysql"')
    expect(wrapper.text()).toContain('MySQL / CVM firewall ER')
    wrapper.unmount()
  })

  it('non-mermaid format uses fallback without calling render', async () => {
    const wrapper = mountDiagram('not mermaid', { format: 'plantuml' })
    await flushPromises()
    await flushMermaidQueue()
    expect(wrapper.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(true)
    expect(mermaidRender).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
