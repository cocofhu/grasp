/** Canvas particle mesh background aligned to the clarified DEMO (no jQuery). */

type ParticleMeshConfig = {
  maxParticles: number
  connectDistance: number
  mouseRadius: number
}

export const DEFAULT_PARTICLE_MESH_CONFIG: ParticleMeshConfig = {
  maxParticles: 250,
  connectDistance: 100,
  mouseRadius: 150,
}

type Rgb = { r: number; g: number; b: number }

type Dot = {
  x: number
  y: number
  vx: number
  vy: number
  radius: number
  color: Rgb & { style: string }
}

export type ParticleMeshMouse = { x: number; y: number }

function colorValue(min = 0): number {
  return Math.floor(Math.random() * 255 + min)
}

function rgba(r: number, g: number, b: number, a: number): string {
  return `rgba(${r},${g},${b},${a})`
}

function makeColor(min = 0): Dot['color'] {
  const r = colorValue(min)
  const g = colorValue(min)
  const b = colorValue(min)
  return { r, g, b, style: rgba(r, g, b, 0.85) }
}

function mix(a: number, w1: number, b: number, w2: number): number {
  return (a * w1 + b * w2) / (w1 + w2)
}

function averageColor(a: Dot, b: Dot): string {
  const c1 = a.color
  const c2 = b.color
  return rgba(
    Math.floor(mix(c1.r, a.radius, c2.r, b.radius)),
    Math.floor(mix(c1.g, a.radius, c2.g, b.radius)),
    Math.floor(mix(c1.b, a.radius, c2.b, b.radius)),
    0.75,
  )
}

function createDot(width: number, height: number): Dot {
  return {
    x: Math.random() * width,
    y: Math.random() * height,
    vx: -0.5 + Math.random(),
    vy: -0.5 + Math.random(),
    radius: Math.random() * 2,
    color: makeColor(),
  }
}

export type ParticleMeshController = {
  rebuild: (width: number, height: number) => void
  setMouse: (x: number, y: number) => void
  resetMouseToCenter: (width: number, height: number) => void
  paint: (ctx: CanvasRenderingContext2D, width: number, height: number) => void
  step: (width: number, height: number) => void
  getMouse: () => ParticleMeshMouse
  getDotCount: () => number
}

export function createParticleMeshController(
  cfg: ParticleMeshConfig = DEFAULT_PARTICLE_MESH_CONFIG,
): ParticleMeshController {
  let dots: Dot[] = []
  const mouse: ParticleMeshMouse = { x: 0, y: 0 }

  function particleCount(width: number, height: number): number {
    const area = width * height
    return Math.max(80, Math.min(cfg.maxParticles, Math.floor(area / 7000)))
  }

  function rebuild(width: number, height: number) {
    mouse.x = width / 2
    mouse.y = height / 2
    const n = particleCount(width, height)
    dots = []
    for (let i = 0; i < n; i++) dots.push(createDot(width, height))
  }

  function setMouse(x: number, y: number) {
    mouse.x = x
    mouse.y = y
  }

  function resetMouseToCenter(width: number, height: number) {
    mouse.x = width / 2
    mouse.y = height / 2
  }

  function move(width: number, height: number) {
    for (let i = 0; i < dots.length; i++) {
      const d = dots[i]
      if (d.y < 0 || d.y > height) d.vy = -d.vy
      else if (d.x < 0 || d.x > width) d.vx = -d.vx
      d.x += d.vx
      d.y += d.vy
    }
  }

  function connect(ctx: CanvasRenderingContext2D) {
    for (let i = 0; i < dots.length; i++) {
      for (let j = i + 1; j < dots.length; j++) {
        const a = dots[i]
        const b = dots[j]
        const dx = a.x - b.x
        const dy = a.y - b.y
        if (Math.abs(dx) < cfg.connectDistance && Math.abs(dy) < cfg.connectDistance) {
          if (
            Math.abs(a.x - mouse.x) < cfg.mouseRadius &&
            Math.abs(a.y - mouse.y) < cfg.mouseRadius
          ) {
            ctx.beginPath()
            ctx.lineWidth = 0.35
            ctx.strokeStyle = averageColor(a, b)
            ctx.moveTo(a.x, a.y)
            ctx.lineTo(b.x, b.y)
            ctx.stroke()
          }
        }
      }
    }
  }

  function paint(ctx: CanvasRenderingContext2D, width: number, height: number) {
    ctx.clearRect(0, 0, width, height)
    connect(ctx)
    for (let i = 0; i < dots.length; i++) {
      const d = dots[i]
      ctx.beginPath()
      ctx.fillStyle = d.color.style
      ctx.arc(d.x, d.y, d.radius, 0, Math.PI * 2, false)
      ctx.fill()
    }
  }

  function step(width: number, height: number) {
    move(width, height)
  }

  return {
    rebuild,
    setMouse,
    resetMouseToCenter,
    paint,
    step,
    getMouse: () => ({ ...mouse }),
    getDotCount: () => dots.length,
  }
}

export function prefersReducedMotion(): boolean {
  if (typeof window === 'undefined' || !window.matchMedia) return false
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}
