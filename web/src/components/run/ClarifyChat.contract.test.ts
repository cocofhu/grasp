// @vitest-environment happy-dom
/**
 * The server contract fixtures drawn by the real ClarifyChat: what the user
 * sees after each frame. Expectations are written here, not generated.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { createI18n } from 'vue-i18n'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { clearDialogues, ingestDialogue } from '@/lib/chat/dialogueStore'
import type { DialogueState } from '@/lib/chat/dialogueState'
import ClarifyChat from './ClarifyChat.vue'

enableAutoUnmount(afterEach)
afterEach(() => clearDialogues())

type Fixture = { frames: DialogueState[] }

function frames(scenario: string): DialogueState[] {
  const file = join(__dirname, '../../lib/chat/__fixtures__/dialogue', `${scenario}.json`)
  return (JSON.parse(readFileSync(file, 'utf8')) as Fixture).frames
}

function mountChat() {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(ClarifyChat, {
    attachTo: document.body,
    props: { runId: 'run-fixture', nodeId: 'clarify', iteration: 1, turns: [], done: false, active: true, hideFinish: true },
    global: { plugins: [i18n], stubs: { Icon: true, ClarifyDemoFrame: true } },
  })
}

type Seen = {
  caret: boolean
  failCard: boolean
  failTitle?: string
  failDesc?: string
  retry?: boolean
  orphanBanner: boolean
  interrupted: boolean
  queue: string[]
  text: string[]
}

async function play(scenario: string, check: (seen: Seen, frame: number) => void) {
  const w = mountChat()
  const list = frames(scenario)
  for (let i = 0; i < list.length; i++) {
    expect(ingestDialogue(list[i]), `frame ${i} applied`).toBe(true)
    await flushPromises()
    const fail = w.find('[data-testid="clarify-empty-fail"]')
    const seen: Seen = {
      caret: w.find('[data-testid="clarify-stream-caret"]').exists(),
      failCard: fail.exists(),
      orphanBanner: w.find('[data-testid="clarify-sandbox-orphan"]').exists(),
      interrupted: w.find('[data-testid="clarify-interrupted"]').exists(),
      queue: w.findAll('[data-testid="clarify-queue-item"]').map((q) => q.text()),
      text: [w.text()],
    }
    if (fail.exists()) {
      seen.failTitle = w.find('[data-testid="clarify-empty-fail-title"]').text()
      seen.failDesc = w.find('[data-testid="clarify-empty-fail-desc"]').text()
      seen.retry = w.find('[data-testid="clarify-empty-fail-retry"]').exists()
    }
    check(seen, i)
  }
}

describe('ClarifyChat draws the server fixtures', () => {
  it('empty: streaming, then「本轮没有输出」with retry', async () => {
    await play('empty', (s, i) => {
      if (i === 0) {
        expect(s.failCard).toBe(false)
        return
      }
      expect(s.failCard).toBe(true)
      expect(s.failTitle).toContain('本轮没有输出')
      expect(s.failDesc).toContain('助手本轮没有返回正文')
      expect(s.retry).toBe(true)
      expect(s.caret).toBe(false)
    })
  })

  it('failed: the failure card shows the server error', async () => {
    await play('failed', (s, i) => {
      if (i === 0) return expect(s.failCard).toBe(false)
      expect(s.failCard).toBe(true)
      expect(s.failTitle).toContain('这一轮没有完成')
      expect(s.failDesc).toContain('sandbox gone')
      expect(s.retry).toBe(true)
    })
  })

  it('sandbox-flip: the reply keeps streaming and never fails while the platform runs it', async () => {
    await play('sandbox-flip', (s, i) => {
      expect(s.failCard, `frame ${i}`).toBe(false)
      expect(s.orphanBanner, `frame ${i}`).toBe(false)
      if (i < 3) {
        expect(s.caret, `frame ${i}`).toBe(true)
        expect(s.text[0]).toContain('字'.repeat(i + 1))
      } else {
        expect(s.caret).toBe(false)
        expect(s.text[0]).toContain('字字字')
      }
    })
  })

  it('running: caret while text streams, none once sealed ok', async () => {
    await play('running', (s, i) => {
      expect(s.failCard).toBe(false)
      expect(s.caret).toBe(i === 2)
      if (i === 2) expect(s.text[0]).toContain('登录页需要')
    })
  })

  it('interrupted: marked as interrupted, not as an empty failure', async () => {
    await play('interrupted', (s, i) => {
      expect(s.failCard).toBe(false)
      expect(s.interrupted).toBe(i === 1)
    })
  })

  it('orphan: banner while the sandbox runs on its own, gone after', async () => {
    await play('orphan', (s, i) => {
      expect(s.orphanBanner).toBe(i === 0)
      expect(s.failCard).toBe(false)
    })
  })

  it('queued: the waiting message is listed, then answered', async () => {
    await play('queued', (s, i) => {
      if (i === 0) expect(s.queue.join('|')).toContain('第二条')
      else expect(s.queue).toEqual([])
      expect(s.failCard).toBe(false)
    })
  })

  it('retry: the empty card goes away when the retry runs and ends ok', async () => {
    await play('retry', (s, i) => {
      expect(s.failCard).toBe(i === 0)
    })
  })
})
