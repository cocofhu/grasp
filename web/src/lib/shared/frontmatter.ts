export type FrontmatterType = 'rules' | 'skill'

export type FrontmatterFields = {
  description?: string
  alwaysApply?: boolean
  name?: string
}

type ParsedFrontmatter = {
  fm: FrontmatterFields | null
  body: string
}

const FM_RE = /^---\r?\n([\s\S]*?)\r?\n---\r?\n([\s\S]*)$/

function parseValue(raw: string): string | boolean {
  let v = raw.trim()
  if ((v.startsWith('"') && v.endsWith('"')) || (v.startsWith("'") && v.endsWith("'"))) {
    v = v.slice(1, -1)
  }
  if (v === 'true') return true
  if (v === 'false') return false
  return v
}

export function parseFrontmatter(text: string): ParsedFrontmatter {
  const m = text.match(FM_RE)
  if (!m) return { fm: null, body: text }
  const fm: FrontmatterFields = {}
  for (const line of m[1].split('\n')) {
    const kv = line.match(/^(\w+):\s*(.*)$/)
    if (!kv) continue
    const val = parseValue(kv[2])
    if (kv[1] === 'alwaysApply') fm.alwaysApply = val === true
    else if (kv[1] === 'description') fm.description = String(val)
    else if (kv[1] === 'name') fm.name = String(val)
  }
  return { fm, body: m[2] }
}

export function buildFrontmatter(fm: FrontmatterFields, type: FrontmatterType): string {
  const lines = ['---']
  if (type === 'skill') {
    lines.push(`name: "${fm.name || ''}"`)
    lines.push(`description: "${fm.description || ''}"`)
  } else {
    lines.push(`description: "${fm.description || ''}"`)
    lines.push(`alwaysApply: ${fm.alwaysApply ? 'true' : 'false'}`)
  }
  lines.push('---', '')
  return lines.join('\n')
}

export function frontmatterTypeForPath(path: string): FrontmatterType | null {
  if (path.startsWith('rules/') && path.endsWith('.md')) return 'rules'
  if (path.includes('skills/') && path.endsWith('.md')) return 'skill'
  return null
}

export function hasFrontmatter(text: string): boolean {
  return FM_RE.test(text)
}
