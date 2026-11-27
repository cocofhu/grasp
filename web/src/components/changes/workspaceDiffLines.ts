type DiffLine = {
  kind: 'hunk' | 'add' | 'remove' | 'context' | 'note'
  text: string
  before?: number
  after?: number
}

export function workspaceDiffLines(patch: string): DiffLine[] {
  const lines: DiffLine[] = []
  let before = 0
  let after = 0
  let inHunk = false
  for (const text of patch.split('\n')) {
    if (text.startsWith('diff --git ')) {
      inHunk = false
      continue
    }
    const header = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(text)
    if (header) {
      before = Number(header[1])
      after = Number(header[2])
      inHunk = true
      lines.push({ kind: 'hunk', text })
      continue
    }
    if (!inHunk) continue
    switch (text.charAt(0)) {
      case '+': lines.push({ kind: 'add', after: after++, text: text.slice(1) }); break
      case '-': lines.push({ kind: 'remove', before: before++, text: text.slice(1) }); break
      case ' ': lines.push({ kind: 'context', before: before++, after: after++, text: text.slice(1) }); break
      case '\\': lines.push({ kind: 'note', text }); break
    }
  }
  return lines
}
