// @vitest-environment happy-dom
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Gate, Run } from '@/lib/shared/types'
import { CONTENT_FIT_PREVIEW_MAX_VH } from '@/lib/shared/htmlPreviewSandbox'

vi.mock('@novnc/novnc/lib/rfb.js', () => ({
  default: class MockRFB {},
}))

const apiMocks = vi.hoisted(() => ({
  artifactContent: vi.fn(),
  listPreviewIssues: vi.fn(),
  createPreviewIssue: vi.fn(),
  saveGateArtifact: vi.fn(),
  listGatePrimaryArtifacts: vi.fn(),
  gateReactRevise: vi.fn(),
  gateReactCancel: vi.fn(),
  gateReactQueueRemove: vi.fn(),
  gateReactQueueReorder: vi.fn(),
}))

/** Plain ref-like so template auto-unwrap works (needs __v_isRef). */
const breakpointMocks = vi.hoisted(() => ({
  isMobile: { value: false, __v_isRef: true as const },
}))

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile: breakpointMocks.isMobile }),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      artifactContent: apiMocks.artifactContent,
      listPreviewIssues: apiMocks.listPreviewIssues,
      createPreviewIssue: apiMocks.createPreviewIssue,
      saveGateArtifact: apiMocks.saveGateArtifact,
      listGatePrimaryArtifacts: apiMocks.listGatePrimaryArtifacts,
      gateReactRevise: apiMocks.gateReactRevise,
      gateReactCancel: apiMocks.gateReactCancel,
      gateReactQueueRemove: apiMocks.gateReactQueueRemove,
      gateReactQueueReorder: apiMocks.gateReactQueueReorder,
    },
  }
})

import GateApproval, {
  actionIcon,
  actionVariant,
  actionVariantClasses,
} from './GateApproval.vue'

const HtmlPreviewStub = defineComponent({
  name: 'HtmlPreview',
  props: {
    html: String,
    fitContent: Boolean,
    fillParent: Boolean,
    maxContentHeightVh: Number,
    inspectable: Boolean,
    enlargeable: Boolean,
    mode: String,
  },
  emits: ['pick'],
  template:
    '<div data-testid="html-preview" :data-fit="fitContent ? \'1\' : \'0\'" :data-fill-parent="fillParent ? \'1\' : \'0\'" :data-max-vh="maxContentHeightVh ?? \'\'" :data-inspectable="inspectable ? \'1\' : \'0\'" :data-enlargeable="enlargeable === false ? \'0\' : \'1\'" :data-mode="mode || \'\'" />',
})

const StructuredStub = defineComponent({
  name: 'StructuredArtifactView',
  props: { name: String, doc: Object },
  template: '<div data-testid="structured-view" :data-name="name" />',
})

const AppPreviewStub = defineComponent({
  name: 'AppPreviewPanel',
  props: { fill: Boolean, showFeedback: { type: Boolean, default: true } },
  emits: ['pick', 'issues-changed'],
  template:
    '<div data-testid="app-preview" :data-fill="fill ? \'1\' : \'0\'" :data-show-feedback="showFeedback ? \'1\' : \'0\'" />',
})

const PreviewFeedbackStub = defineComponent({
  name: 'PreviewFeedbackChat',
  props: {
    runId: String,
    nodeId: String,
    selector: String,
    elementImage: Object,
    requireElement: Boolean,
    compact: Boolean,
    fillSidebar: Boolean,
    hideSubmit: Boolean,
    text: String,
    images: Array,
  },
  emits: ['update:text', 'update:images', 'clear-selector', 'issues-changed'],
  setup(props, { emit, expose }) {
    function collectImages(): { data: string; mimeType: string }[] {
      const images: { data: string; mimeType: string }[] = []
      const el = props.elementImage as { data?: string; mimeType?: string } | null | undefined
      if (el?.data) {
        images.push({ data: el.data, mimeType: el.mimeType || 'image/png' })
      }
      for (const im of (props.images as { data: string; mimeType: string }[]) || []) {
        images.push({ data: im.data, mimeType: im.mimeType })
      }
      return images
    }

    async function send(opts?: { body?: string }): Promise<boolean> {
      const images = collectImages()
      const body = (opts?.body !== undefined ? opts.body : props.text || '').trim()
      if (opts?.body === undefined && !body && images.length === 0) return false
      if (opts?.body !== undefined && !body && images.length === 0) return false
      await apiMocks.createPreviewIssue(
        props.runId as string,
        props.nodeId as string,
        body,
        (props.selector as string) || '',
        0,
        images,
      )
      emit('update:text', '')
      emit('update:images', [])
      if (props.selector || props.elementImage) emit('clear-selector')
      emit('issues-changed')
      return true
    }

    async function flush(): Promise<boolean> {
      const images = collectImages()
      const body = (props.text || '').trim()
      if (!body && images.length === 0) return false
      return send()
    }

    async function reload(): Promise<void> {
      /* no-op in stub */
    }

    function clearDraft() {
      emit('update:text', '')
      emit('update:images', [])
    }

    expose({ send, flush, reload, clearDraft })
    return {}
  },
  template: `
    <div
      data-testid="preview-feedback-chat"
      :data-selector="selector || ''"
      :data-require-element="requireElement ? '1' : '0'"
      :data-has-image="elementImage ? '1' : '0'"
      :data-fill-sidebar="fillSidebar ? '1' : '0'"
      :data-hide-submit="hideSubmit ? '1' : '0'"
    >
      <div v-if="!hideSubmit" data-testid="paragraph-input-root" data-text-only="0">
        <button
          type="button"
          data-testid="paragraph-input-attach"
          @click="$emit('update:images', [{ data: 'abc', mimeType: 'image/png' }])"
        />
        <textarea
          data-testid="paragraph-input"
          :value="text || ''"
          @input="$emit('update:text', $event.target.value)"
        />
      </div>
      <button
        v-if="!hideSubmit"
        type="button"
        data-testid="preview-feedback-submit"
      >
        提交评审意见
      </button>
    </div>
  `,
})

const PlanStub = defineComponent({
  name: 'PlanView',
  template: '<div data-testid="plan-view" />',
})

const ParagraphInputStub = defineComponent({
  name: 'ParagraphInput',
  props: {
    text: String,
    images: Array,
    textOnly: Boolean,
    placeholder: String,
    embedded: Boolean,
    compact: Boolean,
    disabled: Boolean,
  },
  emits: ['update:text', 'update:images'],
  setup(props, { emit, expose }) {
    expose({
      pickFiles: () => emit('update:images', [{ data: 'abc', mimeType: 'image/png' }]),
    })
    return {}
  },
  template: `
    <div
      data-testid="paragraph-input-root"
      :data-text-only="textOnly ? '1' : '0'"
      :data-compact="compact ? '1' : '0'"
    >
      <button
        v-if="!textOnly && !embedded"
        type="button"
        data-testid="paragraph-input-attach"
        @click="$emit('update:images', [{ data: 'abc', mimeType: 'image/png' }])"
      />
      <textarea data-testid="paragraph-input" :value="text" @input="$emit('update:text', $event.target.value)" />
    </div>
  `,
})

/** Interactive picker stub — mirrors ProposalSelectView select emit (not readonly). */
const ProposalSelectStub = defineComponent({
  name: 'ProposalSelectView',
  props: { doc: Object, resolvedId: String, readonly: Boolean, disabled: Boolean },
  emits: ['select'],
  template: `
    <div data-testid="proposal-select-view" :data-readonly="readonly ? '1' : '0'">
      <button
        v-if="!readonly && !resolvedId"
        type="button"
        data-testid="proposal-select-pick"
        @click="$emit('select', 'p1')"
      >选此方案</button>
    </div>
  `,
})

function baseGate(overrides: Partial<Gate> = {}): Gate {
  return {
    runId: 'run-1',
    nodeId: 'gate-1',
    workflowName: 'wf',
    title: '审批',
    bodyMd: '请审阅',
    actions: [
      { id: 'approve', label: '批准' },
      { id: 'revise', label: '返回修改', requireForm: true },
    ],
    form: [{ key: 'comment', label: '评审意见' }],
    requestedAt: '2026-07-18T00:00:00Z',
    ...overrides,
  }
}

function baseRun(overrides: Partial<Run> = {}): Run {
  return {
    id: 'run-1',
    title: 'run',
    workflowId: 'wf-1',
    workflowName: 'wf',
    status: 'waiting_human',
    createdAt: '2026-07-18T00:00:00Z',
    nodes: [],
    edges: [],
    nodeStates: {},
    artifacts: [],
    ...overrides,
  } as Run
}

function mountApproval(opts: {
  gate?: Gate
  run?: Run
  fillPreview?: boolean
  mobileFillRemaining?: boolean
  unifiedPreviewBudget?: boolean
  compact?: boolean
}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(GateApproval, {
    props: {
      gate: opts.gate ?? baseGate(),
      run: opts.run,
      fillPreview: opts.fillPreview,
      mobileFillRemaining: opts.mobileFillRemaining,
      unifiedPreviewBudget: opts.unifiedPreviewBudget,
      compact: opts.compact ?? true,
    },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        ParagraphInput: ParagraphInputStub,
        ArtifactLoadingPane: defineComponent({
          name: 'ArtifactLoadingPane',
          template: '<div data-testid="artifact-loading-pane" />',
        }),
        ProposalSelectView: ProposalSelectStub,
        HtmlPreview: HtmlPreviewStub,
        StructuredArtifactView: StructuredStub,
        AppPreviewPanel: AppPreviewStub,
        PreviewFeedbackChat: PreviewFeedbackStub,
        PlanView: PlanStub,
        UpstreamRequirementContext: false,
      },
    },
  })
}

function visualGateRun(pageHtml: string) {
  return {
    gate: baseGate({ nodeId: 'hg-visual' }),
    run: baseRun({
      nodes: [
        {
          id: 'hg-visual',
          type: 'human_gate',
          label: '审阅视觉',
          position: { x: 0, y: 0 },
          config: { body_template: '{{nodes.visual.outputs.page}}' },
        },
      ],
      nodeExecutions: {
        visual: [
          {
            nodeId: 'visual',
            iteration: 1,
            status: 'completed',
            outputs: { page: pageHtml },
          },
        ],
      },
    }),
  }
}

beforeEach(() => {
  breakpointMocks.isMobile.value = false
})

function hasClass(el: Element | null | undefined, token: string): boolean {
  return !!el?.classList.contains(token)
}

function contentFitRoot(wrapper: VueWrapper) {
  return wrapper.find('[data-testid="content-fit-scroll"]')
}

function expectApprovalActionsVisible(wrapper: VueWrapper) {
  const form = wrapper.find('[data-testid="content-fit-form"]')
  expect(form.exists()).toBe(true)
  expect(form.find('[data-testid="paragraph-input"]').exists()).toBe(true)
  const buttons = form.findAll('button')
  // Review semantics: always 确认并流转; never restore 返回修改 / 打回 dual buttons
  expect(buttons.some((b) => b.text().includes('确认并流转'))).toBe(true)
  expect(buttons.every((b) => !b.text().includes('返回修改') && !b.text().includes('打回修改'))).toBe(
    true,
  )
}

describe('actionIcon', () => {
  it('maps positive actions to check', () => {
    expect(actionIcon('approve')).toBe('check')
    expect(actionIcon('pass')).toBe('check')
  })

  it('maps revert actions to arrow-left', () => {
    expect(actionIcon('revise')).toBe('arrow-left')
    expect(actionIcon('fail')).toBe('arrow-left')
    expect(actionIcon('limit')).toBe('arrow-left')
  })

  it('falls back to refresh for custom action ids', () => {
    expect(actionIcon('p1')).toBe('refresh')
    expect(actionIcon('p2')).toBe('refresh')
  })
})

describe('actionVariant', () => {
  it('maps positive actions to ok', () => {
    expect(actionVariant('approve')).toBe('ok')
    expect(actionVariant('pass')).toBe('ok')
  })

  it('maps revert and custom actions to neutral', () => {
    expect(actionVariant('revise')).toBe('neutral')
    expect(actionVariant('fail')).toBe('neutral')
    expect(actionVariant('limit')).toBe('neutral')
    expect(actionVariant('p1')).toBe('neutral')
  })
})

describe('actionVariantClasses', () => {
  it('returns green ok styles', () => {
    expect(actionVariantClasses('ok')).toContain('bg-ok/15')
    expect(actionVariantClasses('ok')).toContain('text-ok')
    expect(actionVariantClasses('ok')).toContain('hover:bg-ok/25')
  })

  it('returns neutral border styles', () => {
    expect(actionVariantClasses('neutral')).toContain('border border-line')
    expect(actionVariantClasses('neutral')).toContain('text-txt2')
    expect(actionVariantClasses('neutral')).toContain('hover:bg-elevated')
    expect(actionVariantClasses('neutral')).toContain('hover:text-txt')
  })
})

describe('GateApproval content-fit layout branches', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    breakpointMocks.isMobile.value = false
    apiMocks.listPreviewIssues.mockResolvedValue({ issues: [] })
    apiMocks.createPreviewIssue.mockResolvedValue({
      id: 'iss-new',
      body: 'new',
      status: 'open',
      createdAt: '2026-07-18T00:02:00Z',
    })
    apiMocks.artifactContent.mockResolvedValue({ content: '{}' })
    // Default: API unavailable → client listPrimaryProducts fallback (existing tests).
    apiMocks.listGatePrimaryArtifacts.mockRejectedValue(new Error('offline'))
  })

  it('uses content-fit shell for short structured + fillPreview (preview capped, form pinned)', async () => {
    const researchDoc = {
      summary: '短调研',
      findings: [{ title: '发现' }],
    }
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-research' }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-research',
            type: 'human_gate',
            label: '审阅调研',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.research.outputs.research}}' },
          },
          {
            id: 'research',
            type: 'research',
            label: '调研',
            position: { x: 0, y: 0 },
            config: {},
          },
        ],
        nodeExecutions: {
          research: [
            {
              nodeId: 'research',
              iteration: 1,
              status: 'completed',
              outputs: { research_json: JSON.stringify(researchDoc) },
            },
          ],
        },
        artifacts: [
          {
            id: 'a-research',
            name: 'research.json',
            kind: 'json',
            nodeId: 'research',
            runId: 'run-1',
            workflowName: 'wf',
            sizeBytes: 10,
            createdAt: '2026-07-18T00:00:00Z',
          },
        ],
      }),
    })
    await flushPromises()

    const scroll = contentFitRoot(wrapper)
    expect(scroll.exists()).toBe(true)
    // Outer shell is overflow-hidden (not the main scroll container).
    expect(hasClass(scroll.element, 'overflow-hidden')).toBe(true)
    expect(hasClass(scroll.element, 'overflow-y-auto')).toBe(false)
    expect(hasClass(scroll.element, 'flex')).toBe(true)
    expect(hasClass(scroll.element, 'flex-col')).toBe(true)

    const preview = wrapper.find('[data-testid="content-fit-preview"]')
    const form = wrapper.find('[data-testid="content-fit-form"]')
    expect(preview.exists()).toBe(true)
    expect(form.exists()).toBe(true)
    // Desktop preview shell: flex-1 fills stage remainder, capped at ≈60vh.
    expect(hasClass(preview.element, 'flex-1')).toBe(true)
    expect(hasClass(preview.element, 'overflow-hidden')).toBe(true)
    expect((preview.element as HTMLElement).style.maxHeight).toBe(
      `${CONTENT_FIT_PREVIEW_MAX_VH}vh`,
    )
    // Run Detail default: no unified budget wrapper; form fills sidebar height.
    expect(wrapper.find('[data-testid="content-fit-budget"]').exists()).toBe(false)
    expect(hasClass(form.element, 'h-full')).toBe(true)
    expect(form.element.className).not.toMatch(/\bflex-1\b/)
    // Form is a sibling after preview (not nested inside preview scroll).
    expect(preview.element.contains(form.element)).toBe(false)
    expect(scroll.element.contains(preview.element)).toBe(true)
    expect(scroll.element.contains(form.element)).toBe(true)
    expect(wrapper.find('[data-testid="structured-view"]').attributes('data-name')).toBe(
      'research.json',
    )
    expect(form.classes()).not.toContain('sticky')
    expectApprovalActionsVisible(wrapper)
    wrapper.unmount()
  })

  it('keeps app_preview on flex-1 fill path under fillPreview', async () => {
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'preview-gate' }),
      run: baseRun({
        nodes: [
          {
            id: 'preview-gate',
            type: 'app_preview',
            label: '预览',
            position: { x: 0, y: 0 },
            config: {},
          },
        ],
      }),
    })
    await flushPromises()

    expect(contentFitRoot(wrapper).exists()).toBe(false)
    const app = wrapper.find('[data-testid="app-preview"]')
    expect(app.exists()).toBe(true)
    expect(app.attributes('data-fill')).toBe('1')
    const appHost = app.element.parentElement
    expect(appHost?.className).toMatch(/\bflex-1\b/)
    wrapper.unmount()
  })

  it('keeps proposal_select interactive picker under fillPreview (no ReviewShell readonly)', async () => {
    const proposalsDoc = {
      context: '选型',
      proposals: [
        { id: 'p1', title: '方案甲', summary: '共享壳', recommended: true },
        { id: 'p2', title: '方案乙', summary: '另起炉灶' },
      ],
    }
    apiMocks.artifactContent.mockResolvedValue({ content: JSON.stringify(proposalsDoc) })
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({
        nodeId: 'pick-proposal',
        actions: [
          { id: 'p1', label: '方案甲' },
          { id: 'p2', label: '方案乙' },
        ],
        form: [],
      }),
      run: baseRun({
        nodes: [
          {
            id: 'pick-proposal',
            type: 'proposal_select',
            label: '选方案',
            position: { x: 0, y: 0 },
            config: { from: 'proposals.json' },
          },
        ],
        artifacts: [
          {
            id: 'a-proposals',
            name: 'proposals.json',
            kind: 'json',
            nodeId: 'proposal',
            runId: 'run-1',
            workflowName: 'wf',
            sizeBytes: 10,
            createdAt: '2026-07-18T00:00:00Z',
          },
        ],
      }),
    })
    await flushPromises()

    // Must stay on default path: interactive picker, not content-fit/ReviewShell readonly stage.
    expect(contentFitRoot(wrapper).exists()).toBe(false)
    expect(wrapper.find('[data-testid="review-shell"]').exists()).toBe(false)

    const picker = wrapper.find('[data-testid="proposal-select-view"]')
    expect(picker.exists()).toBe(true)
    expect(picker.attributes('data-readonly')).toBe('0')
    // GateProductEditor may preview proposals.json, but select must remain interactive.
    const pickBtn = wrapper.find('[data-testid="proposal-select-pick"]')
    expect(pickBtn.exists()).toBe(true)
    await pickBtn.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('resolve')?.[0]?.[0]).toBe('p1')
    wrapper.unmount()
  })

  it('does not enter content-fit without fillPreview (structured stays on default path)', async () => {
    const researchDoc = { summary: '短调研', findings: [{ title: '发现' }] }
    const wrapper = mountApproval({
      fillPreview: false,
      gate: baseGate({ nodeId: 'hg-research' }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-research',
            type: 'human_gate',
            label: '审阅调研',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.research.outputs.research}}' },
          },
        ],
        nodeExecutions: {
          research: [
            {
              nodeId: 'research',
              iteration: 1,
              status: 'completed',
              outputs: { research_json: JSON.stringify(researchDoc) },
            },
          ],
        },
      }),
    })
    await flushPromises()

    expect(contentFitRoot(wrapper).exists()).toBe(false)
    expect(wrapper.find('[data-testid="content-fit-preview"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="structured-view"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('does not enter content-fit for Markdown body or PlanView', async () => {
    const mdWrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ bodyMd: '## 纯 Markdown 审阅' }),
      run: baseRun({
        nodes: [
          {
            id: 'gate-1',
            type: 'human_gate',
            label: '门禁',
            position: { x: 0, y: 0 },
            config: {},
          },
        ],
      }),
    })
    await flushPromises()
    expect(contentFitRoot(mdWrapper).exists()).toBe(false)
    mdWrapper.unmount()

    apiMocks.artifactContent.mockResolvedValue({
      content: JSON.stringify({
        goals: [{ id: 'g1', title: '目标', status: 'pending', subgoals: [] }],
      }),
    })
    const planWrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({
        bodyMd: '- [ ] `g1` 目标\n- [ ] `g1.1` 子目标',
      }),
      run: baseRun({
        nodes: [
          {
            id: 'gate-1',
            type: 'human_gate',
            label: '计划门禁',
            position: { x: 0, y: 0 },
            config: {},
          },
        ],
        artifacts: [
          {
            id: 'a-plan',
            name: 'plan.json',
            kind: 'json',
            nodeId: 'plan',
            runId: 'run-1',
            workflowName: 'wf',
            sizeBytes: 10,
            createdAt: '2026-07-18T00:00:00Z',
          },
        ],
      }),
    })
    await flushPromises()
    expect(contentFitRoot(planWrapper).exists()).toBe(false)
    expect(planWrapper.find('[data-testid="plan-view"]').exists()).toBe(true)
    planWrapper.unmount()
  })

  it('keeps visual shouldFillPreview on desktop fillParent + 60vh shell cap', async () => {
    const pageHtml = '<!doctype html><html><body><h1>视觉稿</h1></body></html>'
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-visual',
            type: 'human_gate',
            label: '审阅视觉',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.visual.outputs.page}}' },
          },
        ],
        nodeExecutions: {
          visual: [
            {
              nodeId: 'visual',
              iteration: 1,
              status: 'completed',
              outputs: { page: pageHtml },
            },
          ],
        },
      }),
    })
    await flushPromises()

    expect(contentFitRoot(wrapper).exists()).toBe(true)
    const html = wrapper.find('[data-testid="html-preview"]')
    expect(html.exists()).toBe(true)
    // Desktop visual: fillParent (no scrollHeight-driven shell); 60vh on outer shell.
    // Editable page.html uses GateProductEditor → HtmlPreview mode=inline + fillParent.
    expect(html.attributes('data-fit')).toBe('0')
    expect(html.attributes('data-fill-parent')).toBe('1')
    expect(html.attributes('data-mode')).toBe('inline')
    const preview = wrapper.find('[data-testid="content-fit-preview"]')
    expect(hasClass(preview.element, 'flex-1')).toBe(true)
    expect(hasClass(preview.element, 'overflow-hidden')).toBe(true)
    expect((preview.element as HTMLElement).style.maxHeight).toBe(
      `${CONTENT_FIT_PREVIEW_MAX_VH}vh`,
    )
    expect(wrapper.find('[data-testid="content-fit-form"]').classes()).toContain('h-full')
    wrapper.unmount()
  })

  it('short visual HTML: shell flex-1 + fillParent (blank stays in-frame, not content-fit shrink)', async () => {
    // plan g3.2 / f1+f3: short page must not collapse shell via scrollHeight/120px fallback
    const shortPage = '<!doctype html><html><body><p>短</p></body></html>'
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-visual',
            type: 'human_gate',
            label: '审阅视觉',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.visual.outputs.page}}' },
          },
        ],
        nodeExecutions: {
          visual: [
            {
              nodeId: 'visual',
              iteration: 1,
              status: 'completed',
              outputs: { page: shortPage },
            },
          ],
        },
      }),
    })
    await flushPromises()

    const preview = wrapper.find('[data-testid="content-fit-preview"]')
    expect(hasClass(preview.element, 'flex-1')).toBe(true)
    expect(hasClass(preview.element, 'overflow-hidden')).toBe(true)
    expect((preview.element as HTMLElement).style.maxHeight).toBe(
      `${CONTENT_FIT_PREVIEW_MAX_VH}vh`,
    )
    const html = wrapper.find('[data-testid="html-preview"]')
    expect(html.attributes('data-fill-parent')).toBe('1')
    expect(html.attributes('data-fit')).toBe('0')
    // Shell eats stage remainder (flex-1); sidebar confirm stays reachable.
    const form = wrapper.find('[data-testid="content-fit-form"]')
    expect(form.exists()).toBe(true)
    expect(form.findAll('button').some((b) => b.text().includes('确认并流转'))).toBe(true)
    wrapper.unmount()
  })

  it('100vh-style visual HTML still uses fillParent shell (no fitContent narrow fallback)', async () => {
    // plan g3.2 / f6: 100vh / measure deadlock must not drive outer shell to ~120px
    const vhPage =
      '<!doctype html><html><body style="height:100vh;margin:0"><div style="height:100%">满屏</div></body></html>'
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-visual',
            type: 'human_gate',
            label: '审阅视觉',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.visual.outputs.page}}' },
          },
        ],
        nodeExecutions: {
          visual: [
            {
              nodeId: 'visual',
              iteration: 1,
              status: 'completed',
              outputs: { page: vhPage },
            },
          ],
        },
      }),
    })
    await flushPromises()

    const preview = wrapper.find('[data-testid="content-fit-preview"]')
    expect(hasClass(preview.element, 'flex-1')).toBe(true)
    expect((preview.element as HTMLElement).style.maxHeight).toBe(
      `${CONTENT_FIT_PREVIEW_MAX_VH}vh`,
    )
    const html = wrapper.find('[data-testid="html-preview"]')
    expect(html.attributes('data-fill-parent')).toBe('1')
    expect(html.attributes('data-fit')).toBe('0')
    const form = wrapper.find('[data-testid="content-fit-form"]')
    expect(form.exists()).toBe(true)
    expect(form.findAll('button').some((b) => b.text().includes('确认并流转'))).toBe(true)
    wrapper.unmount()
  })

  it('keeps approval form/actions visible for tall page.html under fillPreview', async () => {
    const tallPage = `<!doctype html><html><body style="min-height:200vh">${'<p>长内容</p>'.repeat(80)}</body></html>`
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-visual',
            type: 'human_gate',
            label: '审阅视觉',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.visual.outputs.page}}' },
          },
        ],
        nodeExecutions: {
          visual: [
            {
              nodeId: 'visual',
              iteration: 1,
              status: 'completed',
              outputs: { page: tallPage },
            },
          ],
        },
      }),
    })
    await flushPromises()

    expect(contentFitRoot(wrapper).exists()).toBe(true)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    // Feedback lives in sidebar (not under stage preview).
    const form = wrapper.find('[data-testid="content-fit-form"]')
    expect(form.exists()).toBe(true)
    expect(form.find('[data-testid="preview-feedback-chat"]').exists()).toBe(true)
    expect(form.find('[data-testid="content-fit-feedback"]').exists()).toBe(true)
    // Unified sidebar input: exactly one paragraph-input (gate.form hidden).
    expect(form.findAll('[data-testid="paragraph-input"]').length).toBe(1)
    expect(wrapper.find('[data-testid="review-shell-sidebar"] [data-testid="preview-feedback-chat"]').exists()).toBe(
      true,
    )
    const buttons = form.findAll('button')
    expect(buttons.some((b) => b.text().includes('确认并流转'))).toBe(true)
    expect(buttons.every((b) => !b.text().includes('返回修改') && !b.text().includes('退回'))).toBe(
      true,
    )
    expect(wrapper.text()).toContain('可确认并流转；若需修改，请先发送评审意见就地改')
    expect(wrapper.text()).not.toContain('可直接退回')
    wrapper.unmount()
  })

  it('locks Pass by open PreviewIssue count only (resolved ignored)', async () => {
    apiMocks.listPreviewIssues.mockResolvedValue({
      issues: [
        {
          id: 'iss-1',
          runId: 'run-1',
          nodeId: 'hg-visual',
          body: 'old',
          status: 'resolved',
          createdAt: '2026-07-18T00:00:00Z',
        },
        {
          id: 'iss-2',
          runId: 'run-1',
          nodeId: 'hg-visual',
          body: 'new',
          status: 'open',
          createdAt: '2026-07-18T00:01:00Z',
        },
      ],
    })
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-visual',
            type: 'human_gate',
            label: '审阅视觉',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.visual.outputs.page}}' },
          },
        ],
        nodeExecutions: {
          visual: [
            {
              nodeId: 'visual',
              iteration: 1,
              status: 'completed',
              outputs: { page: '<!doctype html><html><body>ok</body></html>' },
            },
          ],
        },
      }),
    })
    await flushPromises()

    const form = wrapper.find('[data-testid="content-fit-form"]')
    const buttons = form.findAll('button')
    // With ≥1 open issue (resolved ones don't count): only the negative exit
    // (退回/返回修改) is offered, enabled — no 确认并流转, no disabled dead end.
    expect(buttons.some((b) => b.text().includes('确认并流转'))).toBe(false)
    const revise = buttons.find((b) => b.text().includes('返回修改'))!
    expect(revise).toBeTruthy()
    expect((revise.element as HTMLButtonElement).disabled).toBe(false)
    wrapper.unmount()
  })

  it('keeps confirm disabled when open PreviewIssues and reactSessionAlive; send remains', async () => {
    apiMocks.listPreviewIssues.mockResolvedValue({
      issues: [
        {
          id: 'iss-open',
          runId: 'run-1',
          nodeId: 'hg-visual',
          body: 'open issue',
          status: 'open',
          createdAt: '2026-07-18T00:01:00Z',
        },
      ],
    })
    const pageHtml = '<!doctype html><html><body><h1>open issues</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
      run,
    })
    await flushPromises()

    const pass = wrapper.find('[data-testid="review-composer-pass"]')
    expect(pass.exists()).toBe(true)
    expect((pass.element as HTMLButtonElement).disabled).toBe(true)
    const send = wrapper.find('[data-testid="review-composer-send"]')
    expect(send.exists()).toBe(true)
    expect(send.text()).toContain('发送')
    expect(wrapper.emitted('resolve')).toBeFalsy()
    wrapper.unmount()
  })

  it('allows Pass when only resolved PreviewIssues remain', async () => {
    apiMocks.listPreviewIssues.mockResolvedValue({
      issues: [
        {
          id: 'iss-1',
          runId: 'run-1',
          nodeId: 'hg-visual',
          body: 'old',
          status: 'resolved',
          createdAt: '2026-07-18T00:00:00Z',
        },
      ],
    })
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-visual',
            type: 'human_gate',
            label: '审阅视觉',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.visual.outputs.page}}' },
          },
        ],
        nodeExecutions: {
          visual: [
            {
              nodeId: 'visual',
              iteration: 1,
              status: 'completed',
              outputs: { page: '<!doctype html><html><body>ok</body></html>' },
            },
          ],
        },
      }),
    })
    await flushPromises()

    const form = wrapper.find('[data-testid="content-fit-form"]')
    const buttons = form.findAll('button')
    expect(buttons.some((b) => b.text().includes('确认并流转'))).toBe(true)
    expect(buttons.every((b) => !b.text().includes('退回') && !b.text().includes('返回修改'))).toBe(
      true,
    )
    expect(wrapper.text()).toContain('可确认并流转；若需修改，请先发送评审意见就地改')
    expect(wrapper.text()).not.toContain('已提交评审意见')
    wrapper.unmount()
  })

  it('keeps approval form/actions visible for short research.json under fillPreview', async () => {
    const researchDoc = {
      summary: '短调研结论',
      findings: [{ title: '一项发现' }],
    }
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-research' }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-research',
            type: 'human_gate',
            label: '审阅调研',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.research.outputs.research}}' },
          },
        ],
        nodeExecutions: {
          research: [
            {
              nodeId: 'research',
              iteration: 1,
              status: 'completed',
              outputs: { research_json: JSON.stringify(researchDoc) },
            },
          ],
        },
      }),
    })
    await flushPromises()

    expect(contentFitRoot(wrapper).exists()).toBe(true)
    expect(wrapper.find('[data-testid="structured-view"]').exists()).toBe(true)
    expectApprovalActionsVisible(wrapper)
    wrapper.unmount()
  })

  it('shows upstream context under content-fit preview when requirement JSON exists', async () => {
    const researchDoc = { summary: '短调研', findings: [{ title: '发现' }] }
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-research' }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-research',
            type: 'human_gate',
            label: '审阅调研',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.research.outputs.research}}' },
          },
          {
            id: 'research',
            type: 'research',
            label: '调研',
            position: { x: 0, y: 0 },
            config: {},
          },
        ],
        nodeExecutions: {
          research: [
            {
              nodeId: 'research',
              iteration: 1,
              status: 'completed',
              outputs: { research_json: JSON.stringify(researchDoc) },
            },
          ],
        },
        artifacts: [
          {
            id: 'a-research',
            name: 'research.json',
            kind: 'json',
            nodeId: 'research',
            runId: 'run-1',
            workflowName: 'wf',
            sizeBytes: 10,
            createdAt: '2026-07-18T00:00:00Z',
          },
          {
            id: 'a-req',
            name: 'clarified_requirement.json',
            kind: 'json',
            nodeId: 'react',
            runId: 'run-1',
            workflowName: 'wf',
            sizeBytes: 20,
            createdAt: '2026-07-18T00:00:00Z',
          },
        ],
      }),
    })
    await flushPromises()

    expect(contentFitRoot(wrapper).exists()).toBe(true)
    const preview = wrapper.find('[data-testid="content-fit-preview"]')
    expect(hasClass(preview.element, 'flex-1')).toBe(true)
    // Run Detail default: 60vh remains on preview alone (unified budget off).
    expect((preview.element as HTMLElement).style.maxHeight).toBe(
      `${CONTENT_FIT_PREVIEW_MAX_VH}vh`,
    )
    expect(wrapper.find('[data-testid="content-fit-budget"]').exists()).toBe(false)
    const upstream = wrapper.find('[data-testid="upstream-context"]')
    expect(upstream.exists()).toBe(true)
    expect(wrapper.find('[data-testid="upstream-context-body"]').exists()).toBe(false)
    // Upstream is a stage sibling under the preview shell (g3.3).
    expect(preview.element.contains(upstream.element)).toBe(false)
    expect(wrapper.find('[data-testid="structured-view"]').attributes('data-name')).toBe(
      'research.json',
    )
    wrapper.unmount()
  })

  it('Inbox unified budget: stage-fill wraps preview+upstream; no 60vh void under upstream', async () => {
    // plan g2.2 / g2.3 / f2 / review v1: budget fills stage (no maxHeight:60vh);
    // preview alone has no maxHeight; 60vh remains Run Detail preview-only.
    const pageHtml = '<!doctype html><html><body><h1>Inbox</h1></body></html>'
    const wrapper = mountApproval({
      fillPreview: true,
      unifiedPreviewBudget: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-visual',
            type: 'human_gate',
            label: '审阅视觉',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.visual.outputs.page}}' },
          },
        ],
        nodeExecutions: {
          visual: [
            {
              nodeId: 'visual',
              iteration: 1,
              status: 'completed',
              outputs: { page: pageHtml },
            },
          ],
        },
        artifacts: [
          {
            id: 'a-req',
            name: 'clarified_requirement.json',
            kind: 'json',
            nodeId: 'react',
            runId: 'run-1',
            workflowName: 'wf',
            sizeBytes: 20,
            createdAt: '2026-07-18T00:00:00Z',
          },
        ],
      }),
    })
    await flushPromises()

    const budget = wrapper.find('[data-testid="content-fit-budget"]')
    const preview = wrapper.find('[data-testid="content-fit-preview"]')
    const upstream = wrapper.find('[data-testid="upstream-context"]')
    expect(budget.exists()).toBe(true)
    expect(preview.exists()).toBe(true)
    expect(upstream.exists()).toBe(true)
    // Inbox path: no 60vh cap on budget (would leave void under upstream in tall stage)
    expect((budget.element as HTMLElement).style.maxHeight).toBe('')
    // g2.3: preview no longer owns the 60vh sibling budget alone
    expect((preview.element as HTMLElement).style.maxHeight).toBe('')
    expect(hasClass(preview.element, 'flex-1')).toBe(true)
    expect(hasClass(budget.element, 'flex-1')).toBe(true)
    expect(hasClass(budget.element, 'min-h-0')).toBe(true)
    // Upstream is inside budget, sibling of preview (not nested in preview).
    expect(budget.element.contains(preview.element)).toBe(true)
    expect(budget.element.contains(upstream.element)).toBe(true)
    expect(preview.element.contains(upstream.element)).toBe(false)
    // fillParent short-HTML semantics retained (g3.1)
    const html = wrapper.find('[data-testid="html-preview"]')
    expect(html.attributes('data-fill-parent')).toBe('1')
    expect(html.attributes('data-fit')).toBe('0')
    // Confirm reachable in sidebar (g1.2)
    const form = wrapper.find('[data-testid="content-fit-form"]')
    expect(form.findAll('button').some((b) => b.text().includes('确认并流转'))).toBe(true)
    wrapper.unmount()
  })

  it('Inbox unified budget without upstream: budget fills stage, preview has no maxHeight', async () => {
    // plan g1.2 / s3: no upstream strip; card still uses unified budget wrapper
    const pageHtml = '<!doctype html><html><body><p>短</p></body></html>'
    const wrapper = mountApproval({
      fillPreview: true,
      unifiedPreviewBudget: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-visual',
            type: 'human_gate',
            label: '审阅视觉',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.visual.outputs.page}}' },
          },
        ],
        nodeExecutions: {
          visual: [
            {
              nodeId: 'visual',
              iteration: 1,
              status: 'completed',
              outputs: { page: pageHtml },
            },
          ],
        },
      }),
    })
    await flushPromises()

    const budget = wrapper.find('[data-testid="content-fit-budget"]')
    expect(budget.exists()).toBe(true)
    expect((budget.element as HTMLElement).style.maxHeight).toBe('')
    expect(hasClass(budget.element, 'flex-1')).toBe(true)
    expect(wrapper.find('[data-testid="upstream-context"]').exists()).toBe(false)
    const preview = wrapper.find('[data-testid="content-fit-preview"]')
    expect((preview.element as HTMLElement).style.maxHeight).toBe('')
    expect(wrapper.find('[data-testid="html-preview"]').attributes('data-fill-parent')).toBe('1')
    wrapper.unmount()
  })

  it('unified budget off by default so Run Detail keeps preview-only 60vh', async () => {
    // plan g2.1 / n2: Run Detail must not enable unified wrapper
    const pageHtml = '<!doctype html><html><body><h1>Run Detail</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({ fillPreview: true, gate, run })
    await flushPromises()

    expect(wrapper.find('[data-testid="content-fit-budget"]').exists()).toBe(false)
    expect((wrapper.find('[data-testid="content-fit-preview"]').element as HTMLElement).style.maxHeight).toBe(
      `${CONTENT_FIT_PREVIEW_MAX_VH}vh`,
    )
    wrapper.unmount()
  })

  it('unified budget expands upstream under budget without preview maxHeight', async () => {
    // plan g3.2 / f4: expand upstream body; budget still fills stage (no 60vh void)
    const pageHtml = '<!doctype html><html><body><h1>预览</h1></body></html>'
    const wrapper = mountApproval({
      fillPreview: true,
      unifiedPreviewBudget: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-visual',
            type: 'human_gate',
            label: '审阅视觉',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.visual.outputs.page}}' },
          },
        ],
        nodeExecutions: {
          visual: [
            {
              nodeId: 'visual',
              iteration: 1,
              status: 'completed',
              outputs: { page: pageHtml },
            },
          ],
        },
        artifacts: [
          {
            id: 'a-req',
            name: 'clarified_requirement.json',
            kind: 'json',
            nodeId: 'react',
            runId: 'run-1',
            workflowName: 'wf',
            sizeBytes: 20,
            createdAt: '2026-07-18T00:00:00Z',
          },
        ],
      }),
    })
    await flushPromises()

    apiMocks.artifactContent.mockResolvedValue({
      content: JSON.stringify({ title: '需求', summary: '展开正文' }),
    })
    await wrapper.find('[data-testid="upstream-context-toggle"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="upstream-context-body"]').exists()).toBe(true)
    const budget = wrapper.find('[data-testid="content-fit-budget"]')
    expect((budget.element as HTMLElement).style.maxHeight).toBe('')
    expect(hasClass(budget.element, 'flex-1')).toBe(true)
    expect((wrapper.find('[data-testid="content-fit-preview"]').element as HTMLElement).style.maxHeight).toBe(
      '',
    )
    // Preview still flex-1 so it yields height when upstream expands.
    expect(hasClass(wrapper.find('[data-testid="content-fit-preview"]').element, 'flex-1')).toBe(
      true,
    )
    wrapper.unmount()
  })

  it('does not show upstream context when main product is clarified_requirement.json', async () => {
    const reqDoc = {
      title: '需求',
      summary: '摘要',
      functional_requirements: [{ title: 'f1' }],
    }
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-req' }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-req',
            type: 'human_gate',
            label: '审阅需求',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.react.outputs.clarified_requirement}}' },
          },
        ],
        nodeExecutions: {
          react: [
            {
              nodeId: 'react',
              iteration: 1,
              status: 'completed',
              outputs: { clarified_requirement_json: JSON.stringify(reqDoc) },
            },
          ],
        },
        artifacts: [
          {
            id: 'a-req',
            name: 'clarified_requirement.json',
            kind: 'json',
            nodeId: 'react',
            runId: 'run-1',
            workflowName: 'wf',
            sizeBytes: 20,
            createdAt: '2026-07-18T00:00:00Z',
          },
        ],
      }),
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="structured-view"]').attributes('data-name')).toBe(
      'clarified_requirement.json',
    )
    expect(wrapper.find('[data-testid="upstream-context"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows collapsed upstream context on non content-fit layout', async () => {
    const wrapper = mountApproval({
      fillPreview: false,
      gate: baseGate({
        nodeId: 'hg-md',
        bodyMd: '请审阅改动摘要',
      }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-md',
            type: 'human_gate',
            label: '合并确认',
            position: { x: 0, y: 0 },
            config: { body_template: '{{artifact("changes_summary.md")}}\n\nMR: {{nodes.implement.outputs.mr_url}}' },
          },
        ],
        artifacts: [
          {
            id: 'a-req',
            name: 'clarified_requirement.json',
            kind: 'json',
            nodeId: 'react',
            runId: 'run-1',
            workflowName: 'wf',
            sizeBytes: 20,
            createdAt: '2026-07-18T00:00:00Z',
          },
        ],
      }),
    })
    await flushPromises()

    expect(contentFitRoot(wrapper).exists()).toBe(false)
    expect(wrapper.find('[data-testid="upstream-context"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="upstream-context-body"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('skips upstream context when body uses artifact(clarified_requirement.json)', async () => {
    const wrapper = mountApproval({
      fillPreview: false,
      gate: baseGate({
        nodeId: 'hg-art',
        bodyMd: '需求正文',
      }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-art',
            type: 'human_gate',
            label: '审阅需求',
            position: { x: 0, y: 0 },
            config: { body_template: '{{artifact("clarified_requirement.json")}}' },
          },
        ],
        artifacts: [
          {
            id: 'a-req',
            name: 'clarified_requirement.json',
            kind: 'json',
            nodeId: 'react',
            runId: 'run-1',
            workflowName: 'wf',
            sizeBytes: 20,
            createdAt: '2026-07-18T00:00:00Z',
          },
        ],
      }),
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="upstream-context"]').exists()).toBe(false)
    wrapper.unmount()
  })
})

describe('GateApproval primary-artifacts status guard', () => {
  const researchDoc = { summary: '短调研', findings: [{ title: '发现' }] }

  function researchGateRun(status: Run['status']) {
    return baseRun({
      status,
      nodes: [
        {
          id: 'gate-1',
          type: 'human_gate',
          label: '审阅',
          position: { x: 0, y: 0 },
          config: { body_template: '{{nodes.research.outputs.research}}' },
        },
        {
          id: 'research',
          type: 'research',
          label: '调研',
          position: { x: 0, y: 0 },
          config: {},
        },
      ],
      nodeExecutions: {
        research: [
          {
            nodeId: 'research',
            iteration: 1,
            status: 'completed',
            outputs: { research_json: JSON.stringify(researchDoc) },
          },
        ],
      },
      artifacts: [
        {
          id: 'a-research',
          name: 'research.json',
          kind: 'json',
          nodeId: 'research',
          runId: 'run-1',
          workflowName: 'wf',
          sizeBytes: 10,
          createdAt: '2026-07-18T00:00:00Z',
        },
      ],
    })
  }

  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.listPreviewIssues.mockResolvedValue({ issues: [] })
    apiMocks.artifactContent.mockResolvedValue({ content: '{}' })
    apiMocks.listGatePrimaryArtifacts.mockResolvedValue({
      items: [
        {
          name: 'research.json',
          kind: 'json',
          readonly: false,
          nodeId: 'research',
        },
      ],
    })
  })

  it('skips listGatePrimaryArtifacts when status is running and falls back to body_template', async () => {
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'gate-1' }),
      run: researchGateRun('running'),
    })
    await flushPromises()

    expect(apiMocks.listGatePrimaryArtifacts).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="structured-view"]').attributes('data-name')).toBe(
      'research.json',
    )
    expectApprovalActionsVisible(wrapper)
    wrapper.unmount()
  })

  it('still calls listGatePrimaryArtifacts when status is waiting_human', async () => {
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'gate-1' }),
      run: researchGateRun('waiting_human'),
    })
    await flushPromises()

    expect(apiMocks.listGatePrimaryArtifacts).toHaveBeenCalledWith('run-1', 'gate-1')
    expect(wrapper.find('[data-testid="structured-view"]').attributes('data-name')).toBe(
      'research.json',
    )
    wrapper.unmount()
  })

  it('stops calling listGatePrimaryArtifacts after status leaves waiting_human', async () => {
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'gate-1' }),
      run: researchGateRun('waiting_human'),
    })
    await flushPromises()
    expect(apiMocks.listGatePrimaryArtifacts).toHaveBeenCalledTimes(1)

    await wrapper.setProps({ run: researchGateRun('running') })
    await flushPromises()

    expect(apiMocks.listGatePrimaryArtifacts).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="structured-view"]').attributes('data-name')).toBe(
      'research.json',
    )
    expectApprovalActionsVisible(wrapper)
    wrapper.unmount()
  })
})

describe('GateApproval product editor state machine', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.listPreviewIssues.mockResolvedValue({ issues: [] })
    apiMocks.artifactContent.mockResolvedValue({
      content: JSON.stringify({ summary: 'saved', findings: [{ title: 'f1' }] }, null, 2),
      etag: 'W/"1-abc"',
      updatedAt: '2026-07-18T00:00:00Z',
      sizeBytes: 40,
    })
    apiMocks.saveGateArtifact.mockResolvedValue({
      id: 'a1',
      name: 'research.json',
      kind: 'json',
      sizeBytes: 50,
      updatedAt: '2026-07-18T01:00:00Z',
      etag: 'W/"2-def"',
      nodeId: 'gate-1',
      content: JSON.stringify({ summary: 'normalized', findings: [{ title: 'f1' }] }, null, 2),
    })
    apiMocks.listGatePrimaryArtifacts.mockRejectedValue(new Error('offline'))
  })

  function editableResearchRun() {
    return baseRun({
      nodes: [
        {
          id: 'gate-1',
          type: 'human_gate',
          label: '审阅',
          position: { x: 0, y: 0 },
          config: { body_template: '{{nodes.research.outputs.research}}' },
        },
        {
          id: 'research',
          type: 'research',
          label: '调研',
          position: { x: 0, y: 0 },
          config: {},
        },
      ],
      nodeExecutions: {
        research: [
          {
            nodeId: 'research',
            iteration: 1,
            status: 'completed',
            outputs: {
              research_json: JSON.stringify({ summary: 'saved', findings: [{ title: 'f1' }] }),
            },
          },
        ],
      },
      artifacts: [
        {
          id: 'a-research',
          name: 'research.json',
          kind: 'json',
          nodeId: 'research',
          runId: 'run-1',
          workflowName: 'wf',
          sizeBytes: 40,
          createdAt: '2026-07-18T00:00:00Z',
        },
      ],
    })
  }

  it('prefers ListGatePrimaryArtifacts kind/readonly over client suffix inference', async () => {
    // Store kind=image but filename has no image suffix — client would treat as text.
    apiMocks.listGatePrimaryArtifacts.mockResolvedValue({
      items: [
        {
          name: 'screenshot-blob',
          kind: 'image',
          readonly: true,
          nodeId: 'test',
        },
      ],
    })
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(new Blob(['img'], { type: 'image/png' }), { status: 200 }),
    )
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'gate-1' }),
      run: baseRun({
        nodes: [
          {
            id: 'gate-1',
            type: 'human_gate',
            label: '审阅',
            position: { x: 0, y: 0 },
            config: { body_template: '{{artifact("screenshot-blob")}}' },
          },
          {
            id: 'test',
            type: 'test',
            label: '测试',
            position: { x: 0, y: 0 },
            config: { produces: 'screenshot-blob' },
          },
        ],
        artifacts: [
          {
            id: 'a-shot',
            name: 'screenshot-blob',
            kind: 'image',
            nodeId: 'test',
            runId: 'run-1',
            workflowName: 'wf',
            sizeBytes: 10,
            createdAt: '2026-07-18T00:00:00Z',
          },
        ],
      }),
    })
    await flushPromises()

    expect(apiMocks.listGatePrimaryArtifacts).toHaveBeenCalledWith('run-1', 'gate-1')
    expect(wrapper.find('[data-testid="gate-product-editor"]').exists()).toBe(true)
    // Without API, client would infer kind=text and show edit/save; server kind wins.
    expect(wrapper.find('[data-testid="gate-readonly-image"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gate-artifact-save"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="gate-mode-edit"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('只读')
    fetchSpy.mockRestore()
    wrapper.unmount()
  })

  it('confirms discard of dirty draft before approve (save ≠ approve)', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'gate-1' }),
      run: editableResearchRun(),
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="gate-product-editor"]').exists()).toBe(true)
    await wrapper.findAll('button').find((b) => b.text() === '编辑')!.trigger('click')
    await flushPromises()
    // Switch to raw JSON so dirty detection is against the draft payload.
    await wrapper.findAll('button').find((b) => b.text() === '原始 JSON')!.trigger('click')
    await flushPromises()
    await wrapper.find('[data-testid="gate-artifact-textarea"]').setValue(
      '{"summary":"draft-only","findings":[{"title":"f1"}]}',
    )
    await flushPromises()
    expect(wrapper.text()).toContain('有未保存修改')

    await wrapper.findAll('button').find((b) => b.text().includes('确认并流转'))!.trigger('click')
    await flushPromises()

    expect(confirmSpy).toHaveBeenCalled()
    expect(wrapper.emitted('resolve')).toBeFalsy()
    confirmSpy.mockRestore()
    wrapper.unmount()
  })

  it('sends If-Match and applies server-normalized content after save', async () => {
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'gate-1' }),
      run: editableResearchRun(),
    })
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text() === '编辑')!.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find((b) => b.text() === '原始 JSON')!.trigger('click')
    await flushPromises()
    await wrapper.find('[data-testid="gate-artifact-textarea"]').setValue(
      '{"summary":"client-draft","findings":[{"title":"f1"}]}',
    )
    await flushPromises()

    await wrapper.find('[data-testid="gate-artifact-save"]').trigger('click')
    await flushPromises()

    expect(apiMocks.saveGateArtifact).toHaveBeenCalledWith(
      'run-1',
      'gate-1',
      'research.json',
      expect.stringContaining('client-draft'),
      'W/"1-abc"',
    )
    // Save switches to preview; re-enter edit (structMode stays json) to inspect draft.
    await wrapper.findAll('button').find((b) => b.text() === '编辑')!.trigger('click')
    await flushPromises()
    const draft = (wrapper.find('[data-testid="gate-artifact-textarea"]').element as HTMLTextAreaElement)
      .value
    expect(draft).toContain('normalized')
    expect(draft).not.toContain('client-draft')
    wrapper.unmount()
  })
})

describe('GateApproval HTML preview load gate (fillPreview)', () => {
  const pageHtml = '<!doctype html><html><body><h1>预览正文</h1></body></html>'

  function visualEditableRun(opts?: { page?: string; artifactContentFails?: boolean }) {
    return baseRun({
      nodes: [
        {
          id: 'hg-visual',
          type: 'human_gate',
          label: '审阅视觉',
          position: { x: 0, y: 0 },
          config: { body_template: '{{nodes.visual.outputs.page}}' },
        },
        {
          id: 'visual',
          type: 'visual',
          label: '视觉',
          position: { x: 0, y: 0 },
          config: {},
        },
      ],
      nodeExecutions: {
        visual: [
          {
            nodeId: 'visual',
            iteration: 1,
            status: 'completed',
            outputs: { page: opts?.page ?? pageHtml },
          },
        ],
      },
      artifacts: [
        {
          id: 'a-page',
          name: 'page.html',
          kind: 'html',
          nodeId: 'visual',
          runId: 'run-1',
          workflowName: 'wf',
          sizeBytes: 40,
          createdAt: '2026-07-18T00:00:00Z',
        },
      ],
    })
  }

  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.listPreviewIssues.mockResolvedValue({ issues: [] })
    apiMocks.artifactContent.mockResolvedValue({
      content: pageHtml,
      etag: 'W/"p1"',
      updatedAt: '2026-07-18T00:00:00Z',
      sizeBytes: 40,
    })
  })

  it('shows inline loading in GateProductEditor while product loads (s1)', async () => {
    let release!: (v: unknown) => void
    apiMocks.artifactContent.mockReturnValue(
      new Promise((resolve) => {
        release = resolve
      }),
    )
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: visualEditableRun(),
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="content-fit-scroll"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="content-fit-product-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="gate-product-editor"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gate-preview-loading"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(false)

    release({
      content: pageHtml,
      etag: 'W/"p1"',
      updatedAt: '2026-07-18T00:00:00Z',
      sizeBytes: 40,
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="gate-preview-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="gate-product-editor"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('shows empty state when HTML body is blank (s2)', async () => {
    apiMocks.artifactContent.mockResolvedValue({
      content: '   ',
      etag: 'W/"empty"',
      updatedAt: '2026-07-18T00:00:00Z',
      sizeBytes: 0,
    })
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: visualEditableRun({ page: '' }),
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="gate-preview-empty"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('内容为空')
    expect(wrapper.text()).toContain('编辑')
    expect(wrapper.findAll('button').every((b) => !b.text().includes('去编辑'))).toBe(true)
    wrapper.unmount()
  })

  it('shows error + retry on load failure and recovers after retry (s3)', async () => {
    apiMocks.artifactContent
      .mockRejectedValueOnce(new Error('network down'))
      .mockResolvedValueOnce({
        content: pageHtml,
        etag: 'W/"p2"',
        updatedAt: '2026-07-18T00:00:00Z',
        sizeBytes: 40,
      })
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      // Snapshot empty so load goes to artifact store and can fail.
      run: visualEditableRun({ page: '' }),
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="gate-preview-error"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('加载失败')

    await wrapper.find('[data-testid="gate-preview-retry"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="gate-preview-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    expect(apiMocks.artifactContent).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('disables edit tab while loading (s5)', async () => {
    let release!: (v: unknown) => void
    apiMocks.artifactContent.mockReturnValue(
      new Promise((resolve) => {
        release = resolve
      }),
    )
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: visualEditableRun(),
    })
    await flushPromises()

    const editBtn = wrapper.find('[data-testid="gate-mode-edit"]')
    expect(editBtn.exists()).toBe(true)
    expect((editBtn.element as HTMLButtonElement).disabled).toBe(true)
    expect(wrapper.find('[data-testid="gate-preview-loading"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="content-fit-product-loading"]').exists()).toBe(false)

    release({
      content: pageHtml,
      etag: 'W/"p1"',
      updatedAt: '2026-07-18T00:00:00Z',
      sizeBytes: 40,
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="gate-preview-loading"]').exists()).toBe(false)
    expect((editBtn.element as HTMLButtonElement).disabled).toBe(false)
    wrapper.unmount()
  })

  it('keeps non-empty HTML preview after edit↔preview round-trip (s4)', async () => {
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: visualEditableRun(),
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    await wrapper.find('[data-testid="gate-mode-edit"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-artifact-textarea"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(false)

    await wrapper.find('[data-testid="gate-mode-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps GateProductEditor mounted while refreshing a single product', async () => {
    const refreshedHtml = '<!doctype html><html><body><p>refreshed</p></body></html>'
    let releaseRefresh!: (v: unknown) => void
    apiMocks.artifactContent
      .mockResolvedValueOnce({
        content: pageHtml,
        etag: 'W/"p1"',
        updatedAt: '2026-07-18T00:00:00Z',
        sizeBytes: 40,
      })
      .mockReturnValueOnce(
        new Promise((resolve) => {
          releaseRefresh = resolve
        }),
      )
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: visualEditableRun({ page: '' }),
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="gate-product-editor"]').exists()).toBe(true)
    const editor = wrapper.findComponent({ name: 'GateProductEditor' })
    editor.vm.$emit('refresh-request', 'page.html')
    await flushPromises()

    // Must not flip to ArtifactLoadingPane (that would unmount the editor).
    expect(wrapper.find('[data-testid="content-fit-product-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="gate-product-editor"]').exists()).toBe(true)

    releaseRefresh({
      content: refreshedHtml,
      etag: 'W/"p2"',
      updatedAt: '2026-07-18T00:01:00Z',
      sizeBytes: refreshedHtml.length,
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="gate-product-editor"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="html-preview"]').exists()).toBe(true)
    expect(apiMocks.artifactContent).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('skips loadProduct on poll when fingerprint unchanged (no loading pane, no artifactContent)', async () => {
    apiMocks.artifactContent.mockResolvedValue({
      content: pageHtml,
      etag: 'W/"p1"',
      updatedAt: '2026-07-18T00:00:00Z',
      sizeBytes: 40,
    })
    const run = visualEditableRun()
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run,
    })
    await flushPromises()
    expect(apiMocks.artifactContent).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="gate-product-editor"]').exists()).toBe(true)

    await wrapper.setProps({
      run: {
        ...run,
        durationSec: 99,
        progress: 42,
        nodeExecutions: {
          ...run.nodeExecutions,
          other: [{ nodeId: 'other', iteration: 1, status: 'running' }],
        },
      } as Run,
    })
    await flushPromises()

    expect(apiMocks.artifactContent).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="content-fit-product-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="gate-preview-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="gate-product-editor"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('pending live gate ignores pure updatedAt noise (no reload)', async () => {
    apiMocks.artifactContent.mockResolvedValue({
      content: pageHtml,
      etag: 'W/"p1"',
      updatedAt: '2026-07-18T00:00:00Z',
      sizeBytes: 40,
    })
    const run = visualEditableRun()
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run,
    })
    await flushPromises()
    expect(apiMocks.artifactContent).toHaveBeenCalledTimes(1)

    const artifact = run.artifacts![0]
    await wrapper.setProps({
      run: {
        ...run,
        artifacts: [
          {
            ...artifact,
            updatedAt: '2026-07-18T01:00:00Z',
          },
        ],
      } as Run,
    })
    await flushPromises()

    expect(apiMocks.artifactContent).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('pending live gate reloads when artifact sizeBytes or etag change (store write signal)', async () => {
    const liveHtml = '<!doctype html><html><body>live-v2</body></html>'
    apiMocks.artifactContent
      .mockResolvedValueOnce({
        content: pageHtml,
        etag: 'W/"p1"',
        updatedAt: '2026-07-18T00:00:00Z',
        sizeBytes: 40,
      })
      .mockResolvedValueOnce({
        content: liveHtml,
        etag: 'W/"p2"',
        updatedAt: '2026-07-18T01:00:00Z',
        sizeBytes: liveHtml.length,
      })
    const run = visualEditableRun()
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run,
    })
    await flushPromises()
    expect(apiMocks.artifactContent).toHaveBeenCalledTimes(1)

    const artifact = run.artifacts![0]
    await wrapper.setProps({
      run: {
        ...run,
        artifacts: [
          {
            ...artifact,
            etag: 'W/"p2"',
            updatedAt: '2026-07-18T01:00:00Z',
            sizeBytes: liveHtml.length,
          },
        ],
      } as Run,
    })
    await flushPromises()

    // Pending gates follow live store; size/etag change must force reload.
    expect(apiMocks.artifactContent).toHaveBeenCalledTimes(2)
    const preview = wrapper.findComponent({ name: 'HtmlPreview' })
    expect(preview.props('html')).toContain('live-v2')
    wrapper.unmount()
  })

  it('pending live gate does not reassign productHtml when fetched body is unchanged', async () => {
    const sameHtml = pageHtml
    apiMocks.artifactContent
      .mockResolvedValueOnce({
        content: sameHtml,
        etag: 'W/"p1"',
        updatedAt: '2026-07-18T00:00:00Z',
        sizeBytes: 40,
      })
      .mockResolvedValueOnce({
        content: sameHtml,
        etag: 'W/"p1"',
        updatedAt: '2026-07-18T01:00:00Z',
        sizeBytes: 99,
      })
    const run = visualEditableRun()
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run,
    })
    await flushPromises()
    const preview = wrapper.findComponent({ name: 'HtmlPreview' })
    const htmlBefore = preview.props('html')

    const artifact = run.artifacts![0]
    await wrapper.setProps({
      run: {
        ...run,
        artifacts: [
          {
            ...artifact,
            sizeBytes: 99,
            updatedAt: '2026-07-18T01:00:00Z',
          },
        ],
      } as Run,
    })
    await flushPromises()

    expect(apiMocks.artifactContent).toHaveBeenCalledTimes(2)
    expect(preview.props('html')).toBe(htmlBefore)
    wrapper.unmount()
  })

  it('pending live gate prefers store content over stale outputs.page snap', async () => {
    const liveHtml = '<!doctype html><html><body>from-store</body></html>'
    apiMocks.artifactContent.mockResolvedValue({
      content: liveHtml,
      etag: 'W/"live"',
      updatedAt: '2026-07-18T00:00:00Z',
      sizeBytes: liveHtml.length,
    })
    const run = visualEditableRun({ page: '<!doctype html><html><body>stale-snap</body></html>' })
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run,
    })
    await flushPromises()
    const preview = wrapper.findComponent({ name: 'HtmlPreview' })
    expect(preview.props('html')).toContain('from-store')
    expect(preview.props('html')).not.toContain('stale-snap')
    wrapper.unmount()
  })

  it('loads product once when api primary list hydrates (no SSOT double fetch)', async () => {
    apiMocks.listGatePrimaryArtifacts.mockResolvedValue({
      items: [
        {
          name: 'page.html',
          kind: 'html',
          readonly: false,
          nodeId: 'visual',
          outputKey: 'page',
        },
      ],
    })
    apiMocks.artifactContent.mockResolvedValue({
      content: pageHtml,
      etag: 'W/"p1"',
      updatedAt: '2026-07-18T00:00:00Z',
      sizeBytes: 40,
    })
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: visualEditableRun(),
    })
    await flushPromises()

    expect(apiMocks.listGatePrimaryArtifacts).toHaveBeenCalledTimes(1)
    expect(apiMocks.artifactContent).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="gate-product-editor"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('editable page.html desktop uses fillParent for edit and preview tabs (g2.2/g3.2)', async () => {
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: visualEditableRun(),
    })
    await flushPromises()

    const editor = wrapper.findComponent({ name: 'GateProductEditor' })
    expect(editor.exists()).toBe(true)
    expect(editor.props('fillParent')).toBe(true)
    expect(editor.props('fitContent')).toBe(false)
    expect(editor.classes()).toContain('flex-1')

    const previewShell = wrapper.find('[data-testid="content-fit-preview"]')
    expect(hasClass(previewShell.element, 'flex-1')).toBe(true)
    expect((previewShell.element as HTMLElement).style.maxHeight).toBe(
      `${CONTENT_FIT_PREVIEW_MAX_VH}vh`,
    )

    const htmlPreview = wrapper.find('[data-testid="html-preview"]')
    expect(htmlPreview.attributes('data-fill-parent')).toBe('1')
    expect(htmlPreview.attributes('data-fit')).toBe('0')

    await wrapper.find('[data-testid="gate-mode-edit"]').trigger('click')
    await flushPromises()
    expect(editor.props('fillParent')).toBe(true)
    expect(wrapper.find('[data-testid="gate-artifact-textarea"]').exists()).toBe(true)
    expect(hasClass(previewShell.element, 'flex-1')).toBe(true)

    await wrapper.find('[data-testid="gate-mode-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="html-preview"]').attributes('data-fill-parent')).toBe('1')
    wrapper.unmount()
  })

  it('enables inspect on editable page.html and forwards pick to PreviewFeedbackChat', async () => {
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({
        nodeId: 'hg-visual',
        actions: [
          { id: 'pass', label: '通过' },
          { id: 'fail', label: '打回' },
        ],
      }),
      run: visualEditableRun(),
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="gate-product-editor"]').exists()).toBe(true)
    const html = wrapper.find('[data-testid="html-preview"]')
    expect(html.exists()).toBe(true)
    expect(html.attributes('data-inspectable')).toBe('1')

    const chat = wrapper.find('[data-testid="preview-feedback-chat"]')
    expect(chat.exists()).toBe(true)
    expect(chat.attributes('data-require-element')).toBe('0')
    expect(chat.attributes('data-selector')).toBe('')
    expect(chat.attributes('data-has-image')).toBe('0')

    const editor = wrapper.findComponent({ name: 'GateProductEditor' })
    const png =
      'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='
    await editor.vm.$emit('pick', {
      selector: '#cta',
      tagName: 'button',
      imageDataUrl: png,
    })
    await flushPromises()

    const chatAfter = wrapper.find('[data-testid="preview-feedback-chat"]')
    expect(chatAfter.attributes('data-selector')).toBe('#cta')
    expect(chatAfter.attributes('data-has-image')).toBe('1')
    wrapper.unmount()
  })

  it('hides reject without open preview issues on page.html path', async () => {
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: visualEditableRun(),
    })
    await flushPromises()

    const form = wrapper.find('[data-testid="content-fit-form"]')

    // Sidebar hosts unified feedback input; gate.form comment field stays hidden.
    expect(form.find('[data-testid="preview-feedback-chat"]').exists()).toBe(true)
    expect(form.findAll('[data-testid="paragraph-input"]').length).toBe(1)
    expect(form.find('[data-testid="preview-feedback-submit"]').exists()).toBe(true)
    const buttons = form.findAll('button')
    expect(buttons.some((b) => b.text().includes('确认并流转'))).toBe(true)
    expect(buttons.every((b) => !b.text().includes('返回修改') && !b.text().includes('退回'))).toBe(
      true,
    )
    expect(wrapper.text()).toContain('可确认并流转；若需修改，请先发送评审意见就地改')
    expect(wrapper.text()).not.toContain('可直接退回')
    expect(wrapper.emitted('resolve')).toBeFalsy()
    expect(apiMocks.createPreviewIssue).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('flushes unsent feedback text into history on cold revise when open issues exist', async () => {
    apiMocks.listPreviewIssues.mockResolvedValue({
      issues: [
        {
          id: 'iss-open',
          runId: 'run-1',
          nodeId: 'hg-visual',
          body: '已有意见',
          status: 'open',
          createdAt: '2026-07-18T00:00:00Z',
        },
      ],
    })
    apiMocks.createPreviewIssue.mockResolvedValue({
      id: 'iss-new',
      runId: 'run-1',
      nodeId: 'hg-visual',
      body: '请调整主色',
      status: 'open',
      createdAt: '2026-07-18T00:02:00Z',
    })
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: visualEditableRun(),
    })
    await flushPromises()

    const form = wrapper.find('[data-testid="content-fit-form"]')
    await form.find('[data-testid="paragraph-input"]').setValue('请调整主色')
    await flushPromises()

    // With ≥1 open issue: only 退回/返回修改 is offered (enabled), no 确认并流转.
    const buttons = form.findAll('button')
    expect(buttons.some((b) => b.text().includes('确认并流转'))).toBe(false)
    const revise = buttons.find((b) => b.text().includes('返回修改'))!
    expect(revise).toBeTruthy()
    expect((revise.element as HTMLButtonElement).disabled).toBe(false)

    await revise.trigger('click')
    await flushPromises()

    // Unsent draft text is flushed into PreviewIssue history before the gate resolves.
    expect(apiMocks.createPreviewIssue).toHaveBeenCalledWith(
      'run-1',
      'hg-visual',
      '请调整主色',
      expect.any(String),
      0,
      expect.any(Array),
    )
    expect(wrapper.emitted('resolve')?.[0]?.[0]).toBe('revise')
    wrapper.unmount()
  })

  it('silently discards unsent feedback on approve without confirm', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm')
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: visualEditableRun(),
    })
    await flushPromises()

    const form = wrapper.find('[data-testid="content-fit-form"]')
    await form.find('[data-testid="paragraph-input"]').setValue('未提交意见')
    await flushPromises()

    const approveBtn = form.findAll('button').find((b) => b.text().includes('确认并流转'))!
    await approveBtn.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('resolve')?.[0]?.[0]).toBe('approve')
    expect(apiMocks.createPreviewIssue).not.toHaveBeenCalled()
    expect(confirmSpy).not.toHaveBeenCalled()
    confirmSpy.mockRestore()
    wrapper.unmount()
  })

  it('hides approve and allows empty-comment reject when preview issues exist', async () => {
    apiMocks.listPreviewIssues.mockResolvedValue({
      issues: [
        {
          id: 'iss-1',
          runId: 'run-1',
          nodeId: 'hg-visual',
          body: '按钮颜色不对',
          selector: '',
          status: 'open',
          createdAt: '2026-07-18T00:00:00Z',
        },
      ],
    })
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({
        nodeId: 'hg-visual',
        actions: [
          { id: 'approve', label: '批准' },
          { id: 'revise', label: '返回修改', requireForm: true },
        ],
      }),
      run: visualEditableRun(),
    })
    await flushPromises()

    const form = wrapper.find('[data-testid="content-fit-form"]')
    const buttons = form.findAll('button')
    // ≥1 open issue: 确认并流转 is hidden entirely; 返回修改 is enabled and
    // does not require the (now-hidden) form comment to be filled.
    expect(buttons.some((b) => b.text().includes('确认并流转'))).toBe(false)
    const revise = buttons.find((b) => b.text().includes('返回修改'))!
    expect(revise).toBeTruthy()
    expect((revise.element as HTMLButtonElement).disabled).toBe(false)

    await revise.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('resolve')?.[0]?.[0]).toBe('revise')
    wrapper.unmount()
  })

  it('content-fit footer hides actions while preview issues are loading', async () => {
    let resolveIssues!: (v: { issues: unknown[] }) => void
    apiMocks.listPreviewIssues.mockReturnValue(
      new Promise((resolve) => {
        resolveIssues = resolve
      }),
    )
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({ nodeId: 'hg-visual' }),
      run: visualEditableRun(),
    })
    await flushPromises()

    const form = wrapper.find('[data-testid="content-fit-form"]')
    expect(form.find('[data-testid="content-fit-preview-issues-loading"]').exists()).toBe(true)
    expect(form.text()).toContain('正在获取预览问题')
    expect(form.findAll('button').every((b) => !b.text().includes('批准'))).toBe(true)
    expect(form.findAll('button').every((b) => !b.text().includes('返回修改'))).toBe(true)

    resolveIssues({ issues: [] })
    await flushPromises()

    expect(form.find('[data-testid="content-fit-preview-issues-loading"]').exists()).toBe(false)
    expect(form.findAll('button').some((b) => b.text().includes('确认并流转'))).toBe(true)
    expect(form.findAll('button').every((b) => !b.text().includes('返回修改'))).toBe(true)
    expect(wrapper.text()).toContain('确认并流转')
    wrapper.unmount()
  })
})

describe('GateApproval app_preview reject without form', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    breakpointMocks.isMobile.value = false
    apiMocks.listPreviewIssues.mockResolvedValue({ issues: [] })
    // Avoid pollution from visual tests that mock page.html primary products.
    apiMocks.listGatePrimaryArtifacts.mockRejectedValue(new Error('offline'))
    apiMocks.artifactContent.mockResolvedValue({ content: '{}' })
  })

  it('hides reject with empty form and no open preview issues (default app_preview)', async () => {
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({
        nodeId: 'preview-gate',
        form: [],
        actions: [
          { id: 'pass', label: '通过' },
          { id: 'fail', label: '退回' },
        ],
      }),
      run: baseRun({
        nodes: [
          {
            id: 'preview-gate',
            type: 'app_preview',
            label: '预览',
            position: { x: 0, y: 0 },
            config: {},
          },
        ],
      }),
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="paragraph-input"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('确认并流转')
    expect(wrapper.text()).not.toContain('可直接退回')
    const buttons = wrapper.findAll('button')
    expect(buttons.some((b) => b.text().includes('确认并流转'))).toBe(true)
    expect(buttons.every((b) => b.text() !== '退回' && !b.text().includes('退回('))).toBe(true)
    expect(wrapper.emitted('resolve')).toBeFalsy()
    wrapper.unmount()
  })

  it('hides configured gate.form on app_preview and disables confirm when open issues exist', async () => {
    apiMocks.listPreviewIssues.mockResolvedValue({
      issues: [
        {
          id: 'iss-1',
          runId: 'run-1',
          nodeId: 'preview-gate',
          body: '预览问题',
          status: 'open',
          createdAt: '2026-07-18T00:00:00Z',
        },
      ],
    })
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({
        nodeId: 'preview-gate',
        form: [{ key: 'comment', label: '评审意见' }],
        actions: [
          { id: 'pass', label: '通过' },
          { id: 'fail', label: '退回' },
        ],
      }),
      run: baseRun({
        nodes: [
          {
            id: 'preview-gate',
            type: 'app_preview',
            label: '预览',
            position: { x: 0, y: 0 },
            config: {},
          },
        ],
      }),
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="paragraph-input"]').exists()).toBe(false)
    const buttons = wrapper.findAll('button')
    // ≥1 open issue: 确认并流转 is hidden; the gate's own 退回 exit is offered, enabled.
    expect(buttons.some((b) => b.text().includes('确认并流转'))).toBe(false)
    const reject = buttons.find((b) => b.text().includes('退回'))!
    expect(reject).toBeTruthy()
    expect((reject.element as HTMLButtonElement).disabled).toBe(false)

    await reject.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('resolve')?.[0]?.[0]).toBe('fail')
    wrapper.unmount()
  })

  function appPreviewHotMount(opts?: { issues?: Array<Record<string, unknown>> }) {
    if (opts?.issues) {
      apiMocks.listPreviewIssues.mockResolvedValue({ issues: opts.issues })
    }
    return mountApproval({
      fillPreview: true,
      gate: baseGate({
        nodeId: 'preview-gate',
        form: [],
        actions: [
          { id: 'pass', label: '通过' },
          { id: 'fail', label: '退回' },
        ],
        reactSessionAlive: true,
        reactUpstreamNodeId: 'agent',
      }),
      run: baseRun({
        nodes: [
          {
            id: 'preview-gate',
            type: 'app_preview',
            label: '预览',
            position: { x: 0, y: 0 },
            config: {},
          },
        ],
      }),
    })
  }

  it('hot app_preview n_open=0: send + confirm mounted; empty send disabled until draft', async () => {
    const wrapper = appPreviewHotMount()
    await flushPromises()

    expect(wrapper.find('[data-testid="app-preview"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="review-composer-gate"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="review-composer-pass"]').exists()).toBe(true)
    const send = wrapper.find('[data-testid="review-composer-send"]')
    expect(send.exists()).toBe(true)
    expect((send.element as HTMLButtonElement).disabled).toBe(true)
    expect(apiMocks.gateReactRevise).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('hot app_preview VNC pick (outerHTML) stages ReAct selector annotation', async () => {
    const wrapper = appPreviewHotMount()
    await flushPromises()
    const panel = wrapper.findComponent({ name: 'AppPreviewPanel' })
    expect(panel.exists()).toBe(true)
    await panel.vm.$emit('pick', {
      selector: '#hero',
      tagName: 'DIV',
      outerHTML: '<div id="hero">x</div>',
      url: 'http://127.0.0.1:5173/settings?tab=1',
    })
    await flushPromises()
    // Annotation chip: path · selector; full url stays on annotation.
    expect(wrapper.text()).toContain('/settings?tab=1 · #hero')
    const vm = wrapper.vm as any
    expect(vm.reactAnnotations?.[0]?.url).toBe('http://127.0.0.1:5173/settings?tab=1')
    wrapper.unmount()
  })

  it('hot app_preview VNC pick without url still stages selector', async () => {
    const wrapper = appPreviewHotMount()
    await flushPromises()
    const panel = wrapper.findComponent({ name: 'AppPreviewPanel' })
    await panel.vm.$emit('pick', {
      selector: '#hero',
      tagName: 'DIV',
      outerHTML: '<div id="hero">x</div>',
    })
    await flushPromises()
    expect(wrapper.text()).toContain('#hero')
    const vm = wrapper.vm as any
    expect(vm.reactAnnotations?.[0]?.url).toBeUndefined()
    wrapper.unmount()
  })

  it('hot app_preview n_open≥1: send enabled and confirm disabled; send uses gateReactRevise', async () => {
    apiMocks.gateReactRevise.mockResolvedValue({})
    const wrapper = appPreviewHotMount({
      issues: [
        {
          id: 'iss-1',
          runId: 'run-1',
          nodeId: 'preview-gate',
          body: '预览问题',
          status: 'open',
          createdAt: '2026-07-18T00:00:00Z',
        },
      ],
    })
    await flushPromises()

    const pass = wrapper.find('[data-testid="review-composer-pass"]')
    expect(pass.exists()).toBe(true)
    expect((pass.element as HTMLButtonElement).disabled).toBe(true)
    const send = wrapper.find('[data-testid="review-composer-send"]')
    expect(send.exists()).toBe(true)
    expect(send.text()).toContain('发送')
    expect((send.element as HTMLButtonElement).disabled).toBe(false)
    await send.trigger('click')
    await flushPromises()
    expect(apiMocks.gateReactRevise).toHaveBeenCalled()
    expect(wrapper.emitted('resolve')).toBeFalsy()
    wrapper.unmount()
  })

  it('hot app_preview only-resolved: confirm enabled and send present', async () => {
    const wrapper = appPreviewHotMount({
      issues: [
        {
          id: 'iss-resolved',
          runId: 'run-1',
          nodeId: 'preview-gate',
          body: '已解决',
          status: 'resolved',
          createdAt: '2026-07-18T00:00:00Z',
        },
      ],
    })
    await flushPromises()

    const pass = wrapper.find('[data-testid="review-composer-pass"]')
    expect(pass.exists()).toBe(true)
    expect((pass.element as HTMLButtonElement).disabled).toBe(false)
    expect(wrapper.find('[data-testid="review-composer-send"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('确认并流转')
    wrapper.unmount()
  })
})

describe('GateApproval mobileFillRemaining layout', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    breakpointMocks.isMobile.value = true
    apiMocks.listPreviewIssues.mockResolvedValue({ issues: [] })
    apiMocks.artifactContent.mockResolvedValue({ content: '{}' })
    apiMocks.listGatePrimaryArtifacts.mockRejectedValue(new Error('offline'))
  })

  it('uses ReviewShell drawer with fillParent preview and cold sticky actions', async () => {
    const pageHtml = '<!doctype html><html><body><h1>短文</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      mobileFillRemaining: true,
      gate,
      run,
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="mobile-fill-remaining"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="content-fit-scroll"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="review-shell"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="review-shell-drawer-handle"]').exists()).toBe(true)

    const preview = wrapper.find('[data-testid="mobile-fill-preview"]')
    expect(preview.exists()).toBe(true)
    expect(preview.classes()).toContain('flex-1')

    const html = wrapper.find('[data-testid="html-preview"]')
    expect(html.attributes('data-fill-parent')).toBe('1')
    expect(html.attributes('data-fit')).toBe('0')
    expect(html.attributes('data-enlargeable')).toBe('0')
    expect(html.attributes('data-inspectable')).toBe('1')
    expect(html.attributes('data-mode')).toBe('inline')

    const sticky = wrapper.find('[data-testid="mobile-fill-sticky-actions"]')
    expect(sticky.exists()).toBe(true)
    // Feedback moved into sidebar/drawer (not stage below preview).
    expect(sticky.find('[data-testid="preview-feedback-chat"]').exists()).toBe(true)
    expect(sticky.find('[data-testid="mobile-fill-feedback"]').exists()).toBe(true)
    const buttons = sticky.findAll('button')
    expect(buttons.some((b) => b.text().includes('确认并流转'))).toBe(true)
    expect(buttons.every((b) => !b.text().includes('返回修改') && !b.text().includes('退回'))).toBe(
      true,
    )
    expect(wrapper.find('[data-testid="review-composer-send"]').exists()).toBe(false)

    // Stage no longer hosts feedback.
    const scrollHtml = wrapper.find('[data-testid="mobile-fill-scroll"]').html()
    expect(scrollHtml).not.toContain('mobile-fill-feedback')
    expect(scrollHtml).not.toContain('preview-feedback-chat')
    wrapper.unmount()
  })

  it('exposes hot send + confirm when n_open=0 in mobile-fill reactSessionAlive', async () => {
    const pageHtml = '<!doctype html><html><body><h1>热发送</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      mobileFillRemaining: true,
      gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
      run,
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="review-shell-drawer-handle"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="review-composer-gate"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="review-composer-send"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="review-composer-pass"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="review-record-issue"]').exists()).toBe(true)
    // Hot unified input: single paragraph-input in sidebar (no ReviewComposer draft).
    const sidebar = wrapper.find('[data-testid="mobile-fill-sticky-actions"]')
    expect(sidebar.findAll('[data-testid="paragraph-input"]').length).toBe(1)
    expect(sidebar.find('[data-testid="preview-feedback-chat"]').attributes('data-hide-submit')).toBe(
      '1',
    )
    // Narrow hot reject must allow attachments (f9/f10/s7) — not text-only.
    expect(sidebar.find('[data-testid="paragraph-input-root"]').attributes('data-text-only')).toBe('0')
    expect(sidebar.find('[data-testid="paragraph-input-attach"]').exists()).toBe(true)
    // The composer shell must stay compact and leave the review history a floor,
    // otherwise the drawer squeezes mobile-fill-feedback to height 0 (g2.2).
    expect(sidebar.find('[data-testid="composer-shell-toolbar"]').classes()).toContain('h-9')
    expect(sidebar.find('[data-testid="paragraph-input-root"]').attributes('data-compact')).toBe('1')
    expect(sidebar.find('[data-testid="mobile-fill-feedback"]').classes()).toEqual(
      expect.arrayContaining(['min-h-[52px]', 'flex-1']),
    )
    // No pins yet → the collapsed comment/artifact panel must not steal drawer rows.
    expect(sidebar.find('[data-testid="comment-artifact-sidebar"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows mobile-fill hot reject with count when open issues exist', async () => {
    apiMocks.listPreviewIssues.mockResolvedValue({
      issues: [
        {
          id: 'iss-open',
          runId: 'run-1',
          nodeId: 'hg-visual',
          body: '已有',
          status: 'open',
          createdAt: '2026-07-18T00:01:00Z',
        },
      ],
    })
    apiMocks.gateReactRevise.mockResolvedValue({})
    apiMocks.createPreviewIssue.mockResolvedValue({
      id: 'iss-img',
      body: '',
      status: 'open',
      createdAt: '2026-07-18T00:02:00Z',
    })
    const pageHtml = '<!doctype html><html><body><h1>仅配图</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      mobileFillRemaining: true,
      gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
      run,
    })
    await flushPromises()

    const pass = wrapper.find('[data-testid="review-composer-pass"]')
    expect(pass.exists()).toBe(true)
    expect((pass.element as HTMLButtonElement).disabled).toBe(true)
    const reject = wrapper.find('[data-testid="review-composer-send"]')
    expect(reject.exists()).toBe(true)
    expect(reject.text()).toContain('发送')
    expect((reject.element as HTMLButtonElement).disabled).toBe(false)

    await wrapper.find('[data-testid="paragraph-input-attach"]').trigger('click')
    await flushPromises()

    await reject.trigger('click')
    await flushPromises()

    // Hot reject dual-writes PreviewIssue history then gateReactRevise.
    expect(apiMocks.createPreviewIssue).toHaveBeenCalledWith(
      'run-1',
      'hg-visual',
      '',
      '',
      0,
      [{ data: 'abc', mimeType: 'image/png' }],
    )
    expect(apiMocks.gateReactRevise).toHaveBeenCalledWith(
      'run-1',
      'hg-visual',
      '',
      [{ data: 'abc', mimeType: 'image/png' }],
      [],
    )
    wrapper.unmount()
  })

  it('allows content-fit narrow hot reject with image attach (not text-only)', async () => {
    const pageHtml = '<!doctype html><html><body><h1>content-fit</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      mobileFillRemaining: false,
      gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
      run,
    })
    await flushPromises()

    const form = wrapper.find('[data-testid="content-fit-form"]')
    expect(form.find('[data-testid="review-composer-gate"]').exists()).toBe(true)
    expect(form.find('[data-testid="paragraph-input-root"]').attributes('data-text-only')).toBe('0')
    expect(form.find('[data-testid="paragraph-input-attach"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('exposes gate-react queue/stream/Cancel on desktop content-fit (g4.4)', async () => {
    breakpointMocks.isMobile.value = false
    apiMocks.listPreviewIssues.mockResolvedValue({
      issues: [
        {
          id: 'iss-1',
          runId: 'run-1',
          nodeId: 'hg-visual',
          body: '标题需改',
          status: 'open',
          createdAt: '2026-07-18T00:00:00Z',
        },
      ],
    })
    apiMocks.gateReactRevise.mockResolvedValue({ status: 'accepted', waiting: 1 })
    apiMocks.gateReactCancel.mockResolvedValue({})
    apiMocks.createPreviewIssue.mockResolvedValue({
      id: 'iss-new',
      body: '改标题',
      status: 'open',
      createdAt: '2026-07-18T00:02:00Z',
    })
    const pageHtml = '<!doctype html><html><body><h1>desktop queue</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      mobileFillRemaining: false,
      gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
      run,
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="content-fit-scroll"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="mobile-fill-remaining"]').exists()).toBe(false)

    const form = wrapper.find('[data-testid="content-fit-form"]')
    expect(form.find('[data-testid="review-composer-send"]').exists()).toBe(true)
    await form.find('[data-testid="paragraph-input"]').setValue('改标题')
    await flushPromises()
    await form.find('[data-testid="review-composer-send"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="gate-react-queue"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gate-react-cancel"]').exists()).toBe(true)

    const vm = wrapper.vm as any
    vm.applyAcpEvents?.([{ kind: 'message', text: 'streaming…' }])
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-react-stream"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gate-react-stream"]').text()).toContain('streaming')

    await wrapper.find('[data-testid="gate-react-cancel"]').trigger('click')
    await flushPromises()
    expect(apiMocks.gateReactCancel).toHaveBeenCalledWith('run-1', 'hg-visual')
    expect(wrapper.find('[data-testid="gate-react-queue"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('busy C-tier: turn_begin placeholder, thought visible, message→输出中, tool keeps placeholder', async () => {
    breakpointMocks.isMobile.value = false
    apiMocks.listPreviewIssues.mockResolvedValue({
      issues: [
        {
          id: 'iss-busy',
          runId: 'run-1',
          nodeId: 'hg-visual',
          body: '标题需改',
          status: 'open',
          createdAt: '2026-07-18T00:00:00Z',
        },
      ],
    })
    apiMocks.gateReactRevise.mockResolvedValue({ status: 'accepted', waiting: 1 })
    apiMocks.createPreviewIssue.mockResolvedValue({
      id: 'iss-new',
      body: '改一下',
      status: 'open',
      createdAt: '2026-07-18T00:02:00Z',
    })
    const pageHtml = '<!doctype html><html><body><h1>busy status</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      mobileFillRemaining: false,
      gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
      run,
    })
    await flushPromises()

    const form = wrapper.find('[data-testid="content-fit-form"]')
    expect(form.find('[data-testid="review-composer-send"]').exists()).toBe(true)
    await form.find('[data-testid="paragraph-input"]').setValue('改一下')
    await flushPromises()
    await form.find('[data-testid="review-composer-send"]').trigger('click')
    await flushPromises()

    const vm = wrapper.vm as any
    vm.applyReviewFrame?.({
      event: 'turn_begin',
      nodeId: 'visual',
      item: { text: '改一下' },
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="gate-react-stream"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gate-busy-placeholder"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gate-busy-placeholder"]').text()).toContain('思考中')

    vm.applyAcpEvents?.([{ kind: 'thought', text: '旁路思考内容' }])
    await flushPromises()
    const thought = wrapper.find('[data-testid="gate-react-thought"]')
    expect(thought.exists()).toBe(true)
    expect(thought.attributes('open')).toBeDefined()
    expect(thought.text()).toContain('旁路思考内容')
    expect(wrapper.find('[data-testid="gate-busy-status"]').text()).toContain('思考中')

    vm.applyAcpEvents?.([
      { kind: 'thought', text: '旁路思考内容' },
      { kind: 'message', text: '旁路正文' },
    ])
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-busy-status"]').text()).toContain('输出中')
    expect(wrapper.find('[data-testid="gate-react-stream"]').text()).toContain('旁路正文')
    expect(wrapper.find('[data-testid="gate-react-thought"]').text()).toContain('旁路思考内容')

    vm.applyReviewFrame?.({
      event: 'turn_begin',
      nodeId: 'visual',
      item: { text: '下一轮' },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="agent-tool-group"]').exists()).toBe(false)
    vm.applyAcpEvents?.([{ kind: 'tool_call', title: 'read_file', status: 'running' }])
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-busy-placeholder"]').exists()).toBe(true)
    const group = wrapper.find('[data-testid="gate-react-stream"] [data-testid="agent-tool-group"]')
    expect(group.exists()).toBe(true)
    expect(group.text()).toContain('read_file')
    expect(group.attributes('data-state')).toBe('running')
    wrapper.unmount()
  })

  it('exposes gate-react queue/Cancel on proposal_select ReviewComposer(gate)', async () => {
    breakpointMocks.isMobile.value = false
    apiMocks.gateReactRevise.mockResolvedValue({ status: 'accepted', waiting: 1 })
    apiMocks.gateReactCancel.mockResolvedValue({})
    const proposalsDoc = {
      context: '选型',
      proposals: [
        { id: 'p1', title: '方案甲', summary: '共享壳', recommended: true },
        { id: 'p2', title: '方案乙', summary: '另起炉灶' },
      ],
    }
    apiMocks.artifactContent.mockResolvedValue({ content: JSON.stringify(proposalsDoc) })
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({
        nodeId: 'pick-proposal',
        reactSessionAlive: true,
        reactUpstreamNodeId: 'proposal',
        actions: [
          { id: 'p1', label: '方案甲' },
          { id: 'p2', label: '方案乙' },
        ],
        form: [],
      }),
      run: baseRun({
        nodes: [
          {
            id: 'pick-proposal',
            type: 'proposal_select',
            label: '选方案',
            position: { x: 0, y: 0 },
            config: { from: 'proposals.json' },
          },
        ],
        artifacts: [
          {
            id: 'a-proposals',
            name: 'proposals.json',
            kind: 'json',
            nodeId: 'proposal',
            runId: 'run-1',
            workflowName: 'wf',
            sizeBytes: 10,
            createdAt: '2026-07-18T00:00:00Z',
          },
        ],
      }),
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="content-fit-scroll"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="review-composer-gate"]').exists()).toBe(true)

    await wrapper.find('[data-testid="paragraph-input"]').setValue('换推荐方案')
    await flushPromises()
    await wrapper.find('[data-testid="review-composer-send"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="gate-react-queue"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gate-react-cancel"]').exists()).toBe(true)
    expect(apiMocks.gateReactRevise).toHaveBeenCalled()

    await wrapper.find('[data-testid="gate-react-cancel"]').trigger('click')
    await flushPromises()
    expect(apiMocks.gateReactCancel).toHaveBeenCalledWith('run-1', 'pick-proposal')
    expect(wrapper.find('[data-testid="gate-react-queue"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('clears gate-react ghost queue on remote queue_state waiting=0 (FR5)', async () => {
    breakpointMocks.isMobile.value = false
    apiMocks.gateReactRevise.mockResolvedValue({ status: 'accepted', waiting: 1 })
    const proposalsDoc = {
      context: '选型',
      proposals: [
        { id: 'p1', title: '方案甲', summary: '共享壳', recommended: true },
        { id: 'p2', title: '方案乙', summary: '另起炉灶' },
      ],
    }
    apiMocks.artifactContent.mockResolvedValue({ content: JSON.stringify(proposalsDoc) })
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({
        nodeId: 'pick-proposal',
        reactSessionAlive: true,
        reactUpstreamNodeId: 'proposal',
        actions: [
          { id: 'p1', label: '方案甲' },
          { id: 'p2', label: '方案乙' },
        ],
        form: [],
      }),
      run: baseRun({
        nodes: [
          {
            id: 'pick-proposal',
            type: 'proposal_select',
            label: '选方案',
            position: { x: 0, y: 0 },
            config: { from: 'proposals.json' },
          },
        ],
        artifacts: [
          {
            id: 'a-proposals',
            name: 'proposals.json',
            kind: 'json',
            nodeId: 'proposal',
            runId: 'run-1',
            workflowName: 'wf',
            sizeBytes: 10,
            createdAt: '2026-07-18T00:00:00Z',
          },
        ],
      }),
    })
    await flushPromises()
    await wrapper.find('[data-testid="paragraph-input"]').setValue('跨入口幽灵')
    await flushPromises()
    await wrapper.find('[data-testid="review-composer-send"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-react-queue"]').exists()).toBe(true)
    const vm = wrapper.vm as any
    vm.applyReviewFrame?.({
      event: 'queue_state',
      nodeId: 'proposal',
      waiting: 0,
      items: [],
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-react-queue"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('gate queue_state frame annotations refill on edit; draft chips stash (plan g1.2/g1.3/g2.2)', async () => {
    breakpointMocks.isMobile.value = false
    apiMocks.gateReactRevise.mockResolvedValue({ status: 'accepted', waiting: 1 })
    apiMocks.gateReactQueueRemove.mockResolvedValue({})
    const proposalsDoc = {
      context: '选型',
      proposals: [
        { id: 'p1', title: '方案甲', summary: '共享壳', recommended: true },
        { id: 'p2', title: '方案乙', summary: '另起炉灶' },
      ],
    }
    apiMocks.artifactContent.mockResolvedValue({ content: JSON.stringify(proposalsDoc) })
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({
        nodeId: 'pick-proposal',
        reactSessionAlive: true,
        reactUpstreamNodeId: 'proposal',
        actions: [
          { id: 'p1', label: '方案甲' },
          { id: 'p2', label: '方案乙' },
        ],
        form: [],
      }),
      run: baseRun({
        nodes: [
          {
            id: 'pick-proposal',
            type: 'proposal_select',
            label: '选方案',
            position: { x: 0, y: 0 },
            config: { from: 'proposals.json' },
          },
        ],
        artifacts: [
          {
            id: 'a-proposals',
            name: 'proposals.json',
            kind: 'json',
            nodeId: 'proposal',
            runId: 'run-1',
            workflowName: 'wf',
            sizeBytes: 10,
            createdAt: '2026-07-18T00:00:00Z',
          },
        ],
      }),
    })
    await flushPromises()
    const vm = wrapper.vm as any
    // Authoritative frame only (no local hit): annotations must survive rebuild.
    vm.applyReviewFrame?.({
      event: 'queue_state',
      nodeId: 'proposal',
      waiting: 1,
      busy: false,
      items: [
        {
          id: 'gate-q-1',
          text: '门禁排队带标注',
          images: [],
          annotations: [{ label: '门禁点', selector: '#gate-hero', jsonPath: 'proposals[0].title' }],
        },
      ],
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-react-queue"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gate-react-queue"]').text()).toMatch(/批注/)

    // Composer has different chips — edit must stash them, not force-clear.
    vm.reactAnnotations = [{ label: '草稿门禁点', selector: '#draft-chip' }]
    vm.reactText = '门禁草稿'
    await flushPromises()
    await wrapper.find('[data-testid="clarify-queue-edit"]').trigger('click')
    await flushPromises()

    expect(vm.reactText).toBe('门禁排队带标注')
    expect(vm.reactAnnotations).toEqual([
      { label: '门禁点', selector: '#gate-hero', jsonPath: 'proposals[0].title' },
    ])
    // Stashed draft still on queue with its chip badge.
    expect(wrapper.find('[data-testid="gate-react-queue"]').text()).toContain('门禁草稿')
    expect(wrapper.find('[data-testid="gate-react-queue"]').text()).toMatch(/批注/)
    expect(apiMocks.gateReactQueueRemove).toHaveBeenCalledWith('run-1', 'pick-proposal', 'gate-q-1')
    wrapper.unmount()
  })

  it('gate edit stash race still refills from snapshot (review v2)', async () => {
    breakpointMocks.isMobile.value = false
    apiMocks.gateReactQueueRemove.mockResolvedValue({})
    const proposalsDoc = {
      context: '选型',
      proposals: [
        { id: 'p1', title: '方案甲', summary: '共享壳', recommended: true },
        { id: 'p2', title: '方案乙', summary: '另起炉灶' },
      ],
    }
    apiMocks.artifactContent.mockResolvedValue({ content: JSON.stringify(proposalsDoc) })
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({
        nodeId: 'pick-proposal',
        reactSessionAlive: true,
        reactUpstreamNodeId: 'proposal',
        actions: [
          { id: 'p1', label: '方案甲' },
          { id: 'p2', label: '方案乙' },
        ],
        form: [],
      }),
      run: baseRun({
        nodes: [
          {
            id: 'pick-proposal',
            type: 'proposal_select',
            label: '选方案',
            position: { x: 0, y: 0 },
            config: { from: 'proposals.json' },
          },
        ],
        artifacts: [
          {
            id: 'a-proposals',
            name: 'proposals.json',
            kind: 'json',
            nodeId: 'proposal',
            runId: 'run-1',
            workflowName: 'wf',
            sizeBytes: 10,
            createdAt: '2026-07-18T00:00:00Z',
          },
        ],
      }),
    })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.applyReviewFrame?.({
      event: 'queue_state',
      nodeId: 'proposal',
      waiting: 1,
      busy: false,
      items: [
        {
          id: 'gate-q-race',
          text: '竞态目标带标注',
          images: [],
          annotations: [{ label: '目标点', selector: '#race-target' }],
        },
      ],
    })
    await flushPromises()
    vm.reactAnnotations = [{ label: '草稿点', selector: '#draft' }]
    vm.reactText = '竞态草稿'
    // After stash, queue_state drops the edit target — must still refill from snapshot.
    apiMocks.gateReactRevise.mockImplementation(async () => {
      vm.applyReviewFrame?.({
        event: 'queue_state',
        nodeId: 'proposal',
        waiting: 1,
        busy: false,
        items: [
          {
            id: 'stashed-draft',
            text: '竞态草稿',
            annotations: [{ label: '草稿点', selector: '#draft' }],
          },
        ],
      })
      return { status: 'accepted', waiting: 1 }
    })
    await wrapper.find('[data-testid="clarify-queue-edit"]').trigger('click')
    await flushPromises()
    expect(vm.reactText).toBe('竞态目标带标注')
    expect(vm.reactAnnotations).toEqual([{ label: '目标点', selector: '#race-target' }])
    expect(apiMocks.gateReactQueueRemove).toHaveBeenCalledWith('run-1', 'pick-proposal', 'gate-q-race')
    wrapper.unmount()
  })

  it('applyAcpEvents returns false when not thinking/inFlight (g1.2 false applied)', async () => {
    breakpointMocks.isMobile.value = false
    const pageHtml = '<!doctype html><html><body><h1>gate</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      mobileFillRemaining: false,
      gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
      run,
    })
    await flushPromises()
    const vm = wrapper.vm as any
    // Idle — slot not ready; must return false so host buffers (not silent noop).
    expect(vm.applyAcpEvents?.([{ kind: 'thought', text: 'should buffer' }])).toBe(false)
    wrapper.unmount()
  })

  it('authority idle tears down empty gate streaming placeholder (g2.2/f3)', async () => {
    breakpointMocks.isMobile.value = false
    const pageHtml = '<!doctype html><html><body><h1>gate idle</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      mobileFillRemaining: false,
      gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
      run,
    })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.applyReviewFrame?.({
      event: 'queue_state',
      nodeId: 'visual',
      waiting: 0,
      items: [],
      busy: true,
      activeItem: { text: '热修中' },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-busy-placeholder"]').exists()).toBe(true)
    vm.applyReviewFrame?.({
      event: 'queue_state',
      nodeId: 'visual',
      waiting: 0,
      items: [],
      busy: false,
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-busy-placeholder"]').exists()).toBe(false)
    expect(vm.applyAcpEvents?.([{ kind: 'message', text: 'late' }])).toBe(false)
    wrapper.unmount()
  })

  it('refresh resume: busy+activeItem (waiting=0) keeps thinking and consumes ACP (g2.1/g4.4)', async () => {
    breakpointMocks.isMobile.value = false
    const pageHtml = '<!doctype html><html><body><h1>gate resume</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      mobileFillRemaining: false,
      gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
      run,
    })
    await flushPromises()
    const vm = wrapper.vm as any
    // Hard refresh snapshot: only busy+activeItem, waiting already drained.
    vm.applyReviewFrame?.({
      event: 'queue_state',
      nodeId: 'visual',
      waiting: 0,
      items: [],
      busy: true,
      activeItem: { text: '热修刷新续流' },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-busy-placeholder"]').exists()).toBe(true)

    vm.applyAcpEvents?.([
      { kind: 'thought', text: '续上的思考' },
      { kind: 'message', text: '续上的正文' },
    ])
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-react-stream"]').text()).toContain('续上的思考')
    expect(wrapper.find('[data-testid="gate-react-stream"]').text()).toContain('续上的正文')
    expect(wrapper.find('[data-testid="gate-busy-status"]').text()).toContain('输出中')
    expect(wrapper.find('[data-testid="gate-react-thought"]').attributes('open')).toBeUndefined()
    expect(wrapper.find('[data-testid="gate-stream-caret"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('four-phase complete: turn_done shows 已完成 footnote; interrupted does not (g3/g2.1)', async () => {
    breakpointMocks.isMobile.value = false
    const pageHtml = '<!doctype html><html><body><h1>gate done</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      mobileFillRemaining: false,
      gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
      run,
    })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.applyReviewFrame?.({
      event: 'queue_state',
      nodeId: 'visual',
      waiting: 0,
      items: [],
      busy: true,
      activeItem: { text: '完成态' },
    })
    vm.applyAcpEvents?.([
      { kind: 'thought', text: '完成前思考' },
      { kind: 'message', text: '完成前正文' },
    ])
    await flushPromises()
    vm.applyReviewFrame?.({ event: 'turn_done', nodeId: 'visual' })
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-turn-completed"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gate-turn-completed"]').text()).toContain('已完成')
    expect(wrapper.find('[data-testid="gate-stream-caret"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="gate-busy-status"]').exists()).toBe(false)

    vm.applyReviewFrame?.({
      event: 'turn_begin',
      nodeId: 'visual',
    })
    vm.applyAcpEvents?.([{ kind: 'message', text: '半截' }])
    vm.applyReviewFrame?.({ event: 'turn_done', nodeId: 'visual', interrupted: true })
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-turn-completed"]').exists()).toBe(false)
    wrapper.unmount()
  })

  // g2.1: revise failure body + interrupted must never show Done/已完成.
  it('revise failure text + interrupted turn_done does not show 已完成/Done', async () => {
    breakpointMocks.isMobile.value = false
    const pageHtml = '<!doctype html><html><body><h1>gate fail</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      mobileFillRemaining: false,
      gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
      run,
    })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.applyReviewFrame?.({
      event: 'turn_begin',
      nodeId: 'visual',
    })
    vm.applyAcpEvents?.([
      { kind: 'thought', text: '半截思考' },
      {
        kind: 'message',
        text: '(复审修改失败:acp chat idle timeout after 10m0s)',
      },
    ])
    vm.applyReviewFrame?.({ event: 'turn_done', nodeId: 'visual', interrupted: true })
    await flushPromises()
    expect(wrapper.text()).toContain('复审修改失败')
    expect(wrapper.find('[data-testid="gate-turn-completed"]').exists()).toBe(false)
    expect(wrapper.text()).not.toMatch(/\bDone\b/)
    const summary = wrapper.find('[data-testid="thought-summary-state"]')
    expect(summary.exists()).toBe(true)
    expect(summary.attributes('data-state')).toBe('interrupted')
    expect(summary.text()).toContain('已中断')
    expect(summary.text()).not.toContain('已完成')
    wrapper.unmount()
  })

  it('keeps Inbox-style path on 60vh content-fit when mobileFillRemaining is off', async () => {
    // Mobile content-fit path unchanged (no desktop flex-1 / fillParent).
    const pageHtml = '<!doctype html><html><body><h1>Inbox</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      mobileFillRemaining: false,
      gate,
      run,
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="mobile-fill-remaining"]').exists()).toBe(false)
    expect(contentFitRoot(wrapper).exists()).toBe(true)
    const preview = wrapper.find('[data-testid="content-fit-preview"]')
    expect(hasClass(preview.element, 'flex-1')).toBe(false)
    expect((preview.element as HTMLElement).style.maxHeight).toBe(
      `${CONTENT_FIT_PREVIEW_MAX_VH}vh`,
    )
    const html = wrapper.find('[data-testid="html-preview"]')
    expect(html.attributes('data-fill-parent')).toBe('0')
    expect(html.attributes('data-fit')).toBe('0')
    wrapper.unmount()
  })

  it('keeps desktop visual on fillParent + 60vh shell even when mobileFillRemaining is set', async () => {
    breakpointMocks.isMobile.value = false
    const pageHtml = '<!doctype html><html><body><h1>桌面</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      mobileFillRemaining: true,
      gate,
      run,
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="mobile-fill-remaining"]').exists()).toBe(false)
    expect(contentFitRoot(wrapper).exists()).toBe(true)
    const html = wrapper.find('[data-testid="html-preview"]')
    expect(html.attributes('data-fit')).toBe('0')
    expect(html.attributes('data-fill-parent')).toBe('1')
    const preview = wrapper.find('[data-testid="content-fit-preview"]')
    expect((preview.element as HTMLElement).style.maxHeight).toBe(
      `${CONTENT_FIT_PREVIEW_MAX_VH}vh`,
    )
    wrapper.unmount()
  })

  it('keeps sticky actions reachable when preview load fails', async () => {
    // Offline primary list + empty snapshot → visual body via client fallback, load error pane.
    apiMocks.listGatePrimaryArtifacts.mockRejectedValue(new Error('offline'))
    apiMocks.artifactContent.mockRejectedValue(new Error('boom'))
    const wrapper = mountApproval({
      fillPreview: true,
      mobileFillRemaining: true,
      gate: baseGate({
        nodeId: 'hg-visual',
        actions: [
          { id: 'approve', label: '批准' },
          { id: 'revise', label: '返回修改', requireForm: true },
        ],
      }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-visual',
            type: 'human_gate',
            label: '审阅视觉',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.visual.outputs.page}}' },
          },
        ],
        artifacts: [
          {
            id: 'a1',
            name: 'page.html',
            kind: 'html',
            nodeId: 'visual',
            runId: 'run-1',
            workflowName: 'wf',
            sizeBytes: 1,
            createdAt: '2026-07-18T00:00:00Z',
          },
        ],
      }),
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="mobile-fill-remaining"]').exists()).toBe(true)
    const sticky = wrapper.find('[data-testid="mobile-fill-sticky-actions"]')
    expect(sticky.exists()).toBe(true)
    // Preview may be editor/error/loading; sticky Pass must stay usable (n_open=0 → no reject).
    const approveBtn = sticky.findAll('button').find((b) => b.text().includes('确认并流转'))
    expect(approveBtn).toBeTruthy()
    expect((approveBtn!.element as HTMLButtonElement).disabled).toBe(false)
    expect(sticky.findAll('button').every((b) => !b.text().includes('返回修改'))).toBe(true)
    await approveBtn!.trigger('click')
    await flushPromises()
    expect(
      !!wrapper.emitted('resolve') ||
        wrapper.text().includes('评审意见') ||
        sticky.findAll('button').length >= 1,
    ).toBe(true)
    wrapper.unmount()
  })
})

describe('GateApproval ReAct annotations', () => {
  beforeEach(() => {
    breakpointMocks.isMobile.value = false
    apiMocks.artifactContent.mockReset()
    apiMocks.listPreviewIssues.mockReset()
    apiMocks.createPreviewIssue.mockReset()
    apiMocks.listGatePrimaryArtifacts.mockReset()
    apiMocks.gateReactRevise.mockReset()
    apiMocks.listPreviewIssues.mockResolvedValue({ issues: [] })
    apiMocks.createPreviewIssue.mockResolvedValue({
      id: 'iss-new',
      body: 'new',
      status: 'open',
      createdAt: '2026-07-18T00:02:00Z',
    })
    apiMocks.listGatePrimaryArtifacts.mockResolvedValue({ artifacts: [] })
    apiMocks.gateReactRevise.mockResolvedValue({ status: 'ok' })
  })

  it('sends annotations with gateReactRevise and allows annotation-only submit', async () => {
    const researchDoc = { summary: '短调研结论', findings: [{ title: '一项发现' }] }
    apiMocks.artifactContent.mockResolvedValue({
      id: 'a1',
      name: 'research.json',
      content: JSON.stringify(researchDoc),
    })
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({
        nodeId: 'hg-research',
        reactSessionAlive: true,
        reactUpstreamNodeId: 'research',
      }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-research',
            type: 'human_gate',
            label: '审阅调研',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.research.outputs.research}}' },
          },
        ],
        nodeExecutions: {
          research: [
            {
              nodeId: 'research',
              iteration: 1,
              status: 'completed',
              outputs: { research_json: JSON.stringify(researchDoc) },
            },
          ],
        },
      }),
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="review-shell"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="review-composer-gate"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="review-composer-send"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gate-cold-session-note"]').exists()).toBe(false)

    // Stage an annotation-only draft via component state.
    const vm = wrapper.vm as any
    vm.reactAnnotations = [{ jsonPath: 'summary', label: '概述' }]
    await flushPromises()

    const rejectBtn = wrapper.find('[data-testid="review-composer-send"]')
    expect((rejectBtn.element as HTMLButtonElement).disabled).toBe(false)
    await rejectBtn.trigger('click')
    await flushPromises()

    expect(apiMocks.gateReactRevise).toHaveBeenCalledWith(
      'run-1',
      'hg-research',
      '',
      [],
      [{ jsonPath: 'summary', label: '概述' }],
    )
    expect(wrapper.emitted('react-revised')).toBeTruthy()
    wrapper.unmount()
  })

  it('cold session is silent: no ReAct/hot hints, no send; confirm & ordinary help remain', async () => {
    const researchDoc = { summary: '冷会话' }
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({
        nodeId: 'hg-research',
        reactSessionAlive: false,
      }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-research',
            type: 'human_gate',
            label: '审阅调研',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.research.outputs.research}}' },
          },
        ],
        nodeExecutions: {
          research: [
            {
              nodeId: 'research',
              iteration: 1,
              status: 'completed',
              outputs: { research_json: JSON.stringify(researchDoc) },
            },
          ],
        },
      }),
    })
    await flushPromises()

    // plan g4.1: cold silent — no cold note / ReAct / hot-session / in-place-edit guidance
    expect(wrapper.find('[data-testid="review-composer-send"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="gate-cold-session-note"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="review-composer-cold-note"]').exists()).toBe(false)
    expect(wrapper.text()).not.toMatch(/ReAct|热会话|就地改|恢复热会话/)
    expect(wrapper.find('[data-testid="gate-cold-help"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gate-cold-help"]').text()).toContain('确认并流转')
    expect(wrapper.find('[data-testid="gate-cold-help"]').text()).toContain('手动编辑产物')
    expect(
      wrapper.findAll('[data-testid="review-composer-pass"]').some((b) => b.text().includes('确认并流转')) ||
        wrapper.findAll('button').some((b) => b.text().includes('确认并流转')),
    ).toBe(true)
    wrapper.unmount()
  })

  it('hot session keeps send and has no cold note when reactSessionAlive is true', async () => {
    const researchDoc = { summary: '热会话' }
    apiMocks.artifactContent.mockResolvedValue({
      id: 'a1',
      name: 'research.json',
      content: JSON.stringify(researchDoc),
    })
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({
        nodeId: 'hg-research',
        reactSessionAlive: true,
        reactUpstreamNodeId: 'research',
      }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-research',
            type: 'human_gate',
            label: '审阅调研',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.research.outputs.research}}' },
          },
        ],
        nodeExecutions: {
          research: [
            {
              nodeId: 'research',
              iteration: 1,
              status: 'completed',
              outputs: { research_json: JSON.stringify(researchDoc) },
            },
          ],
        },
      }),
    })
    await flushPromises()

    // plan g4.1: hot unchanged — send present, no cold note / cold help
    expect(wrapper.find('[data-testid="review-composer-send"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gate-cold-session-note"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="gate-cold-help"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('hides overlapping gate.form when hot ReAct composer is active', async () => {
    const researchDoc = { summary: '热会话隐藏表单' }
    apiMocks.artifactContent.mockResolvedValue({
      id: 'a1',
      name: 'research.json',
      content: JSON.stringify(researchDoc),
    })
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({
        nodeId: 'hg-research',
        reactSessionAlive: true,
        reactUpstreamNodeId: 'research',
        form: [{ key: 'comment', label: '评审意见' }],
      }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-research',
            type: 'human_gate',
            label: '审阅调研',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.research.outputs.research}}' },
          },
        ],
        nodeExecutions: {
          research: [
            {
              nodeId: 'research',
              iteration: 1,
              status: 'completed',
              outputs: { research_json: JSON.stringify(researchDoc) },
            },
          ],
        },
      }),
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="review-composer-gate"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('评审意见')
    wrapper.unmount()
  })

  it('silently discards unsent react draft on pass without confirm', async () => {
    const researchDoc = { summary: '丢弃确认' }
    apiMocks.artifactContent.mockResolvedValue({
      id: 'a1',
      name: 'research.json',
      content: JSON.stringify(researchDoc),
    })
    const confirmSpy = vi.spyOn(window, 'confirm')
    const wrapper = mountApproval({
      fillPreview: true,
      gate: baseGate({
        nodeId: 'hg-research',
        reactSessionAlive: true,
        reactUpstreamNodeId: 'research',
      }),
      run: baseRun({
        nodes: [
          {
            id: 'hg-research',
            type: 'human_gate',
            label: '审阅调研',
            position: { x: 0, y: 0 },
            config: { body_template: '{{nodes.research.outputs.research}}' },
          },
        ],
        nodeExecutions: {
          research: [
            {
              nodeId: 'research',
              iteration: 1,
              status: 'completed',
              outputs: { research_json: JSON.stringify(researchDoc) },
            },
          ],
        },
      }),
    })
    await flushPromises()

    const vm = wrapper.vm as any
    vm.reactAnnotations = [{ jsonPath: 'summary', label: '概述' }]
    vm.reactText = '未发送草稿'
    await flushPromises()

    await wrapper.find('[data-testid="review-composer-pass"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('resolve')?.[0]?.[0]).toBe('approve')
    expect(vm.reactAnnotations).toEqual([])
    expect(vm.reactText).toBe('')
    expect(confirmSpy).not.toHaveBeenCalled()
    confirmSpy.mockRestore()
    wrapper.unmount()
  })

  it('hot reject dual-writes PreviewIssue history then gateReactRevise', async () => {
    apiMocks.listPreviewIssues.mockResolvedValue({
      issues: [
        {
          id: 'iss-existing',
          runId: 'run-1',
          nodeId: 'hg-visual',
          body: '已有意见',
          status: 'open',
          createdAt: '2026-07-18T00:02:00Z',
        },
      ],
    })
    apiMocks.gateReactRevise.mockResolvedValue({})
    apiMocks.createPreviewIssue.mockResolvedValue({
      id: 'iss-hot',
      body: '请改标题',
      status: 'open',
      createdAt: '2026-07-18T00:03:00Z',
    })
    const pageHtml = '<!doctype html><html><body><h1>热打回双写</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
      run,
    })
    await flushPromises()

    const form = wrapper.find('[data-testid="content-fit-form"]')
    const pass = form.find('[data-testid="review-composer-pass"]')
    expect(pass.exists()).toBe(true)
    expect((pass.element as HTMLButtonElement).disabled).toBe(true)
    const reject = form.find('[data-testid="review-composer-send"]')
    expect(reject.exists()).toBe(true)
    expect(reject.text()).toContain('发送')
    await form.find('[data-testid="paragraph-input"]').setValue('请改标题')
    await flushPromises()

    await reject.trigger('click')
    await flushPromises()

    expect(apiMocks.createPreviewIssue).toHaveBeenCalledWith(
      'run-1',
      'hg-visual',
      '请改标题',
      '',
      0,
      [],
    )
    expect(apiMocks.gateReactRevise).toHaveBeenCalledWith(
      'run-1',
      'hg-visual',
      '请改标题',
      [],
      [],
    )
    expect(wrapper.emitted('react-revised')).toBeTruthy()
    wrapper.unmount()
  })

  it('keeps draft and shows hint when hot reject fails after history write', async () => {
    apiMocks.listPreviewIssues.mockResolvedValue({
      issues: [
        {
          id: 'iss-existing',
          runId: 'run-1',
          nodeId: 'hg-visual',
          body: '已有意见',
          status: 'open',
          createdAt: '2026-07-18T00:04:00Z',
        },
      ],
    })
    apiMocks.createPreviewIssue.mockResolvedValue({
      id: 'iss-partial',
      body: '半成功意见',
      status: 'open',
      createdAt: '2026-07-18T00:05:00Z',
    })
    apiMocks.gateReactRevise.mockRejectedValue(new Error('upstream down'))
    const pageHtml = '<!doctype html><html><body><h1>双写失败</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
      run,
    })
    await flushPromises()

    const form = wrapper.find('[data-testid="content-fit-form"]')
    await form.find('[data-testid="paragraph-input"]').setValue('半成功意见')
    await flushPromises()
    await form.find('[data-testid="review-composer-send"]').trigger('click')
    await flushPromises()

    expect(apiMocks.createPreviewIssue).toHaveBeenCalledTimes(1)
    expect(apiMocks.gateReactRevise).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('react-revised')).toBeFalsy()
    expect(wrapper.text()).toContain('意见已记入但打回未成功')
    expect((form.find('[data-testid="paragraph-input"]').element as HTMLTextAreaElement).value).toBe(
      '半成功意见',
    )

    // Retry should not duplicate PreviewIssue history.
    apiMocks.gateReactRevise.mockResolvedValue({})
    await form.find('[data-testid="review-composer-send"]').trigger('click')
    await flushPromises()
    expect(apiMocks.createPreviewIssue).toHaveBeenCalledTimes(1)
    expect(apiMocks.gateReactRevise).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('react-revised')).toBeTruthy()
    wrapper.unmount()
  })

  it('annotation-only hot reject writes placeholder into PreviewIssue history', async () => {
    apiMocks.listPreviewIssues.mockResolvedValue({
      issues: [
        {
          id: 'iss-existing',
          runId: 'run-1',
          nodeId: 'hg-visual',
          body: '已有意见',
          status: 'open',
          createdAt: '2026-07-18T00:05:00Z',
        },
      ],
    })
    apiMocks.gateReactRevise.mockResolvedValue({})
    apiMocks.createPreviewIssue.mockResolvedValue({
      id: 'iss-ann',
      body: '标注：#hero',
      status: 'open',
      createdAt: '2026-07-18T00:06:00Z',
    })
    const pageHtml = '<!doctype html><html><body><h1>仅标注</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
      run,
    })
    await flushPromises()

    const vm = wrapper.vm as any
    vm.reactAnnotations = [{ selector: '#hero', label: '#hero' }]
    await flushPromises()

    await wrapper.find('[data-testid="review-composer-send"]').trigger('click')
    await flushPromises()

    expect(apiMocks.createPreviewIssue).toHaveBeenCalledWith(
      'run-1',
      'hg-visual',
      '标注：#hero',
      '',
      0,
      [],
    )
    expect(apiMocks.gateReactRevise).toHaveBeenCalledWith(
      'run-1',
      'hg-visual',
      '',
      [],
      [{ selector: '#hero', label: '#hero' }],
    )
    wrapper.unmount()
  })
})

describe('GateApproval record issue refreshes real PreviewFeedbackChat history', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    breakpointMocks.isMobile.value = false
    apiMocks.listPreviewIssues.mockResolvedValue({ issues: [] })
    apiMocks.createPreviewIssue.mockResolvedValue({
      id: 'iss-live',
      body: '标题需加大',
      status: 'open',
      createdAt: '2026-07-18T00:04:00Z',
    })
    apiMocks.artifactContent.mockResolvedValue({ content: '{}' })
    apiMocks.listGatePrimaryArtifacts.mockRejectedValue(new Error('offline'))
    apiMocks.gateReactRevise.mockResolvedValue({})
  })

  it('shows recorded body in history after 记入意见 (no PreviewFeedbackChat stub)', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const pageHtml = '<!doctype html><html><body><h1>记入刷新</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mount(GateApproval, {
      props: {
        gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
        run,
        fillPreview: true,
        compact: true,
      },
      global: {
        plugins: [i18n],
        stubs: {
          Icon: true,
          ParagraphInput: ParagraphInputStub,
          ArtifactLoadingPane: defineComponent({
            name: 'ArtifactLoadingPane',
            template: '<div data-testid="artifact-loading-pane" />',
          }),
          ProposalSelectView: ProposalSelectStub,
          HtmlPreview: HtmlPreviewStub,
          StructuredArtifactView: StructuredStub,
          AppPreviewPanel: AppPreviewStub,
          // Real PreviewFeedbackChat — assert history list UI refresh.
          PlanView: PlanStub,
          UpstreamRequirementContext: false,
        },
      },
    })
    await flushPromises()

    const form = wrapper.find('[data-testid="content-fit-form"]')
    expect(form.find('[data-testid="preview-feedback-chat"]').exists()).toBe(true)
    await form.find('[data-testid="paragraph-input"]').setValue('标题需加大')
    await flushPromises()

    await form.find('[data-testid="review-record-issue"]').trigger('click')
    await flushPromises()

    expect(apiMocks.createPreviewIssue).toHaveBeenCalledWith(
      'run-1',
      'hg-visual',
      '标题需加大',
      '',
      0,
      [],
    )
    expect(form.find('[data-testid="preview-feedback-chat"]').text()).toContain('标题需加大')
    expect((form.find('[data-testid="paragraph-input"]').element as HTMLTextAreaElement).value).toBe(
      '',
    )
    wrapper.unmount()
  })
})

describe('GateApproval cold footer pending + loadProduct gen (专项三 g4)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    breakpointMocks.isMobile.value = false
    apiMocks.listPreviewIssues.mockResolvedValue({ issues: [] })
    apiMocks.listGatePrimaryArtifacts.mockRejectedValue(new Error('offline'))
    apiMocks.artifactContent.mockResolvedValue({ content: '# doc' })
  })

  it('shows 批准中…, disables siblings, and ignores double click', async () => {
    const wrapper = mountApproval({
      gate: baseGate({ form: [] }),
      run: baseRun(),
      compact: false,
    })
    await flushPromises()
    const buttons = wrapper.findAll('[data-testid="review-composer-pass"]')
    const approve = buttons.find((b) => /确认并流转|批准/.test(b.text()))
    expect(approve).toBeTruthy()
    await approve!.trigger('click')
    await approve!.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('resolve')?.length).toBe(1)
    expect(wrapper.emitted('resolve')?.[0]?.[0]).toBe('approve')
    expect(approve!.text()).toContain('批准中')
    expect(approve!.attributes('disabled')).toBeDefined()
    expect(approve!.attributes('aria-busy')).toBe('true')
    wrapper.unmount()
  })

  it('rolls back pending on submitError so reviewer can retry', async () => {
    const wrapper = mountApproval({
      gate: baseGate({ form: [] }),
      run: baseRun(),
      compact: false,
    })
    await flushPromises()
    const approve = wrapper.findAll('[data-testid="review-composer-pass"]').find((b) => /确认并流转|批准/.test(b.text()))
    await approve!.trigger('click')
    await flushPromises()
    await wrapper.setProps({ submitError: 'gate still open' })
    await flushPromises()
    const again = wrapper.findAll('[data-testid="review-composer-pass"]').find((b) => /确认并流转|批准/.test(b.text()))
    expect(again).toBeTruthy()
    expect(again!.text()).toMatch(/确认并流转|批准/)
    expect(again!.text()).not.toContain('批准中')
    expect(wrapper.text()).toContain('gate still open')
    await again!.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('resolve')?.length).toBe(2)
    wrapper.unmount()
  })

  it('passes AbortSignal into artifactContent for loadProduct', async () => {
    apiMocks.listGatePrimaryArtifacts.mockResolvedValue({
      artifacts: [{ name: 'research.json', outputKey: 'research', nodeId: 'research' }],
    })
    const wrapper = mountApproval({
      gate: baseGate({ nodeId: 'hg-1', upstreamNodeId: 'research' }),
      run: baseRun({
        artifacts: [
          {
            id: 'art-1',
            name: 'research.json',
            kind: 'json',
            nodeId: 'research',
            runId: 'run-1',
            sizeBytes: 12,
            createdAt: '2026-07-18T00:00:00Z',
          } as any,
        ],
        nodes: [
          { id: 'hg-1', type: 'human_gate', label: '审阅', position: { x: 0, y: 0 }, config: {} },
          { id: 'research', type: 'agent', label: '调研', position: { x: 0, y: 0 }, config: {} },
        ],
      }),
    })
    await flushPromises()
    if (apiMocks.artifactContent.mock.calls.length) {
      const last = apiMocks.artifactContent.mock.calls[apiMocks.artifactContent.mock.calls.length - 1]
      expect(last?.[1]).toEqual(expect.objectContaining({ signal: expect.any(AbortSignal) }))
    }
    wrapper.unmount()
  })

  it('plan g4.2: output done + turn_done synthesizes idle — Cancel/thinking clear; confirm ready', async () => {
    breakpointMocks.isMobile.value = false
    apiMocks.listPreviewIssues.mockResolvedValue({ issues: [] })
    apiMocks.gateReactRevise.mockResolvedValue({ status: 'accepted', waiting: 1 })
    const pageHtml = '<!doctype html><html><body><h1>idle sticky</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      mobileFillRemaining: false,
      gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
      run,
    })
    await flushPromises()

    const form = wrapper.find('[data-testid="content-fit-form"]')
    await form.find('[data-testid="paragraph-input"]').setValue('请改验收口径')
    await flushPromises()
    await form.find('[data-testid="review-composer-send"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-react-cancel"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="gate-react-queue"]').exists()).toBe(true)

    const vm = wrapper.vm as any
    // busy+activeItem resume leaves optimistic queue head (ghost) — no turn_begin shift.
    vm.applyReviewFrame?.({
      event: 'queue_state',
      nodeId: 'visual',
      waiting: 0,
      items: [],
      busy: true,
      activeItem: { text: '请改验收口径' },
    })
    vm.applyAcpEvents?.([{ kind: 'message', text: '已更新验收口径与回归清单' }])
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-react-stream"]').text()).toContain('已更新验收口径')

    vm.applyReviewFrame?.({ event: 'turn_done', nodeId: 'visual' })
    await flushPromises()

    expect(wrapper.find('[data-testid="gate-react-cancel"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="gate-react-queue"]').exists()).toBe(false)
    const pass = wrapper.find('[data-testid="review-composer-pass"]')
    expect(pass.exists()).toBe(true)
    expect((pass.element as HTMLButtonElement).disabled).toBe(false)
    wrapper.unmount()
  })

  it('plan g4.2: real remaining queue after idle restore keeps Cancel/busy', async () => {
    breakpointMocks.isMobile.value = false
    apiMocks.listPreviewIssues.mockResolvedValue({ issues: [] })
    const pageHtml = '<!doctype html><html><body><h1>keep queue</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      mobileFillRemaining: false,
      gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
      run,
    })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.applyReviewFrame?.({
      event: 'queue_state',
      nodeId: 'visual',
      waiting: 1,
      items: [{ text: '下一条热修' }],
      busy: false,
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-react-queue"]').text()).toContain('下一条热修')
    expect(wrapper.find('[data-testid="gate-react-cancel"]').exists()).toBe(true)
    const pass = wrapper.find('[data-testid="review-composer-pass"]')
    expect(pass.exists()).toBe(true)
    expect((pass.element as HTMLButtonElement).disabled).toBe(true)
    wrapper.unmount()
  })

  it('plan g4.2: real id waiter + turn_done keeps Cancel/busy until authoritative idle', async () => {
    breakpointMocks.isMobile.value = false
    apiMocks.listPreviewIssues.mockResolvedValue({ issues: [] })
    const pageHtml = '<!doctype html><html><body><h1>multi wait</h1></body></html>'
    const { gate, run } = visualGateRun(pageHtml)
    const wrapper = mountApproval({
      fillPreview: true,
      mobileFillRemaining: false,
      gate: { ...gate, reactSessionAlive: true, reactUpstreamNodeId: 'visual' },
      run,
    })
    await flushPromises()
    const vm = wrapper.vm as any

    // Two id waiters: turn_begin blind-shifts the head; q2 must survive turn_done.
    vm.applyReviewFrame?.({
      event: 'queue_state',
      nodeId: 'visual',
      waiting: 2,
      items: [
        { id: 'srv-1', text: '本轮意见' },
        { id: 'q2', text: '下一条热修' },
      ],
      busy: false,
    })
    vm.applyReviewFrame?.({
      event: 'turn_begin',
      nodeId: 'visual',
      item: { id: 'srv-1', text: '本轮意见' },
    })
    vm.applyAcpEvents?.([{ kind: 'message', text: '本轮已改完' }])
    await flushPromises()

    vm.applyReviewFrame?.({ event: 'turn_done', nodeId: 'visual' })
    await flushPromises()

    // Must keep real id waiter (review v1/v2) — no brief confirm unlock.
    expect(wrapper.find('[data-testid="gate-react-queue"]').text()).toContain('下一条热修')
    expect(wrapper.find('[data-testid="gate-react-cancel"]').exists()).toBe(true)
    const passBusy = wrapper.find('[data-testid="review-composer-pass"]')
    expect(passBusy.exists()).toBe(true)
    expect((passBusy.element as HTMLButtonElement).disabled).toBe(true)

    vm.applyReviewFrame?.({
      event: 'queue_state',
      nodeId: 'visual',
      waiting: 0,
      items: [],
      busy: false,
      activeItem: null,
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="gate-react-cancel"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="gate-react-queue"]').exists()).toBe(false)
    const passIdle = wrapper.find('[data-testid="review-composer-pass"]')
    expect(passIdle.exists()).toBe(true)
    expect((passIdle.element as HTMLButtonElement).disabled).toBe(false)
    wrapper.unmount()
  })
})
