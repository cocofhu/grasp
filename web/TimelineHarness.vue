<script setup lang="ts">
import { onMounted } from 'vue'
import type { AgentPart } from '@/lib/shared/types'
import AgentTimeline from '@/components/run/AgentTimeline.vue'

const startedAt = '2026-10-26T00:00:00.000Z'
const endedAt = '2026-10-26T00:01:05.000Z'

const foldedParts: AgentPart[] = [
  { kind: 'tool', title: 'Read', status: 'completed' },
  { kind: 'message', text: '只有一段正文。' },
  { kind: 'thought', text: '先对齐这一行。' },
  { kind: 'tool', title: 'Grep', status: 'completed' },
  { kind: 'message', text: '第一段说明。\n\n第二段还在。\n\n- 列表甲\n- 列表乙' },
  { kind: 'message', text: '代码如下：\n\n```\nconst n = 1\n```' },
  { kind: 'thought', text: '最后这段还在生成。' },
]

const outputParts: AgentPart[] = [
  { kind: 'tool', title: 'Shell', status: 'failed', input: 'ls', output: 'exit 1' },
  { kind: 'thought', text: '输出后面的思考。' },
  { kind: 'tool', title: 'Read', status: 'completed' },
]

const endPara: AgentPart[] = [
  { kind: 'tool', title: 'Read', status: 'completed' },
  { kind: 'message', text: '第一段仍保持段距。\n\n第二段是最后一块。' },
]
const endList: AgentPart[] = [
  { kind: 'message', text: '前面一段。\n\n- 甲\n- 乙' },
]
const endCode: AgentPart[] = [
  { kind: 'message', text: '见代码。\n\n```\nconst n = 1\n```' },
]
const endTable: AgentPart[] = [
  { kind: 'message', text: '一张表。\n\n| a | b |\n| - | - |\n| 1 | 2 |' },
]

const settledParts: AgentPart[] = [
  { kind: 'thought', text: '思考第一段。\n\n思考第二段。' },
  { kind: 'message', text: '过程里的第一段。\n\n过程里的第二段。' },
  { kind: 'tool', title: 'Shell', status: 'failed', input: 'pwd', output: 'exit 1' },
  { kind: 'message', text: '分隔线下方的最终答复。\n\n第二段收尾。' },
]

function round(n: number) {
  return Math.round(n * 100) / 100
}

function box(el: Element) {
  const r = el.getBoundingClientRect()
  return {
    top: round(r.top),
    bottom: round(r.bottom),
    left: round(r.left),
    right: round(r.right),
    height: round(r.height),
    width: round(r.width),
  }
}

function gap(a: Element, b: Element) {
  return round(b.getBoundingClientRect().top - a.getBoundingClientRect().bottom)
}

function blocksOf(message: Element): Element[] {
  const span = message.querySelector(':scope > span')
  if (!span) return []
  if (span.classList.contains('stream-md')) {
    return [...span.querySelectorAll(':scope > .stream-md-block')].flatMap((block) => [...block.children])
  }
  return [...span.children]
}

function px(value: string) {
  const n = Number.parseFloat(value)
  return Number.isFinite(n) ? round(n) : value
}

function proseMetrics(root: ParentNode) {
  const message = root.querySelector('.agent-live-message, .agent-elapsed-note, .chat-prose.md')
  const sample = message?.querySelector('p')
  if (!sample) return null
  const cs = getComputedStyle(sample)
  return { fontSize: cs.fontSize, lineHeight: cs.lineHeight, marginTop: px(cs.marginTop), marginBottom: px(cs.marginBottom) }
}

function measureSection(name: string) {
  const root = document.querySelector(`[data-section="${name}"]`)
  if (!root) return { name, missing: true }
  const timeline = root.querySelector('[data-testid="agent-timeline"]')
  if (!timeline) return { name, missing: true }
  const tools = [...timeline.querySelectorAll(':scope > .agent-tools')]
  const thoughts = [...timeline.querySelectorAll(':scope > .agent-thought')]
  const messages = [...timeline.querySelectorAll(':scope > .agent-live-message')]
  const head = tools[0]?.querySelector('.agent-tools-head') ?? null
  const summary = thoughts[0]?.querySelector('.agent-thought-summary') ?? null
  const toolIcon = head?.querySelector('.agent-tool-icon') ?? null
  const thoughtIcon = summary?.querySelector('.thought-summary-icon') ?? null
  const toolText = head?.querySelector('.agent-tools-count') ?? null
  const thoughtText = summary?.querySelector('.thought-summary-title') ?? null
  const toolChevron = head?.querySelector('.agent-tool-chevron') ?? null
  const thoughtChevron = summary?.querySelector('.agent-thought-chevron') ?? null
  const marker = summary ? getComputedStyle(summary, '::marker').content : ''
  const rows = thoughts.map((details) => {
    const row = details.querySelector('.agent-thought-summary')!
    const icon = row.querySelector('.thought-summary-icon')
    return {
      open: (details as HTMLDetailsElement).open,
      state: row.querySelector('[data-testid="thought-summary-state"]')?.getAttribute('data-state') ?? '',
      icon: icon?.getAttribute('data-icon') ?? '',
      height: box(row).height,
      iconLeft: icon ? box(icon).left : null,
      textLeft: box(row.querySelector('.thought-summary-title')!).left,
      chevronRight: box(row.querySelector('.agent-thought-chevron')!).right,
    }
  })
  const messageGaps = messages.map((message) => {
    const blocks = blocksOf(message)
    const next = message.nextElementSibling
    const cs = getComputedStyle(message)
    return {
      text: (message.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 40),
      marginBottom: px(cs.marginBottom),
      blocks: blocks.map((block) => {
        const style = getComputedStyle(block)
        return { tag: block.tagName.toLowerCase(), marginTop: px(style.marginTop), marginBottom: px(style.marginBottom) }
      }),
      internal: blocks.slice(0, -1).map((block, i) => gap(block, blocks[i + 1]!)),
      after: next ? gap(blocks[blocks.length - 1] ?? message, next.matches('.agent-thought') ? next.querySelector('.agent-thought-summary')! : next.matches('.agent-tools') ? next.querySelector('.agent-tools-head')! : next) : null,
      tail: next ? null : round(timeline.getBoundingClientRect().bottom - (blocks[blocks.length - 1] ?? message).getBoundingClientRect().bottom),
    }
  })
  const foldedToolGaps = tools.map((group) => {
    const button = group.querySelector('.agent-tools-head')!
    const next = group.nextElementSibling
    const open = button.getAttribute('aria-expanded') === 'true'
    return {
      open,
      height: box(button).height,
      after: next
        ? gap(button, next.matches('.agent-thought') ? next.querySelector('.agent-thought-summary')! : next)
        : null,
    }
  })
  const thoughtGaps = thoughts.map((details) => {
    const row = details.querySelector('.agent-thought-summary')!
    const next = details.nextElementSibling
    return {
      open: (details as HTMLDetailsElement).open,
      marginBottom: px(getComputedStyle(details).marginBottom),
      after: !next || (details as HTMLDetailsElement).open
        ? null
        : gap(row, next.matches('.agent-tools') ? next.querySelector('.agent-tools-head')! : next.matches('.agent-live-message') ? (blocksOf(next)[0] ?? next) : next),
    }
  })
  const detail = timeline.querySelector('.agent-tool-detail')
  const output = timeline.querySelector('[data-testid="agent-tool-output"]')
  const detailNext = detail?.closest('.agent-tools')?.nextElementSibling
  const detailTarget = detailNext?.matches('.agent-thought')
    ? detailNext.querySelector('.agent-thought-summary')
    : detailNext?.matches('.agent-tools')
      ? detailNext.querySelector('.agent-tools-head')
      : detailNext
  return {
    name,
    prose: proseMetrics(timeline),
    marker,
    align: head && summary && toolIcon && thoughtIcon && toolText && thoughtText && toolChevron && thoughtChevron
      ? {
          toolIconLeft: box(toolIcon).left,
          thoughtIconLeft: box(thoughtIcon).left,
          iconDelta: round(box(thoughtIcon).left - box(toolIcon).left),
          toolTextLeft: box(toolText).left,
          thoughtTextLeft: box(thoughtText).left,
          textDelta: round(box(thoughtText).left - box(toolText).left),
          toolChevronRight: box(toolChevron).right,
          thoughtChevronRight: box(thoughtChevron).right,
          chevronDelta: round(box(thoughtChevron).right - box(toolChevron).right),
          toolHeight: box(head).height,
          thoughtHeight: box(summary).height,
        }
      : null,
    rows,
    messageGaps,
    foldedToolGaps,
    thoughtGaps,
    detail: detail
      ? {
          margin: getComputedStyle(detail).margin,
          marginBottom: px(getComputedStyle(detail).marginBottom),
          toNext: detailTarget ? gap(detail, detailTarget) : null,
          outputToNext: output && detailTarget ? gap(output, detailTarget) : null,
          hasInput: !!timeline.querySelector('[data-testid="agent-tool-input"]'),
          hasOutput: !!output,
        }
      : null,
  }
}

function measureSettled(name: string) {
  const root = document.querySelector(`[data-section="${name}"]`)
  const work = root?.querySelector('[data-testid="agent-timeline-work"]')
  if (!root || !work) return { name, missing: true }
  const thought = work.querySelector('.agent-elapsed-thought')
  const note = work.querySelector('.agent-elapsed-note')
  const group = work.querySelector('.agent-tools')
  const paras = thought ? [...thought.querySelectorAll(':scope > p')] : []
  const noteBlocks = note ? blocksOf(note) : []
  const detail = work.querySelector('.agent-tool-detail')
  const output = work.querySelector('[data-testid="agent-tool-output"]')
  const rule = root.querySelector('[data-testid="agent-timeline-rule"]')
  const tail = [...(root.querySelectorAll('[data-testid="agent-timeline-message"]') ?? [])].find((el) => !work.contains(el))
  const tailBlocks = tail ? blocksOf(tail) : []
  const head = group?.querySelector('.agent-tools-head')
  return {
    name,
    thoughtMargin: thought ? px(getComputedStyle(thought).marginBottom) : null,
    noteMargin: note ? px(getComputedStyle(note).marginBottom) : null,
    thoughtInternal: paras.length > 1 ? gap(paras[0]!, paras[1]!) : null,
    thoughtToNote: thought && note ? gap(thought, noteBlocks[0] ?? note) : null,
    noteInternal: noteBlocks.length > 1 ? gap(noteBlocks[0]!, noteBlocks[1]!) : null,
    noteToTool: note && head ? gap(noteBlocks[noteBlocks.length - 1] ?? note, head) : null,
    detailMarginBottom: detail ? px(getComputedStyle(detail).marginBottom) : null,
    detailToRule: detail && rule ? gap(detail, rule) : null,
    outputToRule: output && rule ? gap(output, rule) : null,
    tailBlocks: tailBlocks.map((block) => {
      const style = getComputedStyle(block)
      return { tag: block.tagName.toLowerCase(), marginTop: px(style.marginTop), marginBottom: px(style.marginBottom) }
    }),
    tailInternal: tailBlocks.length > 1 ? gap(tailBlocks[0]!, tailBlocks[1]!) : null,
    tailSlack: tailBlocks.length && root.querySelector('[data-testid="agent-timeline"]')
      ? round(root.querySelector('[data-testid="agent-timeline"]')!.getBoundingClientRect().bottom - tailBlocks[tailBlocks.length - 1]!.getBoundingClientRect().bottom)
      : null,
    prose: proseMetrics(work),
    tailProse: tail ? proseMetrics(tail.parentElement!) : null,
  }
}

function measureAll() {
  return {
    pointer: matchMedia('(pointer: coarse)').matches ? 'coarse' : 'fine',
    light: document.documentElement.classList.contains('light'),
    folded: measureSection('clarify-folded'),
    solo: measureSection('solo-folded'),
    output: measureSection('clarify-output'),
    endPara: measureSection('end-para'),
    endList: measureSection('end-list'),
    endCode: measureSection('end-code'),
    endTable: measureSection('end-table'),
    settled: measureSettled('settled-clarify'),
    settledSolo: measureSettled('settled-solo'),
  }
}

onMounted(() => {
  ;(window as unknown as { __measureTimeline: () => unknown }).__measureTimeline = measureAll
})
</script>

<template>
  <main class="harness">
    <h1>时间线间距与对齐</h1>
    <p class="lead">澄清对话包在 chat-thread 里；Solo 一列不包 chat-thread。组件、样式都是仓库里的原件。</p>

    <section data-section="clarify-folded" class="panel">
      <h2>澄清 · 直播折叠</h2>
      <div class="chat-thread column">
        <AgentTimeline :parts="foldedParts" streaming />
      </div>
    </section>

    <section data-section="solo-folded" class="panel">
      <h2>Solo · 直播折叠</h2>
      <div class="column">
        <AgentTimeline :parts="foldedParts" streaming />
      </div>
    </section>

    <section data-section="clarify-output" class="panel">
      <h2>澄清 · 失败工具默认展开输出</h2>
      <div class="chat-thread column">
        <AgentTimeline :parts="outputParts" streaming expanded />
      </div>
    </section>

    <section data-section="end-para" class="panel">
      <h2>澄清 · 末块是多段正文</h2>
      <div class="chat-thread column">
        <AgentTimeline :parts="endPara" streaming />
      </div>
    </section>

    <section data-section="end-list" class="panel">
      <h2>澄清 · 末块是列表</h2>
      <div class="chat-thread column">
        <AgentTimeline :parts="endList" streaming />
      </div>
    </section>

    <section data-section="end-code" class="panel">
      <h2>澄清 · 末块是代码</h2>
      <div class="chat-thread column">
        <AgentTimeline :parts="endCode" streaming />
      </div>
    </section>

    <section data-section="end-table" class="panel">
      <h2>澄清 · 末块是表格</h2>
      <div class="chat-thread column">
        <AgentTimeline :parts="endTable" streaming />
      </div>
    </section>

    <section data-section="settled-clarify" class="panel">
      <h2>澄清 · 回合结束并展开过程</h2>
      <div class="chat-thread column">
        <AgentTimeline :parts="settledParts" :started-at="startedAt" :ended-at="endedAt" expanded />
      </div>
    </section>

    <section data-section="settled-solo" class="panel">
      <h2>Solo · 回合结束并展开过程</h2>
      <div class="column">
        <AgentTimeline :parts="settledParts" :started-at="startedAt" :ended-at="endedAt" expanded />
      </div>
    </section>
  </main>
</template>

<style>
body { margin: 0; background: rgb(var(--c-base)); color: rgb(var(--c-txt)); }
.harness { max-width: 920px; margin: 0 auto; padding: 24px 20px 80px; }
h1 { margin: 0 0 8px; font-size: 20px; }
h2 { margin: 0 0 10px; font-size: 14px; font-weight: 600; }
.lead { margin: 0 0 20px; color: rgb(var(--c-txt2)); font-size: 13px; }
.panel { margin: 0 0 28px; }
.column { width: 420px; max-width: 100%; }
</style>
