import '../src/styles/global.css'
import { createApp, defineComponent, h, onBeforeUnmount, onMounted, ref } from 'vue'
import Icon from '../src/components/ui/Icon.vue'
import { iconDefinitions, iconNames } from '../src/lib/icons/catalog'
import { installRuntimeIconMotion, runtimeIcon } from '../src/lib/icons/runtime'

const nativeMarkup = `
  <h2>Native runtime icons</h2>
  <button data-testid="native-before">Before keyboard target</button>
  <button data-testid="native-keyboard">${runtimeIcon('copy')} Keyboard</button>
  <button data-testid="native-disabled" disabled>${runtimeIcon('trash')} Disabled</button>
  <div data-testid="native-outer" data-icon-host style="padding:20px;border:1px solid #aaa">
    ${runtimeIcon('check-draw')}
    <button data-testid="native-inner">${runtimeIcon('copy')} Nested</button>
  </div>
  <button data-testid="native-busy">${runtimeIcon('arc-spinner', 16, { busy: true })} Actual busy</button>
`

const App = defineComponent({
  setup() {
    const busy = ref(false)
    const revision = ref(0)
    const native = ref<HTMLElement | null>(null)
    let disposeNative: (() => void) | undefined
    onMounted(() => { if (native.value) disposeNative = installRuntimeIconMotion(native.value) })
    onBeforeUnmount(() => disposeNative?.())
    return () => h('main', { style: 'padding:24px;max-width:960px;margin:auto;color:var(--color-text,#4c4458)' }, [
      h('h1', 'Icon motion'),
      h('div', { 'data-testid': 'dynamic-wrapper' }, [
        h('button', { 'data-testid': 'keyboard-host', style: 'padding:16px' }, [h(Icon, { name: 'copy', busy: false }), 'Keyboard']),
      ]),
      h('button', { 'data-testid': 'disabled-host', disabled: true }, [h(Icon, { name: 'trash' }), 'Disabled']),
      h('div', { 'data-testid': 'outer-host', 'data-icon-motion-host': '', style: 'padding:16px;border:1px solid #aaa;display:flex;gap:24px' }, [
        h(Icon, { name: 'check-draw', 'data-testid': 'outer-icon' }),
        h('button', { 'data-testid': 'inner-host' }, [h(Icon, { name: 'copy' }), 'Inner button']),
      ]),
      h('div', { 'data-testid': 'readonly', style: 'padding:16px' }, [h(Icon, { name: 'folder' }), 'Read-only metadata']),
      h('button', { 'data-testid': 'toggle-busy', onClick: () => { busy.value = !busy.value } }, 'Toggle busy'),
      h('button', { 'data-testid': 'busy-host' }, [h(Icon, { name: 'arc-spinner', busy: busy.value }), 'Live loading']),
      h('button', { 'data-testid': 'new-event', onClick: () => { revision.value++ } }, 'New notification'),
      h('span', { 'data-testid': 'event-bell' }, [h(Icon, { name: 'bell', motionKey: revision.value })]),
      h('span', { 'data-testid': 'event-check' }, [h(Icon, { name: 'check-draw', motionKey: revision.value })]),
      h('div', { 'data-testid': 'all-icons', style: 'display:grid;grid-template-columns:repeat(auto-fit,minmax(110px,1fr));gap:12px;margin-top:24px' },
        iconNames.map(name => h('button', {
          'data-testid': `icon-${name}`,
          'data-duration': iconDefinitions[name].duration,
          style: 'display:flex;align-items:center;justify-content:center;flex-direction:column;gap:8px;padding:16px;min-height:76px;border:1px solid #b7aec7;border-radius:8px;font-size:12px',
        }, [h(Icon, { name, size: 24, busy: false }), h('span', name)])),
      ),
      h('section', { ref: native, 'data-testid': 'native-icons', innerHTML: nativeMarkup }),
    ])
  },
})

createApp(App).mount('#app')
