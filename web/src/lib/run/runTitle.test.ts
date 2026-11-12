import { describe, expect, it } from 'vitest'
import { clipRunTitle, displayRunTitle, formatRepoNames } from './runTitle'

describe('formatRepoNames', () => {
  it('joins up to two names and counts the rest without Chinese copy', () => {
    expect(formatRepoNames([])).toBe('')
    expect(formatRepoNames(['web', 'api'])).toBe('web · api')
    expect(formatRepoNames(['a', 'b', 'c'])).toBe('a · b +1')
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

  it('hides raw JSON', () => {
    expect(displayRunTitle('[{"name":"approving","url":"https://git.example/approving.git"}]')).toBe('')
    expect(displayRunTitle('{"foo":1}')).toBe('')
    expect(displayRunTitle('')).toBe('')
    expect(displayRunTitle(null)).toBe('')
  })
})
