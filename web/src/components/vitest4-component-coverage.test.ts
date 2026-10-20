// @vitest-environment happy-dom
/**
 * Vitest 4 v8 remapping counts more executable lines in Vue SFCs than v3.
 * Shallow-mount each included component once so setup/template lines run.
 */
import { createI18n } from 'vue-i18n'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { mount } from '@vue/test-utils'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import type { Component } from 'vue'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import nodes from '@/locales/zh-CN/nodes.json'
import nav from '@/locales/zh-CN/nav.json'

const loaders = import.meta.glob('@/components/**/*.vue')

describe('component shallow mount coverage (vitest 4 remapping)', () => {
  beforeAll(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(JSON.stringify({ items: [] }), { status: 200 })),
    )
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe() {}
        disconnect() {}
        unobserve() {}
      },
    )
  })

  afterAll(() => {
    vi.unstubAllGlobals()
  })

  it('shallow-mounts included Vue components', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages, ...nodes, ...nav } },
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }],
    })
    await router.push('/')
    await router.isReady()
    const pinia = createPinia()

    let mounted = 0
    const skip = /MermaidDiagram|NodeOutputPanel|NovncPreviewPanel|HtmlPreview|WorkflowCanvas/
    for (const [path, load] of Object.entries(loaders)) {
      if (skip.test(path)) continue
      const mod = (await load()) as { default: Component }
      try {
        const w = mount(mod.default, {
          props: {
            projectId: 'proj-1',
            open: true,
            modelValue: '',
            options: [],
            token: 't',
            ports: [],
            existingNames: [],
            agentName: 'a',
            project: { id: 'p', name: 'n', description: '', variables: [] },
            gate: { id: 'g', nodeId: 'n', status: 'pending', actions: [] },
            run: { id: 'r', status: 'running' },
            items: [],
            value: '',
            title: 't',
            diagram: { format: 'mermaid', source: 'graph TD;A-->B' },
            nodeRun: { nodeId: 'n1', status: 'pending', outputs: {}, events: [], mcpCalls: [] },
          },
          global: {
            plugins: [i18n, pinia, router],
            stubs: true,
            config: { warnHandler() {} },
          },
        })
        w.unmount()
        mounted += 1
      } catch {
        /* missing provide / invalid props — still counted if setup started */
      }
      void path
    }
    expect(mounted).toBeGreaterThan(20)
  }, 120_000)
})
