// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'

vi.mock('@/lib/api/api', () => ({
  api: { sandboxAudioWsUrl: (id: number) => `ws://host/sandbox-audio/${id}/ws` },
}))

import { useDesktopAudio } from './useDesktopAudio'

type Sock = { url: string; closed: boolean; onopen: (() => void) | null; onclose: (() => void) | null; close: () => void }

function harness(initial: number | undefined) {
  const id = ref(initial)
  const sockets: Sock[] = []
  const deps = {
    createContext: () =>
      ({ currentTime: 0, resume: () => Promise.resolve(), close: () => Promise.resolve() }) as unknown as AudioContext,
    createSocket: (url: string) => {
      const s: Sock = {
        url,
        closed: false,
        onopen: null,
        onclose: null,
        close() {
          this.closed = true
        },
      }
      sockets.push(s)
      return s as unknown as WebSocket
    },
  }
  let audio!: ReturnType<typeof useDesktopAudio>
  const w = mount(
    defineComponent({
      setup() {
        audio = useDesktopAudio(() => id.value, deps)
        return () => h('div')
      },
    }),
  )
  return { w, id, sockets, audio: () => audio }
}

describe('useDesktopAudio', () => {
  it('toggles sound for the sandbox and stops on change and unmount', async () => {
    const { w, id, sockets, audio } = harness(3)
    expect(audio().available.value).toBe(true)
    audio().toggle()
    expect(sockets[0]!.url).toBe('ws://host/sandbox-audio/3/ws')
    expect(audio().state.value).toBe('connecting')
    sockets[0]!.onopen?.()
    expect(audio().state.value).toBe('on')
    audio().toggle()
    expect(sockets[0]!.closed).toBe(true)
    expect(audio().state.value).toBe('off')

    audio().toggle()
    sockets[1]!.onclose?.()
    expect(audio().state.value).toBe('error')
    audio().toggle() // retry after an error
    expect(sockets).toHaveLength(3)

    id.value = 4
    await nextTick()
    expect(sockets[2]!.closed).toBe(true)
    expect(audio().state.value).toBe('off')

    audio().toggle()
    w.unmount()
    expect(sockets[3]!.closed).toBe(true)
  })

  it('is unavailable without a sandbox', () => {
    const { sockets, audio } = harness(undefined)
    expect(audio().available.value).toBe(false)
    audio().toggle()
    expect(sockets).toHaveLength(0)
    expect(audio().state.value).toBe('off')
  })
})
