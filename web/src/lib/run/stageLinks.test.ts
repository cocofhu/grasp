import { describe, expect, it } from 'vitest'
import { registerStageLinks, stageLinksFor, type StageLinks } from './stageLinks'

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
