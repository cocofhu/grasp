export const HUE_COUNT = 8

/** Stable 1..8 palette slot for a name (FNV-1a), so an Agent keeps its color everywhere. */
export function agentHueIndex(name: string): number {
  let h = 0x811c9dc5
  for (const ch of String(name || '')) {
    h ^= ch.codePointAt(0)!
    h = Math.imul(h, 0x01000193) >>> 0
  }
  return (h % HUE_COUNT) + 1
}

export function agentHueVar(name: string): string {
  return `--c-hue-${agentHueIndex(name)}`
}

export function agentInitial(name: string): string {
  const s = String(name || '').trim()
  if (!s) return '?'
  return Array.from(s)[0]!.toUpperCase()
}
