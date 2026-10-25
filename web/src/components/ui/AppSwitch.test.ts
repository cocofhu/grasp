// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AppSwitch from './AppSwitch.vue'

const switchSrc = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'AppSwitch.vue'), 'utf8')

/** On + enabled hover/focus rule. Disabled and off tracks must not match it. */
const onHoverRule =
  /\.app-switch\[aria-checked=true\]:is\(:hover,\s*:focus-visible\):not\(:disabled\)\s*\{([^}]*)\}/

describe('AppSwitch', () => {
  it('renders capsule accent track when on', () => {
    const w = mount(AppSwitch, {
      props: { modelValue: true },
      attrs: { 'aria-label': 'Enable feature' },
    })
    const btn = w.get('button[role="switch"]')
    expect(btn.attributes('aria-checked')).toBe('true')
    expect(btn.attributes('aria-label')).toBe('Enable feature')
    expect(btn.classes().join(' ')).toContain('bg-accent')
    expect(btn.classes().join(' ')).toContain('rounded-full')
    expect(btn.classes().join(' ')).not.toContain('rounded-none')
    w.unmount()
  })

  it('renders off-state track when false', () => {
    const w = mount(AppSwitch, { props: { modelValue: false } })
    const btn = w.get('button[role="switch"]')
    expect(btn.attributes('aria-checked')).toBe('false')
    expect(btn.classes().join(' ')).toContain('bg-base')
    w.unmount()
  })

  it('emits update:modelValue on click and Space/Enter', async () => {
    const w = mount(AppSwitch, { props: { modelValue: false } })
    await w.get('button[role="switch"]').trigger('click')
    expect(w.emitted('update:modelValue')?.[0]).toEqual([true])

    await w.get('button[role="switch"]').trigger('keydown', { key: ' ' })
    expect(w.emitted('update:modelValue')?.[1]).toEqual([true])

    await w.get('button[role="switch"]').trigger('keydown', { key: 'Enter' })
    expect(w.emitted('update:modelValue')?.[2]).toEqual([true])
    w.unmount()
  })

  it('does not toggle when disabled', async () => {
    const w = mount(AppSwitch, { props: { modelValue: false, disabled: true } })
    const btn = w.get('button[role="switch"]')
    expect(btn.attributes('disabled')).toBeDefined()
    await btn.trigger('click')
    expect(w.emitted('update:modelValue')).toBeUndefined()
    w.unmount()
  })

  it('paints the on, enabled hover and focus track with accent-hover', () => {
    const match = switchSrc.match(onHoverRule)
    expect(match, 'on hover/focus selector').toBeTruthy()
    const body = match![1]
    expect(match![0]).toContain(':hover')
    expect(match![0]).toContain(':focus-visible')
    expect(match![0]).toContain(':not(:disabled)')
    expect(body).toContain('background-color: rgb(var(--c-accent-hover))')
    expect(body).toContain('border-color: rgb(var(--c-accent-hover))')
    expect(switchSrc).not.toContain('--c-accent-2')
    expect(switchSrc).not.toContain('hover-ink')
  })

  it('does not apply the accent-hover fill on disabled hover', () => {
    const hoverRules = [...switchSrc.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
    const accentHoverFills = hoverRules.filter(([, , body]) => body.includes('--c-accent-hover'))
    expect(accentHoverFills.length).toBeGreaterThan(0)
    for (const [rule] of accentHoverFills) {
      expect(rule).toContain(':not(:disabled)')
      const withoutExclusion = rule.replace(/:not\(:disabled\)/g, '')
      expect(withoutExclusion).not.toContain(':disabled')
    }
  })

  it('drops color transitions when the user prefers reduced motion', () => {
    const reduced = switchSrc.match(/@media\s*\(\s*prefers-reduced-motion:\s*reduce\s*\)\s*\{([\s\S]*?)\n\}/)
    expect(reduced, 'reduced-motion block').toBeTruthy()
    const block = reduced![1]
    expect(block).toContain('.app-switch')
    expect(block).not.toMatch(/background-color/)
    expect(block).not.toMatch(/border-color/)
    expect(block).toMatch(/transition:/)
  })
})
