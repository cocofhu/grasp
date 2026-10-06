/** User-visible sandbox purpose / source i18n keys (display mapping only). */

export function sandboxPurposeLabelKey(purpose: string): string {
  if (purpose === 'run') return 'pages.sandboxes.purpose.run'
  if (purpose === 'agent') return 'pages.sandboxes.purpose.agent'
  return 'pages.sandboxes.purpose.test'
}

export function sandboxSourceTextKey(purpose: string): string | null {
  if (purpose === 'run') return null
  if (purpose === 'agent') return 'pages.sandboxes.source.pmConsult'
  return 'pages.sandboxes.source.chatTest'
}
