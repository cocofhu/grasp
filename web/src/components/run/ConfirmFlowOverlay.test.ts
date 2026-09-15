// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { defineComponent, nextTick } from 'vue'
import ConfirmFlowOverlay from './ConfirmFlowOverlay.vue'
import ReviewShell from './ReviewShell.vue'
import {
  createConfirmFlowCeremony,
  provideConfirmFlowCeremony,
} from '@/lib/inbox/confirmFlowCeremony'

function i18n() {
  return createI18n({
    legacy: false,
    locale: 'zh',
    messages: {
      zh: {
        pages: {
          clarify: {
            confirmFlowOverlayTitle: '已确认并流转',
            confirmFlowOverlaySub: '进入下一节点',
          },
          reviewShell: {
            drawerHandle: '拖动手柄调整高度',
            drawerHandleAria: '拖动手柄上下调整复审抽屉高度',
            resizeSash: '拖动调整侧栏宽度 · 双击恢复默认',
          },
        },
      },
    },
  })
}

describe('ConfirmFlowOverlay (g1.1 / g1.2 / g1.3 / g3.2 / g3.3)', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation((query: string) => ({
        matches: false,
        media: query,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('uses a single check path with stroke-dashoffset draw (g1.1 / g3.3)', () => {
    const w = mount(ConfirmFlowOverlay, {
      props: { phase: 'play', playToken: 1 },
      global: { plugins: [i18n()] },
    })
    const paths = w.findAll('[data-testid="confirm-flow-check-path"]')
    expect(paths).toHaveLength(1)
    expect(paths[0].attributes('pathLength')).toBe('28')
    // Disk is a sibling of the SVG, not a wrapping transform host (g1.2).
    const ring = w.get('[data-testid="confirm-flow-ring"]')
    expect(ring.find('[data-testid="confirm-flow-disk"]').exists()).toBe(true)
    expect(ring.find('[data-testid="confirm-flow-check"]').exists()).toBe(true)
    expect(w.get('[data-testid="confirm-flow-title"]').text()).toBe('已确认并流转')
    expect(w.get('[data-testid="confirm-flow-sub"]').text()).toBe('进入下一节点')
  })

  it('replay bumps playToken so the check remounts from hidden (g1.3)', async () => {
    const ceremony = createConfirmFlowCeremony()
    const Host = defineComponent({
      components: { ConfirmFlowOverlay },
      setup() {
        return {
          phase: ceremony.phase,
          reduceMotion: ceremony.reduceMotion,
          playToken: ceremony.playToken,
          play: ceremony.play,
        }
      },
      template: `
        <ConfirmFlowOverlay
          :phase="phase"
          :reduce-motion="reduceMotion"
          :play-token="playToken"
        />
      `,
    })
    const w = mount(Host, { global: { plugins: [i18n()] } })
    const firstToken = ceremony.playToken.value
    const p1 = ceremony.play()
    await nextTick()
    expect(ceremony.phase.value).toBe('play')
    expect(ceremony.playToken.value).toBe(firstToken + 1)
    await vi.runAllTimersAsync()
    await p1
    expect(ceremony.phase.value).toBe('idle')

    const p2 = ceremony.play()
    await nextTick()
    expect(ceremony.playToken.value).toBe(firstToken + 2)
    expect(w.get('[data-testid="confirm-flow-check"]').exists()).toBe(true)
    await vi.runAllTimersAsync()
    await p2
  })

  it('reduced-motion skips check-draw and disk scale (g3.2)', async () => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation((query: string) => ({
        matches: String(query).includes('prefers-reduced-motion'),
        media: query,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    })
    const ceremony = createConfirmFlowCeremony()
    const p = ceremony.play()
    await nextTick()
    expect(ceremony.reduceMotion.value).toBe(true)
    expect(ceremony.phase.value).toBe('play')
    await vi.advanceTimersByTimeAsync(500)
    await p
    expect(ceremony.phase.value).toBe('idle')
  })

  it('ReviewShell hosts overlay idle until play (g3.1)', async () => {
    const w = mount(ReviewShell, {
      global: { plugins: [i18n()] },
      slots: {
        stage: '<div>stage</div>',
        sidebar: '<div>sidebar</div>',
      },
      attachTo: document.body,
    })
    const overlay = w.get('[data-testid="confirm-flow-overlay"]')
    expect(overlay.attributes('data-phase')).toBe('idle')
    const play = (w.vm as { playConfirmCeremony: () => Promise<void> }).playConfirmCeremony()
    await nextTick()
    await nextTick()
    expect(w.get('[data-testid="confirm-flow-overlay"]').attributes('data-phase')).toBe('play')
    await vi.runAllTimersAsync()
    await play
    expect(w.get('[data-testid="confirm-flow-overlay"]').attributes('data-phase')).toBe('idle')
    w.unmount()
  })

  it('provideConfirmFlowCeremony wires inject host', () => {
    let exposed: ReturnType<typeof provideConfirmFlowCeremony> | null = null
    const Host = defineComponent({
      setup() {
        exposed = provideConfirmFlowCeremony()
        return () => null
      },
    })
    mount(Host)
    expect(exposed?.phase.value).toBe('idle')
  })
})
