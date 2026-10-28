import { parseModelRoute } from '@/lib/agent/modelBindings'
import type { ConfiguredModelChoice } from '@/lib/solo/configuredModels'

/** Model and effort sent with a home workflow start. Empty modelId is the primary model. */
export type HomeLaunchModel = {
  configurationId: string
  modelId: string
  reasoningEffort: string
}

export type HomeLaunchDecision =
  | { action: 'omit'; notice?: 'default' }
  | { action: 'block'; notice: 'blocked' | 'invalid' }
  | { action: 'send'; model: HomeLaunchModel }

export function listedEfforts(efforts?: string[]): string[] {
  return [...new Set((efforts || []).map((value) => value.trim()).filter(Boolean))]
}

/**
 * What the home composer should attach to this start.
 * A clarify-first workflow with a ready catalog always sends the effort on
 * screen, including the highest level when the user left it untouched.
 */
export function resolveHomeLaunchModel(input: {
  clarify: boolean
  ready: boolean
  /** True when this visit changed the picker, or restored a non-default memory. */
  custom: boolean
  modelId: string
  effort: string
  choices: ConfiguredModelChoice[]
}): HomeLaunchDecision {
  if (!input.clarify) return { action: 'omit' }
  if (!input.ready) {
    if (input.custom) return { action: 'block', notice: 'blocked' }
    return { action: 'omit', notice: 'default' }
  }
  if (input.modelId) {
    const picked = input.choices.find((choice) => choice.id === input.modelId)
    if (!picked || picked.unavailable) return { action: 'block', notice: 'invalid' }
  }
  const choices = input.choices.filter((choice) => !choice.unavailable)
  if (!choices.length) {
    if (input.custom) return { action: 'block', notice: 'invalid' }
    return { action: 'omit', notice: 'default' }
  }
  const primary = choices[0]!
  const picked = input.modelId ? choices.find((choice) => choice.id === input.modelId) ?? null : primary
  if (!picked) return { action: 'block', notice: 'invalid' }
  const listed = listedEfforts(picked.efforts)
  let effort = input.effort.trim()
  if (effort && !listed.includes(effort)) return { action: 'block', notice: 'invalid' }
  if (!effort && listed.length) effort = listed[listed.length - 1]!
  const modelId = input.modelId ? (picked.modelId ?? parseModelRoute(picked.id).modelId) : ''
  return {
    action: 'send',
    model: {
      configurationId: picked.configurationId || parseModelRoute(picked.id).configurationId || '',
      modelId,
      reasoningEffort: effort,
    },
  }
}
