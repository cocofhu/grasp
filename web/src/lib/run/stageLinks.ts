import { shallowReactive } from 'vue'

/**
 * What a mounted artifact stage lets the chat beside it do: tool rows offer
 * "查看" for an artifact the call wrote and "打开预览" for set_preview. The
 * chat and the stage are siblings under varying layouts, so stages register
 * here by run id instead of through provide/inject.
 */
export interface StageLinks {
  hasArtifact(name: string): boolean
  openArtifact(name: string): void
  canOpenPreview(): boolean
  openPreview(): void
  /** Opens url in a new tab of the sandbox browser; false when there is none. */
  openUrl?(url: string): boolean
}

const LOOPBACK_HOSTS = new Set(['localhost', '127.0.0.1', '0.0.0.0', '[::1]'])

/** True for an http(s) URL on loopback, which only resolves inside the sandbox. */
export function isLoopbackUrl(href: string): boolean {
  try {
    const u = new URL(href)
    if (u.protocol !== 'http:' && u.protocol !== 'https:') return false
    const host = u.hostname.toLowerCase()
    return LOOPBACK_HOSTS.has(host) || host.endsWith('.localhost') || /^127(\.\d{1,3}){3}$/.test(host)
  } catch {
    return false
  }
}

const stages = shallowReactive(new Map<symbol, { runId: string; links: StageLinks }>())

/** Registers a stage for runId; returns the unregister function. */
export function registerStageLinks(runId: string, links: StageLinks): () => void {
  const key = Symbol(runId)
  stages.set(key, { runId, links })
  return () => {
    stages.delete(key)
  }
}

/**
 * The newest stage for runId. Without a run id (hosts that do not know it),
 * the only mounted stage, if exactly one run has one.
 */
export function stageLinksFor(runId?: string): StageLinks | null {
  let found: StageLinks | null = null
  const runs = new Set<string>()
  for (const s of stages.values()) {
    runs.add(s.runId)
    if (!runId || s.runId === runId) found = s.links
  }
  if (!runId && runs.size !== 1) return null
  return found
}
