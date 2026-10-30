import { describe, expect, it } from 'vitest'
import type { MessageReference } from '@/lib/api/apiTypes'
import { activeTrigger, applyEdit, insertToken, insideToken, parseGitHubReference, referenceLabel, removeToken, type ReferenceToken } from './referenceTokens'
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
