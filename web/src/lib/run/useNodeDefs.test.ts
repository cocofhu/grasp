// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { describe, expect, it } from 'vitest'
import { useNodeDefs } from './useNodeDefs'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import nodes from '@/locales/zh-CN/nodes.json'

describe('useNodeDefs', () => {
  it('translates node defs', () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages, ...nodes } },
    })
    let defs: ReturnType<typeof useNodeDefs>['NODE_DEFS'] | null = null
    mount(
      defineComponent({
        setup() {
          defs = useNodeDefs().NODE_DEFS
          return () => h('div')
        },
      }),
      { global: { plugins: [i18n] } },
    )
    expect(defs!.value.input.label).toBeTruthy()
    expect(Object.keys(defs!.value).sort()).toEqual(
      ['agent', 'branch', 'human_gate', 'input', 'output', 'set_var'],
    )
  })
})
