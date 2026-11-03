import { nextTick, ref } from 'vue'

export function isChatNearBottom(el: HTMLElement, threshold: number): boolean {
  return el.scrollHeight - el.scrollTop - el.clientHeight <= threshold
}

/** Follow live replies until the reader scrolls up. Sending can rejoin the tail. */
export function useChatTailScroll(threshold = 80) {
  const scroller = ref<HTMLElement | null>(null)
  const followBottom = ref(true)

  function onScroll() {
    if (scroller.value) followBottom.value = isChatNearBottom(scroller.value, threshold)
  }

  function scrollDown(force = false) {
    if (force) followBottom.value = true
    void nextTick(() => {
      const el = scroller.value
      if (el && followBottom.value) el.scrollTop = el.scrollHeight
    })
  }

  return { scroller, followBottom, onScroll, scrollDown }
}
