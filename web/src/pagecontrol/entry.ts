import { createExecutor, type PageCommand, type PageResult } from './executor'

export type PageControlOptions = {
  /** Hides the preview-pick UI (bar, drawer) while the page is read. */
  hideOwnUi?: (hidden: boolean) => void
}

const MASK_CSS =
  ':host{all:initial}' +
  '.mask{position:fixed;inset:0;z-index:2147483645;cursor:progress;' +
  'box-shadow:inset 0 0 0 3px rgba(99,102,241,.75);background:transparent}' +
  '.mask.through{pointer-events:none}' +
  '.dot{position:fixed;width:14px;height:14px;margin:-7px 0 0 -7px;border-radius:50%;' +
  'background:rgba(99,102,241,.9);box-shadow:0 0 0 4px rgba(99,102,241,.3);' +
  'transition:left .25s ease,top .25s ease;pointer-events:none}' +
  '[hidden]{display:none!important}'

/**
 * Blocks user input on the page while an agent action runs and shows where it
 * clicks. PageController signals pointer moves and hit-test pass-through via
 * window events.
 */
function createMask() {
  const host = document.createElement('grasp-page-control')
  host.setAttribute('data-page-agent-not-interactive', '')
  const shadow = host.attachShadow({ mode: 'open' })
  shadow.innerHTML = `<style>${MASK_CSS}</style><div class="mask" hidden><div class="dot" hidden></div></div>`
  const mask = shadow.querySelector('.mask') as HTMLElement
  const dot = shadow.querySelector('.dot') as HTMLElement

  const onMove = (e: Event) => {
    const d = (e as CustomEvent<{ x: number; y: number }>).detail
    if (!d) return
    dot.style.left = `${d.x}px`
    dot.style.top = `${d.y}px`
    dot.hidden = false
  }
  const through = () => mask.classList.add('through')
  const block = () => mask.classList.remove('through')
  window.addEventListener('PageAgent::MovePointerTo', onMove)
  window.addEventListener('PageAgent::EnablePassThrough', through)
  window.addEventListener('PageAgent::DisablePassThrough', block)

  return {
    host,
    show() {
      if (!host.isConnected) (document.body || document.documentElement).appendChild(host)
      dot.hidden = true
      block()
      mask.hidden = false
    },
    hide() {
      mask.hidden = true
    },
    dispose() {
      window.removeEventListener('PageAgent::MovePointerTo', onMove)
      window.removeEventListener('PageAgent::EnablePassThrough', through)
      window.removeEventListener('PageAgent::DisablePassThrough', block)
      host.remove()
    },
  }
}

export function create(opts: PageControlOptions = {}) {
  const mask = createMask()
  const executor = createExecutor({
    hideOwnUi(hidden) {
      mask.host.style.display = hidden ? 'none' : ''
      opts.hideOwnUi?.(hidden)
    },
  })
  let running = 0

  async function run(cmd: PageCommand, signal?: AbortSignal): Promise<PageResult> {
    const busy = cmd.action !== 'state'
    if (busy && running++ === 0) mask.show()
    try {
      return await executor.run(cmd, signal)
    } finally {
      if (busy && --running === 0) mask.hide()
    }
  }

  return {
    run,
    dispose() {
      executor.dispose()
      mask.dispose()
    },
  }
}

declare global {
  interface Window {
    __graspPageControl?: { version: number; create: typeof create }
  }
}

window.__graspPageControl = { version: 1, create }
