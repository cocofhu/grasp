type ActionIconName = 'check' | 'arrow-left' | 'refresh'
type ActionVariant = 'ok' | 'neutral'

export const POSITIVE_ACTION_IDS = new Set(['pass', 'approve'])
export const REVERT_ACTION_IDS = new Set(['fail', 'revise', 'limit'])

export function actionIcon(id: string): ActionIconName {
  if (POSITIVE_ACTION_IDS.has(id)) return 'check'
  if (REVERT_ACTION_IDS.has(id)) return 'arrow-left'
  return 'refresh'
}

export function actionVariant(id: string): ActionVariant {
  return POSITIVE_ACTION_IDS.has(id) ? 'ok' : 'neutral'
}

export function actionVariantClasses(variant: ActionVariant): string {
  if (variant === 'ok') return 'bg-ok/15 text-ok hover:bg-ok/25'
  return 'border border-line text-txt2 hover:bg-elevated hover:text-txt'
}
