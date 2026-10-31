export type SettingsStatTone = 'neutral' | 'ok' | 'warn' | 'accent'

export interface SettingsStatItem {
  key: string
  icon: string
  label: string
  /** Items without a value are not rendered; never substitute a placeholder number. */
  value?: number | string
  total?: number
  unit?: string
  tone?: SettingsStatTone
}
