import { describe, expect, it } from 'vitest'
import type { MessageReference } from '@/lib/api/apiTypes'
import { MAX_REFERENCES, activeTrigger, applyEdit, insertToken, insideToken, parseGitHubReference, referenceLabel, removeToken, restoreTokens, type ReferenceToken } from './referenceTokens'
import { githubRepoOf, referenceHref, referenceIcon } from './referenceDisplay'

const solo: MessageReference = { kind: 'solo', target: 's2', title: '登录  重构' }
const issue: MessageReference = { kind: 'github_issue', target: 'acme/web#12', title: 'Bug' }

describe('parseGitHubReference', () => {
  it('reads issue and pull URLs and the short form', () => {
    expect(parseGitHubReference('https://github.com/acme/web/pull/0042/files')).toEqual({ kind: 'github_pr', target: 'acme/web#42' })
    expect(parseGitHubReference(' https://github.com/acme/web/issues/7 ')).toEqual({ kind: 'github_issue', target: 'acme/web#7' })
    expect(parseGitHubReference('acme/web#3')).toEqual({ kind: 'github_issue', target: 'acme/web#3' })
  })
  it('ignores anything else', () => {
    expect(parseGitHubReference('see acme/web#3 please')).toBeNull()
    expect(parseGitHubReference('https://gitlab.com/a/b/issues/1')).toBeNull()
  })
})

describe('referenceLabel', () => {
  it('uses the collapsed title for sessions, falling back to the id', () => {
    expect(referenceLabel(solo)).toBe('登录 重构')
    expect(referenceLabel({ ...solo, title: '  ' })).toBe('s2')
  })
  it('shortens to #n inside the session repo only', () => {
    expect(referenceLabel(issue, 'ACME/web')).toBe('#12')
    expect(referenceLabel(issue, 'acme/api')).toBe('acme/web#12')
    expect(referenceLabel(issue)).toBe('acme/web#12')
  })
})

describe('token editing', () => {
  const tok = (start: number, end: number, ref = solo): ReferenceToken => ({ ref, start, end })

  it('shifts tokens after an edit and keeps those before it', () => {
    const before = 'a @x b @y c'
    const tokens = [tok(2, 4), tok(7, 9, issue)]
    expect(applyEdit(tokens, before, 'a @x bbb @y c')).toEqual([tok(2, 4), tok(9, 11, issue)])
    expect(applyEdit(tokens, before, before)).toBe(tokens)
    expect(applyEdit([], before, 'z')).toEqual([])
  })

  it('drops a token whose span the edit touches', () => {
    expect(applyEdit([tok(2, 4)], 'a @x b', 'a @ b')).toEqual([])
    expect(applyEdit([tok(2, 4)], 'a @x b', '')).toEqual([])
  })

  it('inserts with spacing and reports the caret', () => {
    const res = insertToken('hi@lo', 2, 5, issue, '#12')
    expect(res.text).toBe('hi @#12 ')
    expect(res.token).toEqual({ ref: issue, start: 3, end: 7 })
    expect(res.caret).toBe(8)
    expect(insertToken('@q rest', 0, 2, solo, 'T').text).toBe('@T rest')
  })

  it('removes a token and its trailing space', () => {
    const tokens = [tok(0, 2), tok(5, 7, issue)]
    const res = removeToken('@x a @y', tokens, 0)
    expect(res.text).toBe('a @y')
    expect(res.tokens).toEqual([tok(2, 4, issue)])
    expect(removeToken('abc', tokens, 9)).toEqual({ text: 'abc', tokens })
  })

  it('finds the trigger before the caret and tokens under it', () => {
    expect(activeTrigger('hi @lo', 6)).toEqual({ start: 3, query: 'lo' })
    expect(activeTrigger('@', 1)).toEqual({ start: 0, query: '' })
    expect(activeTrigger('a@b', 3)).toBeNull()
    expect(activeTrigger('@a b', 4)).toBeNull()
    expect(insideToken([tok(2, 4)], 3)).toBe(true)
    expect(insideToken([tok(2, 4)], 2)).toBe(false)
  })
})

describe('restoreTokens', () => {
  const label = (ref: MessageReference) => referenceLabel(ref, 'acme/web')

  it('claims each @label already in the text', () => {
    const res = restoreTokens('看 @登录 重构 和 @#12，谢谢', [issue, solo], label)
    expect(res.text).toBe('看 @登录 重构 和 @#12，谢谢')
    expect(res.tokens).toEqual([{ ref: solo, start: 2, end: 8 }, { ref: issue, start: 11, end: 15 }])
  })

  it('appends a reference whose label is missing or only part of a longer word', () => {
    expect(restoreTokens('', [issue], label)).toEqual({ text: '@#12 ', tokens: [{ ref: issue, start: 0, end: 4 }] })
    const res = restoreTokens('see @#123 and a@#12', [issue], label)
    expect(res.text).toBe('see @#123 and a@#12 @#12 ')
    expect(res.tokens).toEqual([{ ref: issue, start: 20, end: 24 }])
  })

  it('gives repeated labels separate spans', () => {
    const twin: MessageReference = { kind: 'solo', target: 's3', title: '登录 重构' }
    const res = restoreTokens('@登录 重构 vs @登录 重构', [solo, twin], label)
    expect(res.tokens).toEqual([{ ref: solo, start: 0, end: 6 }, { ref: twin, start: 10, end: 16 }])
    const one = restoreTokens('@登录 重构', [solo, twin], label)
    expect(one.text).toBe('@登录 重构 @登录 重构 ')
    expect(one.tokens.map((t) => [t.ref.target, t.start])).toEqual([['s2', 0], ['s3', 7]])
  })

  it('keeps at most MAX_REFERENCES', () => {
    const refs = Array.from({ length: MAX_REFERENCES + 2 }, (_, i): MessageReference => ({ kind: 'github_issue', target: `acme/web#${i + 1}`, title: '' }))
    const res = restoreTokens(refs.map((r) => `@#${r.target.split('#')[1]}`).join(' '), refs, label)
    expect(res.tokens.map((t) => t.ref.target)).toEqual(refs.slice(0, MAX_REFERENCES).map((r) => r.target))
  })
})

describe('referenceDisplay', () => {
  it('picks icons by kind, state and access', () => {
    expect(referenceIcon(solo)).toBe('chat')
    expect(referenceIcon({ kind: 'github_pr', state: 'merged' })).toBe('git')
    expect(referenceIcon({ kind: 'github_pr', state: 'open' })).toBe('branch')
    expect(referenceIcon({ kind: 'github_issue', state: 'closed' })).toBe('check')
    expect(referenceIcon({ kind: 'github_issue', state: 'open' })).toBe('circle')
    expect(referenceIcon({ kind: 'github_issue', access: 'denied' })).toBe('warning')
    expect(referenceIcon({ kind: 'solo', error: 'deleted' })).toBe('warning')
  })
  it('links sessions in-app and GitHub items to their URL', () => {
    expect(referenceHref({ kind: 'solo', target: 'a b' })).toBe('/solo/a%20b')
    expect(referenceHref({ kind: 'github_pr', target: 'x', url: 'https://github.com/x' })).toBe('https://github.com/x')
    expect(referenceHref({ kind: 'github_pr', target: 'x' })).toBe('')
  })
  it('reads owner/repo from clone URLs', () => {
    expect(githubRepoOf('https://github.com/acme/web.git')).toBe('acme/web')
    expect(githubRepoOf('git@github.com:acme/web')).toBe('acme/web')
    expect(githubRepoOf('https://gitlab.com/acme/web')).toBe('')
    expect(githubRepoOf(undefined)).toBe('')
  })
})
