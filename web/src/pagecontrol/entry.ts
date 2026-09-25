import { createExecutor, type PageCommand, type PageResult } from './executor'

export type PageControlOptions = {
  /** Hides the preview-pick UI (bar, drawer) while the page is read. */
  hideOwnUi?: (hidden: boolean) => void
}

// Arrow and ring shapes from page-agent's SimulatorMask (MIT, Alibaba Group).
const CURSOR_BORDER_SVG =
  "data:image/svg+xml,%3csvg%20xmlns='http://www.w3.org/2000/svg'%20viewBox='0%200%20100%20100'%20fill='none'%3e%3cg%3e%3cpath%20d='M%2015%2042%20L%2015%2036.99%20Q%2015%2031.99%2023.7%2031.99%20L%2028.05%2031.99%20Q%2032.41%2031.99%2032.41%2021.99%20L%2032.41%2017%20Q%2032.41%2012%2041.09%2016.95%20L%2076.31%2037.05%20Q%2085%2042%2076.31%2046.95%20L%2041.09%2067.05%20Q%2032.41%2072%2032.41%2062.01%20L%2032.41%2057.01%20Q%2032.41%2052.01%2023.7%2052.01%20L%2019.35%2052.01%20Q%2015%2052.01%2015%2047.01%20Z'%20fill='none'%20stroke='%23000000'%20stroke-width='6'%20stroke-miterlimit='10'%20style='stroke:%20light-dark(rgb(0,%200,%200),%20rgb(255,%20255,%20255));'/%3e%3c/g%3e%3c/svg%3e"
const CURSOR_FILL_SVG =
  "data:image/svg+xml,%3csvg%20xmlns='http://www.w3.org/2000/svg'%20viewBox='0%200%20100%20100'%3e%3cdefs%3e%3c/defs%3e%3cg%20xmlns='http://www.w3.org/2000/svg'%20style='filter:%20drop-shadow(light-dark(rgba(0,%200,%200,%200.4),%20rgba(237,%20237,%20237,%200.4))%203px%204px%204px);'%3e%3cpath%20d='M%2015%2042%20L%2015%2036.99%20Q%2015%2031.99%2023.7%2031.99%20L%2028.05%2031.99%20Q%2032.41%2031.99%2032.41%2021.99%20L%2032.41%2017%20Q%2032.41%2012%2041.09%2016.95%20L%2076.31%2037.05%20Q%2085%2042%2076.31%2046.95%20L%2041.09%2067.05%20Q%2032.41%2072%2032.41%2062.01%20L%2032.41%2057.01%20Q%2032.41%2052.01%2023.7%2052.01%20L%2019.35%2052.01%20Q%2015%2052.01%2015%2047.01%20Z'%20fill='%23ffffff'%20stroke='none'%20style='fill:%20%23ffffff;'/%3e%3c/g%3e%3c/svg%3e"

const MASK_CSS =
  ':host{all:initial}' +
  '.mask{position:fixed;inset:0;z-index:2147483645;cursor:progress;' +
  'box-shadow:inset 0 0 0 3px rgba(99,102,241,.75);background:transparent}' +
  '.mask.through{pointer-events:none}' +
  '.cursor{position:fixed;left:0;top:0;width:75px;height:75px;z-index:2147483646;pointer-events:none}' +
  '.border,.fill{position:absolute;width:100%;height:100%;transform-origin:center;' +
  'transform:rotate(-135deg) scale(1.2);margin-left:-10px;margin-top:-18px}' +
  '.border{background:linear-gradient(45deg,rgb(57,182,255),rgb(189,69,251));' +
  '-webkit-mask-image:url("' + CURSOR_BORDER_SVG + '");mask-image:url("' + CURSOR_BORDER_SVG + '");' +
  '-webkit-mask-size:100% 100%;mask-size:100% 100%;-webkit-mask-repeat:no-repeat;mask-repeat:no-repeat}' +
  '.fill{background:url("' + CURSOR_FILL_SVG + '") no-repeat;background-size:100% 100%}' +
  '.ripple{position:absolute;width:100%;height:100%;margin-left:-50%;margin-top:-50%}' +
  '.ripple::after{content:"";opacity:0;position:absolute;inset:0;border:4px solid rgb(57,182,255);border-radius:50%}' +
  '.cursor.clicking .ripple::after{animation:ripple .3s ease-out forwards}' +
  '@keyframes ripple{0%{transform:scale(0);opacity:1}100%{transform:scale(2);opacity:0}}' +
  '[hidden]{display:none!important}'

/**
 * The agent's pointer (shown while the user allows page control) and the
 * overlay that blocks user input while an action runs. PageController signals
 * pointer moves, clicks and hit-test pass-through via window events.
 */
function createMask() {
  const host = document.createElement('grasp-page-control')
  host.setAttribute('data-page-agent-not-interactive', '')
  const shadow = host.attachShadow({ mode: 'open' })
  shadow.innerHTML =
    `<style>${MASK_CSS}</style><div class="mask" hidden></div>` +
    '<div class="cursor" hidden><div class="ripple"></div><div class="fill"></div><div class="border"></div></div>'
  const mask = shadow.querySelector('.mask') as HTMLElement
  const cursor = shadow.querySelector('.cursor') as HTMLElement
  const pos = { x: 0, y: 0, tx: 0, ty: 0 }
  let frame = 0

  const place = () => {
    cursor.style.transform = `translate(${pos.x}px, ${pos.y}px)`
  }
  const step = () => {
    frame = 0
    pos.x += (pos.tx - pos.x) * 0.2
    pos.y += (pos.ty - pos.y) * 0.2
    if (Math.abs(pos.tx - pos.x) < 1 && Math.abs(pos.ty - pos.y) < 1) {
      pos.x = pos.tx
      pos.y = pos.ty
    } else {
      frame = requestAnimationFrame(step)
    }
    place()
  }
  const mount = () => {
    if (!host.isConnected) (document.body || document.documentElement).appendChild(host)
  }

  const onMove = (e: Event) => {
    const d = (e as CustomEvent<{ x: number; y: number }>).detail
    if (!d) return
    if (!cursor.hidden) mount()
    pos.tx = d.x
    pos.ty = d.y
    if (!frame) frame = requestAnimationFrame(step)
  }
  const onClick = () => {
    cursor.classList.remove('clicking')
    void cursor.offsetWidth
    cursor.classList.add('clicking')
  }
  const through = () => mask.classList.add('through')
  const block = () => mask.classList.remove('through')
  window.addEventListener('PageAgent::MovePointerTo', onMove)
  window.addEventListener('PageAgent::ClickPointer', onClick)
  window.addEventListener('PageAgent::EnablePassThrough', through)
  window.addEventListener('PageAgent::DisablePassThrough', block)

  return {
    host,
    /** Shows the pointer in the middle of the viewport, or hides it. */
    arm(on: boolean) {
      if (on && cursor.hidden) {
        mount()
        pos.x = pos.tx = window.innerWidth / 2
        pos.y = pos.ty = window.innerHeight / 2
        place()
      }
      cursor.hidden = !on
    },
    show() {
      mount()
      block()
      mask.hidden = false
    },
    hide() {
      mask.hidden = true
    },
    dispose() {
      cancelAnimationFrame(frame)
      window.removeEventListener('PageAgent::MovePointerTo', onMove)
      window.removeEventListener('PageAgent::ClickPointer', onClick)
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
    /** Called when the user turns page control on or off. */
    setArmed(on: boolean) {
      mask.arm(on)
    },
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
