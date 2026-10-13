import type { ProjectCredentialItem } from '@/lib/api/api'

function meta(item: ProjectCredentialItem, key: string): string {
  const v = item.metadata?.[key]
  return typeof v === 'string' ? v.trim() : ''
}

/** Vendor id stored on a model-vendor credential (deepseek, openai, custom, …). */
export function credentialVendor(item: ProjectCredentialItem): string {
  return meta(item, 'provider')
}

/** The credential's default model, as the `provider/model` id OpenCode expects. */
export function credentialModel(item: ProjectCredentialItem): string {
  return meta(item, 'model')
}
