import '../src/styles/global.css'
import { createApp, h, ref } from 'vue'
import { RouterView, createMemoryHistory, createRouter } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import OnboardingWizard from '../src/components/onboarding/OnboardingWizard.vue'
import type { OnboardingMode } from '../src/lib/pm/onboardingWizard'
import { useAuth } from '../src/lib/composables/useAuth'

async function boot() {
  await initLocale()
  const params = new URLSearchParams(window.location.search)
  await setLocale(params.get('lang') === 'en' ? 'en' : 'zh-CN')
  setTheme('dark')

  useAuth().setUser({ username: 'onboarding', expiresAt: '2099-01-01T00:00:00Z', isAdmin: true })
  const mode: OnboardingMode = params.get('mode') === 'retry' ? 'retry' : 'firstInstall'

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      {
        path: '/workflows',
        component: {
          render: () => h('div', { 'data-testid': 'workflow-list-landing' }, 'agents'),
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
          // Shows the empty workspace CTA alongside the wizard.
          h(
            'p',
            { 'data-testid': 'onboarding-empty-desc' },
            String(i18n.global.t('pages.onboarding.emptyDesc')),
          ),
          h(OnboardingWizard, {
            open: open.value,
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
