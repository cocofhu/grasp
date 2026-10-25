type Translate = (key: string, named?: Record<string, unknown>) => string

function repoList(value: unknown, locale: string): string {
  const names = Array.isArray(value) ? value.map((item) => String(item)) : []
  return names.join(locale.toLowerCase().startsWith('zh') ? '、' : ', ')
}

/** Localizes a typed Solo / project API error by its code, else its message. */
export function soloErrorText(e: unknown, t: Translate, te: (key: string) => boolean, locale = 'zh-CN'): string {
  const err = e as { code?: string; message?: string; params?: Record<string, unknown> } | null
  const code = typeof err?.code === 'string' ? err.code : ''
  const key = `pages.solo.errors.${code}`
  if (code && te(key)) {
    const params: Record<string, unknown> = { ...(err?.params ?? {}) }
    if (Array.isArray(params.repos)) params.repos = repoList(params.repos, locale)
    return t(key, params)
  }
  return String(err?.message || e || '')
}

export function isSoloLimitError(e: unknown): boolean {
  return (e as { code?: string } | null)?.code === 'solo_limit'
}
