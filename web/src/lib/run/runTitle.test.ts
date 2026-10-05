import { describe, expect, it } from 'vitest'
import { clipRunTitle, displayRunTitle, formatRepoNames, runRepoNames } from './runTitle'

describe('runRepoNames', () => {
  it('reads repo names from repos variables only', () => {
    const vars = [
      { type: 'string', value: 'x' },
      { type: 'repos', value: [{ url: 'https://github.com/octocat/Hello-World.git', name: '' }, { url: '', name: '' }] },
    ]
    expect(runRepoNames(vars)).toEqual(['Hello-World'])
    expect(formatRepoNames(runRepoNames(vars))).toBe('Hello-World')
  })

  it('is empty for an artifact-only run', () => {
    expect(runRepoNames(undefined)).toEqual([])
    expect(runRepoNames([{ type: 'repos', value: [{ url: '', name: '' }] }])).toEqual([])
    expect(formatRepoNames([])).toBe('')
  })
})

describe('clipRunTitle', () => {
  it('trims and caps at 80 code points', () => {
    expect(clipRunTitle('  hello  ')).toBe('hello')
    expect(clipRunTitle('啊'.repeat(90))).toBe('啊'.repeat(80))
  })
})

describe('displayRunTitle', () => {
  it('keeps ordinary titles', () => {
    expect(displayRunTitle('邮箱验证码登录')).toBe('邮箱验证码登录')
  })

  it('formats a repos JSON dump as repo names', () => {
    expect(
      displayRunTitle('[{"branch":"","name":"approving","url":"https://git.example/approving.git"}]'),
    ).toBe('approving')
    expect(
      displayRunTitle(
        '[{"name":"web","url":"https://h/w.git"},{"name":"api","url":"https://h/a.git"}]',
      ),
    ).toBe('web · api')
  })

  it('counts extra repos without Chinese copy', () => {
    expect(
      displayRunTitle('[{"name":"a","url":"u"},{"name":"b","url":"u"},{"name":"c","url":"u"}]'),
    ).toBe('a · b +1')
  })

  it('hides other raw JSON', () => {
    expect(displayRunTitle('{"foo":1}')).toBe('')
    expect(displayRunTitle('')).toBe('')
    expect(displayRunTitle(null)).toBe('')
  })
})
