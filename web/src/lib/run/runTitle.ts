/** Matches engine maxRunTitleRunes (Unicode code points). */
const RUN_TITLE_MAX = 80

/** Trim and cap a launch title override. */
export function clipRunTitle(raw: string, max = RUN_TITLE_MAX): string {
  const s = String(raw ?? '').trim()
  const chars = Array.from(s)
  return chars.length > max ? chars.slice(0, max).join('') : s
}

/** Human-readable run title. Repo JSON dumps become repo names; other raw JSON is hidden. */
export function displayRunTitle(raw: string | undefined | null): string {
  const s = String(raw ?? '').trim()
  if (!s) return ''
  const repos = reposTitleFromValue(s)
  if (repos) return repos
  if (s.startsWith('[') || s.startsWith('{')) return ''
  return s
}

function reposTitleFromValue(raw: string): string {
  if (!raw.startsWith('[')) return ''
  try {
    const parsed = JSON.parse(raw) as unknown
    return formatRepoNames(repoNames(parsed))
  } catch {
    return ''
  }
}

/** Repo names a run was started with, from its `repos`-typed variables. */
export function runRepoNames(vars: readonly { type: string; value: unknown }[] | undefined | null): string[] {
  return (vars ?? []).filter((v) => v.type === 'repos').flatMap((v) => repoNames(v.value))
}

/** Repo names joined for one line; the count stays language-neutral. */
export function formatRepoNames(names: string[]): string {
  if (names.length <= 2) return names.join(' · ')
  return `${names[0]} · ${names[1]} +${names.length - 2}`
}

function repoNames(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  const names: string[] = []
  for (const item of value) {
    if (!item || typeof item !== 'object') continue
    const rec = item as { name?: unknown; url?: unknown }
    const name = String(rec.name ?? '').trim()
    if (name) {
      names.push(name)
      continue
    }
    const url = String(rec.url ?? '').trim()
    const fromUrl = url.split('/').pop()?.replace(/\.git$/i, '') ?? ''
    if (fromUrl) names.push(fromUrl)
  }
  return names
}
