import { describe, expect, it } from 'vitest'
import { resolveArtifactPreviewBranch } from './artifactPreviewBranch'

const researchDoc = {
  title: '调研标题',
  summary: '概述',
  questions: [{ question: 'Q1', answer: 'A1' }],
}

describe('resolveArtifactPreviewBranch (ArtifactPreview routing)', () => {
  it('routes valid reserved JSON to structured (platform or run entry)', () => {
    const content = JSON.stringify(researchDoc)
    for (const name of [
      'research.json',
      'review.json',
      'plan.json',
      'clarified_requirement.json',
      'implementation_result.json',
      'test_result.json',
    ]) {
      const branch = resolveArtifactPreviewBranch({ name, kind: 'json', content })
      expect(branch.kind).toBe('structured')
      if (branch.kind === 'structured') {
        expect(branch.doc).toEqual(JSON.parse(content))
      }
    }
  })

  it('keeps ordinary JSON on the source-highlight branch', () => {
    const branch = resolveArtifactPreviewBranch({
      name: 'data.json',
      kind: 'json',
      content: '{"a":1}',
    })
    expect(branch).toEqual({ kind: 'json' })
  })

  it('falls back to json for reserved names with unparseable content', () => {
    const branch = resolveArtifactPreviewBranch({
      name: 'research.json',
      kind: 'json',
      content: '{ "title": "broken,\n status: pending',
    })
    expect(branch).toEqual({ kind: 'json' })
  })

  it('prefers HTML over structured/json when kind is html', () => {
    const branch = resolveArtifactPreviewBranch({
      name: 'page.html',
      kind: 'html',
      content: '<html><body>hi</body></html>',
    })
    expect(branch).toEqual({ kind: 'html' })
  })

  it('uses the same branch for inline and zoom (shared resolver)', () => {
    const input = {
      name: 'plan.json',
      kind: 'json' as const,
      content: JSON.stringify({ title: '计划', goals: [] }),
    }
    const inline = resolveArtifactPreviewBranch(input)
    const zoom = resolveArtifactPreviewBranch(input)
    expect(inline).toEqual(zoom)
    expect(inline.kind).toBe('structured')
  })

  it('does not depend on scope — platform and run share structured routing', () => {
    // Scope is intentionally absent from the resolver so ArtifactsView (platform)
    // and ArtifactPanel (run) cannot diverge.
    const content = JSON.stringify(researchDoc)
    const platformLike = resolveArtifactPreviewBranch({
      name: 'research.json',
      kind: 'json',
      content,
    })
    const runLike = resolveArtifactPreviewBranch({
      name: 'research.json',
      kind: 'json',
      content,
    })
    expect(platformLike).toEqual(runLike)
    expect(platformLike.kind).toBe('structured')
  })

  it('routes markdown when content is non-json and not html', () => {
    const branch = resolveArtifactPreviewBranch({
      name: 'notes.md',
      kind: 'markdown',
      content: '# Hello',
    })
    expect(branch).toEqual({ kind: 'markdown' })
  })

  it('returns empty when there is no content', () => {
    expect(
      resolveArtifactPreviewBranch({ name: 'research.json', kind: 'json', content: '' }),
    ).toEqual({ kind: 'empty' })
    expect(
      resolveArtifactPreviewBranch({ name: 'data.json', kind: 'json', content: '' }),
    ).toEqual({ kind: 'empty' })
  })

  it('routes kind=image to image branch instead of markdown', () => {
    const branch = resolveArtifactPreviewBranch({
      name: 'screenshot-ui-test.png',
      kind: 'image',
      content: 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJ',
    })
    expect(branch).toEqual({ kind: 'image' })
    expect(branch.kind).not.toBe('markdown')
  })

  it('infers image branch from common image suffixes', () => {
    const branch = resolveArtifactPreviewBranch({
      name: 'screenshot.png',
      kind: 'text',
      content: 'base64-payload',
    })
    expect(branch).toEqual({ kind: 'image' })
  })

  it('routes empty image artifacts to image branch (not markdown)', () => {
    const branch = resolveArtifactPreviewBranch({
      name: 'screenshot.png',
      kind: 'image',
      content: '',
    })
    expect(branch).toEqual({ kind: 'image' })
  })
})
