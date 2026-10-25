// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import commonEn from '@/locales/en/common.json'
import pagesEn from '@/locales/en/pages.json'
import ClarifyThinkingRow from './ClarifyThinkingRow.vue'
import GateReactStreamPanel from './GateReactStreamPanel.vue'

const here = dirname(fileURLToPath(import.meta.url))

function readSrc(rel: string) {
  return readFileSync(join(here, rel), 'utf8')
}

function mountRow(props: Record<string, unknown> = {}, locale: 'zh-CN' | 'en' = 'zh-CN') {
  const i18n = createI18n({
    legacy: false,
    locale,
    messages: {
      'zh-CN': { ...common, ...pages },
      en: { ...commonEn, ...pagesEn },
    },
  })
  return mount(ClarifyThinkingRow, { props, global: { plugins: [i18n] } })
}

function mountGate(props: Record<string, unknown> = {}, locale: 'zh-CN' | 'en' = 'zh-CN') {
  const i18n = createI18n({
    legacy: false,
    locale,
    messages: {
      'zh-CN': { ...common, ...pages },
      en: { ...commonEn, ...pagesEn },
    },
  })
  return mount(GateReactStreamPanel, { props, global: { plugins: [i18n], stubs: { Icon: true } } })
}

function expectThinkingCue(root: ReturnType<typeof mountRow>, copy: string) {
  const lamp = root.get('[data-testid="thinking-lamp"]')
  const core = root.get('[data-testid="thinking-lamp-core"]')
  const halo = root.get('[data-testid="thinking-lamp-halo"]')
  const sheen = root.get('[data-testid="thinking-sheen"]')
  expect(lamp.attributes('aria-hidden')).toBe('true')
  expect(core.classes()).toContain('thinking-lamp-core')
  expect(halo.classes()).toContain('thinking-lamp-halo')
  expect(sheen.classes()).toContain('thinking-sheen')
  expect(sheen.text()).toBe(copy)
  expect(root.find('[data-testid="thinking-lamp"]').exists()).toBe(true)
  expect(root.findAll('[data-testid="thinking-lamp"]')).toHaveLength(1)
}

describe('ThinkingBusyCue', () => {
  it('clarify placeholder and step tail share the lamp and gray sheen', () => {
    const placeholder = mountRow()
    expect(placeholder.get('[data-testid="clarify-busy-placeholder"]').text()).toBe('思考中…')
    expectThinkingCue(placeholder, '思考中…')

    const tail = mountRow({ variant: 'status' })
    expect(tail.get('[data-testid="clarify-busy-status"]').attributes('data-state')).toBe('thinking')
    expectThinkingCue(tail, '思考中…')
    expect(tail.find('.clarify-outputting').exists()).toBe(false)

    const english = mountRow({}, 'en')
    expectThinkingCue(english, 'Thinking…')
    placeholder.unmount()
    tail.unmount()
    english.unmount()
  })

  it('clarify outputting drops the lamp and sheen and stays tertiary text', () => {
    const w = mountRow({ variant: 'status', outputting: true })
    const row = w.get('[data-testid="clarify-busy-status"]')
    expect(row.attributes('data-state')).toBe('outputting')
    expect(row.classes()).toContain('clarify-outputting')
    expect(row.text()).toBe('输出中')
    expect(w.find('[data-testid="thinking-lamp"]').exists()).toBe(false)
    expect(w.find('[data-testid="thinking-sheen"]').exists()).toBe(false)
    expect(readSrc('ClarifyThinkingRow.vue')).toMatch(/\.clarify-outputting \{ color: rgb\(var\(--c-txt3\)\); \}/)
    w.unmount()
  })

  it('gate thinking branches use the same cue, and outputting stays accent text', async () => {
    const waiting = mountGate({ thinking: true })
    expectThinkingCue(waiting, '思考中…')
    expect(waiting.find('[data-testid="inbox-motion-pulse"]').exists()).toBe(false)

    await waiting.setProps({ streamThought: '先看边界' })
    const thinking = waiting.get('[data-testid="gate-busy-status"]')
    expect(thinking.text()).toBe('思考中…')
    expectThinkingCue(waiting, '思考中…')

    await waiting.setProps({ streamText: '正文开始了' })
    const outputting = waiting.get('[data-testid="gate-busy-status"]')
    expect(outputting.text()).toBe('输出中')
    expect(outputting.find('.text-accent-2').exists()).toBe(true)
    expect(outputting.find('[data-testid="thinking-lamp"]').exists()).toBe(false)
    expect(outputting.find('[data-testid="thinking-sheen"]').exists()).toBe(false)

    const english = mountGate({ thinking: true }, 'en')
    expectThinkingCue(english, 'Thinking…')
    waiting.unmount()
    english.unmount()
  })

  it('sizes the core at 4px, keeps the halo within 10px, and cycles at 0.8s / 1s', () => {
    const css = readSrc('ThinkingBusyCue.vue')
    const lamp = css.slice(css.indexOf('.thinking-lamp {'), css.indexOf('.thinking-lamp-core,'))
    expect(lamp).toMatch(/width:\s*12px/)
    expect(lamp).toMatch(/height:\s*12px/)
    expect(lamp).toMatch(/color:\s*rgb\(var\(--c-accent-2\)\)/)

    const coreBox = css.slice(css.indexOf('.thinking-lamp-core,'), css.indexOf('.thinking-lamp-core {'))
    expect(coreBox).toMatch(/width:\s*4px/)
    expect(coreBox).toMatch(/height:\s*4px/)

    expect(css).toMatch(/animation:\s*thinking-lamp-core 0\.8s ease-in-out infinite/)
    expect(css).toMatch(/animation:\s*thinking-lamp-halo 0\.8s ease-out infinite/)
    expect(css).toMatch(/animation:\s*thinking-sheen 1s linear infinite/)
    expect(css).not.toMatch(/1\.2s|1\.6s|2\.1s|2100ms/)

    const haloEnd = css.match(/@keyframes thinking-lamp-halo \{[\s\S]*?100% \{ transform: scale\(([\d.]+)\)/)
    expect(haloEnd).not.toBeNull()
    const reach = 4 * Number(haloEnd?.[1])
    expect(reach).toBeCloseTo(8.8)
    expect(reach).toBeLessThanOrEqual(10)
    expect(reach).toBeGreaterThan(8)

    const sheen = css.slice(css.indexOf('@supports'), css.indexOf('@keyframes thinking-lamp-core'))
    expect(sheen).toMatch(/rgb\(var\(--c-txt2\) \/ \.82\)/)
    expect(sheen).toMatch(/background-clip:\s*text/)
    expect(sheen).not.toMatch(/--c-accent|--grad-logo|#7[Bb]61[Ff]{2}|brand/)
    expect(css).toMatch(/font-size:\s*11px/)
    expect(css).toMatch(/font-weight:\s*400/)
  })

  it('stops motion for reduced motion and keeps a solid gray fallback', () => {
    const css = readSrc('ThinkingBusyCue.vue')
    const base = css.slice(css.indexOf('.thinking-sheen {'), css.indexOf('@supports'))
    expect(base).toMatch(/color:\s*rgb\(var\(--c-txt2\) \/ \.82\)/)
    expect(base).toMatch(/-webkit-text-fill-color:\s*rgb\(var\(--c-txt2\) \/ \.82\)/)
    expect(base).toMatch(/background-image:\s*none/)

    const reduced = css.slice(css.indexOf('@media (prefers-reduced-motion: reduce)'))
    expect(reduced).toMatch(/animation:\s*none/)
    expect(reduced).toMatch(/\.thinking-lamp-halo[\s\S]*opacity:\s*0/)
    expect(reduced).toMatch(/\.thinking-lamp-core[\s\S]*opacity:\s*1/)
    expect(reduced).toMatch(/transform:\s*none/)
    expect(reduced).toMatch(/box-shadow:\s*none/)
    expect(reduced).toMatch(/background-image:\s*none/)
    expect(reduced).toMatch(/color:\s*rgb\(var\(--c-txt2\) \/ \.82\)/)
  })

  it('leaves the shared busy breath and the next-round line alone', () => {
    const motion = readFileSync(join(here, '../../styles/icon-motion.css'), 'utf8')
    expect(motion).toMatch(/animation: iconBusyCore 2100ms ease-in-out infinite/)
    expect(motion).toMatch(/animation: iconBusyRing 2100ms ease-out infinite/)
    expect(motion).toMatch(/@keyframes iconBusyCore \{ 0%,100% \{ transform: scale\(1\); \} 50% \{ transform: scale\(1\.06\); \} \}/)
    expect(motion).toMatch(/@keyframes iconBusyRing \{ 0% \{ transform: scale\(1\); opacity: \.5; \} 100% \{ transform: scale\(2\.1\); opacity: 0; \} \}/)

    const row = readSrc('ClarifyThinkingRow.vue')
    const gate = readSrc('GateReactStreamPanel.vue')
    const chat = readFileSync(join(here, 'ClarifyChat.vue'), 'utf8')
    expect(row).not.toMatch(/breath-dot|iconBusyCore|InboxMotionMark/)
    expect(gate).not.toMatch(/InboxMotionMark|kind="pulse"|breath-dot/)
    expect(chat).toMatch(/name="breath-dot"[\s\S]{0,200}pages\.clarify\.thinking'\)/)
    expect(chat).not.toMatch(/pages\.clarify\.thinkingBusy/)
  })
})
