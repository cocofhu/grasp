// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Artifact } from '@/lib/shared/types'
import ArtifactList from './ArtifactList.vue'

const packRunArtifacts = vi.fn()
const downloadZip = vi.fn()
const toastError = vi.fn()

vi.mock('@/lib/api/api', () => ({
  api: {
    packRunArtifacts: (...args: unknown[]) => packRunArtifacts(...args),
  },
}))

vi.mock('@/lib/agent/agentIO', () => ({
  downloadZip: (...args: unknown[]) => downloadZip(...args),
}))

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: toastError }),
}))

function artifact(name: string, id = name, runId = 'run-1'): Artifact {
  return {
    id,
    name,
    kind: 'json',
    nodeId: 'research',
    runId,
    workflowName: 'wf',
    sizeBytes: 10,
    createdAt: '2026-07-18T00:00:00Z',
  }
}

function mountList(props: Partial<InstanceType<typeof ArtifactList>['$props']> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ArtifactList, {
    props: {
      artifacts: props.artifacts ?? [artifact('research.json'), artifact('plan.json', 'a2')],
      scope: props.scope ?? 'run',
      activeId: props.activeId ?? null,
      ...props,
    },
    global: { plugins: [i18n], stubs: { Icon: true } },
  })
}

describe('ArtifactList', () => {
  afterEach(() => {
    packRunArtifacts.mockReset()
    downloadZip.mockReset()
    toastError.mockReset()
  })

  it('renders friendly names with technical filenames in run scope', () => {
    const wrapper = mountList()
    expect(wrapper.text()).toContain('调研')
    expect(wrapper.text()).toContain('计划')
    expect(wrapper.text()).toContain('research.json')
    expect(wrapper.text()).toContain('plan.json')
    wrapper.unmount()
  })

  it('matches platform artifacts by either friendly or technical name', async () => {
    const requirement = artifact('clarified_requirement.json')
    const research = artifact('research.json', 'research')
    const wrapper = mountList({
      artifacts: [requirement, research],
      scope: 'platform',
      runSections: [
        { runId: 'run-1', runTitle: 'Run 1', items: [requirement, research] },
      ] as any,
    })
    const input = wrapper.get('input[type="search"]')
    const rowFor = (name: string) =>
      wrapper.get(`[title="${name}"]`).element.closest('button') as HTMLButtonElement
    await input.setValue('需求澄清')
    expect(rowFor('clarified_requirement.json').style.display).not.toBe('none')
    expect(rowFor('research.json').style.display).toBe('none')
    await input.setValue('research.json')
    expect(rowFor('research.json').style.display).not.toBe('none')
    expect(rowFor('clarified_requirement.json').style.display).toBe('none')
    wrapper.unmount()
  })

  it('filters artifacts locally by search', async () => {
    const wrapper = mountList()
    const input = wrapper.find('input[type="search"], input')
    if (input.exists()) {
      await input.setValue('plan')
      expect(wrapper.text()).toContain('plan.json')
      expect(wrapper.text()).not.toContain('research.json')
    }
    wrapper.unmount()
  })

  it('emits select when artifact clicked', async () => {
    const arts = [artifact('research.json')]
    const wrapper = mountList({ artifacts: arts })
    const row = wrapper.findAll('button').find((b) => b.text().includes('research.json'))
    expect(row).toBeTruthy()
    await row!.trigger('click')
    expect(wrapper.emitted('select')).toBeTruthy()
    expect((wrapper.emitted('select')![0][0] as Artifact).name).toBe('research.json')
    wrapper.unmount()
  })

  it('shows empty text when no artifacts', () => {
    const wrapper = mountList({ artifacts: [] })
    expect(wrapper.text()).toMatch(/暂无|没有/)
    wrapper.unmount()
  })

  // A long review produces one product per round; listing them all inline would
  // bury the deliverables the list exists to show.
  it('folds the feedback ledger into one collapsed group', async () => {
    const wrapper = mountList({
      artifacts: [
        artifact('research.json'),
        artifact('feedback_index.json', 'f0'),
        artifact('feedback.review.research.i1r1.json', 'f1'),
        artifact('feedback.review.research.i1r2.json', 'f2'),
      ],
    })
    const rowFor = (name: string) =>
      wrapper.findAll('button').find((b) => b.text().includes(name))!
    const ledgerFold = () => wrapper.get('[data-testid="artifact-feedback-group"] + div')

    expect(rowFor('research.json').isVisible()).toBe(true)
    expect(ledgerFold().classes()).toContain('ui-fold')
    expect(ledgerFold().classes()).not.toContain('is-open')

    const group = wrapper.get('[data-testid="artifact-feedback-group"]')
    expect(group.text()).toContain('3')

    await group.trigger('click')
    expect(ledgerFold().classes()).toContain('is-open')
    expect(ledgerFold().text()).toContain('feedback_index.json')
    expect(ledgerFold().text()).toContain('feedback.review.research.i1r2.json')

    await rowFor('feedback_index.json').trigger('click')
    expect((wrapper.emitted('select')![0][0] as Artifact).name).toBe('feedback_index.json')
    wrapper.unmount()
  })

  it('scope=run has no pack button; scope=platform shows pack', () => {
    const arts = [artifact('research.json')]
    const run = mountList({ artifacts: arts, scope: 'run' })
    expect(run.find('[data-testid="artifact-run-pack"]').exists()).toBe(false)
    run.unmount()

    const platform = mountList({
      artifacts: arts,
      scope: 'platform',
      runSections: [{ runId: 'run-1', runTitle: 'Demo', items: arts }] as any,
    })
    const pack = platform.get('[data-testid="artifact-run-pack"]')
    expect(pack.text()).toContain('打包')
    expect(pack.attributes('aria-label')).toBe('打包')
    platform.unmount()
  })

  it('pack click does not change activeId or collapse, and downloads zip', async () => {
    const arts = [artifact('research.json')]
    packRunArtifacts.mockResolvedValue({ blob: new Blob(['x']), filename: 'Demo-artifacts.zip' })
    const wrapper = mountList({
      artifacts: arts,
      scope: 'platform',
      activeId: 'research.json',
      runSections: [{ runId: 'run-1', runTitle: 'Demo', items: arts }] as any,
    })
    const fold = wrapper.get('.ui-fold')
    expect(fold.classes()).toContain('is-open')
    await wrapper.get('[data-testid="artifact-run-pack"]').trigger('click')
    expect(packRunArtifacts).toHaveBeenCalledWith('run-1')
    expect(downloadZip).toHaveBeenCalled()
    expect(wrapper.emitted('select')).toBeFalsy()
    expect(wrapper.props('activeId')).toBe('research.json')
    expect(fold.classes()).toContain('is-open')
    wrapper.unmount()
  })

  it('empty run pack button is disabled; pack failure toasts without download', async () => {
    const other = artifact('plan.json', 'p2', 'run-2')
    const emptySec = { runId: 'run-empty', runTitle: 'Empty', items: [] as Artifact[] }
    const filled = { runId: 'run-2', runTitle: 'Filled', items: [other] }
    const wrapper = mountList({
      artifacts: [other],
      scope: 'platform',
      runSections: [emptySec, filled] as any,
    })
    const emptyPack = wrapper.findAll('[data-testid="artifact-run-pack"]').find(
      (b) => b.attributes('data-run-id') === 'run-empty',
    )!
    expect(emptyPack.attributes('disabled')).toBeDefined()

    packRunArtifacts.mockRejectedValue(new Error('boom'))
    const filledPack = wrapper.findAll('[data-testid="artifact-run-pack"]').find(
      (b) => b.attributes('data-run-id') === 'run-2',
    )!
    await filledPack.trigger('click')
    await Promise.resolve()
    expect(toastError).toHaveBeenCalled()
    expect(downloadZip).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('pack remains available when run section is collapsed', async () => {
    const arts = [artifact('research.json')]
    packRunArtifacts.mockResolvedValue({ blob: new Blob(['x']), filename: 'x.zip' })
    const wrapper = mountList({
      artifacts: arts,
      scope: 'platform',
      runSections: [{ runId: 'run-1', runTitle: 'Demo', items: arts }] as any,
    })
    const foldBtn = wrapper.findAll('button').find((b) => b.text().includes('Demo'))!
    await foldBtn.trigger('click')
    expect(wrapper.get('.ui-fold').classes()).not.toContain('is-open')
    await wrapper.get('[data-testid="artifact-run-pack"]').trigger('click')
    expect(packRunArtifacts).toHaveBeenCalledWith('run-1')
    wrapper.unmount()
  })
})
