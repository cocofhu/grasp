// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentFilesPanel from './AgentFilesPanel.vue'

const ModalStub = {
  props: ['open', 'title'],
  emits: ['close'],
  template: '<div v-if="open" class="modal"><h2>{{ title }}</h2><button class="modal-close" @click="$emit(\'close\')">x</button><slot/><slot name="footer"/></div>',
}
const ContextStub = {
  name: 'ExplorerContextMenu',
  props: ['open', 'target'],
  emits: ['close', 'action'],
  template: '<div v-if="open" data-testid="context"><button @click="$emit(\'action\', \'newFile\')">ctx-new</button><button @click="$emit(\'close\')">ctx-close</button></div>',
}

function draft(files = [
  { path: 'README.md', content: '# hello' },
  { path: 'rules/base.md', content: 'rule' },
  { path: 'src/main.ts', content: 'const x = 1' },
]) {
  return {
    name: 'demo',
    projectId: 'p1',
    acpBackend: 'cursor',
    files,
    mcp: [],
    env: [],
    layout: { configRoot: '/root/.cursor', workspaceDir: '/root/workspace' },
    capabilities: null,
  } as any
}

function mountPanel(extra: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(AgentFilesPanel, {
    props: {
      draft: draft(),
      dirty: false,
      isMobile: false,
      agentName: 'demo',
      historyRefreshKey: 0,
      save: vi.fn().mockResolvedValue(true),
      ...extra,
    },
    attachTo: document.body,
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        AppButton: { template: '<button type="button" v-bind="$attrs"><slot /></button>' },
        AppModal: ModalStub,
        CodeEditor: { props: ['modelValue'], emits: ['update:modelValue'], template: '<textarea data-testid="code" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />' },
        MarkdownSplitEditor: { props: ['modelValue'], emits: ['update:modelValue'], template: '<textarea data-testid="markdown" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />' },
        ExplorerContextMenu: ContextStub,
        AgentWorkspaceHistoryPanel: {
          emits: ['toggle-collapse', 'restored'],
          template: '<div data-testid="history"><button @click="$emit(\'toggle-collapse\')">history-toggle</button><button @click="$emit(\'restored\')">restored</button></div>',
        },
      },
    },
  })
}

describe('AgentFilesPanel interactions', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
  })

  it('opens files, tabs, folders, rename/create/delete and context actions on desktop', async () => {
    const d = draft()
    const w = mountPanel({ draft: d })
    const vm = w.vm as any
    vm.selectDefaultFile()
    await flushPromises()
    expect(w.find('[data-testid="markdown"]').exists()).toBe(true)
    expect(w.text()).toContain('README.md')
    expect(w.find('[data-testid="history"]').exists()).toBe(true)

    const src = w.findAll('button').find((b) => b.text().includes('src'))!
    await src.trigger('click')
    await flushPromises()
    const main = w.findAll('button').find((b) => b.text().includes('main.ts'))!
    await main.trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="code"]').exists()).toBe(true)
    expect(w.text()).toContain('typescript')

    const row = main.element.closest('.group')!
    await row.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))
    await flushPromises()
    const rename = w.get('input[data-rename]')
    await rename.setValue('app.ts')
    await rename.trigger('keyup', { key: 'Enter' })
    expect(d.files.some((f: any) => f.path === 'src/app.ts')).toBe(true)

    const newRoot = w.findAll('button').find((b) => b.attributes('title')?.includes('新建文件'))!
    await newRoot.trigger('click')
    await flushPromises()
    const create = w.get('input[data-create]')
    await create.setValue('notes.txt')
    await create.trigger('keyup', { key: 'Enter' })
    expect(d.files.some((f: any) => f.path === 'notes.txt')).toBe(true)

    const notesButton = w.findAll('button').find((b) => b.text().includes('notes.txt'))!
    const notesRow = notesButton.element.closest('.group')!
    const deleteButton = Array.from(notesRow.querySelectorAll('button')).at(-1) as HTMLButtonElement
    deleteButton.click()
    await flushPromises()
    expect(w.find('.modal').exists()).toBe(true)
    const confirm = w.findAll('.modal button').find((b) => b.text().includes('删除'))!
    await confirm.trigger('click')
    await flushPromises()
    expect(d.files.some((f: any) => f.path === 'notes.txt')).toBe(false)

    const explorer = w.find('.scroll-area')
    await explorer.trigger('contextmenu', { clientX: 10, clientY: 20 })
    await flushPromises()
    expect(w.find('[data-testid="context"]').exists()).toBe(true)
    await w.get('[data-testid="context"] button').trigger('click')
    await flushPromises()
    expect(w.find('input[data-create]').exists()).toBe(true)
    w.unmount()
  })

  it('toggles sidebars, renders empty state, and emits history restoration', async () => {
    const w = mountPanel({ draft: draft([]), agentName: 'demo' })
    expect(w.text()).toContain('空目录')
    const collapse = w.findAll('button').find((b) => b.attributes('aria-label')?.includes('收起资源'))!
    await collapse.trigger('click')
    expect(localStorage.getItem('agent-studio-explorer-collapsed')).toBe('true')
    const expand = w.findAll('button').find((b) => b.attributes('aria-label')?.includes('展开资源'))!
    await expand.trigger('click')

    await w.get('[data-testid="history"] button').trigger('click')
    await w.findAll('[data-testid="history"] button')[1].trigger('click')
    expect(w.emitted('restored')).toBeTruthy()
    expect(localStorage.getItem('agent-studio-history-collapsed')).toBe('false')

    ;(w.vm as any).openPathOrCreate('settings.json', '{}')
    await flushPromises()
    expect(w.find('[data-testid="code"]').exists()).toBe(true)
    const snap = (w.vm as any).snapshot()
    expect(snap.path).toBe('settings.json')
    ;(w.vm as any).resetForSelect()
    ;(w.vm as any).restoreAfterDiscard(snap)
    await flushPromises()
    expect(w.text()).toContain('settings.json')
    w.unmount()
  })

  it('uses the mobile list/edit flow, more menu, and dirty back confirmation', async () => {
    const save = vi.fn().mockResolvedValue(true)
    const w = mountPanel({ isMobile: true, dirty: true, save, agentName: '' })
    await w.findAll('button').find((b) => b.text().includes('README.md'))!.trigger('click')
    await flushPromises()
    expect((w.vm as any).filesStep).toBe('edit')
    expect(w.text()).toContain('返回')

    await w.findAll('button').find((b) => b.text().includes('返回'))!.trigger('click')
    await flushPromises()
    expect(w.find('.modal').exists()).toBe(true)
    const saveBack = w.findAll('.modal button').find((b) => b.text().includes('保存并返回'))!
    await saveBack.trigger('click')
    await flushPromises()
    expect(save).toHaveBeenCalled()
    expect(w.emitted('update:just-saved')?.[0]).toEqual([true])
    expect((w.vm as any).filesStep).toBe('list')

    const more = w.find('[data-test="file-row-more"]')
    await more.trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-test="explorer-more-menu"]')).toBeTruthy()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[data-test="explorer-more-menu"]')).toBeNull()

    await w.setProps({ isMobile: false })
    await flushPromises()
    expect(document.querySelector('[data-test="explorer-more-menu"]')).toBeNull()
    w.unmount()
  })

  it('drives remaining desktop template callbacks', async () => {
    const d = draft()
    const w = mountPanel({ draft: d })
    ;(w.vm as any).selectDefaultFile()
    await flushPromises()
    await w.get('[data-testid="markdown"]').setValue('# changed')
    expect(d.files[0].content).toBe('# changed')

    const rootFolder = w.findAll('button').find((b) => b.attributes('title')?.includes('新建文件夹'))!
    await rootFolder.trigger('click')
    await flushPromises()
    let create = w.get('input[data-create]')
    await create.setValue('tmp')
    await create.trigger('keyup', { key: 'Escape' })
    expect(w.find('input[data-create]').exists()).toBe(false)

    await rootFolder.trigger('click')
    await flushPromises()
    create = w.get('input[data-create]')
    await create.setValue('docs')
    await create.trigger('blur')
    expect(w.text()).toContain('docs')

    const srcToggle = w.findAll('button').find((b) => b.text().includes('src'))!
    await srcToggle.trigger('click')
    await flushPromises()
    const srcAction = (index: number) => {
      const toggle = w.findAll('button').find((b) => b.text().includes('src'))!
      const row = toggle.element.closest('.group')!
      return Array.from(row.querySelectorAll('[data-test="file-row-action"]'))[index] as HTMLButtonElement
    }
    srcAction(0).click()
    await flushPromises()
    create = w.get('input[data-create]')
    await create.setValue('util.ts')
    await create.trigger('blur')
    expect(d.files.some((f: any) => f.path === 'src/util.ts')).toBe(true)

    srcAction(1).click()
    await flushPromises()
    create = w.get('input[data-create]')
    await create.setValue('nested')
    await create.trigger('keyup', { key: 'Enter' })
    expect(w.text()).toContain('nested')

    const main = w.findAll('button').find((b) => b.text().includes('main.ts'))!
    const mainRow = main.element.closest('.group')!
    await mainRow.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, clientX: 5, clientY: 5 }))
    await flushPromises()
    expect(w.find('[data-testid="context"]').exists()).toBe(true)
    const renameButton = Array.from(mainRow.querySelectorAll('[data-test="file-row-action"]')).at(-2) as HTMLButtonElement
    renameButton.click()
    await flushPromises()
    await w.get('input[data-rename]').trigger('keyup', { key: 'Escape' })

    await main.trigger('click')
    await flushPromises()
    await w.get('[data-testid="code"]').setValue('updated')
    const readmeTab = w.findAll('.group').find((r) => r.text().includes('README.md') && r.find('button').exists())
    if (readmeTab) await readmeTab.trigger('click')
    const closeTab = w.findAll('button').find((b) => b.attributes('title')?.includes('关闭'))!
    await closeTab.trigger('click')

    const deleteMain = Array.from(mainRow.querySelectorAll('[data-test="file-row-action"]')).at(-1) as HTMLButtonElement
    deleteMain.click()
    await flushPromises()
    await w.get('.modal-close').trigger('click')
    w.unmount()
  })

  it('executes mobile menu backdrop and each action', async () => {
    const w = mountPanel({ isMobile: true, dirty: false, agentName: '' })
    const openMore = async (path: string) => {
      const button = w.get(`[data-test="file-row-more"][data-path="${path}"]`)
      await button.trigger('click')
      await flushPromises()
    }
    await openMore('README.md')
    const backdrop = document.querySelector('[data-test="explorer-more-backdrop"]') as HTMLElement
    backdrop.click()
    await flushPromises()

    await openMore('README.md')
    ;(document.querySelector('[data-action="rename"]') as HTMLElement).click()
    await flushPromises()
    await w.get('input[data-rename]').trigger('blur')

    await openMore('README.md')
    ;(document.querySelector('[data-action="delete"]') as HTMLElement).click()
    await flushPromises()
    await w.get('.modal-close').trigger('click')

    await w.findAll('button').find((b) => b.text().includes('src'))!.trigger('click')
    await flushPromises()
    await openMore('src')
    ;(document.querySelector('[data-action="newFile"]') as HTMLElement).click()
    await flushPromises()
    await w.get('input[data-create]').trigger('keyup', { key: 'Escape' })

    await openMore('src')
    ;(document.querySelector('[data-action="newFolder"]') as HTMLElement).click()
    await flushPromises()
    await w.get('input[data-create]').trigger('keyup', { key: 'Escape' })
    w.unmount()
  })
})
