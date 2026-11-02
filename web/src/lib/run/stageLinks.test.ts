import { describe, expect, it } from 'vitest'
import { isLoopbackUrl, registerStageLinks, stageLinksFor, type StageLinks } from './stageLinks'

const links = (): StageLinks => ({ hasArtifact: () => true, openArtifact: () => {}, canOpenPreview: () => false, openPreview: () => {} })

describe('stageLinksFor', () => {
  it('finds the newest stage of a run and the only stage without a run id', () => {
    expect(stageLinksFor('a')).toBeNull()
    expect(stageLinksFor()).toBeNull()
    const a1 = links()
    const a2 = links()
    const offA1 = registerStageLinks('a', a1)
    const offA2 = registerStageLinks('a', a2)
    expect(stageLinksFor('a')).toBe(a2)
    expect(stageLinksFor()).toBe(a2)
    const offB = registerStageLinks('b', links())
    expect(stageLinksFor()).toBeNull()
    expect(stageLinksFor('a')).toBe(a2)
    offA2()
    expect(stageLinksFor('a')).toBe(a1)
    offA1()
    offB()
    expect(stageLinksFor('a')).toBeNull()
  })
})

describe('isLoopbackUrl', () => {
  it('matches http(s) loopback hosts only', () => {
    for (const u of ['http://127.0.0.1:5173/', 'https://localhost/x', 'http://0.0.0.0:3000', 'http://[::1]:8080/', 'http://app.localhost:3000', 'http://127.1.2.3/']) {
      expect(isLoopbackUrl(u)).toBe(true)
    }
    for (const u of ['https://github.com/', 'http://127.0.0.1.evil.com/', 'ftp://localhost/', '/solo/s2', 'not a url', 'http://10.0.0.1/']) {
      expect(isLoopbackUrl(u)).toBe(false)
    }
  })
})
