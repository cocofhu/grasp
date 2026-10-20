import type { CredentialItem } from '@/lib/api/api'

function meta(item: CredentialItem, key: string): string {
  const v = item.metadata?.[key]
  return typeof v === 'string' ? v.trim() : ''
}

/** Vendor id stored on a model-vendor credential (deepseek, openai, custom, …). */
export function credentialVendor(item: CredentialItem): string {
  return meta(item, 'provider')
}

/** The credential's default model, as the `provider/model` id OpenCode expects. */
export function credentialModel(item: CredentialItem): string {
  return meta(item, 'model')
}
