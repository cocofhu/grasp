// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('./executor', () => ({
  createExecutor: () => ({ run: async () => ({ ok: true }), dispose() {} }),
}))

const { create } = await import('./entry')

function parts() {
  const host = document.querySelector('grasp-page-control') as HTMLElement | null
  const shadow = host?.shadowRoot
  return {
    host,
    cursor: shadow?.querySelector('.cursor') as HTMLElement | undefined,
    mask: shadow?.querySelector('.mask') as HTMLElement | undefined,
  }
}

const frames = () => new Promise((r) => setTimeout(r, 400))

describe('page control pointer', () => {
  let pc: ReturnType<typeof create> | undefined
  afterEach(() => {
    pc?.dispose()
    pc = undefined
  })

  it('appears in the viewport centre when armed and hides when disarmed', () => {
    pc = create()
    expect(parts().host).toBeNull()
    pc.setArmed(true)
    const { cursor, mask } = parts()
    expect(cursor?.hidden).toBe(false)
    expect(mask?.hidden).toBe(true)
    expect(cursor?.style.transform).toBe(`translate(${window.innerWidth / 2}px, ${window.innerHeight / 2}px)`)
    pc.setArmed(false)
    expect(cursor?.hidden).toBe(true)
  })

  it('glides to the pointer target and ripples on click', async () => {
    pc = create()
    pc.setArmed(true)
    const { cursor } = parts()
    window.dispatchEvent(new CustomEvent('PageAgent::MovePointerTo', { detail: { x: 40, y: 60 } }))
    await frames()
    expect(cursor?.style.transform).toBe('translate(40px, 60px)')
    window.dispatchEvent(new CustomEvent('PageAgent::ClickPointer'))
    expect(cursor?.classList.contains('clicking')).toBe(true)
  })

  it('blocks input only while an action runs', async () => {
    pc = create()
    pc.setArmed(true)
    const { mask } = parts()
    const run = pc.run({ action: 'click', args: { index: 0 } } as never).then(() => mask?.hidden)
    expect(mask?.hidden).toBe(false)
    expect(await run).toBe(true)
  })

  it('removes the host on dispose', () => {
    pc = create()
    pc.setArmed(true)
    pc.dispose()
    pc = undefined
    expect(parts().host).toBeNull()
  })
})
