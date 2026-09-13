// @vitest-environment happy-dom
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Artifact } from '@/lib/shared/types'
import ArtifactPanel from './ArtifactPanel.vue'

const ListStub = defineComponent({
  name: 'ArtifactList',
  props: { artifacts: Array, activeId: String, scope: String },
  emits: ['select'],
  template:
    '<div data-testid="artifact-list"><button v-for="a in artifacts" :key="a.id" @click="$emit(\'select\', a)">{{ a.name }}</button></div>',
})

const PreviewStub = defineComponent({
  name: 'ArtifactPreview',
  props: { artifact: Object, scope: String },
  template: '<div data-testid="artifact-preview">{{ artifact?.name || "none" }}</div>',
})

function mountPanel(artifacts: Artifact[]) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ArtifactPanel, {
    props: { artifacts, scope: 'run' },
    global: {
      plugins: [i18n],
      stubs: { ArtifactList: ListStub, ArtifactPreview: PreviewStub },
    },
  })
}

describe('ArtifactPanel', () => {
  const artifacts: Artifact[] = [
    {
      id: 'a1',
      name: 'research.json',
      kind: 'json',
      nodeId: 'research',
      runId: 'run-1',
      workflowName: 'wf',
      sizeBytes: 10,
      createdAt: '2026-07-18T00:00:00Z',
    },
    {
      id: 'a2',
      name: 'plan.json',
      kind: 'json',
      nodeId: 'plan',
      runId: 'run-1',
      workflowName: 'wf',
      sizeBytes: 20,
      createdAt: '2026-07-18T00:00:00Z',
    },
  ]

  it('auto-selects first artifact on mount', async () => {
    const wrapper = mountPanel(artifacts)
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-preview"]').text()).toBe('research.json')
    wrapper.unmount()
  })

  it('updates preview when list emits select', async () => {
    const wrapper = mountPanel(artifacts)
    const planBtn = wrapper.findAll('button').find((b) => b.text() === 'plan.json')
    expect(planBtn).toBeTruthy()
    await planBtn!.trigger('click')
    expect(wrapper.find('[data-testid="artifact-preview"]').text()).toBe('plan.json')
    wrapper.unmount()
  })

  it('emits deleted and advances selection', async () => {
    const wrapper = mountPanel(artifacts)
    const preview = wrapper.findComponent({ name: 'ArtifactPreview' })
    await preview.vm.$emit('deleted', 'a1')
    expect(wrapper.emitted('deleted')).toEqual([['a1']])
    wrapper.unmount()
  })

  it('run-detail ArtifactPanel (scope=run) does not render pack button', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const wrapper = mount(ArtifactPanel, {
      props: { artifacts, scope: 'run' },
      global: {
        plugins: [i18n],
        stubs: { ArtifactPreview: PreviewStub, Icon: true },
      },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="artifact-run-pack"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
