import { describe, expect, it } from 'vitest'
import { fileBadge, sizeLabel } from './artifactBadge'

describe('fileBadge', () => {
  it.each([
    ['需求说明.pdf', 'pdf', 'PDF', 'pdf'],
    ['报告.md', 'markdown', 'MD', 'doc'],
    ['notes.txt', 'text', 'TXT', 'doc'],
    ['page.html', 'html', 'HTML', 'doc'],
    ['fast-config.json', 'json', 'JSON', 'data'],
    ['ci.yml', 'yaml', 'YML', 'data'],
    ['提示音.wav', 'audio', 'WAV', 'audio'],
    ['旧版截图.png', 'image', 'PNG', 'media'],
    ['clip.mp4', 'video', 'MP4', 'media'],
    ['资料.zip', 'file', 'ZIP', 'archive'],
  ] as const)('%s → %s label %s', (name, kind, label, tone) => {
    expect(fileBadge({ name, kind })).toEqual({ label, tone })
  })

  it('falls back to the kind label when the extension is missing, too long or not alphanumeric', () => {
    expect(fileBadge({ name: 'README', kind: 'markdown' })).toEqual({ label: 'MD', tone: 'doc' })
    expect(fileBadge({ name: 'diagram.drawio', kind: 'file' })).toEqual({ label: 'FILE', tone: 'archive' })
    expect(fileBadge({ name: 'shot.jpeg', kind: 'image' })).toEqual({ label: 'JPEG', tone: 'media' })
    expect(fileBadge({ name: 'clip.quicktime', kind: 'video' })).toEqual({ label: 'VID', tone: 'media' })
    expect(fileBadge({ name: '.env', kind: 'text' })).toEqual({ label: 'TXT', tone: 'doc' })
    expect(fileBadge({ name: 'a.c++', kind: 'text' })).toEqual({ label: 'TXT', tone: 'doc' })
  })
})

describe('sizeLabel', () => {
  it('formats bytes as B, KB and MB', () => {
    expect(sizeLabel(0)).toBe('0 B')
    expect(sizeLabel(48)).toBe('48 B')
    expect(sizeLabel(14029)).toBe('13.7 KB')
    expect(sizeLabel(3 * 1024 ** 3)).toBe('3072.0 MB')
  })
})
