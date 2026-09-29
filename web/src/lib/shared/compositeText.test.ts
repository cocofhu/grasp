import { beforeAll, describe, expect, it } from 'vitest'
import {
  compositeImages,
  formatImageCountChip,
  formatImageCountFull,
  formatVarChip,
  formatVarValue,
  chatImageSrc,
  imgSrc,
  isCompositeText,
} from './compositeText'
import { i18n } from './i18n'
import { loadLocaleMessages } from './loadLocaleMessages'

beforeAll(async () => {
  const messages = await loadLocaleMessages('zh-CN')
  i18n.global.setLocaleMessage('zh-CN', messages)
  i18n.global.locale.value = 'zh-CN'
})

describe('isCompositeText', () => {
  it('detects composite shape', () => {
    expect(isCompositeText({ text: 'a', images: [] })).toBe(true)
    expect(isCompositeText({ text: 'a' })).toBe(true)
    expect(isCompositeText({ foo: 'bar' })).toBe(false)
  })
})

describe('compositeImages', () => {
  it('returns images from composite value', () => {
    const imgs = [{ data: 'x', mimeType: 'image/png' }]
    expect(compositeImages({ text: '', images: imgs })).toEqual(imgs)
    expect(compositeImages('plain')).toEqual([])
  })
})

describe('imgSrc', () => {
  it('prefers blob ref URL over inline data', () => {
    expect(imgSrc({ ref: 'blob:abc123', data: 'xx', mimeType: 'image/png' })).toBe('/api/blobs/abc123')
    expect(imgSrc({ data: 'QUJD', mimeType: 'image/png' })).toBe('data:image/png;base64,QUJD')
    expect(imgSrc({ mimeType: 'image/png' })).toBe('')
  })

  it('prefers public gate url over blob ref (g2.1)', () => {
    expect(
      imgSrc({
        url: '/public/gate-approvals/images/0?token=abc',
        ref: 'blob:abc123',
        mimeType: 'image/png',
      }),
    ).toBe('/public/gate-approvals/images/0?token=abc')
  })
})

describe('chatImageSrc', () => {
  it('prefers live url then falls back to imgSrc', () => {
    expect(
      chatImageSrc({ url: 'data:image/png;base64,LIVE', data: 'QUJD', mimeType: 'image/png' }),
    ).toBe('data:image/png;base64,LIVE')
    expect(chatImageSrc({ data: 'QUJD', mimeType: 'image/png' })).toBe('data:image/png;base64,QUJD')
  })
})

describe('formatImageCountFull', () => {
  it('matches backend run title style', () => {
    expect(formatImageCountFull(1)).toBe('1张图')
    expect(formatImageCountFull(3)).toBe('3张图')
  })
})

describe('formatVarValue', () => {
  it('handles null and empty', () => {
    expect(formatVarValue(null)).toBe('—')
    expect(formatVarValue('')).toBe('—')
  })

  it('handles boolean with locale', () => {
    expect(formatVarValue(true)).toBe('true')
    expect(formatVarValue(false, { localeBool: true })).toBe('否')
    expect(formatVarValue(true, { localeBool: true })).toBe('是')
  })

  it('joins arrays', () => {
    expect(formatVarValue(['a', 'b'])).toBe('a, b')
    expect(formatVarValue([])).toBe('—')
  })

  it('handles composite text-only', () => {
    expect(formatVarValue({ text: 'hello', images: [] })).toBe('hello')
  })

  it('handles composite images-only', () => {
    expect(
      formatVarValue({
        text: '',
        images: [{ data: 'x', mimeType: 'image/png' }],
      }),
    ).toBe('1张图')
  })

  it('JSON-stringifies plain objects', () => {
    expect(formatVarValue({ foo: 1 })).toBe('{"foo":1}')
  })
})

describe('formatVarChip', () => {
  it('combines text and image count', () => {
    const v = {
      text: '看看怎么支持登录',
      images: [{ data: 'x', mimeType: 'image/png' }],
    }
    expect(formatVarChip(v)).toBe(`看看怎么支持登录 · ${formatImageCountChip(1)}`)
  })

  it('shows images-only label', () => {
    expect(
      formatVarChip({
        text: '',
        images: [{ data: 'x', mimeType: 'image/png' }],
      }),
    ).toBe('1张图')
  })

  it('truncates long text', () => {
    const long = 'a'.repeat(100)
    expect(formatVarChip(long).length).toBe(81)
    expect(formatVarChip(long).endsWith('…')).toBe(true)
  })

  it('passes through scalar values', () => {
    expect(formatVarChip('hello')).toBe('hello')
  })
})
