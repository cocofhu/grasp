<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import BrandLogo from '@/components/shell/BrandLogo.vue'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import { authApi } from '@/lib/api/api'
import { useAuth } from '@/lib/composables/useAuth'
import { authRedirectPath } from '@/lib/composables/useAuth'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { setUser, user, ready } = useAuth()

const username = ref('')
const password = ref('')
const error = ref('')
const rateLimited = ref(false)
const loading = ref(false)
const fieldError = ref(false)

const redirectTarget = computed(() => authRedirectPath(String(route.query.redirect || '')))
/** Brand-only until session probe finishes — avoids flashing the login form to logged-in users. */
const showLoginForm = computed(() => ready.value && !user.value)

if (user.value) {
  router.replace(redirectTarget.value)
}

async function onSubmit() {
  error.value = ''
  rateLimited.value = false
  fieldError.value = false
  loading.value = true
  try {
    const res = await authApi.login(username.value.trim(), password.value, redirectTarget.value)
    setUser({ username: res.username, expiresAt: res.expires_at })
    await router.replace(authRedirectPath(res.redirect || redirectTarget.value))
  } catch (e: any) {
    const msg = e?.message || t('pages.login.loginFailed')
    if (msg.includes('429') || msg.includes('过于频繁') || msg.includes('Too many')) {
      rateLimited.value = true
    } else {
      error.value = t('pages.login.badCredentials')
      fieldError.value = true
    }
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="flex min-h-full items-center justify-center bg-base px-4 py-8 [background:radial-gradient(ellipse_80%_60%_at_50%_0%,rgba(123,97,255,.08),transparent),rgb(var(--c-base))]">
    <div class="w-full max-w-[400px] overflow-hidden rounded-xl border border-line bg-surface p-7 shadow-card">
      <div class="mb-7 flex flex-col items-center text-center">
        <BrandLogo size="lg" align="center" />
      </div>

      <template v-if="showLoginForm">
        <div class="mb-1 text-[15px] font-semibold text-txt">{{ t('pages.login.title') }}</div>
        <div class="mb-6 text-[12px] text-txt3">{{ t('pages.login.subtitle') }}</div>

        <div
          v-if="route.query.redirect"
          class="mb-4 flex items-center gap-1.5 rounded-lg border border-info/25 bg-info/10 px-2.5 py-2 text-xs text-info"
        >
          <Icon name="chevron-right" :size="14" class="rotate-[-45deg]" />
          {{ t('pages.login.redirectHint') }} <code class="font-mono text-[11px] text-txt2">{{ redirectTarget }}</code>
        </div>

        <div v-if="error" class="mb-4 rounded-lg border border-err/30 bg-err/10 px-3 py-2.5 text-[13px] text-err">{{ error }}</div>
        <div v-if="rateLimited" class="mb-4 rounded-lg border border-warn/30 bg-warn/10 px-3 py-2.5 text-[13px] text-warn">
          {{ t('pages.login.rateLimited') }}
        </div>

        <form @submit.prevent="onSubmit">
          <div class="mb-4">
            <label class="mb-1.5 block text-xs font-medium text-txt2" for="username">{{ t('pages.login.username') }}</label>
            <input
              id="username"
              v-model="username"
              type="text"
              autocomplete="username"
              placeholder="admin"
              class="w-full rounded border border-line bg-base px-3 py-2 text-sm text-txt outline-none transition focus:border-accent focus:shadow-[0_0_0_2px_rgba(123,97,255,.3)]"
              :class="fieldError ? 'border-err' : ''"
            />
          </div>
          <div class="mb-4">
            <label class="mb-1.5 block text-xs font-medium text-txt2" for="password">{{ t('pages.login.password') }}</label>
            <input
              id="password"
              v-model="password"
              type="password"
              autocomplete="current-password"
              placeholder="••••••••"
              class="w-full rounded border border-line bg-base px-3 py-2 text-sm text-txt outline-none transition focus:border-accent focus:shadow-[0_0_0_2px_rgba(123,97,255,.3)]"
              :class="fieldError ? 'border-err' : ''"
            />
          </div>
          <AppButton type="submit" variant="primary" block :disabled="loading">
            {{ loading ? t('pages.login.submitting') : t('pages.login.submit') }}
          </AppButton>
        </form>
      </template>
    </div>
  </div>
</template>
