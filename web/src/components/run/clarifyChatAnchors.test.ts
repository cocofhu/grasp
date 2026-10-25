import { describe, expect, it } from 'vitest'
import type { ClarifyTurn } from '@/lib/shared/types'
import { createClarifyChatAnchors, priorAnchorId, priorRailItems } from './clarifyChatAnchors'
import type { ClarifyPriorSegment } from './ClarifyChat.vue'

const human = (text: string, at: string): ClarifyTurn => ({ role: 'human', text, at })
const agent = (text: string): ClarifyTurn => ({ role: 'agent', text, at: 'reply' })

describe('Clarify conversation anchors', () => {
  it('keeps question and reply keys across ACP growth and persisted snapshot replacement', () => {
    const resolve = createClarifyChatAnchors('test')
    const first = resolve('session', [human('请检查发布', 'live'), { ...agent('正在检查'), streaming: true }])
    const second = resolve('session', [human('请检查发布', 'persisted'), agent('检查完成')])
    expect(second.keys).toEqual(first.keys)
    expect(second.anchors[0]).toBe(first.anchors[0])
    expect(second.anchors[1]).toBeUndefined()
  })

  it('preserves loaded anchors when older identical questions are prepended or a repeated request is appended', () => {
    const resolve = createClarifyChatAnchors('test')
    const first = resolve('session', [human('继续', 'today'), agent('今天')])
    const older = resolve('session', [human('继续', 'yesterday'), agent('昨天'), human('继续', 'today'), agent('今天')])
    expect(older.anchors[2]).toBe(first.anchors[0])
    expect(older.anchors[0]).not.toBe(first.anchors[0])
    const next = resolve('session', [human('继续', 'yesterday'), agent('昨天'), human('继续', 'today'), agent('今天'), human('继续', 'tomorrow')])
    expect(next.anchors.slice(0, 4)).toEqual(older.anchors)
    expect(next.anchors[4]).not.toBe(next.anchors[2])
  })

  it('matches attachment-only requests after files become server blob references', () => {
    const resolve = createClarifyChatAnchors('test')
    const first = resolve('session', [{ ...human('', 'live'), images: [{ data: 'upload', mimeType: 'image/png', name: 'screen.png' }] }])
    const saved = resolve('session', [{ ...human('', 'server'), images: [{ ref: 'blob:abc', mimeType: 'image/png', name: 'screen.png' }] }])
    expect(saved.anchors[0]).toBe(first.anchors[0])
  })

  it('uses separate reply segments and resets identities for another run/node/iteration', () => {
    const resolve = createClarifyChatAnchors('test')
    const rows = [human('需求', 'same'), agent('第一段'), { ...agent('第二段'), opId: 'operation', streaming: true }]
    const first = resolve('run:a:1', rows)
    expect(new Set(first.keys).size).toBe(3)
    expect(resolve('run:a:1', [...rows, agent('第三段')]).keys.slice(0, 3)).toEqual(first.keys)
    expect(resolve('run:a:2', rows).anchors[0]).not.toBe(first.anchors[0])
  })
})

describe('Earlier visit rail items', () => {
  const seg = (over: Partial<ClarifyPriorSegment>): ClarifyPriorSegment => ({
    nodeId: 'clarify', iteration: 1, label: '需求澄清', kind: 'chat', status: 'completed', at: 'start', open: false, turns: [], ...over,
  })

  it('indexes each human turn with the last agent text before the next question', () => {
    const items = priorRailItems([
      seg({ turns: [
        human('做个导出', 'q1'),
        agent('先看看'),
        { role: 'agent', text: '', at: 'r', parts: [{ kind: 'message', text: '需求已整理' }] },
        { role: 'human', text: '', at: 'q2', images: [{ data: 'x', mimeType: 'image/png', name: 'screen.png' }] },
        { role: 'agent', text: ' ', at: 'r', questions: [{ id: 'm', prompt: '选择模块', options: [] }] },
        { role: 'human', text: '', at: 'q3', annotations: [{ id: 'a' } as never] },
      ] }),
      seg({ nodeId: 'code', kind: 'auto', turns: [] }),
      seg({ nodeId: 'review', iteration: 2, kind: 'auto', turns: [human('自动提示', 'q4'), agent('完成')] }),
    ], { fallbackTitle: '我的选择' })
    expect(items).toEqual([
      { id: 'prior:clarify:1:0', title: '做个导出', preview: '需求已整理' },
      { id: 'prior:clarify:1:3', title: 'screen.png', preview: '选择模块' },
      { id: 'prior:clarify:1:5', title: '我的选择', preview: '' },
      { id: 'prior:review:2:0', title: '自动提示', preview: '完成' },
    ])
    expect(priorAnchorId({ nodeId: 'clarify', iteration: 1 }, 3)).toBe(items[1]!.id)
  })

  it('uses the host bubble text and skips blank turns', () => {
    const live: ClarifyTurn = { role: 'human', text: 'Live · generated', at: 'q', live: { sid: 's', op: 'o', variant: 'v', prompt: '换个配色', generated: true } as never }
    const segments = [seg({ turns: [live, human('', 'blank'), agent('ok')] })]
    expect(priorRailItems(segments, { fallbackTitle: '我的选择' }).map(item => item.title)).toEqual(['换个配色'])
    expect(priorRailItems(segments, { fallbackTitle: '我的选择', bubbleText: () => '' }).map(item => item.title)).toEqual(['我的选择'])
    expect(priorRailItems(undefined, { fallbackTitle: '我的选择' })).toEqual([])
  })
})
