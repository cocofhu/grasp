import { describe, expect, it } from 'vitest'
import {
  isHtmlArtifactName,
  isVisualHtmlCard,
  looksLikeFullHtmlDocument,
  parseOutputCardDoc,
} from './isVisualHtmlCard'

const PAGE_HTML = `<!doctype html>
<html>
<body>
<div class="scenes" id="scenes">
  <button class="scene-btn on" type="button">1. Inbox 待澄清</button>
</div>
</body>
</html>`

describe('isHtmlArtifactName', () => {
  it('matches page.html and .html/.htm (case-insensitive)', () => {
    expect(isHtmlArtifactName('page.html')).toBe(true)
    expect(isHtmlArtifactName('PAGE.HTML')).toBe(true)
    expect(isHtmlArtifactName('demo.htm')).toBe(true)
    expect(isHtmlArtifactName('Report.HTM')).toBe(true)
    expect(isHtmlArtifactName('notes.txt')).toBe(false)
    expect(isHtmlArtifactName('file.html.bak')).toBe(false)
    expect(isHtmlArtifactName('')).toBe(false)
    expect(isHtmlArtifactName(undefined)).toBe(false)
  })
})

describe('looksLikeFullHtmlDocument', () => {
  it('sniffs doctype / <html after leading whitespace only', () => {
    expect(looksLikeFullHtmlDocument(PAGE_HTML)).toBe(true)
    expect(looksLikeFullHtmlDocument('  <!DOCTYPE HTML><html></html>')).toBe(true)
    expect(looksLikeFullHtmlDocument('<html lang="zh"><body></body></html>')).toBe(true)
    expect(looksLikeFullHtmlDocument('<HTML>')).toBe(true)
  })

  it('does not treat fragments or ordinary markdown as documents', () => {
    expect(looksLikeFullHtmlDocument('<div class="scenes">')).toBe(false)
    expect(looksLikeFullHtmlDocument('## 计划\n<div>x</div>')).toBe(false)
    expect(looksLikeFullHtmlDocument('<!-- Inbox --><html>')).toBe(false)
    expect(looksLikeFullHtmlDocument('')).toBe(false)
  })
})

describe('isVisualHtmlCard (g1.1 three rules + structured JSON exclusion)', () => {
  it('hits outputKey=page', () => {
    expect(isVisualHtmlCard({ outputKey: 'page', markdown: 'not html' })).toBe(true)
  })

  it('hits artifactName .html and .htm', () => {
    expect(isVisualHtmlCard({ artifactName: 'page.html' })).toBe(true)
    expect(isVisualHtmlCard({ artifactName: 'report.htm' })).toBe(true)
    expect(isVisualHtmlCard({ structuredArtifactName: 'page.html' })).toBe(false)
  })

  it('sniffs available body (markdown or fetched artifact)', () => {
    expect(isVisualHtmlCard({ markdown: PAGE_HTML })).toBe(true)
    expect(isVisualHtmlCard({ markdown: 'hello' }, { artifactHtml: PAGE_HTML })).toBe(true)
    expect(isVisualHtmlCard({ markdown: '<div>only fragment</div>' })).toBe(false)
  })

  it('does not misclassify parseable structured JSON (non-html name)', () => {
    const card = {
      structuredArtifactName: 'research.json',
      jsonSnapshot: JSON.stringify({ summary: 'ok', findings: [] }),
      markdown: PAGE_HTML,
    }
    expect(parseOutputCardDoc(card)).toEqual({ summary: 'ok', findings: [] })
    expect(isVisualHtmlCard(card)).toBe(false)
    expect(isVisualHtmlCard(card, { parsedDoc: { summary: 'ok' } })).toBe(false)
  })
})
