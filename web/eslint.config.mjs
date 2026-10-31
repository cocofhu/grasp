import pluginVue from 'eslint-plugin-vue'
import { defineConfigWithVueTs, vueTsConfigs } from '@vue/eslint-config-typescript'

// First-pass: essential Vue + recommended TS (not type-aware). High-noise
// rules are off so CI can gate on errors without a huge backlog.
export default defineConfigWithVueTs(
  {
    ignores: ['dist/**', 'coverage/**', 'e2e/**', 'node_modules/**', 'public/page-control.js', 'public/live-overlay.js'],
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
  // The clarify chat draws only the server's dialogue state. These files must
  // not go back to reading ACP / review frames or guessing a failed turn from
  // empty text: a wrong chat is fixed in the server's state, not here.
  // dialogueLintGate.test.ts checks this block still rejects such code.
  {
    files: [
      'src/components/run/ClarifyChat.vue',
      'src/lib/inbox/useClarifyChat.ts',
      'src/lib/chat/dialogueState.ts',
      'src/lib/chat/dialogueStore.ts',
      'src/lib/chat/dialogueOverlay.ts',
      'src/lib/inbox/clarifyReplyText.ts',
    ],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            {
              regex: '(^|/)(sessionQueue|reviewSessionState|acpUnpack|mergeAcpEvents|applyLiveWsAcpPage|dialogueAcpDelivery|pendingAcpBuffer|liveLogSnapshotCache|acpFailure|mergeClarifyLiveTurns)$',
              message: 'The clarify chat reads only dialogueStore; do not consume session/ACP frames here.',
            },
          ],
        },
      ],
      'no-restricted-syntax': [
        'error',
        {
          selector: "BinaryExpression[operator=/^[!=]==?$/] > Literal[value=/^(acp|queue_state|turn_begin|turn_done)$/]",
          message: 'The clarify chat does not handle ACP / review frames; the server dialogue state already applied them.',
        },
        {
          selector: "SwitchCase > Literal.test[value=/^(acp|queue_state|turn_begin|turn_done)$/]",
          message: 'The clarify chat does not handle ACP / review frames; the server dialogue state already applied them.',
        },
        {
          selector:
            "LogicalExpression:has(UnaryExpression[operator='!'] > MemberExpression.argument[property.name='text']):has(UnaryExpression[operator='!'] > MemberExpression.argument[property.name='thought'])",
          message: 'A failed or empty turn is row.outcome (empty / failed); do not infer it from missing text or thought.',
        },
      ],
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
