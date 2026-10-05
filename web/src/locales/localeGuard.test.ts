import { describe, expect, it } from 'vitest'

const zhFiles = import.meta.glob<Record<string, unknown>>('./zh-CN/*.json', { eager: true, import: 'default' })
const enFiles = import.meta.glob<Record<string, unknown>>('./en/*.json', { eager: true, import: 'default' })

const CJK = /[\u3400-\u9fff\uff00-\uffef]/
const ENV_TOKEN = /\b[A-Z][A-Z0-9]*_[A-Z0-9_]+\b/

/** Every leaf as [dotted key, value]. */
function leaves(obj: unknown, prefix = ''): [string, string][] {
  if (typeof obj === 'string') return [[prefix, obj]]
  if (!obj || typeof obj !== 'object') return []
  return Object.entries(obj as Record<string, unknown>).flatMap(([k, v]) => leaves(v, prefix ? `${prefix}.${k}` : k))
}

function merged(files: Record<string, Record<string, unknown>>): [string, string][] {
  return Object.values(files).flatMap((f) => leaves(f))
}

const en = merged(enFiles)
const zh = merged(zhFiles)

/** English values that intentionally contain Chinese: the bilingual language picker, a brand name, a literal agents must write. */
const CJK_ALLOWED = new Set([
  'shell.langSelect',
  'pages.projectDetail.pm.channel.badgeFeishu',
  'pages.projectDetail.pm.channelBadgeFeishu',
  'mcp.artifactStore.tools.set_plan.desc',
])

describe('locale guard', () => {
  it('zh-CN and en have the same keys', () => {
    const zhKeys = new Set(zh.map(([k]) => k))
    const enKeys = new Set(en.map(([k]) => k))
    expect([...zhKeys].filter((k) => !enKeys.has(k))).toEqual([])
    expect([...enKeys].filter((k) => !zhKeys.has(k))).toEqual([])
  })

  it('English copy has no Chinese text', () => {
    expect(en.length).toBeGreaterThan(1000)
    const bad = en.filter(([k, v]) => !CJK_ALLOWED.has(k) && CJK.test(v))
    expect(bad).toEqual([])
  })

  it('onboarding and the create wizard never show environment variable names', () => {
    const scoped = (rows: [string, string][]) =>
      rows.filter(([k, v]) => (k.startsWith('pages.onboarding.') || k.startsWith('pages.agentStudio.wizard.apiKey.')) && ENV_TOKEN.test(v))
    expect(scoped(en)).toEqual([])
    expect(scoped(zh)).toEqual([])
  })
})
