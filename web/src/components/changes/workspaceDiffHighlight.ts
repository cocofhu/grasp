import hljs from 'highlight.js/lib/common'
import type { WorkspaceChangedPath, WorkspaceChangeFile } from '@/lib/api/clients/workspaceChangesClient'
import type { DiffLine } from './workspaceDiffLines'

type Token = { text: string; className?: string }
export type HighlightedDiffLine = DiffLine & { tokens?: Token[] }
type Side = 'before' | 'after'

const maxSourceLength = 64 * 1024

function tokenize(source: string, language: string): Token[][] {
  // Keep the tokenizer's markup in an inert template. The diff renders only
  // text and token classes, with a separate row for every source line.
  const fragment = document.createElement('template')
  fragment.innerHTML = hljs.highlight(source.replace(/\r\n/g, '\n'), { language, ignoreIllegals: true }).value
  let line: Token[] = []
  const result = [line]
  function collect(node: Node, className?: string) {
    if (node.nodeType === Node.TEXT_NODE) {
      for (const [index, text] of (node.nodeValue ?? '').split('\n').entries()) {
        if (index) {
          line = []
          result.push(line)
        }
        if (text) line.push({ text, className })
      }
    } else if (node instanceof Element && node.localName === 'span') {
      const scope = Array.from(node.classList).find(value => value.startsWith('hljs-')) ?? className
      node.childNodes.forEach(child => collect(child, scope))
    }
  }
  fragment.content.childNodes.forEach(node => collect(node))
  return result
}

function highlightSide(lines: DiffLine[], side: Side, path: string, source?: string) {
  const filename = path.split('/').at(-1)?.toLowerCase() || ''
  const extension = filename.slice(filename.lastIndexOf('.') + 1)
  const language = extension === 'vue' || extension === 'svelte' ? 'xml' : extension
  if (!hljs.getLanguage(language)) return
  if (source !== undefined && source.length <= maxSourceLength) {
    const rows = tokenize(source, language)
    return (line: DiffLine) => rows[(line[side] ?? 0) - 1]
  }

  // An archive may contain just the patch. Color each visible hunk with
  // its own context, within the same budget as a full source file.
  const tokens = new Map<DiffLine, Token[]>()
  let group: DiffLine[] = []
  let remaining = maxSourceLength
  function flush() {
    if (!group.length) return
    const source = group.map(line => line.text).join('\n')
    if (source.length <= remaining) {
      const rows = tokenize(source, language)
      group.forEach((line, index) => tokens.set(line, rows[index]))
      remaining -= source.length
    }
    group = []
  }
  for (const line of lines) {
    if (line.kind === 'hunk') flush()
    else if (line[side] !== undefined) group.push(line)
  }
  flush()
  return (line: DiffLine) => tokens.get(line)
}

export function highlightWorkspaceDiff(
  lines: DiffLine[],
  file: Pick<WorkspaceChangedPath, 'path' | 'previousPath'>,
  versions: WorkspaceChangeFile,
): HighlightedDiffLine[] {
  if (!lines.length) return lines
  // The original and updated file can have different lexical state at the
  // same position, including a comment or string that starts outside a hunk.
  const before = highlightSide(lines, 'before', file.previousPath || file.path, versions.before)
  const after = highlightSide(lines, 'after', file.path, versions.after)
  if (!before && !after) return lines

  return lines.map(line => {
    const tokens = after?.(line) ?? before?.(line)
    if (!tokens?.some(token => token.className) || tokens.map(token => token.text).join('') !== line.text.replace(/\r$/, '')) return line
    return { ...line, tokens }
  })
}
