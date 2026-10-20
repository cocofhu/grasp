import { onBeforeUnmount, onMounted, ref, type Ref } from 'vue'
import { api, type PlatformStatusMetrics } from '@/lib/api/api'
import { clientTimezoneParams } from '@/components/token-analytics/tokenStatsShared'

/** Visible-tab poll interval for StatusMetrics (within 10–15s SLA). */
export const PLATFORM_STATUS_POLL_MS = 12_000

export type UsePlatformStatusMetrics = {
  metrics: Ref<PlatformStatusMetrics | null>
  stale: Ref<boolean>
  error: Ref<string | null>
  refresh: () => Promise<void>
  start: () => void
  stop: () => void
}

/**
 * Visibility-aware poller for GET /stats/platform-status.
 * - ~12s while document.visible; truly pauses when hidden
 * - on visible: immediate fetch + restart interval
 * - failures keep lastSuccess (stale=true); never flash tokens to 0
 */
export function usePlatformStatusMetrics(): UsePlatformStatusMetrics {
  const metrics = ref<PlatformStatusMetrics | null>(null)
  const stale = ref(false)
  const error = ref<string | null>(null)
  let timer: ReturnType<typeof setInterval> | null = null
  let inFlight = false

  async function refresh() {
    if (typeof document !== 'undefined' && document.hidden) return
    if (inFlight) return
    inFlight = true
    try {
      const tz = clientTimezoneParams()
      const next = await api.platformStatus({
        timezone: tz.timezone,
        utcOffsetMinutes: tz.utcOffsetMinutes,
      })
      metrics.value = next
      stale.value = false
      error.value = null
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
      // Keep lastSuccess; mark stale so UI may show a weak hint.
      if (metrics.value != null) stale.value = true
    } finally {
      inFlight = false
    }
  }

  function stop() {
    if (timer != null) {
      clearInterval(timer)
      timer = null
    }
  }

  function start() {
    stop()
    if (typeof document !== 'undefined' && document.hidden) return
    void refresh()
    timer = setInterval(() => {
      void refresh()
    }, PLATFORM_STATUS_POLL_MS)
  }

  function onVisibility() {
    if (document.hidden) {
      stop()
      return
    }
    start()
  }

  onMounted(() => {
    start()
    document.addEventListener('visibilitychange', onVisibility)
  })

  onBeforeUnmount(() => {
    stop()
    document.removeEventListener('visibilitychange', onVisibility)
  })

  return { metrics, stale, error, refresh, start, stop }
}
