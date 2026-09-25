import pluginVue from 'eslint-plugin-vue'
import { defineConfigWithVueTs, vueTsConfigs } from '@vue/eslint-config-typescript'

// First-pass: essential Vue + recommended TS (not type-aware). High-noise
// rules are off so CI can gate on errors without a huge backlog.
export default defineConfigWithVueTs(
  {
    ignores: ['dist/**', 'coverage/**', 'e2e/**', 'node_modules/**', 'public/page-control.js'],
  },
  pluginVue.configs['flat/essential'],
  vueTsConfigs.recommended,
  {
    rules: {
      '@typescript-eslint/no-explicit-any': 'off',
      '@typescript-eslint/no-unused-vars': 'off',
      '@typescript-eslint/no-this-alias': 'off',
      'vue/multi-word-component-names': 'off',
      'vue/no-mutating-props': 'off',
    },
  },
  // Dead-symbol anti-churn: warn only under src/lib (not views). Keep as warn —
  // do not promote to error or enable for views/knip.
  {
    files: ['src/lib/**/*.{ts,vue}'],
    rules: {
      '@typescript-eslint/no-unused-vars': [
        'warn',
        {
          argsIgnorePattern: '^_',
          varsIgnorePattern: '^_',
          caughtErrorsIgnorePattern: '^_',
        },
      ],
    },
  },
)
