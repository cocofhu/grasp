import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import { vHoverInk } from '../src/lib/shared/hoverInkDirective'
import ConfirmFlowDialog from '../src/components/run/ConfirmFlowDialog.vue'
import {
  armSidebarHandoff,
  decideSidebarFlight,
  holdSidebarConfirmWindow,
  isSidebarConfirmWindowHeld,
  releaseSidebarConfirmWindow,
  type SidebarFlightDecision,
} from '../src/lib/inbox/sidebarHandoff'

const RUN_ID = 'run-leave'

interface ConfirmLeaveProbe {
  visualCloseCount: number
  atSignal: { dialogConnected: boolean } | null
  decision: SidebarFlightDecision | null
  isHeld: () => boolean
}

const probe: ConfirmLeaveProbe = {
  visualCloseCount: 0,
  atSignal: null,
  decision: null,
  isHeld: () => isSidebarConfirmWindowHeld(RUN_ID),
}

;(window as Window & { __confirmLeave: ConfirmLeaveProbe }).__confirmLeave = probe

const Fixture = defineComponent({
  name: 'ConfirmFlowLeaveFixture',
  setup() {
    const open = ref(true)
    function onConfirm() {
      // Same order as clarify / approval: hold and arm, then the parent closes.
      holdSidebarConfirmWindow(RUN_ID)
      armSidebarHandoff(RUN_ID, 'clarify', Date.now(), { nodeId: 'auto-1', name: '实现' })
      open.value = false
    }
    function onCancel() {
      open.value = false
    }
    function onVisualClose() {
      const dialog = document.querySelector('[role="dialog"]')
      probe.atSignal = { dialogConnected: !!dialog && (dialog as HTMLElement).isConnected }
      probe.visualCloseCount += 1
      releaseSidebarConfirmWindow(RUN_ID)
      probe.decision = decideSidebarFlight({
        previous: 'running',
        next: 'auto',
        handoffArmed: true,
        confirmedAuto: true,
        reducedMotion: window.matchMedia('(prefers-reduced-motion: reduce)').matches,
        targetCollapsed: false,
        confirmWindowHeld: isSidebarConfirmWindowHeld(RUN_ID),
      })
    }
    return () =>
      h(ConfirmFlowDialog, {
        open: open.value,
        discard: 'none',
        onConfirm,
        onCancel,
        onVisualClose,
      })
  },
})

async function boot() {
  await initLocale()
  await setLocale('zh-CN')
  setTheme('dark')
  const app = createApp(Fixture)
  app.use(i18n)
  app.directive('hover-ink', vHoverInk)
  app.mount('#app')
}

void boot()
