// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick, reactive, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { ClarifyImage, ReactAnnotation } from '@/lib/shared/types'
import { clearDialogues } from '@/lib/chat/dialogueStore'
import { resetArtifactEditorsForTest, useArtifactEditor } from '@/lib/run/useArtifactEditor'
import { useClarifyChat } from './useClarifyChat'

const apiMocks = vi.hoisted(() => ({
  artifactContent: vi.fn(),
  saveRunArtifact: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api, ...apiMocks } }
})

function withChat(over: Record<string, unknown> = {}) {
  let chat!: ReturnType<typeof useClarifyChat>
  const emit = vi.fn()
  const props = reactive({
    runId: 'run-g',
    nodeId: 'node-1',
    iteration: 0,
    turns: [],
    done: false,
    active: true,
    reviewMode: false,
    annotateEnabled: false,
    confirmError: null,
    ...over,
  })
  const models = {
    draft: ref(''),
    attachments: ref<ClarifyImage[]>([]),
    annotations: ref<ReactAnnotation[]>([]),
  }
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  const Comp = defineComponent({
    setup() {
      chat = useClarifyChat(props as never, emit as never, models)
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.mount(document.createElement('div'))
  return { chat, app, emit, props, models }
}

const sends = (emit: ReturnType<typeof vi.fn>) => emit.mock.calls.filter((c) => c[0] === 'send')

async function dirtyPlan() {
  const ed = useArtifactEditor('run-g')
  await ed.begin({ id: 'p', name: 'plan.json', kind: 'json', revision: 3 })
  ed.update('plan.json', '{"title":"edited"}')
  return ed
}

describe('useClarifyChat unsaved artifact guard', () => {
  beforeEach(() => {
    resetArtifactEditorsForTest()
    vi.clearAllMocks()
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
    apiMocks.artifactContent.mockResolvedValue({ content: '{"title":"p"}', etag: 'e3', revision: 3 })
  })
  afterEach(() => {
    clearDialogues()
    vi.unstubAllGlobals()
  })

  it('sends as before without unsaved edits', () => {
    const { chat, emit, models, app } = withChat()
    models.draft.value = 'hello'
    chat.send()
    expect(chat.sendGuardOpen.value).toBe(false)
    expect(sends(emit)).toHaveLength(1)
    app.unmount()
  })

  it('asks first, then saves and sends', async () => {
    await dirtyPlan()
    apiMocks.saveRunArtifact.mockResolvedValue({ name: 'plan.json', revision: 4, changed: true, content: '' })
    const { chat, emit, models, app } = withChat()
    models.draft.value = 'hello'
    chat.send()
    expect(chat.sendGuardOpen.value).toBe(true)
    expect(chat.sendGuardNames.value).toEqual(['plan.json'])
    expect(sends(emit)).toHaveLength(0)
    await chat.sendGuardSaveAndSend()
    expect(apiMocks.saveRunArtifact).toHaveBeenCalledWith('run-g', 'plan.json', '{"title":"edited"}', 'e3')
    expect(chat.sendGuardOpen.value).toBe(false)
    expect(sends(emit)).toHaveLength(1)
    app.unmount()
  })

  it('does not send when a save fails', async () => {
    await dirtyPlan()
    apiMocks.saveRunArtifact.mockRejectedValue(Object.assign(new Error('bad'), { status: 400 }))
    const { chat, emit, models, app } = withChat()
    models.draft.value = 'hello'
    chat.send()
    await chat.sendGuardSaveAndSend()
    expect(sends(emit)).toHaveLength(0)
    expect(chat.sendNotice.value).toContain('plan.json')
    expect(models.draft.value).toBe('hello')
    app.unmount()
  })

  it('send anyway keeps the draft; cancel sends nothing', async () => {
    const ed = await dirtyPlan()
    const { chat, emit, models, app } = withChat()
    models.draft.value = 'hello'
    chat.send()
    chat.sendGuardCancel()
    expect(chat.sendGuardOpen.value).toBe(false)
    expect(sends(emit)).toHaveLength(0)
    chat.send()
    chat.sendGuardSendAnyway()
    expect(sends(emit)).toHaveLength(1)
    expect(ed.isDirty('plan.json')).toBe(true)
    app.unmount()
  })

  const questionTurns = [{
    role: 'agent',
    text: 'pick',
    questions: [{ id: 'q1', prompt: '范围？', options: [{ id: 'a', label: '只做 A', recommended: true }, { id: 'b', label: 'B' }] }],
  }]
  const formTurns = [{
    role: 'agent',
    text: 'fill',
    forms: [{ id: 'f1', title: '信息', fields: [{ name: 'repo', label: '仓库' }] }],
  }]

  it('question card answers wait for the guard and send after saving', async () => {
    await dirtyPlan()
    apiMocks.saveRunArtifact.mockResolvedValue({ name: 'plan.json', revision: 4, changed: true, content: '' })
    const { chat, emit, app } = withChat({ turns: questionTurns })
    await nextTick()
    chat.submitChoices()
    expect(chat.sendGuardOpen.value).toBe(true)
    expect(sends(emit)).toHaveLength(0)
    await chat.sendGuardSaveAndSend()
    expect(sends(emit)).toHaveLength(1)
    expect(String(sends(emit)[0][1])).toContain('只做 A')
    app.unmount()
  })

  it('submit recommended is guarded; cancel sends nothing and keeps the picks', async () => {
    await dirtyPlan()
    const { chat, emit, app } = withChat({ turns: questionTurns })
    await nextTick()
    chat.submitRecommended()
    expect(chat.sendGuardOpen.value).toBe(true)
    chat.sendGuardCancel()
    expect(sends(emit)).toHaveLength(0)
    expect(chat.isSelected('q1', 'a')).toBe(true)
    chat.sendGuardSendAnyway()
    expect(sends(emit)).toHaveLength(0)
    app.unmount()
  })

  it('form card submit is guarded; send anyway sends the answers', async () => {
    await dirtyPlan()
    const { chat, emit, app } = withChat({ turns: formTurns })
    await nextTick()
    chat.formValues.value = { [chat.formFieldKey(0, 'repo')]: 'grasp' }
    chat.submitForms()
    expect(chat.sendGuardOpen.value).toBe(true)
    expect(sends(emit)).toHaveLength(0)
    chat.sendGuardSendAnyway()
    expect(sends(emit)).toHaveLength(1)
    expect(String(sends(emit)[0][1])).toContain('grasp')
    app.unmount()
  })

  it('a failed save-and-send asks the stage to show the failing draft', async () => {
    const ed = await dirtyPlan()
    apiMocks.saveRunArtifact.mockRejectedValue(Object.assign(new Error('bad'), { status: 400 }))
    const { chat, emit, app } = withChat({ turns: questionTurns })
    await nextTick()
    chat.submitChoices()
    await chat.sendGuardSaveAndSend()
    expect(sends(emit)).toHaveLength(0)
    expect(ed.focusRequest.value?.name).toBe('plan.json')
    app.unmount()
  })

  it('confirm flow warns about and discards unsaved edits', async () => {
    const ed = await dirtyPlan()
    const { chat, emit, app } = withChat()
    chat.finishEarly()
    expect(chat.confirmFlowDiscard.value).toBe('edits')
    chat.acceptConfirmFlow()
    expect(emit).toHaveBeenCalledWith('finish')
    expect(ed.hasDirty.value).toBe(false)
    app.unmount()

    const clean = withChat()
    clean.chat.finishEarly()
    expect(clean.chat.confirmFlowDiscard.value).toBe('none')
    clean.app.unmount()
  })

  it('publishes the conversation busy flag to the run editors', async () => {
    const ed = useArtifactEditor('run-g')
    const { chat, models, app, props } = withChat()
    await nextTick()
    expect(ed.sessionBusy.value).toBe(false)
    models.draft.value = 'hello'
    chat.send()
    await flushPromises()
    expect(ed.sessionBusy.value).toBe(true)
    props.runId = 'run-other'
    await nextTick()
    expect(ed.sessionBusy.value).toBeNull()
    app.unmount()
    expect(useArtifactEditor('run-other').sessionBusy.value).toBeNull()
  })
})
