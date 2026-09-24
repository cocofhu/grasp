import '../src/styles/global.css'
import { createApp, h } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import { setTheme } from '../src/lib/shared/theme'
import { vHoverInk } from '../src/lib/shared/hoverInkDirective'
import RequirementDraftsPanel from '../src/components/project/RequirementDraftsPanel.vue'

installIdleScrollbar()

async function bootstrap() {
  const params = new URLSearchParams(window.location.search)
  await initLocale()
  await setLocale(params.get('lang') === 'en' ? 'en' : 'zh-CN')
  if (params.get('theme') === 'light') setTheme('light')

  const projectId = params.get('projectId') || 'proj-1'

  createApp({
    render: () => h(RequirementDraftsPanel, { projectId }),
  })
    .directive('hover-ink', vHoverInk)
    .use(i18n)
    .mount('#app')
}

void bootstrap()
