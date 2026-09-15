// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { describe, expect, it } from 'vitest'
import { useNodeDefs, usePaletteGroups } from './useNodeDefs'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import nodes from '@/locales/zh-CN/nodes.json'

describe('useNodeDefs', () => {
  it('translates node defs and palette groups', () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages, ...nodes } },
    })
    let defs: ReturnType<typeof useNodeDefs>['NODE_DEFS'] | null = null
    let groups: ReturnType<typeof usePaletteGroups>['PALETTE_GROUPS'] | null = null
    mount(
      defineComponent({
        setup() {
          defs = useNodeDefs().NODE_DEFS
          groups = usePaletteGroups().PALETTE_GROUPS
          return () => h('div')
        },
      }),
      { global: { plugins: [i18n] } },
    )
    expect(defs!.value.input.label).toBeTruthy()
    expect(groups!.value.length).toBeGreaterThan(0)
    expect(groups!.value[0].title).toBeTruthy()
    const agentGroup = groups!.value.find((g) => g.types.includes('grasp'))
    expect(agentGroup?.types).toContain('grasp')
    expect(agentGroup?.types).not.toContain('agent')
    expect(defs!.value.approve.label).toBe('Grasp')
  })
})
