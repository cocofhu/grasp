/** Doodle-jump style easter egg for the home logo (canvas + rAF). */

export const JUMP_COLORS = {
  bg: "#121214",
  bgDeep: "#0A0A0B",
  coreStart: "#7B61FF",
  coreEnd: "#A78BFA",
  ink: "#3b2f4a",
  pink: "#ff9db5",
  sky: "#9fe3ff",
  sun: "#ffd36e",
  cream: "#fff8ef",
  lav: "#e9e3ff",
  platform: "#2a2438",
  platformTop: "#7B61FF",
  spring: "#4ade80",
  cloud: "rgba(233,227,255,0.12)",
} as const

export type JumpPhase = "idle" | "entering" | "playing" | "exiting"

export type JumpKeys = { left: boolean; right: boolean }

type PlatformKind = "normal" | "spring"

type Platform = {
  x: number
  y: number
  w: number
  h: number
  kind: PlatformKind
}

type Particle = {
  x: number
  y: number
  vx: number
  vy: number
  life: number
  maxLife: number
  color: string
  size: number
}

type Cloud = { x: number; y: number; r: number; parallax: number }

export type JumpGameSnapshot = {
  score: number
  lives: number
  phase: JumpPhase
  cameraY: number
}

export type JumpGameCallbacks = {
  onScore?: (score: number) => void
  onPhase?: (phase: JumpPhase) => void
  onGameOver?: (score: number) => void
}

export type JumpGameController = {
  resize: (width: number, height: number) => void
  setKeys: (keys: JumpKeys) => void
  startEnter: (originX: number, originY: number) => void
  step: (dtMs: number) => JumpGameSnapshot
  paint: (ctx: CanvasRenderingContext2D) => void
  getSnapshot: () => JumpGameSnapshot
  destroy: () => void
}

const GRAVITY = 0.42
const JUMP_V = -11.2
const SPRING_V = -16.5
const MOVE_ACCEL = 0.75
const MOVE_MAX = 6.2
const FRICTION = 0.86
const PLAYER_R = 16
const PLATFORM_W = 72
const PLATFORM_H = 12
const PLATFORM_GAP_MIN = 70
const PLATFORM_GAP_MAX = 105
const ENTER_MS = 720
const EXIT_MS = 900

function clamp(n: number, a: number, b: number) {
  return Math.max(a, Math.min(b, n))
}

function rand(a: number, b: number) {
  return a + Math.random() * (b - a)
}

function hexToRgba(hex: string, a: number) {
  const h = hex.replace("#", "")
  const n = parseInt(h.length === 3 ? h.split("").map((c) => c + c).join("") : h, 16)
  const r = (n >> 16) & 255
  const g = (n >> 8) & 255
  const b = n & 255
  return `rgba(${r},${g},${b},${a})`
}

export function createJumpGameController(cb: JumpGameCallbacks = {}): JumpGameController {
  let width = 0
  let height = 0
  let phase: JumpPhase = "idle"
  let keys: JumpKeys = { left: false, right: false }

  let playerX = 0
  let playerY = 0
  let vx = 0
  let vy = 0
  let squash = 1 // y scale; x = 1/squash-ish
  let stretchX = 1
  let stretchY = 1
  let facing = 1

  let cameraY = 0 // world y of top of view; increases as we go up (player y decreases)
  let maxHeight = 0 // highest (most negative) world y reached
  let score = 0
  let lives = 3

  let platforms: Platform[] = []
  let particles: Particle[] = []
  let clouds: Cloud[] = []

  let enterT = 0
  let exitT = 0
  let originX = 0
  let originY = 0
  let exitStartCam = 0
  let deadFall = false

  function setPhase(p: JumpPhase) {
    if (phase === p) return
    phase = p
    cb.onPhase?.(p)
  }

  function spawnClouds() {
    clouds = []
    for (let i = 0; i < 10; i++) {
      clouds.push({
        x: rand(0, Math.max(width, 1)),
        y: rand(-height * 4, height),
        r: rand(28, 70),
        parallax: rand(0.15, 0.45),
      })
    }
  }

  function makePlatform(y: number, kind?: PlatformKind): Platform {
    const w = PLATFORM_W + rand(-8, 16)
    return {
      x: rand(24, Math.max(25, width - w - 24)),
      y,
      w,
      h: PLATFORM_H,
      kind: kind ?? (Math.random() < 0.12 ? "spring" : "normal"),
    }
  }

  function seedPlatforms() {
    platforms = []
    // starter under player
    platforms.push({
      x: width / 2 - PLATFORM_W / 2,
      y: originY + 28,
      w: PLATFORM_W + 20,
      h: PLATFORM_H,
      kind: "normal",
    })
    let y = originY - 40
    for (let i = 0; i < 18; i++) {
      y -= rand(PLATFORM_GAP_MIN, PLATFORM_GAP_MAX)
      platforms.push(makePlatform(y))
    }
  }

  function ensurePlatforms() {
    const topNeeded = cameraY - height
    let minY = platforms.reduce((m, p) => Math.min(m, p.y), originY)
    while (minY > topNeeded - 40) {
      minY -= rand(PLATFORM_GAP_MIN, PLATFORM_GAP_MAX)
      platforms.push(makePlatform(minY))
    }
    // drop far below camera
    const cut = cameraY + height * 1.6
    platforms = platforms.filter((p) => p.y < cut)
  }

  function burst(x: number, y: number, color: string, n = 10) {
    for (let i = 0; i < n; i++) {
      const ang = rand(0, Math.PI * 2)
      const sp = rand(1.2, 4.5)
      particles.push({
        x,
        y,
        vx: Math.cos(ang) * sp,
        vy: Math.sin(ang) * sp - 1,
        life: rand(280, 520),
        maxLife: 520,
        color,
        size: rand(2, 4.5),
      })
    }
  }

  function trail(x: number, y: number) {
    particles.push({
      x: x + rand(-4, 4),
      y: y + rand(-2, 6),
      vx: rand(-0.4, 0.4),
      vy: rand(0.2, 1.2),
      life: rand(180, 320),
      maxLife: 320,
      color: Math.random() < 0.5 ? JUMP_COLORS.pink : JUMP_COLORS.sky,
      size: rand(1.5, 3),
    })
  }

  function resize(w: number, h: number) {
    width = Math.max(1, w)
    height = Math.max(1, h)
    if (phase === "idle") {
      playerX = width / 2
      playerY = height * 0.28
    }
  }

  function startEnter(ox: number, oy: number) {
    originX = ox
    originY = oy
    playerX = ox
    playerY = oy
    vx = 0
    vy = JUMP_V * 0.55
    stretchX = 1.25
    stretchY = 0.72
    cameraY = oy - height * 0.35
    maxHeight = oy
    score = 0
    lives = 3
    deadFall = false
    enterT = 0
    exitT = 0
    particles = []
    spawnClouds()
    seedPlatforms()
    setPhase("entering")
    cb.onScore?.(0)
  }

  function beginExit() {
    if (phase === "exiting") return
    exitT = 0
    exitStartCam = cameraY
    deadFall = true
    setPhase("exiting")
  }

  function stepParticles(dt: number) {
    const f = dt / 16.67
    for (const p of particles) {
      p.x += p.vx * f
      p.y += p.vy * f
      p.vy += 0.08 * f
      p.life -= dt
    }
    particles = particles.filter((p) => p.life > 0)
  }

  function collidePlatforms() {
    if (vy < 0) return
    const left = playerX - PLAYER_R * 0.7
    const right = playerX + PLAYER_R * 0.7
    const foot = playerY + PLAYER_R * 0.85
    for (const p of platforms) {
      if (right < p.x || left > p.x + p.w) continue
      if (foot < p.y || foot > p.y + p.h + 10) continue
      // land
      playerY = p.y - PLAYER_R * 0.85
      if (p.kind === "spring") {
        vy = SPRING_V
        stretchX = 0.75
        stretchY = 1.35
        burst(playerX, p.y, JUMP_COLORS.spring, 14)
        burst(playerX, p.y, JUMP_COLORS.sun, 6)
      } else {
        vy = JUMP_V
        stretchX = 1.3
        stretchY = 0.7
        burst(playerX, p.y, JUMP_COLORS.coreStart, 6)
      }
      return
    }
  }

  function stepPlaying(dt: number) {
    const f = dt / 16.67
    if (keys.left) {
      vx -= MOVE_ACCEL * f
      facing = -1
    } else if (keys.right) {
      vx += MOVE_ACCEL * f
      facing = 1
    } else {
      vx *= Math.pow(FRICTION, f)
    }
    vx = clamp(vx, -MOVE_MAX, MOVE_MAX)

    vy += GRAVITY * f
    playerX += vx * f
    playerY += vy * f

    // wrap horizontally
    if (playerX < -PLAYER_R) playerX = width + PLAYER_R
    if (playerX > width + PLAYER_R) playerX = -PLAYER_R

    collidePlatforms()

    // squash recover
    stretchX += (1 - stretchX) * 0.12 * f
    stretchY += (1 - stretchY) * 0.12 * f

    if (vy < -2) trail(playerX, playerY + PLAYER_R)

    // camera only goes up (cameraY decreases in world? we define cameraY as world-y at top)
    // player ascending => playerY decreases. Keep player around 40% from top.
    const targetCam = playerY - height * 0.4
    if (targetCam < cameraY) {
      cameraY = targetCam
    }

    if (playerY < maxHeight) {
      maxHeight = playerY
      const next = Math.floor((originY - maxHeight) / 40)
      if (next !== score) {
        score = next
        cb.onScore?.(score)
      }
    }

    ensurePlatforms()

    // fall below view
    if (playerY > cameraY + height + 40) {
      lives -= 1
      if (lives <= 0) {
        beginExit()
      } else {
        // soft respawn on highest platform in view
        const visible = platforms
          .filter((p) => p.y > cameraY && p.y < cameraY + height * 0.85)
          .sort((a, b) => a.y - b.y)
        const land = visible[0] ?? platforms[0]
        if (land) {
          playerX = land.x + land.w / 2
          playerY = land.y - PLAYER_R
          vx = 0
          vy = JUMP_V * 0.4
          burst(playerX, playerY, JUMP_COLORS.pink, 8)
        }
      }
    }
  }

  function stepEnter(dt: number) {
    enterT += dt
    const f = dt / 16.67
    // squash then leap
    if (enterT < 160) {
      stretchX = 1.35
      stretchY = 0.65
    } else {
      stretchX += (0.85 - stretchX) * 0.15 * f
      stretchY += (1.25 - stretchY) * 0.15 * f
      vy = JUMP_V * 1.05
      playerY += vy * f * 0.35
      const targetCam = playerY - height * 0.4
      if (targetCam < cameraY) cameraY = targetCam
    }
    if (enterT >= ENTER_MS) {
      stretchX = 1
      stretchY = 1
      setPhase("playing")
    }
  }

  function stepExit(dt: number) {
    exitT += dt
    const t = clamp(exitT / EXIT_MS, 0, 1)
    // ease-in-out roll down camera toward origin
    const ease = t < 0.5 ? 2 * t * t : 1 - Math.pow(-2 * t + 2, 2) / 2
    const targetCam = originY - height * 0.35
    cameraY = exitStartCam + (targetCam - exitStartCam) * ease

    // character falls toward origin with bounce at end
    const fallEase = 1 - Math.pow(1 - t, 2)
    playerX = playerX + (originX - playerX) * 0.08
    playerY = playerY + (originY - playerY) * (0.04 + fallEase * 0.12)
    if (t > 0.75) {
      const bounce = Math.sin((t - 0.75) * Math.PI * 4) * (1 - t) * 10
      playerY = originY - bounce
      stretchX = 1.2 - (t - 0.75) * 0.8
      stretchY = 0.8 + (t - 0.75) * 0.8
    }
    if (deadFall && t < 0.5) {
      // sweep platforms up visually by filtering lower ones
      platforms = platforms.filter((p) => p.y < cameraY + height)
    }
    if (t >= 1) {
      playerX = originX
      playerY = originY
      stretchX = 1
      stretchY = 1
      vx = 0
      vy = 0
      cameraY = targetCam
      platforms = []
      particles = []
      setPhase("idle")
      cb.onGameOver?.(score)
    }
  }

  function step(dtMs: number): JumpGameSnapshot {
    const dt = clamp(dtMs, 0, 48)
    if (phase === "entering") stepEnter(dt)
    else if (phase === "playing") stepPlaying(dt)
    else if (phase === "exiting") stepExit(dt)
    stepParticles(dt)
    return getSnapshot()
  }

  function worldToScreen(x: number, y: number) {
    return { x, y: y - cameraY }
  }

  function paintPlayer(ctx: CanvasRenderingContext2D, x: number, y: number) {
    ctx.save()
    ctx.translate(x, y)
    ctx.scale(stretchX * facing, stretchY)

    // ink shadow
    ctx.fillStyle = hexToRgba(JUMP_COLORS.ink, 0.35)
    ctx.beginPath()
    ctx.ellipse(3, 3, PLAYER_R * 0.95, PLAYER_R * 0.95, Math.PI / 4, 0, Math.PI * 2)
    ctx.fill()

    // diamond core
    const g = ctx.createLinearGradient(-PLAYER_R, -PLAYER_R, PLAYER_R, PLAYER_R)
    g.addColorStop(0, JUMP_COLORS.coreStart)
    g.addColorStop(1, JUMP_COLORS.coreEnd)
    ctx.fillStyle = g
    ctx.strokeStyle = JUMP_COLORS.ink
    ctx.lineWidth = 4
    ctx.beginPath()
    ctx.moveTo(0, -PLAYER_R)
    ctx.lineTo(PLAYER_R, 0)
    ctx.lineTo(0, PLAYER_R)
    ctx.lineTo(-PLAYER_R, 0)
    ctx.closePath()
    ctx.fill()
    ctx.stroke()

    // highlight
    ctx.fillStyle = hexToRgba(JUMP_COLORS.cream, 0.9)
    ctx.beginPath()
    ctx.arc(-5, -6, 3.2, 0, Math.PI * 2)
    ctx.fill()

    // grips
    ctx.lineCap = "round"
    ctx.strokeStyle = JUMP_COLORS.ink
    ctx.lineWidth = 5
    ctx.beginPath()
    ctx.moveTo(-10, -14)
    ctx.quadraticCurveTo(-22, -2, -16, 14)
    ctx.stroke()
    ctx.strokeStyle = JUMP_COLORS.pink
    ctx.lineWidth = 3
    ctx.beginPath()
    ctx.moveTo(-10, -14)
    ctx.quadraticCurveTo(-22, -2, -16, 14)
    ctx.stroke()

    ctx.strokeStyle = JUMP_COLORS.ink
    ctx.lineWidth = 5
    ctx.beginPath()
    ctx.moveTo(10, 14)
    ctx.quadraticCurveTo(22, 2, 16, -14)
    ctx.stroke()
    ctx.strokeStyle = JUMP_COLORS.sky
    ctx.lineWidth = 3
    ctx.beginPath()
    ctx.moveTo(10, 14)
    ctx.quadraticCurveTo(22, 2, 16, -14)
    ctx.stroke()

    ctx.restore()
  }

  function paint(ctx: CanvasRenderingContext2D) {
    ctx.clearRect(0, 0, width, height)
    if (phase === "idle") return

    // soft vignette bg (transparent so shell shows; light tint)
    const bg = ctx.createLinearGradient(0, 0, 0, height)
    bg.addColorStop(0, hexToRgba(JUMP_COLORS.bgDeep, 0.35))
    bg.addColorStop(1, hexToRgba(JUMP_COLORS.bg, 0.15))
    ctx.fillStyle = bg
    ctx.fillRect(0, 0, width, height)

    // clouds (parallax)
    for (const c of clouds) {
      const sy = (c.y - cameraY * c.parallax) % (height + 200)
      const y = ((sy + height + 200) % (height + 200)) - 100
      ctx.fillStyle = JUMP_COLORS.cloud
      ctx.beginPath()
      ctx.ellipse(c.x, y, c.r, c.r * 0.55, 0, 0, Math.PI * 2)
      ctx.fill()
    }

    // platforms
    for (const p of platforms) {
      const s = worldToScreen(p.x, p.y)
      if (s.y < -40 || s.y > height + 40) continue
      ctx.fillStyle = hexToRgba(JUMP_COLORS.ink, 0.45)
      ctx.fillRect(s.x + 3, s.y + 3, p.w, p.h)
      ctx.fillStyle = JUMP_COLORS.platform
      ctx.strokeStyle = JUMP_COLORS.ink
      ctx.lineWidth = 2
      roundRect(ctx, s.x, s.y, p.w, p.h, 5)
      ctx.fill()
      ctx.stroke()
      ctx.fillStyle = JUMP_COLORS.platformTop
      ctx.fillRect(s.x + 2, s.y + 1, p.w - 4, 3)
      if (p.kind === "spring") {
        ctx.fillStyle = JUMP_COLORS.spring
        ctx.fillRect(s.x + p.w * 0.35, s.y - 8, p.w * 0.3, 8)
        ctx.strokeStyle = JUMP_COLORS.ink
        ctx.strokeRect(s.x + p.w * 0.35, s.y - 8, p.w * 0.3, 8)
      }
    }

    // particles
    for (const p of particles) {
      const s = worldToScreen(p.x, p.y)
      const a = clamp(p.life / p.maxLife, 0, 1)
      ctx.fillStyle = hexToRgba(p.color, a)
      ctx.beginPath()
      ctx.arc(s.x, s.y, p.size, 0, Math.PI * 2)
      ctx.fill()
    }

    const ps = worldToScreen(playerX, playerY)
    paintPlayer(ctx, ps.x, ps.y)

    // HUD
    ctx.font = "600 13px ui-sans-serif, system-ui, sans-serif"
    ctx.fillStyle = hexToRgba(JUMP_COLORS.cream, 0.9)
    ctx.fillText(`Score ${score}`, 16, 28)
    for (let i = 0; i < 3; i++) {
      ctx.fillStyle = i < lives ? JUMP_COLORS.pink : hexToRgba(JUMP_COLORS.ink, 0.5)
      ctx.beginPath()
      ctx.arc(width - 24 - i * 18, 24, 6, 0, Math.PI * 2)
      ctx.fill()
    }
  }

  function getSnapshot(): JumpGameSnapshot {
    return { score, lives, phase, cameraY }
  }

  function destroy() {
    platforms = []
    particles = []
    clouds = []
    setPhase("idle")
  }

  return {
    resize,
    setKeys: (k) => {
      keys = { ...k }
    },
    startEnter,
    step,
    paint,
    getSnapshot,
    destroy,
  }
}

function roundRect(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  w: number,
  h: number,
  r: number,
) {
  const rr = Math.min(r, w / 2, h / 2)
  ctx.beginPath()
  ctx.moveTo(x + rr, y)
  ctx.arcTo(x + w, y, x + w, y + h, rr)
  ctx.arcTo(x + w, y + h, x, y + h, rr)
  ctx.arcTo(x, y + h, x, y, rr)
  ctx.arcTo(x, y, x + w, y, rr)
  ctx.closePath()
}
