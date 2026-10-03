import { describe, expect, it, vi } from 'vitest'
import { createStreamMarkdownPreview } from './streamMarkdownPreview'

describe('createStreamMarkdownPreview', () => {
  it('coalesces many deltas into a single render per scheduled flush', () => {
    const renders: string[] = []
    const render = vi.fn((src: string) => {
      renders.push(src)
      return `<p>${src}</p>`
    })
    const queued: Array<() => void> = []
    const preview = createStreamMarkdownPreview({
      render,
      schedule: (cb) => {
        queued.push(cb)
        return queued.length
      },
      cancel: () => {
        queued.length = 0
      },
    })

    preview.append('a')
    preview.append('b')
    preview.append('c')
    expect(render).not.toHaveBeenCalled()
    expect(queued).toHaveLength(1)

    queued.shift()!()
    expect(render).toHaveBeenCalledTimes(1)
    expect(render).toHaveBeenCalledWith('abc')
    expect(preview.getHtml()).toBe('<p>abc</p>')

    preview.append('d')
    expect(queued).toHaveLength(1)
    queued.shift()!()
    expect(render).toHaveBeenCalledTimes(2)
    expect(render).toHaveBeenLastCalledWith('abcd')
  })

  it('setText replaces absolute snapshot without appending', () => {
    const render = vi.fn((src: string) => src)
    const queued: Array<() => void> = []
    const preview = createStreamMarkdownPreview({
      render,
      schedule: (cb) => {
        queued.push(cb)
        return queued.length
      },
      cancel: () => {
        queued.length = 0
      },
    })
    preview.append('old')
    queued.shift()!()
    preview.setText('fresh')
    queued.shift()!()
    expect(preview.getText()).toBe('fresh')
    expect(render).toHaveBeenLastCalledWith('fresh')
  })

  it('flush forces an immediate render without waiting', () => {
    const render = vi.fn((src: string) => src)
    const preview = createStreamMarkdownPreview({
      render,
      schedule: () => 0,
      cancel: () => {},
    })
    preview.append('x')
    expect(render).not.toHaveBeenCalled()
    preview.flush()
    expect(render).toHaveBeenCalledWith('x')
  })

  it('publishes block arrays with a typed empty value', () => {
    const seen: string[][] = []
    const preview = createStreamMarkdownPreview<string[]>({
      render: (src) => src.split('\n\n'),
      empty: [],
      schedule: (cb) => {
        cb()
        return 1
      },
      cancel: () => {},
    })
    preview.subscribe((b) => seen.push(b))
    expect(preview.getHtml()).toEqual([])
    preview.setText('a\n\nb')
    expect(preview.getHtml()).toEqual(['a', 'b'])
    preview.reset()
    expect(preview.getHtml()).toEqual([])
    expect(seen).toEqual([['a', 'b'], []])
  })
})
