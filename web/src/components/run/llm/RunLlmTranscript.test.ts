// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { LlmTranscriptResponse } from '@/lib/api/apiTypes'
import type { Run, WFNode } from '@/lib/shared/types'

const llmTranscript = vi.fn()
vi.mock('@/lib/api/api', () => ({ api: { llmTranscript: (...a: unknown[]) => llmTranscript(...a) } }))

import RunLlmTranscript from './RunLlmTranscript.vue'
import LiveLogPanel from '../LiveLogPanel.vue'

const longPrompt = Array.from({ length: 12 }, (_, i) => `line ${i + 1}`).join('\n')

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
    trace: [{ at: '2026-10-01T00:00:30Z', nodeId: 'a', event: 'transition', to: 'b', kind: 'success' }],
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
        { t: 0, kind: 'prompt', text: longPrompt, at: '2026-10-01T00:00:01Z' },
        { t: 1, kind: 'thought', text: 'secret reasoning' },
        { t: 2, kind: 'tool_call', title: 'write_artifact prd.md', status: 'completed', artifact: { name: 'prd.md', kind: 'markdown' } },
        { t: 3, kind: 'message', text: '**done**' },
        { t: 4, kind: 'turn_end', at: '2026-10-01T00:00:15Z', usage: { inputTokens: 1000, outputTokens: 200, cacheReadTokens: 0, cacheWriteTokens: 0 } },
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
  llmTranscript.mockResolvedValue(transcript)
})
afterEach(() => {
  vi.useRealTimers()
})

describe('RunLlmTranscript', () => {
  it('renders the run as Q → A chat with node dividers and transitions in order', async () => {
    const w = mountView()
    await flushPromises()
    expect(llmTranscript).toHaveBeenCalledWith('r1', expect.anything())
    const types = w.findAll('[data-item-type]').map((li) => li.attributes('data-item-type'))
    expect(types).toEqual(['node', 'turn', 'trace', 'node'])
    expect(w.find('[data-testid="llm-prompt-source"]').text()).toBe('节点指令')
    expect(w.find('[data-testid="llm-answer-text"]').html()).toContain('<strong>done</strong>')
    expect(w.find('[data-testid="llm-trace"]').text()).toContain('流转到「人工审批」')
    expect(w.find('[data-testid="llm-summary"]').text()).toContain('2 个节点 · 1 轮对话')
  })

  it('collapses long prompts and details until expanded', async () => {
    const w = mountView()
    await flushPromises()
    const text = () => w.find('[data-testid="llm-prompt-text"]').text()
    expect(text()).not.toContain('line 12')
    expect(w.find('[data-testid="llm-thought"]').exists()).toBe(false)
    expect(w.find('[data-testid="llm-tools"]').exists()).toBe(false)

    await w.find('[data-testid="llm-prompt-toggle"]').trigger('click')
    expect(text()).toContain('line 12')

    await w.find('[data-testid="llm-expand-all"]').trigger('click')
    expect(w.find('[data-testid="llm-thought"]').text()).toBe('secret reasoning')
    expect(w.find('[data-testid="llm-tools"]').text()).toContain('write_artifact prd.md')
    expect(JSON.parse(localStorage.getItem('grasp.llmTranscript.prefs')!).expandAll).toBe(true)
  })

  it('hides thinking when the toggle is off', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-testid="llm-thought-toggle"]').exists()).toBe(true)
    await w.find('[data-testid="llm-show-thought"]').setValue(false)
    expect(w.find('[data-testid="llm-thought-toggle"]').exists()).toBe(false)
  })

  it('emits locate and open-artifacts', async () => {
    const w = mountView()
    await flushPromises()
    await w.find('[data-testid="llm-locate"]').trigger('click')
    expect(w.emitted('locate')?.[0]).toEqual(['a', 0])
    await w.find('[data-testid="llm-artifact-chip"]').trigger('click')
    expect(w.emitted('open-artifacts')).toHaveLength(1)
  })

  it('falls back to run detail previews and shows a retry banner when the transcript fails', async () => {
    llmTranscript.mockRejectedValueOnce(new Error('boom'))
    const w = mountView({
      run: run({
        nodeExecutions: {
          a: [
            {
              nodeId: 'a',
              status: 'completed',
              startedAt: '2026-10-01T00:00:00Z',
              events: [
                { t: 0, kind: 'prompt', text: 'preview…', truncated: true },
                { t: 1, kind: 'message', text: 'reply' },
              ],
            },
          ],
        },
      }),
    })
    await flushPromises()
    expect(w.find('[data-testid="llm-load-error"]').exists()).toBe(true)
    expect(w.find('[data-testid="llm-prompt"]').text()).toContain('仅为预览')
    llmTranscript.mockResolvedValueOnce(transcript)
    await w.find('[data-testid="llm-load-error"] button').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="llm-load-error"]').exists()).toBe(false)
  })

  it('shows the in-flight prompt with a waiting indicator for a running node', async () => {
    llmTranscript.mockResolvedValue({
      executions: [{ id: 1, nodeId: 'a', iteration: 1, status: 'running', startedAt: '2026-10-01T00:00:00Z' }],
      inflight: { a: { prompt: 'implement it', at: '2026-10-01T00:00:01Z' } },
    })
    const w = mountView({ run: run({ status: 'running', trace: [] }) })
    await flushPromises()
    expect(w.find('[data-testid="llm-prompt-text"]').text()).toBe('implement it')
    expect(w.find('[data-testid="llm-answer"]').text()).toContain('等待模型响应')
    await w.setProps({ liveEvents: { a: [{ t: 0, kind: 'message', text: 'on it' }] } })
    expect(w.find('[data-testid="llm-answer"]').text()).toContain('on it')
    expect(w.find('[data-testid="llm-answer"]').text()).toContain('正在回答')
  })

  it('shows the empty state for a run without executions', async () => {
    llmTranscript.mockResolvedValue({ executions: [] })
    const w = mountView({ run: run({ trace: [] }) })
    await flushPromises()
    expect(w.find('[data-testid="llm-empty"]').exists()).toBe(true)
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
