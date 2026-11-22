import { applyAcp, type AcpReply, type ReplyStep, type ToolLabeler } from '@/lib/run/acpReply'
import type { ArtifactAttachment, ChatMessageMetadata, ClarifyImage, ReactAnnotation, ReactForm, ReactQuestion } from '@/lib/shared/types'
import type { MessageReference, SoloMessage } from '@/lib/api/apiTypes'

export type SoloTurn = ChatMessageMetadata & AcpReply & {
  files?: ArtifactAttachment[]
  id: string
  role: 'user' | 'agent' | 'system'
  steps: ReplyStep[]
  streaming: boolean
  /** Agent turn: bumped each time a batch of replayed frames is applied. */
  replayBatches?: number
  images?: ClarifyImage[]
  annotations?: ReactAnnotation[]
  /** User turn: contexts explicitly picked with @. */
  references?: MessageReference[]
  /** Agent turn: what it asked the user through ask_question / ask_form. */
  questions?: ReactQuestion[]
  forms?: ReactForm[]
  /** User turn: failed | stopped when its reply did not complete. */
  status?: string
  startedAt?: string
  endedAt?: string
  createdAt?: string
}

export function emptyAgentTurn(id: string, streaming: boolean, startedAt?: string): SoloTurn {
  return { id, role: 'agent', text: '', tools: [], steps: [], plan: [], streaming, startedAt }
}

function addMs(iso: string, ms?: number): string | undefined {
  const t = Date.parse(iso)
  if (!Number.isFinite(t) || !ms) return undefined
  return new Date(t - ms).toISOString()
}

/** Rebuilds the transcript from persisted messages. */
export function turnsFromMessages(messages: SoloMessage[], label: ToolLabeler, toolFallback: string): SoloTurn[] {
  const out: SoloTurn[] = []
  for (const m of messages) {
    if (m.role === 'user') {
      out.push({
        id: `u:${m.id}`,
        role: 'user',
        text: m.content,
        tools: [],
        steps: [],
        plan: [],
        streaming: false,
        images: m.images?.length ? m.images : undefined,
        annotations: m.annotations,
        references: m.references?.length ? m.references : undefined,
        status: m.status || undefined,
        createdAt: m.createdAt,
      })
    } else if (m.role === 'system') {
      const contextCommand = m.status === 'context_new' ? 'new' : m.status === 'context_compact' ? 'compact' : undefined
      out.push({ id: `s:${m.id}`, role: 'system', text: m.content, contextCommand, tools: [], steps: [], plan: [], streaming: false, createdAt: m.createdAt })
    } else {
      const turn = emptyAgentTurn(`a:${m.id}`, false, m.execution?.startedAt || addMs(m.createdAt, m.durationMs))
      turn.endedAt = m.createdAt
      turn.createdAt = m.createdAt
      turn.files = m.files
      if (m.questions?.length) turn.questions = m.questions
      if (m.forms?.length) turn.forms = m.forms
      turn.execution = m.execution
      turn.usage = m.usage
      turn.usageByModel = m.usageByModel
      turn.durationMs = m.durationMs
      for (const ev of m.events ?? []) applyAcp(ev, turn, label, toolFallback)
      if (!turn.steps.length && m.content) {
        turn.text = m.content
        turn.steps.push({ kind: 'message', text: m.content })
      }
      if (m.status && m.status !== 'ok') turn.status = m.status
      if (m.error) turn.error = m.error
      out.push(turn)
    }
  }
  return out
}
