// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

vi.mock('monaco-editor/esm/vs/editor/contrib/find/browser/findController', () => ({}))
vi.mock('monaco-editor/esm/vs/basic-languages/markdown/markdown.contribution', () => ({}))
vi.mock('monaco-editor/esm/vs/basic-languages/shell/shell.contribution', () => ({}))
vi.mock('monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution', () => ({}))
vi.mock('monaco-editor/esm/vs/basic-languages/html/html.contribution', () => ({}))
vi.mock('monaco-editor/esm/vs/basic-languages/javascript/javascript.contribution', () => ({}))
vi.mock('monaco-editor/esm/vs/basic-languages/typescript/typescript.contribution', () => ({}))
vi.mock('monaco-editor/esm/vs/basic-languages/python/python.contribution', () => ({}))
vi.mock('monaco-editor/esm/vs/basic-languages/ini/ini.contribution', () => ({}))
vi.mock('monaco-editor/esm/vs/language/json/monaco.contribution', () => ({}))
vi.mock('monaco-editor/esm/vs/editor/editor.worker?worker', () => ({ default: class {} }))
vi.mock('monaco-editor/esm/vs/language/json/json.worker?worker', () => ({ default: class {} }))

vi.mock('monaco-editor/esm/vs/editor/editor.api', () => ({
  editor: {
    create: vi.fn(() => ({
      getValue: () => 'hello',
      setValue: vi.fn(),
      onDidChangeModelContent: vi.fn(() => ({ dispose: vi.fn() })),
      onDidScrollChange: vi.fn(() => ({ dispose: vi.fn() })),
      updateOptions: vi.fn(),
      dispose: vi.fn(),
      getModel: () => ({ setValue: vi.fn(), dispose: vi.fn() }),
      getScrollTop: () => 0,
      getScrollHeight: () => 100,
      getLayoutInfo: () => ({ height: 100 }),
    })),
    setModelLanguage: vi.fn(),
    defineTheme: vi.fn(),
    setTheme: vi.fn(),
  },
  languages: {
    register: vi.fn(),
    registerTokensProviderFactory: vi.fn(),
    setMonarchTokensProvider: vi.fn(),
  },
}))

import CodeEditor from './CodeEditor.vue'

describe('CodeEditor', () => {
  it('mounts editor host and syncs modelValue', async () => {
    const wrapper = mount(CodeEditor, {
      props: { modelValue: 'hello', language: 'markdown' },
    })
    expect(wrapper.find('[ref="host"], div').exists()).toBe(true)
    await wrapper.setProps({ modelValue: 'world' })
    wrapper.unmount()
  })
})
