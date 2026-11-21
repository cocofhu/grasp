import { getCurrentScope, onScopeDispose, ref, watch } from 'vue'
import { api } from '@/lib/api/api'

/** Retry interval while the sandbox cannot answer the desktop status yet. */
export const DESKTOP_IMAGE_RETRY_MS = 3_000
/** Gives up after ~5 minutes: a stopped sandbox or an old runtime. */
export const DESKTOP_IMAGE_MAX_TRIES = 100

/**
 * Whether the sandbox runs the desktop image. A starting sandbox fails the
 * status request, so it is retried until the sandbox answers, and asked again
 * whenever `refreshKey` changes (the sandbox finished booting). `onBrowser`
 * runs when the sandbox reports the browser image.
 */
export function useDesktopImage(
  sandboxId: () => number | null | undefined,
  opts: { enabled?: () => boolean; refreshKey?: () => unknown; onBrowser?: () => void } = {},
) {
  const desktopImage = ref(false)
  let timer: ReturnType<typeof setTimeout> | undefined
  let gen = 0

  function stop() {
    if (timer) clearTimeout(timer)
    timer = undefined
    gen++
  }

  async function probe(id: number, g: number, tries: number) {
    try {
      const st = await api.sandboxDesktop(id)
      if (g !== gen) return
      desktopImage.value = st.mode === 'desktop'
      if (!desktopImage.value) opts.onBrowser?.()
    } catch {
      if (g !== gen || tries + 1 >= DESKTOP_IMAGE_MAX_TRIES) return
      timer = setTimeout(() => void probe(id, g, tries + 1), DESKTOP_IMAGE_RETRY_MS)
    }
  }

  watch(
    () => (opts.enabled?.() ?? true ? sandboxId() : null),
    (id) => {
      stop()
      desktopImage.value = false
      if (id) void probe(id, gen, 0)
    },
    { immediate: true },
  )

  watch(
    () => opts.refreshKey?.(),
    () => {
      const id = opts.enabled?.() ?? true ? sandboxId() : null
      if (!id) return
      stop()
      void probe(id, gen, 0)
    },
  )

  if (getCurrentScope()) onScopeDispose(stop)

  return { desktopImage }
}
