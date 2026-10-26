import type { CliCatalogModel } from '@/lib/api/apiTypes'
import type { BackendId } from '@/lib/shared/regionPolicy'
import { OPENCODE_MODEL_ENV } from './openCodeProvider'

/** Selected native model id; the sandbox applies it through the backend protocol. */
export const CLI_MODEL_ENV = OPENCODE_MODEL_ENV
/** Codex only: becomes `-c model_reasoning_effort=…`. */
export const CLI_EFFORT_ENV = 'ACP_BRIDGE_REASONING_EFFORT'

/** Every level Codex knows; each model accepts a subset (catalog `efforts`). */
export const CLI_EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'] as const
export type CliEffort = (typeof CLI_EFFORTS)[number]
/** Offered when the model is auto or typed by hand, so its own levels are unknown. */
export const CLI_COMMON_EFFORTS: readonly CliEffort[] = ['low', 'medium', 'high']

/** The levels to offer for a model: its catalog list, or the common set when unlisted. */
export function cliEffortsFor(model: CliCatalogModel | undefined): string[] {
  if (!model) return [...CLI_COMMON_EFFORTS]
  return model.efforts ?? []
}

/** Keys the Runtime card owns for a coding CLI; the env editor shows them read-only. */
export function managedCliEnvKeys(backend: BackendId | string): string[] {
  if (!backend || backend === 'opencode') return []
  return backend === 'codex' ? [CLI_MODEL_ENV, CLI_EFFORT_ENV] : [CLI_MODEL_ENV]
}

export function isManagedCliEnvKey(backend: BackendId | string, key: string): boolean {
  return managedCliEnvKeys(backend).includes(key.trim())
}

function setOrDelete(env: Record<string, string>, key: string, value: string): Record<string, string> {
  const out = { ...env }
  const v = value.trim()
  if (v) out[key] = v
  else delete out[key]
  return out
}

export function readCliModel(env: Record<string, string>): string {
  return (env[CLI_MODEL_ENV] || '').trim()
}

export function writeCliModel(env: Record<string, string>, model: string): Record<string, string> {
  return setOrDelete(env, CLI_MODEL_ENV, model)
}

export function readCliEffort(env: Record<string, string>): CliEffort | '' {
  const v = (env[CLI_EFFORT_ENV] || '').trim().toLowerCase()
  return (CLI_EFFORTS as readonly string[]).includes(v) ? (v as CliEffort) : ''
}

export function writeCliEffort(env: Record<string, string>, effort: string): Record<string, string> {
  return setOrDelete(env, CLI_EFFORT_ENV, effort === 'auto' ? '' : effort)
}

/**
 * Model ids are not shared between CLIs, so a backend switch drops the pin.
 * OpenCode keeps its own model on the credential, so switching to it clears too.
 * `cleared` reports whether anything was dropped, for the undo toast.
 */
export function switchCliModelEnv(
  env: Record<string, string>,
  prev: string,
  next: string,
): { env: Record<string, string>; cleared: boolean } {
  if (prev === next) return { env, cleared: false }
  const out = { ...env }
  let cleared = false
  for (const key of [CLI_MODEL_ENV, CLI_EFFORT_ENV]) {
    if (key in out) {
      delete out[key]
      cleared = true
    }
  }
  return { env: out, cleared }
}
