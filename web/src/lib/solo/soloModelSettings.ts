import { ref, type Ref, type ShallowRef } from 'vue'
import { parseModelRoute } from '@/lib/agent/modelBindings'
import type { SoloSession } from '@/lib/api/apiTypes'
import type { SoloChatTransport } from './useSoloChat'

type ModelSettings = Pick<SoloSession, 'currentModelId' | 'currentConfigurationId' | 'currentModelSet' | 'modelNotice' | 'reasoningEffort' | 'reasoningEfforts' | 'backend' | 'model'>

function listedEfforts(efforts?: string[]) {
  return [...new Set((efforts || []).map(value => value.trim()).filter(Boolean))]
}

export function modelSettings(row: SoloSession): ModelSettings {
  return { currentConfigurationId: row.currentConfigurationId, backend: row.backend, model: row.model, currentModelId: row.currentModelId, currentModelSet: row.currentModelSet, modelNotice: row.modelNotice, reasoningEffort: row.reasoningEffort, reasoningEfforts: row.reasoningEfforts ? [...row.reasoningEfforts] : undefined }
}

type SoloModelSettingsContext = {
  sessionId: Ref<string>
  session: ShallowRef<SoloSession | null>
  sessionOpen: Ref<boolean>
  actionError: Ref<string>
  transport: Pick<SoloChatTransport, 'setSoloSessionModel' | 'setSoloSessionReasoningEffort'>
  disposed: () => boolean
  /** Bumped by the chat whenever a newer session row supersedes in-flight reads. */
  sessionGeneration: () => number
  patchSession: (patch: Partial<SoloSession>) => void
  errorText: (error: unknown) => string
}

/**
 * Optimistic model and reasoning-effort selection for one Solo session. Both
 * controls share one request queue; the last confirmed server row is restored
 * when the latest request fails or the session closes underneath it.
 */
export function createSoloModelSettings(ctx: SoloModelSettingsContext) {
  let epoch = 0
  let sequence = 0
  let pendingSettings = 0
  let pendingModels = 0
  let queue: Promise<void> = Promise.resolve()
  let confirmed: ModelSettings | null = null
  const modelChangePending = ref(false)

  /** Drops every in-flight request, e.g. after a runtime rollback. */
  function invalidate() {
    epoch++
    sequence++
    pendingSettings = 0
    pendingModels = 0
    modelChangePending.value = false
  }

  function reset() {
    invalidate()
    confirmed = null
    queue = Promise.resolve()
  }

  function confirm(row: SoloSession) {
    confirmed = modelSettings(row)
  }

  /** A fetched row keeps the user's pending selection instead of reverting it. */
  function mergeFetched(row: SoloSession): SoloSession {
    const cur = ctx.session.value
    if (pendingSettings > 0 && cur) return { ...row, ...modelSettings(cur) }
    confirm(row)
    return row
  }

  async function save(kind: 'model' | 'effort', value: string): Promise<boolean> {
    const id = ctx.sessionId.value
    const cur = ctx.session.value
    if (!id || !cur || !ctx.sessionOpen.value || ctx.disposed()) return false
    const route = kind === 'model' ? parseModelRoute(value.trim()) : undefined
    const next = route?.modelId ?? value.trim()
    const configurationId = route?.configurationId
    if (kind === 'model' ? cur.currentModelSet && (cur.currentModelId || '') === next && (configurationId === undefined || cur.currentConfigurationId === configurationId) : (cur.reasoningEffort || '') === next) return true
    const generation = ctx.sessionGeneration()
    const requestEpoch = epoch
    const requestSequence = ++sequence
    confirmed ||= modelSettings(cur)
    pendingSettings++
    if (kind === 'model') { pendingModels++; modelChangePending.value = true }
    ctx.actionError.value = ''
    ctx.patchSession(kind === 'model' ? { currentModelId: next, ...(configurationId === undefined ? {} : { currentConfigurationId: configurationId }), currentModelSet: true, reasoningEffort: '', reasoningEfforts: [], modelNotice: '' } : { reasoningEffort: next, modelNotice: '' })
    const isCurrentSession = () => !ctx.disposed() && id === ctx.sessionId.value && generation === ctx.sessionGeneration() && requestEpoch === epoch
    const latest = () => isCurrentSession() && requestSequence === sequence
    const request = queue.then(async () => {
      try {
        if (!isCurrentSession()) return false
        if (!ctx.sessionOpen.value) {
          if (latest() && confirmed) ctx.patchSession(confirmed)
          return false
        }
        let row = kind === 'model' ? configurationId === undefined ? await ctx.transport.setSoloSessionModel(id, next) : await ctx.transport.setSoloSessionModel(id, next, undefined, configurationId) : await ctx.transport.setSoloSessionReasoningEffort(id, next)
        if (!isCurrentSession() || row.id !== id) return false
        // A model switch clears an effort the new model does not offer. The
        // selector shows that gap as the highest level, so persist that token.
        if (kind === 'model' && latest() && !row.reasoningEffort?.trim()) {
          const highest = listedEfforts(row.reasoningEfforts).at(-1)
          if (highest) {
            row = await ctx.transport.setSoloSessionReasoningEffort(id, highest)
            if (!isCurrentSession() || row.id !== id) return false
          }
        }
        confirmed = modelSettings(row)
        if (latest()) ctx.patchSession(modelSettings(row))
        return true
      } catch (error) {
        if (latest()) {
          if (confirmed) ctx.patchSession(confirmed)
          ctx.actionError.value = ctx.errorText(error)
        }
        return false
      } finally {
        if (isCurrentSession()) {
          pendingSettings = Math.max(0, pendingSettings - 1)
          if (kind === 'model') pendingModels = Math.max(0, pendingModels - 1)
          modelChangePending.value = pendingModels > 0
        }
      }
    })
    // Both controls share one queue, so an effort always follows earlier model
    // changes and rapid slider updates reach the server in selection order.
    queue = request.then(() => undefined)
    return request
  }

  return {
    modelChangePending,
    pending: () => pendingSettings > 0,
    sequence: () => sequence,
    confirm,
    mergeFetched,
    invalidate,
    reset,
    setCurrentModel: (modelId: string) => save('model', modelId),
    setCurrentReasoningEffort: (effort: string) => save('effort', effort),
  }
}
