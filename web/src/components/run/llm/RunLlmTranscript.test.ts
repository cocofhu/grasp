// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { LlmTranscriptResponse } from '@/lib/api/apiTypes'
import type { Run, WFNode } from '@/lib/shared/types'

const llmTranscript = vi.fn()
const reactReply = vi.fn()
const nodeEvents = vi.fn()
vi.mock('@/lib/api/api', () => ({
  api: {
    llmTranscript: (...args: unknown[]) => llmTranscript(...args),
    reactReply: (...args: unknown[]) => reactReply(...args),
    nodeEvents: (...args: unknown[]) => nodeEvents(...args),
  },
}))

import RunLlmTranscript from './RunLlmTranscript.vue'
import LiveLogPanel from '../LiveLogPanel.vue'

const transcriptSrc = readFileSync(join(process.cwd(), 'src/components/run/llm/RunLlmTranscript.vue'), 'utf8')
const chatSrc = readFileSync(join(process.cwd(), 'src/components/run/ClarifyChat.vue'), 'utf8')

function run(over: Partial<Run> = {}): Run {
  return {
    id: 'r1',
    workflowId: 'wf',
    workflowName: 'WF',
    status: 'completed',
    trigger: 'manual',
    startedAt: '2026-10-01T00:00:00Z',
    durationSec: 60,
    progress: 100,
    nodeRuns: {},
    artifacts: [],
    trace: [],
    ...over,
  }
}

const nodes = [
  { id: 'a', type: 'agent', label: '需求分析' },
  { id: 'b', type: 'gate', label: '人工审批' },
] as unknown as WFNode[]

const transcript: LlmTranscriptResponse = {
  executions: [
    {
      id: 1,
      nodeId: 'a',
      iteration: 1,
      status: 'completed',
      startedAt: '2026-10-01T00:00:00Z',
      durationSec: 20,
      usage: { inputTokens: 1000, outputTokens: 200, cacheReadTokens: 0, cacheWriteTokens: 0 },
      events: [
        { t: 0, kind: 'prompt', text: '先看需求', at: '2026-10-01T00:00:01Z' },
        { t: 1, kind: 'thought', text: 'secret reasoning' },
        { t: 2, kind: 'tool_call', title: 'write_artifact prd.md', status: 'completed' },
        { t: 3, kind: 'message', text: '**done**' },
        { t: 4, kind: 'turn_end', at: '2026-10-01T00:00:15Z' },
      ],
    },
    { id: 2, nodeId: 'b', iteration: 1, status: 'waiting_human', startedAt: '2026-10-01T00:00:31Z' },
  ],
}

function mountView(props: Partial<InstanceType<typeof RunLlmTranscript>['$props']> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(RunLlmTranscript, {
    props: { run: run(), nodes, ...props },
    global: { plugins: [i18n] },
  })
}

beforeEach(() => {
  localStorage.clear()
  llmTranscript.mockReset()
  reactReply.mockReset()
  nodeEvents.mockReset()
  llmTranscript.mockResolvedValue(transcript)
  reactReply.mockResolvedValue({ status: 'ok' })
  nodeEvents.mockResolvedValue({ events: [] })
})
afterEach(() => {
  vi.useRealTimers()
})

describe('RunLlmTranscript', () => {
  it('calls the conversation message component and drops the parallel bubbles', () => {
    expect(transcriptSrc).not.toContain('LlmPromptBubble')
    expect(transcriptSrc).not.toContain('LlmAnswerBubble')
    expect(transcriptSrc).toContain('ClarifyMessageThread')
    expect(transcriptSrc).toContain('ClarifyChat')
    expect(chatSrc).toContain('ClarifyMessageThread')
    expect(chatSrc).toContain('ChatUserMessage')
    expect(chatSrc).toContain('ClarifyThinkingRow')
    expect(chatSrc).not.toContain('v-for="seg in priorRows"')
  })

  it('folds the earlier auto node and keeps the open turn on the conversation timeline', async () => {
    const w = mountView()
    await flushPromises()
    expect(llmTranscript).toHaveBeenCalledWith('r1', expect.anything())
    expect(w.text()).not.toContain('跳到节点')
    expect(w.text()).not.toContain('显示思考')
    expect(w.text()).not.toContain('展开全部')
    expect(w.text()).not.toContain('跟随最新')
    expect(w.text()).not.toContain('在时间线查看')
    expect(w.text()).not.toContain('节点指令')
    expect(w.text()).not.toContain('2 个节点')
    expect(w.find('[data-testid="clarify-input"]').exists()).toBe(false)

    const prior = w.get('[data-testid="clarify-prior-segment"]')
    expect(prior.attributes('data-kind')).toBe('auto')
    expect(prior.text()).toContain('「需求分析」自动执行')
    expect(prior.find('[data-testid="clarify-input"]').exists()).toBe(false)
    ;(prior.element as HTMLDetailsElement).open = true
    await prior.trigger('toggle')
    await flushPromises()
    expect(prior.text()).toContain('先看需求')
    expect(prior.get('.chat-user-message').text()).toBe('先看需求')
    expect(prior.text()).toContain('done')
    expect(prior.find('[data-testid="agent-timeline"]').exists()).toBe(true)
    expect(w.find('[data-testid="llm-prompt-source"]').exists()).toBe(false)
  })

  it('draws an in-flight turn with the conversation thinking row and timeline', async () => {
    llmTranscript.mockResolvedValue({
      executions: [{ id: 1, nodeId: 'a', iteration: 1, status: 'running', startedAt: '2026-10-01T00:00:00Z' }],
      inflight: { a: { prompt: 'implement it', at: '2026-10-01T00:00:01Z' } },
    })
    const w = mountView({ run: run({ status: 'running' }) })
    await flushPromises()
    expect(w.get('[data-testid="clarify-human-message"]').text()).toBe('implement it')
    expect(w.get('[data-testid="clarify-busy-placeholder"]').text()).toContain('思考中…')
    expect(w.text()).not.toContain('等待模型响应')
    expect(w.find('[data-testid="clarify-input"]').exists()).toBe(false)

    await w.setProps({
      liveEvents: {
        a: [{
          t: 0,
          kind: 'timeline',
          parts: [
            { kind: 'thought', text: '先看文件' },
            { kind: 'tool', title: 'Read', status: 'completed' },
          ],
        }],
      },
    })
    await flushPromises()
    expect(w.get('[data-testid="agent-timeline"]').text()).toContain('先看文件')
    expect(w.get('[data-testid="agent-timeline"]').text()).toContain('读取文件')
    expect(w.text()).toContain('思考中…')
    expect(w.text()).not.toContain('正在回答')

    await w.setProps({
      liveEvents: {
        a: [{
          t: 0,
          kind: 'timeline',
          parts: [
            { kind: 'thought', text: '先看文件' },
            { kind: 'tool', title: 'Read', status: 'completed' },
            { kind: 'message', text: 'on it' },
          ],
        }],
      },
    })
    await flushPromises()
    expect(w.text()).toContain('on it')
    expect(w.get('[data-testid="clarify-busy-status"]').text()).toContain('输出中')
  })

  it('labels an earlier conversation with its message count', async () => {
    llmTranscript.mockResolvedValue({
      executions: [
        {
          id: 1,
          nodeId: 'a',
          iteration: 1,
          status: 'completed',
          startedAt: '2026-10-01T00:00:00Z',
          events: [
            { t: 0, kind: 'prompt', text: '用户说的', at: '2026-10-01T00:00:01Z' },
            { t: 1, kind: 'message', text: '好的' },
          ],
        },
        {
          id: 2,
          nodeId: 'b',
          iteration: 1,
          status: 'completed',
          startedAt: '2026-10-01T00:00:20Z',
          events: [{ t: 0, kind: 'prompt', text: '下一步', at: '2026-10-01T00:00:21Z' }],
        },
      ],
    })
    const w = mountView({
      run: run({
        clarifyByNode: {
          a: { turns: [{ role: 'human', text: '用户说的', at: '2026-10-01T00:00:01Z' }] },
        },
      } as Partial<Run>),
    })
    await flushPromises()
    const prior = w.get('[data-testid="clarify-prior-segment"]')
    expect(prior.attributes('data-kind')).toBe('chat')
    expect(prior.text()).toContain('「需求分析」· 2 条消息')
    expect(prior.find('[data-testid="clarify-input"]').exists()).toBe(false)
  })

  it('shows the conversation composer only on the current conversational node', async () => {
    llmTranscript.mockResolvedValue({
      executions: [
        {
          id: 1,
          nodeId: 'a',
          iteration: 1,
          status: 'completed',
          startedAt: '2026-10-01T00:00:00Z',
          durationSec: 6,
          events: [
            { t: 0, kind: 'prompt', text: '自动做完', at: '2026-10-01T00:00:01Z' },
            { t: 1, kind: 'message', text: '完成' },
          ],
        },
        {
          id: 2,
          nodeId: 'b',
          iteration: 1,
          status: 'waiting_human',
          startedAt: '2026-10-01T00:00:20Z',
          events: [
            { t: 0, kind: 'prompt', text: '请确认范围', at: '2026-10-01T00:00:21Z' },
          ],
        },
      ],
    })
    const w = mountView({
      run: run({
        status: 'waiting_human',
        currentNode: { id: 'b', name: '人工审批', iteration: 1, kind: 'clarify', mode: 'manual', state: 'waiting_human' },
        clarifyByNode: { b: { turns: [{ role: 'human', text: '请确认范围', at: '2026-10-01T00:00:21Z' }] } },
      } as Partial<Run>),
    })
    await flushPromises()
    const prior = w.get('[data-testid="clarify-prior-segment"]')
    expect(prior.text()).toContain('「需求分析」自动执行')
    expect(prior.find('textarea, [data-testid="clarify-input"]').exists()).toBe(false)
    expect(w.get('[data-testid="clarify-input"]').exists()).toBe(true)
    expect(w.findAll('[data-testid="clarify-input"]')).toHaveLength(1)
  })

  it('keeps angle brackets as text and folds a finished timeline into 用时', async () => {
    llmTranscript.mockResolvedValue({
      executions: [{
        id: 1,
        nodeId: 'a',
        iteration: 1,
        status: 'completed',
        startedAt: '2026-10-01T00:00:00Z',
        events: [
          { t: 0, kind: 'prompt', text: '看 <tag> 和 **原文**', at: '2026-10-01T00:00:00Z' },
          {
            t: 1,
            kind: 'timeline',
            parts: [
              { kind: 'thought', text: 'secret reasoning' },
              { kind: 'tool', title: 'Read', status: 'failed' },
              { kind: 'message', text: '正文在线下' },
            ],
          },
          { t: 2, kind: 'turn_end', at: '2026-10-01T00:03:34Z' },
        ],
      }],
    })
    const w = mountView()
    await flushPromises()
    const bubble = w.get('[data-testid="clarify-human-message"]')
    expect(bubble.text()).toContain('<tag>')
    expect(bubble.text()).toContain('**原文**')
    expect(bubble.html()).not.toContain('<strong>')
    expect(w.get('[data-testid="agent-timeline-elapsed"]').text()).toContain('用时 3m 34s')
    expect(w.get('[data-testid="agent-timeline-elapsed-failed"]').text()).toBe('1 个失败')
    expect(w.get('[data-testid="clarify-agent-message"]').text()).toContain('正文在线下')
    expect(w.find('[data-testid="agent-timeline-thought"]').exists()).toBe(false)
  })

  it('uses the conversation failure row when the transcript cannot be loaded', async () => {
    llmTranscript.mockRejectedValueOnce(new Error('boom'))
    const w = mountView({
      run: run({
        nodeExecutions: {
          a: [{
            nodeId: 'a',
            status: 'completed',
            startedAt: '2026-10-01T00:00:00Z',
            events: [
              { t: 0, kind: 'prompt', text: 'preview…', truncated: true },
              { t: 1, kind: 'message', text: 'reply' },
            ],
          }],
        },
      }),
    })
    await flushPromises()
    expect(w.get('[data-testid="clarify-empty-fail"]').text()).toContain('完整对话记录加载失败')
    expect(w.text()).toContain('preview…')
    expect(w.find('[data-testid="llm-load-error"]').exists()).toBe(false)
    llmTranscript.mockResolvedValueOnce(transcript)
    await w.get('[data-testid="clarify-empty-fail-retry"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="clarify-empty-fail"]').exists()).toBe(false)
  })

  it('shows the conversation empty hint when a run has no messages', async () => {
    llmTranscript.mockResolvedValue({ executions: [] })
    const w = mountView()
    await flushPromises()
    expect(w.get('[data-testid="clarify-approve-empty-hint"]').text()).toContain('先说明本次要做的目标')
    expect(w.find('[data-testid="clarify-input"]').exists()).toBe(false)
  })

  it('does not invent 环境已就绪 without a boot record', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-testid="clarify-boot-done"]').exists()).toBe(false)
    expect(w.find('[data-testid="clarify-boot-loader"]').exists()).toBe(false)
  })
})

describe('LiveLogPanel transcript kinds', () => {
  it('does not render prompt / turn_end rows in the agent log', () => {
    const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
    const w = mount(LiveLogPanel, {
      props: {
        events: [
          { t: 0, kind: 'prompt', text: 'HIDDEN PROMPT' },
          { t: 1, kind: 'message', text: 'visible reply' },
          { t: 2, kind: 'turn_end', text: 'HIDDEN END' },
        ],
        status: 'completed',
      },
      global: { plugins: [i18n] },
    })
    expect(w.text()).toContain('visible reply')
    expect(w.text()).not.toContain('HIDDEN')
  })
})
