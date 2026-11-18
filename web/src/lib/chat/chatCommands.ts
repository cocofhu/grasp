export type ContextCommand = 'new' | 'compact'

export const chatCommandIcons = { new: 'new-request', compact: 'layers', skill: 'skills' } as const

// Exact context commands do not consume a pending question or form.
export function contextSlashCommand(text: string): ContextCommand | undefined {
  const value = text.trim()
  if (/^\/new(?:\s+chat)?$/.test(value)) return 'new'
  if (value === '/compact') return 'compact'
}

export function isPlatformSlashCommand(text: string): boolean {
  return !!contextSlashCommand(text) || /^\/skill(?:\s|$)/.test(text.trimStart())
}

export function readSkillCommand(text: string): { name: string; task: string } | undefined {
  const match = /^\/skill\s+([^\s/\\]+)(?:\s+([\s\S]*))?$/.exec(text.trim())
  if (!match || match[1] === '.' || match[1] === '..') return
  return { name: match[1]!, task: match[2] ?? '' }
}

export function configuredSkillNames(files: readonly { path: string }[]): string[] {
  const names = new Set<string>()
  for (const { path } of files) {
    const match = /^skills\/([^\s/\\]+)\/SKILL\.md$/.exec(path)
    if (match && match[1] !== '.' && match[1] !== '..') names.add(match[1]!)
  }
  return [...names].sort()
}
