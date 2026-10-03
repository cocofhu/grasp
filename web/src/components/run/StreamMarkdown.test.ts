// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import StreamMarkdown from './StreamMarkdown.vue'

describe('StreamMarkdown', () => {
  it('keeps settled blocks in the DOM while the last block grows', async () => {
    const w = mount(StreamMarkdown, { props: { blocks: ['<h1>Title</h1>', '<p>para</p>'] } })
    const blocks = () => w.findAll('[data-testid="stream-md-block"]')
    expect(blocks()).toHaveLength(2)
    const h1 = w.find('h1').element
    // A selection-like marker survives because the block's innerHTML is never reset.
    h1.setAttribute('data-marker', '1')

    await w.setProps({ blocks: ['<h1>Title</h1>', '<p>para grows</p>', '<ul><li>item</li></ul>'] })
    expect(blocks()).toHaveLength(3)
    expect(w.find('h1').element).toBe(h1)
    expect(h1.getAttribute('data-marker')).toBe('1')
    expect(w.text()).toContain('para grows')
    expect(w.find('li').text()).toBe('item')
  })

  it('renders nothing for no blocks', () => {
    const w = mount(StreamMarkdown, { props: { blocks: [] } })
    expect(w.findAll('[data-testid="stream-md-block"]')).toHaveLength(0)
  })
})
