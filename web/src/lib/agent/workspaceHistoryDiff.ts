type DiffLineKind = 'add' | 'del' | 'hunk' | 'file' | 'context'

export function classifyDiffLine(line: string): DiffLineKind {
  if (line.startsWith('@@')) return 'hunk'
  if (line.startsWith('+++') || line.startsWith('---')) return 'file'
  if (line.startsWith('+')) return 'add'
  if (line.startsWith('-')) return 'del'
  return 'context'
}

export const DIFF_LINE_CLASS: Record<DiffLineKind, string> = {
  add: 'text-ok bg-ok/10',
  del: 'text-err bg-err/10',
  hunk: 'text-accent-2',
  file: 'text-txt3',
  context: 'text-txt2',
}

export function revisionMatchesFile(
  changes: { path: string; fromPath?: string }[] | undefined,
  filePath: string,
): boolean {
  if (!filePath) return false
  return (changes ?? []).some((ch) => ch.path === filePath || ch.fromPath === filePath)
}
