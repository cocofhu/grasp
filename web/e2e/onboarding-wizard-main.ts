import '../src/styles/global.css'
import { createApp, h, ref } from 'vue'
import { RouterView, createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import OnboardingWizard from '../src/components/onboarding/OnboardingWizard.vue'
import type { OnboardingMode } from '../src/lib/pm/onboardingWizard'
import { DEFAULT_PROJECT_ID } from '../src/lib/pm/onboardingWizard'

async function boot() {
  await initLocale()
  const params = new URLSearchParams(window.location.search)
  await setLocale(params.get('lang') === 'en' ? 'en' : 'zh-CN')
  setTheme('dark')

  const mode = (params.get('mode') === 'createProject' ? 'createProject' : 'firstInstall') as OnboardingMode
  const projectId = mode === 'createProject' ? '' : DEFAULT_PROJECT_ID

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      {
        path: '/projects/:id',
        component: {
          render: () => h('div', { 'data-testid': 'project-agents-landing' }, 'agents'),
        },
      },
      {
        path: '/workflows/:id/edit',
        component: {
          render: () => h('div', { 'data-testid': 'workflow-editor-landing' }, 'editor'),
        },
      },
    ],
  })
  await router.push('/')

  const open = ref(true)
  const app = createApp({
    setup() {
      return () =>
        h('div', [
          h(RouterView),
          // Mirrors ProjectDetailView empty CTA copy for shell i18n e2e assertions.
          h(
            'p',
            { 'data-testid': 'onboarding-empty-desc' },
            String(i18n.global.t('pages.onboarding.emptyDesc')),
          ),
          h(OnboardingWizard, {
            open: open.value,
            projectId,
            mode,
            onClose: () => {
              open.value = false
            },
          }),
        ])
    },
  })
  app.use(i18n)
  app.use(router)
  app.mount('#app')
  document.getElementById('app')?.setAttribute('data-testid', 'onboarding-wizard-root')
}

void boot()
