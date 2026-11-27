// @vitest-environment happy-dom

import { describe, expect, it } from 'vitest'
import { highlightWorkspaceDiff } from './workspaceDiffHighlight'
import { workspaceDiffLines } from './workspaceDiffLines'

describe('workspace diff highlighting', () => {
  it('keeps both sides inside a multiline comment that starts outside the hunk', () => {
    const before = 'package server\n\n/*\nold note\n*/\n'
    const after = before.replace('old note', 'new note')
    const lines = workspaceDiffLines('@@ -4 +4 @@\n-old note\n+new note\n')

    const result = highlightWorkspaceDiff(lines, { path: 'server/main.go' }, { before, after })
    expect(result[1].tokens).toEqual([{ className: 'hljs-comment', text: 'old note' }])
    expect(result[2].tokens).toEqual([{ className: 'hljs-comment', text: 'new note' }])
  })

  it('preserves text with angle brackets and ignores CRLF line endings when matching tokens', () => {
    const before = 'const message = "waiting"\r\n'
    const after = 'const message = "<span>ready & live</span>"\r\n'
    const lines = workspaceDiffLines('@@ -1 +1 @@\n-' + before + '+' + after)

    const [header, removed, added] = highlightWorkspaceDiff(lines, { path: 'app.ts' }, { before, after })
    expect(header.tokens).toBeUndefined()
    expect(removed.tokens).toContainEqual({ className: 'hljs-string', text: '"waiting"' })
    expect(added.tokens).toContainEqual({ className: 'hljs-string', text: '"<span>ready & live</span>"' })
    expect(added.tokens?.map(token => token.text).join('')).toBe(after.trimEnd())
  })

  it('renders the patch text when tokens cannot be matched to a source line', () => {
    const lines = workspaceDiffLines('@@ -0,0 +1 @@\n+const port = 8090\n')
    const result = highlightWorkspaceDiff(lines, { path: 'port.ts' }, { after: 'const port = 8080\n' })
    expect(result).toEqual(lines)
    expect(highlightWorkspaceDiff(lines, { path: 'port.txt' }, { after: 'const port = 8090\n' })).toEqual(lines)
  })

  it('highlights each side when an archive contains only the patch', () => {
    const lines = workspaceDiffLines('@@ -14 +14 @@\n-return 8080\n+return 8090\n')
    const [, removed, added] = highlightWorkspaceDiff(lines, { path: 'port.py' }, {})
    expect(removed.tokens).toContainEqual({ className: 'hljs-number', text: '8080' })
    expect(added.tokens).toContainEqual({ className: 'hljs-number', text: '8090' })
  })
})
