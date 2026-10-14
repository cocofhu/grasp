// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import { createJumpGameController } from './homeLogoJumpGame'

function mockCtx() {
  return {
    clearRect: vi.fn(),
    fillRect: vi.fn(),
    strokeRect: vi.fn(),
    beginPath: vi.fn(),
    moveTo: vi.fn(),
    lineTo: vi.fn(),
    closePath: vi.fn(),
    fill: vi.fn(),
    stroke: vi.fn(),
    arc: vi.fn(),
    arcTo: vi.fn(),
    ellipse: vi.fn(),
    quadraticCurveTo: vi.fn(),
    save: vi.fn(),
    restore: vi.fn(),
    translate: vi.fn(),
    scale: vi.fn(),
    fillText: vi.fn(),
    createLinearGradient: vi.fn(() => ({ addColorStop: vi.fn() })),
  } as unknown as CanvasRenderingContext2D
}

describe('homeLogoJumpGame', () => {
  it('starts idle and enters playing after enter sequence', () => {
    const onPhase = vi.fn()
    const onScore = vi.fn()
    const game = createJumpGameController({ onPhase, onScore })
    game.resize(400, 700)
    expect(game.getSnapshot().phase).toBe('idle')
    game.startEnter(200, 200)
    expect(game.getSnapshot().phase).toBe('entering')
    expect(onPhase).toHaveBeenCalledWith('entering')
    for (let i = 0; i < 50; i++) game.step(20)
    expect(game.getSnapshot().phase).toBe('playing')
    game.setKeys({ left: true, right: false })
    for (let i = 0; i < 10; i++) game.step(16)
    expect(game.getSnapshot().phase).toBe('playing')
    game.destroy()
    expect(game.getSnapshot().phase).toBe('idle')
  })

  it('paints without throwing while playing', () => {
    const game = createJumpGameController()
    game.resize(320, 480)
    game.startEnter(160, 120)
    const ctx = mockCtx()
    for (let i = 0; i < 40; i++) {
      game.step(16)
      expect(() => game.paint(ctx)).not.toThrow()
    }
    expect(ctx.clearRect).toHaveBeenCalled()
    game.destroy()
  })
})
