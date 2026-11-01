// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import PendingSendQueuePanel, { type PendingQueueRow } from './PendingSendQueuePanel.vue'
import pages from '@/locales/zh-CN/pages.json'

const row = (id: string, over: Partial<PendingQueueRow> = {}): PendingQueueRow => ({ id, text: id, images: [], annotations: [], ...over })

function mountPanel(props: Record<string, unknown>, slots: Record<string, unknown> = {}) {
  return mount(PendingSendQueuePanel, {
    props: props as never,
    slots: slots as never,
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': { pages } } })],
      stubs: { Icon: true },
    },
  })
}

function drop(w: ReturnType<typeof mountPanel>, from: number, to: number) {
  const rows = w.findAll('[data-testid="clarify-queue-item"]')
  return rows[from]!.trigger('dragstart', { dataTransfer: { setData() {}, getData: () => String(from) } }).then(() =>
    rows[to]!.trigger('drop', { dataTransfer: { getData: () => String(from) } }))
}

describe('PendingSendQueuePanel', () => {
  it('shows reference chips on queued rows that carry them', () => {
    const w = mountPanel({ items: [row('a', { references: [{ id: 'r1', kind: 'solo', target: 's1', title: '登录重构' }] }), row('b')] })
    const rows = w.findAll('[data-testid="clarify-queue-item"]')
    expect(rows[0]!.find('[data-testid="clarify-queue-references"]').text()).toContain('登录重构')
    expect(rows[1]!.find('[data-testid="clarify-queue-references"]').exists()).toBe(false)
  })

  it('renders unlocked rows with handle, edit and cancel, and emits each action', async () => {
    const w = mountPanel({ items: [row('a'), row('b')] })
    const rows = w.findAll('[data-testid="clarify-queue-item"]')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.attributes('draggable')).toBe('true')
    expect(rows[0]!.findAll('button')).toHaveLength(3)
    await rows[1]!.find('[data-testid="clarify-queue-edit"]').trigger('click')
    await rows[1]!.find('[data-testid="clarify-queue-cancel"]').trigger('click')
    await drop(w, 1, 0)
    expect(w.emitted('edit')).toEqual([[1]])
    expect(w.emitted('cancel')).toEqual([[1]])
    expect(w.emitted('reorder')).toEqual([[1, 0]])
  })

  it('locks a row: no handle or buttons, not draggable, and ignored as drag source or drop target', async () => {
    const w = mountPanel({ items: [row('a'), row('b'), row('local', { locked: true })] })
    const rows = w.findAll('[data-testid="clarify-queue-item"]')
    expect(rows[2]!.attributes('draggable')).toBe('false')
    expect(rows[2]!.findAll('button')).toHaveLength(0)
    expect(rows[2]!.text()).toContain('local')
    await drop(w, 0, 2)
    await drop(w, 2, 0)
    expect(w.emitted('reorder')).toBeUndefined()
    await drop(w, 1, 0)
    expect(w.emitted('reorder')).toEqual([[1, 0]])
  })

  it('renders the row-extra slot with its row before the badges', () => {
    const w = mountPanel(
      { items: [row('a', { images: [{ mimeType: 'image/png', data: 'x' }] })] },
      { 'row-extra': ({ item, index }: { item: PendingQueueRow; index: number }) => h('span', { 'data-testid': 'extra' }, `${index}:${item.id}`) },
    )
    const extra = w.find('[data-testid="extra"]')
    expect(extra.text()).toBe('0:a')
    const html = w.find('[data-testid="clarify-queue-item"]').html()
    expect(html.indexOf('data-testid="extra"')).toBeLessThan(html.indexOf('rounded-full border border-line px-1.5'))
  })

  it('keeps readonly rows free of actions without the new fields', () => {
    const w = mountPanel({ items: [row('a')], readonly: true })
    const item = w.find('[data-testid="clarify-queue-item"]')
    expect(item.attributes('draggable')).toBe('false')
    expect(item.findAll('button')).toHaveLength(0)
  })
})
