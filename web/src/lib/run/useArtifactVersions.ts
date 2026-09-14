import { ref } from 'vue'
import { api } from '@/lib/api/api'
import { isAbortError } from '@/lib/run/liveLogRehydrate'
import {
  artifactRevision,
  buildArtifactVersionChoices,
  type ArtifactVersionChoice,
  type ArtifactVersionMeta,
} from '@/lib/run/reactArtifactPreview'
import type { Artifact } from '@/lib/shared/types'

function versionContentKey(id: string, rev: number): string {
  return `${id}:${rev}`
}

/** Shared cache for artifact version lists + historical content. */
export function useArtifactVersions() {
  const archivedById = ref<Record<string, ArtifactVersionMeta[]>>({})
  const contentByKey = ref<Record<string, string>>({})
  const inflight = new Map<string, Promise<void>>()

  function choicesFor(artifact: Artifact | null | undefined): ArtifactVersionChoice[] {
    if (!artifact?.id) return []
    return buildArtifactVersionChoices(artifactRevision(artifact), archivedById.value[artifact.id] || [])
  }

  async function ensureVersions(id: string, signal?: AbortSignal, refresh = false): Promise<void> {
    const key = String(id || '').trim()
    if (!key || (!refresh && archivedById.value[key])) return
    const pending = inflight.get(key)
    if (pending) {
      await pending
      return
    }
    const task = (async () => {
      try {
        const rows = await api.artifactVersions(key, signal ? { signal } : undefined)
        archivedById.value = { ...archivedById.value, [key]: rows || [] }
      } catch (e) {
        if (isAbortError(e) || signal?.aborted) return
        archivedById.value = { ...archivedById.value, [key]: [] }
      } finally {
        inflight.delete(key)
      }
    })()
    inflight.set(key, task)
    await task
  }

  async function ensureVersionContent(id: string, rev: number, signal?: AbortSignal): Promise<string> {
    const key = versionContentKey(id, rev)
    if (contentByKey.value[key] !== undefined) return contentByKey.value[key]
    const row = await api.artifactVersionContent(id, rev, signal ? { signal } : undefined)
    const content = row.content ?? ''
    contentByKey.value = { ...contentByKey.value, [key]: content }
    return content
  }

  function historicalContent(id: string, rev: number): string | undefined {
    return contentByKey.value[versionContentKey(id, rev)]
  }

  function resolveVersionedArtifact(
    artifact: Artifact,
    choice: ArtifactVersionChoice | null | undefined,
  ): Artifact {
    if (!choice || choice.latest || !choice.available) return artifact
    const html = historicalContent(artifact.id, choice.revision)
    if (html === undefined) return artifact
    return {
      ...artifact,
      content: html,
      sizeBytes: html.length,
    }
  }

  return {
    archivedById,
    choicesFor,
    ensureVersions,
    ensureVersionContent,
    historicalContent,
    resolveVersionedArtifact,
  }
}
