type Translate = (key: string, named?: Record<string, unknown>) => string

/** Localizes a typed Solo / project API error by its code, else its message. */
export function soloErrorText(e: unknown, t: Translate, te: (key: string) => boolean): string {
  const err = e as { code?: string; message?: string } | null
  const code = typeof err?.code === 'string' ? err.code : ''
  if (code && te(`pages.solo.errors.${code}`)) return t(`pages.solo.errors.${code}`)
  return String(err?.message || e || '')
}

export function isSoloLimitError(e: unknown): boolean {
  return (e as { code?: string } | null)?.code === 'solo_limit'
}
