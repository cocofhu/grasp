/**
 * Golden lock: every locale message string must compile under
 * @intlify/message-compiler (vue-i18n message format). Catches unescaped
 * interpolation syntax that would fail when a translation is rendered.
 *
 * plan coverage: g2.1 / g2.2 / g2.3 — full zh-CN+en JSON scan via `npm test`.
 */
import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { baseCompile, CompileErrorCodes } from '@intlify/message-compiler'

const LOCALES_ROOT = dirname(fileURLToPath(import.meta.url))
const LOCALE_DIRS = ['zh-CN', 'en'] as const

type Leaf = { keyPath: string; value: string }

function collectStringLeaves(node: unknown, prefix: string, out: Leaf[]): void {
  if (typeof node === 'string') {
    out.push({ keyPath: prefix, value: node })
    return
  }
  if (node && typeof node === 'object' && !Array.isArray(node)) {
    for (const [k, v] of Object.entries(node as Record<string, unknown>)) {
      collectStringLeaves(v, prefix ? `${prefix}.${k}` : k, out)
    }
    return
  }
  if (Array.isArray(node)) {
    node.forEach((item, i) => collectStringLeaves(item, `${prefix}[${i}]`, out))
  }
}

function loadAllLocaleLeaves(): Leaf[] {
  const leaves: Leaf[] = []
  for (const locale of LOCALE_DIRS) {
    const dir = join(LOCALES_ROOT, locale)
    for (const file of readdirSync(dir).filter((f) => f.endsWith('.json'))) {
      const json = JSON.parse(readFileSync(join(dir, file), 'utf8')) as unknown
      collectStringLeaves(json, `${locale}/${file}`, leaves)
    }
  }
  return leaves
}

function compileErrors(source: string): number[] {
  const codes: number[] = []
  baseCompile(source, {
    onError: (err) => {
      codes.push(err.code)
    },
  })
  return codes
}

describe('locale message-compiler scan', () => {
  it('baseCompile fails on unescaped ${GRASP_MEMORY_URL/TOKEN} (regression probe)', () => {
    const bad =
      '在 Agent Studio 添加 memory-store（${GRASP_MEMORY_URL/TOKEN}）。PM 咨询沙箱会自动注入。'
    const codes = compileErrors(bad)
    expect(codes.length).toBeGreaterThan(0)
    expect(codes).toContain(CompileErrorCodes.INVALID_TOKEN_IN_PLACEHOLDER)
  })

  it('compiles every zh-CN and en locale string without CompileError', () => {
    const leaves = loadAllLocaleLeaves()
    expect(leaves.length).toBeGreaterThan(1000)

    const failures: { keyPath: string; codes: number[]; preview: string }[] = []
    for (const leaf of leaves) {
      const codes = compileErrors(leaf.value)
      if (codes.length > 0) {
        failures.push({
          keyPath: leaf.keyPath,
          codes,
          preview: leaf.value.slice(0, 120),
        })
      }
    }

    expect(failures, JSON.stringify(failures, null, 2)).toEqual([])
  })

  it('memoryStore.convention uses literal interpolation and still displays ${GRASP_MEMORY_URL/TOKEN}', async () => {
    const leaves = loadAllLocaleLeaves()
    const conventions = leaves.filter((l) => l.keyPath.includes('memoryStore') && l.keyPath.endsWith('.convention'))
    expect(conventions).toHaveLength(2)
    for (const c of conventions) {
      expect(compileErrors(c.value)).toEqual([])
      // Source uses literal interpolation {'${...}'}
      expect(c.value).toContain("{'${GRASP_MEMORY_URL/TOKEN}'}")
    }

    // Runtime render (vue-i18n) must still show the env-var literal, not strip braces.
    const { createI18n } = await import('vue-i18n')
    const zh = conventions.find((c) => c.keyPath.startsWith('zh-CN/'))!.value
    const en = conventions.find((c) => c.keyPath.startsWith('en/'))!.value
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: {
        'zh-CN': { mcp: { memoryStore: { convention: zh } } },
        en: { mcp: { memoryStore: { convention: en } } },
      },
    })
    expect(String(i18n.global.t('mcp.memoryStore.convention'))).toContain('${GRASP_MEMORY_URL/TOKEN}')
    i18n.global.locale.value = 'en'
    expect(String(i18n.global.t('mcp.memoryStore.convention'))).toContain('${GRASP_MEMORY_URL/TOKEN}')
  })
})
