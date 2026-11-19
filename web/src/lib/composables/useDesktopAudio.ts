import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { api } from '@/lib/api/api'
import { DesktopAudioPlayer, type DesktopAudioDeps, type DesktopAudioState } from '@/lib/shared/desktopAudio'

/**
 * Desktop sound beside a VNC panel. Off until the viewer turns it on (browsers
 * only start audio from a click); stops when the sandbox changes or the panel
 * unmounts. Public share tickets pass undefined: they have no sound route.
 */
export function useDesktopAudio(sandboxId: () => number | undefined, deps?: DesktopAudioDeps) {
  const state = ref<DesktopAudioState>('off')
  const available = computed(() => (sandboxId() ?? 0) > 0)
  let player: DesktopAudioPlayer | null = null

  function stop() {
    const p = player
    player = null
    p?.stop()
    state.value = 'off'
  }

  function start() {
    const id = sandboxId()
    if (!id || id <= 0) return
    stop()
    const p = new DesktopAudioPlayer(api.sandboxAudioWsUrl(id), (s) => {
      if (player === p) state.value = s
    }, deps)
    player = p
    p.start()
  }

  function toggle() {
    if (state.value === 'on' || state.value === 'connecting') stop()
    else start()
  }

  watch(sandboxId, stop)
  onBeforeUnmount(stop)

  return { state, available, toggle, stop }
}
