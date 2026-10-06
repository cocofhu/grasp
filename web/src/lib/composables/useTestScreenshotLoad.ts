import { onBeforeUnmount, ref, watch, type Ref } from 'vue'
import { api } from '@/lib/api/api'
import type { Artifact } from '@/lib/shared/types'

export type TestScreenshotInput = {
  artifact?: string
  mimeType?: string
  caption?: string
}

export type TestScreenshotState =
  | { status: 'loading'; artifact: string; caption?: string }
  | { status: 'success'; src: string; caption?: string; blobUrl: string }
  | { status: 'error'; artifact: string; caption?: string }

/** Run statuses where a final screenshot error (red frame) is allowed. */
const TERMINAL_RUN_STATUSES = new Set(['completed', 'failed', 'cancelled'])

/** True when red-frame error is allowed. Missing/empty runStatus defaults to terminal (preview-safe). */
export function allowsScreenshotError(runStatus?: string | null): boolean {
  if (runStatus == null || runStatus === '') return true
  return TERMINAL_RUN_STATUSES.has(runStatus)
}

function resolveArtifact(name: string, artifacts: Artifact[]): Artifact | undefined {
  return artifacts.find((a) => a.name === name)
}

/** Content identity: screenshot name + matched artifact id|size|updatedAt|etag. */
export function buildScreenshotContentFingerprint(
  s: TestScreenshotInput,
  artifacts: Artifact[],
): string {
  const artifactName = s.artifact?.trim() || ''
  if (!artifactName) return 'unknown'
  const art = resolveArtifact(artifactName, artifacts)
  if (!art) return `${artifactName}|missing`
  return `${artifactName}|${art.id}|${art.sizeBytes ?? 0}|${art.updatedAt ?? ''}|${art.etag ?? ''}`
}

function withCaption(st: TestScreenshotState, caption?: string): TestScreenshotState {
  if (st.caption === caption) return st
  return { ...st, caption }
}

/** Lazy-load test screenshots by artifact name. */
export function useTestScreenshotLoad(
  screenshots: Ref<TestScreenshotInput[]>,
  artifacts: Ref<Artifact[]>,
  runStatus?: Ref<string | undefined | null>,
) {
  const states = ref<TestScreenshotState[]>([])
  const contentFingerprints = ref<string[]>([])
  const blobUrls = new Set<string>()
  const itemGens: number[] = []
  /** Indices that finished a fetch/resolve attempt unsuccessfully (not in-flight). */
  const softFailed: boolean[] = []

  function revokeOne(url: string | undefined) {
    if (!url || !blobUrls.has(url)) return
    URL.revokeObjectURL(url)
    blobUrls.delete(url)
  }

  function revokeAll() {
    for (const url of blobUrls) URL.revokeObjectURL(url)
    blobUrls.clear()
  }

  function failureState(
    artifact: string,
    caption: string | undefined,
    prev: TestScreenshotState | undefined,
  ): TestScreenshotState {
    if (prev?.status === 'success') {
      return withCaption(prev, caption)
    }
    if (allowsScreenshotError(runStatus?.value)) {
      return { status: 'error', artifact, caption }
    }
    return { status: 'loading', artifact, caption }
  }

  async function fetchArtifact(
    artifact: string,
    caption: string | undefined,
    index: number,
    gen: number,
    prev: TestScreenshotState | undefined,
  ) {
    const id = resolveArtifact(artifact, artifacts.value)?.id ?? null
    if (!id) {
      if (itemGens[index] !== gen) return
      softFailed[index] = true
      states.value[index] = failureState(artifact, caption, prev)
      return
    }
    try {
      const res = await fetch(api.artifactDownloadUrl(id), { credentials: 'include' })
      if (!res.ok) throw new Error(String(res.status))
      const blob = await res.blob()
      if (itemGens[index] !== gen) return
      softFailed[index] = false
      const blobUrl = URL.createObjectURL(blob)
      blobUrls.add(blobUrl)
      const old = states.value[index]
      if (old?.status === 'success' && old.blobUrl !== blobUrl) {
        revokeOne(old.blobUrl)
      }
      states.value[index] = { status: 'success', src: blobUrl, blobUrl, caption }
    } catch {
      if (itemGens[index] !== gen) return
      softFailed[index] = true
      // Prefer the frame we started from (SWR), not a mid-flight state.
      states.value[index] = failureState(artifact, caption, prev)
    }
  }

  function startFetch(i: number, s: TestScreenshotInput, prev: TestScreenshotState | undefined) {
    const artifact = s.artifact?.trim()
    if (!artifact) return
    softFailed[i] = false
    itemGens[i] = (itemGens[i] || 0) + 1
    const gen = itemGens[i]
    void fetchArtifact(artifact, s.caption, i, gen, prev)
  }

  watch(
    [screenshots, artifacts, () => runStatus?.value],
    () => {
      const items = screenshots.value || []
      const arts = artifacts.value || []
      const prevStates = states.value
      const prevFps = contentFingerprints.value
      const nextStates: TestScreenshotState[] = new Array(items.length)
      const nextFps: string[] = new Array(items.length)
      const toFetch: number[] = []

      for (let i = 0; i < items.length; i++) {
        const s = items[i]
        const contentFp = buildScreenshotContentFingerprint(s, arts)
        nextFps[i] = contentFp
        const prev = prevStates[i]
        const prevFp = prevFps[i]

        // F1: same content identity + already painted → keep frame (poll noise / new array refs).
        // If a prior SWR fetch soft-failed while we still show a success frame, keep the
        // frame but background-retry when the artifact is still resolvable (transient 5xx).
        if (contentFp === prevFp && prev?.status === 'success') {
          nextStates[i] = withCaption(prev, s.caption)
          if (softFailed[i]) {
            const artifact = s.artifact?.trim()
            if (artifact && resolveArtifact(artifact, arts)) toFetch.push(i)
          }
          continue
        }

        // Same identity while loading/error: in-flight stays put; soft-fail may retry or promote.
        if (contentFp === prevFp && prev) {
          if (prev.status === 'loading' || prev.status === 'error') {
            const artifact = s.artifact?.trim() || '(unknown)'
            const art = artifact !== '(unknown)' ? resolveArtifact(artifact, arts) : undefined
            if (!art) {
              softFailed[i] = true
              nextStates[i] = failureState(artifact, s.caption, undefined)
            } else if (prev.status === 'error' && !allowsScreenshotError(runStatus?.value)) {
              // Non-terminal again: demote hard error to loading and retry when resolvable.
              nextStates[i] = { status: 'loading', artifact, caption: s.caption }
              if (art) toFetch.push(i)
            } else if (prev.status === 'loading' && softFailed[i]) {
              // Soft-fail: distinguish from in-flight. Terminal → promote error (F4);
              // non-terminal → retry on poll when artifact is still resolvable (transient 5xx).
              if (allowsScreenshotError(runStatus?.value)) {
                nextStates[i] = failureState(artifact, s.caption, undefined)
              } else {
                nextStates[i] = withCaption(prev, s.caption)
                if (art) toFetch.push(i)
              }
            } else {
              // In-flight loading, or terminal hard error: keep.
              nextStates[i] = withCaption(prev, s.caption)
            }
            continue
          }
        }

        // Identity changed or first paint.
        const artifact = s.artifact?.trim()
        if (!artifact) {
          softFailed[i] = true
          nextStates[i] = failureState('(unknown)', s.caption, prev)
          continue
        }

        const art = resolveArtifact(artifact, arts)
        // F3: keep prior success until the new blob is ready (silent replace).
        if (prev?.status === 'success') {
          nextStates[i] = withCaption(prev, s.caption)
          if (art) toFetch.push(i)
          else {
            softFailed[i] = true
            nextStates[i] = failureState(artifact, s.caption, prev)
          }
          continue
        }

        if (!art) {
          softFailed[i] = true
          nextStates[i] = failureState(artifact, s.caption, prev)
          continue
        }

        nextStates[i] = { status: 'loading', artifact, caption: s.caption }
        toFetch.push(i)
      }

      // Drop blobs for removed trailing items.
      for (let i = items.length; i < prevStates.length; i++) {
        const st = prevStates[i]
        if (st?.status === 'success') revokeOne(st.blobUrl)
        itemGens[i] = (itemGens[i] || 0) + 1
        softFailed[i] = false
      }

      states.value = nextStates
      contentFingerprints.value = nextFps

      for (const i of toFetch) {
        startFetch(i, items[i], prevStates[i])
      }
    },
    { immediate: true, deep: true },
  )

  onBeforeUnmount(revokeAll)

  const successIndices = () =>
    states.value
      .map((st, i) => (st.status === 'success' ? i : -1))
      .filter((i) => i >= 0)

  return { states, successIndices }
}
