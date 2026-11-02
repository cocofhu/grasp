// Untracked preview entry for ReferencePicker tab counts.
import './src/lib/shared/locale'
import { createApp, defineComponent, h, ref } from 'vue'
import { i18n } from './src/lib/shared/i18n'
import './src/styles/global.css'
import './src/lib/shared/theme'
import ReferencePicker from './src/components/solo/ReferencePicker.vue'

const Root = defineComponent(() => {
  const query = ref('')
  return () => h('div', { style: 'max-width:560px;margin:340px auto 0;padding:0 16px' }, [
    h('div', { style: 'position:relative' }, [
      h(ReferencePicker, { scope: { sessionId: 'demo' }, query: query.value, taken: [], full: false }),
      h('input', {
        value: query.value,
        placeholder: '输入查询（含 err → GitHub 报错，含 big → 99+，#5 → 仅 GitHub）',
        style: 'width:100%;padding:10px 12px;border-radius:10px;border:1px solid rgb(var(--c-line-strong));background:rgb(var(--c-surface));color:rgb(var(--c-txt))',
        onInput: (e: Event) => (query.value = (e.target as HTMLInputElement).value),
      }),
    ]),
  ])
})

createApp(Root).use(i18n).mount('#app')
