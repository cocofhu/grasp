// @vitest-environment node
/**
 * The eslint block that keeps the clarify chat on the server dialogue state
 * (eslint.config.mjs). Runs the real config so a weakened rule fails here.
 */
import { fileURLToPath } from 'node:url'
import { ESLint } from 'eslint'
import { describe, expect, it } from 'vitest'

const cwd = fileURLToPath(new URL('../../..', import.meta.url))
const eslint = new ESLint({ cwd })

async function ruleIds(code: string, filePath = 'src/lib/inbox/useClarifyChat.ts') {
  const [result] = await eslint.lintText(code, { filePath })
  return result!.messages.map((m) => m.ruleId)
}

describe('clarify chat dialogue lint gate', () => {
  it('rejects consuming ACP / review frames again', async () => {
    const ids = await ruleIds(`
      export function onFrame(f: { type: string; event?: string }) {
        if (f.type === 'acp') return 1
        if (f.event === 'turn_done') return 2
        switch (f.event) { case 'queue_state': return 3 }
        return 0
      }
    `)
    expect(ids.filter((id) => id === 'no-restricted-syntax')).toHaveLength(3)
  })

  it('rejects the old frame and queue modules', async () => {
    const ids = await ruleIds(`
      import { createSessionQueue } from '@/lib/chat/sessionQueue'
      import { createReviewSessionState } from '@/lib/chat/reviewSessionState'
      import { mergeAcpEvents } from '../run/mergeAcpEvents'
      export const used = [createSessionQueue, createReviewSessionState, mergeAcpEvents]
    `)
    expect(ids.filter((id) => id === 'no-restricted-imports')).toHaveLength(3)
  })

  it('rejects inferring a failed turn from empty text and thought', async () => {
    const ids = await ruleIds(`
      export const failed = (t: { text?: string; thought?: string }) => !t.text && !t.thought
    `, 'src/lib/chat/dialogueState.ts')
    expect(ids).toContain('no-restricted-syntax')
  })

  it('covers ClarifyChat.vue and leaves other files alone', async () => {
    const vue = await ruleIds(`<script setup lang="ts">
const kind = (f: { type: string }) => f.type === 'acp'
defineExpose({ kind })
</script>
<template><div /></template>
`, 'src/components/run/ClarifyChat.vue')
    expect(vue).toContain('no-restricted-syntax')
    const gate = await ruleIds(`export const isAcp = (f: { type: string }) => f.type === 'acp'\n`, 'src/lib/inbox/useGateApproval.ts')
    expect(gate).not.toContain('no-restricted-syntax')
  })
})
