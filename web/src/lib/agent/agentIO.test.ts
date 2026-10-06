import { describe, expect, it } from 'vitest'
import {
  peekAgentZipName,
  peekZipPackage,
  resolveImportName,
  suggestRename,
  validateAgentName,
  normalizeAgentName,
} from './agentIO'

/** ZIP produced by Go archive/zip (agent.json stored; rules/*.md may be deflate). */
const GO_EXPORT_ZIP_B64 =
  'UEsDBBQACAAAAAAAAAAAAAAAAAAAAAAAAAAKAAAAYWdlbnQuanNvbnsKICAibmFtZSI6ICJwZWVrX3Rlc3QiLAogICJtY3AiOiBbCiAgICB7CiAgICAgICJuYW1lIjogImFydGlmYWN0LXN0b3JlIiwKICAgICAgInVybCI6ICIke0NPREVGTE9XX0FSVElGQUNUX1VSTH0iLAogICAgICAiaGVhZGVycyI6IHsKICAgICAgICAiQXV0aG9yaXphdGlvbiI6ICJCZWFyZXIgJHtDT0RFRkxPV19BUlRJRkFDVF9UT0tFTn0iCiAgICAgIH0KICAgIH0KICBdLAogICJsYXlvdXQiOiB7CiAgICAiY29uZmlnUm9vdCI6ICIvcm9vdC8uY3Vyc29yIiwKICAgICJ3b3Jrc3BhY2VEaXIiOiAiL3Jvb3Qvd29ya3NwYWNlIgogIH0sCiAgInNjaGVtYVZlcnNpb24iOiAxLAogICJleHBvcnRlZEF0IjogIjIwMjYtMDctMDlUMTE6MTU6MDRaIgp9UEsHCMzkgm5wAQAAcAEAAFBLAwQUAAgACAAAAAAAAAAAAAAAAAAAAAAACgAAAHJ1bGVzL2EubWRSVkgEBAAA//9QSwcIx1R7agkAAAADAAAAUEsBAhQAFAAIAAAAAAAAAMzkgm5wAQAAcAEAAAoAAAAAAAAAAAAAAAAAAAAAAGFnZW50Lmpzb25QSwECFAAUAAgACAAAAAAAx1R7agkAAAADAAAACgAAAAAAAAAAAAAAAACoAQAAcnVsZXMvYS5tZFBLBQYAAAAAAgACAHAAAADpAQAAAAA='

function goExportFile(): File {
  const bytes = Uint8Array.from(atob(GO_EXPORT_ZIP_B64), (c) => c.charCodeAt(0))
  return new File([bytes], 'ignored.zip', { type: 'application/zip' })
}

describe('validateAgentName', () => {
  it('accepts platform ASCII names', () => {
    expect(validateAgentName('Agent_1-test')).toBe('')
  })

  it('accepts Demo screenshot Chinese mix sample', () => {
    expect(validateAgentName('Approve需求澄清视觉研发')).toBe('')
  })

  it('accepts mixed hyphen Chinese', () => {
    expect(validateAgentName('Approve-需求澄清')).toBe('')
  })

  it('rejects invalid names (Demo illegal samples)', () => {
    expect(validateAgentName('bad name')).toBe('invalid')
    expect(validateAgentName('Approve 需求')).toBe('invalid')
    expect(validateAgentName('clarify.v1')).toBe('invalid')
    expect(validateAgentName('需求－澄清')).toBe('invalid')
    expect(validateAgentName('a/b')).toBe('invalid')
    expect(validateAgentName('')).toBe('required')
    expect(validateAgentName('   ')).toBe('required')
  })

  it('rejects over 64 runes', () => {
    expect(validateAgentName('中'.repeat(65))).toBe('invalid')
    expect(validateAgentName('中'.repeat(64))).toBe('')
  })

  it('NFC-normalizes before validate and normalizeAgentName', () => {
    const decomposed = 'A\u0301gent'
    expect(validateAgentName(decomposed)).toBe('')
    expect(normalizeAgentName(decomposed)).toBe('Ágent')
    expect(normalizeAgentName(decomposed)).toBe(normalizeAgentName('Ágent'))
  })
})

describe('resolveImportName', () => {
  it('prefers agent.json name over zip filename', () => {
    expect(resolveImportName('from_json', 'other.zip')).toBe('from_json')
  })

  it('falls back to zip filename', () => {
    expect(resolveImportName(undefined, 'fallback.zip')).toBe('fallback')
  })

  it('supports Chinese names from agent.json', () => {
    expect(resolveImportName('视觉研发助手', 'x.zip')).toBe('视觉研发助手')
    expect(validateAgentName('视觉研发助手')).toBe('')
  })
})

describe('suggestRename', () => {
  it('picks first free _vN suffix', () => {
    expect(suggestRename('agent', ['agent', 'agent_v2'])).toBe('agent_v3')
  })

  it('works with Chinese base names', () => {
    expect(suggestRename('视觉研发', ['视觉研发'])).toBe('视觉研发_v2')
    expect(validateAgentName(suggestRename('视觉研发', ['视觉研发']))).toBe('')
  })
})

describe('peekAgentZipName', () => {
  it('reads name from Go-exported ZIP without hanging', async () => {
    const result = await Promise.race([
      peekAgentZipName(goExportFile()),
      new Promise<never>((_, reject) => setTimeout(() => reject(new Error('timeout')), 3000)),
    ])
    expect(result.error).toBeUndefined()
    expect(result.name).toBe('peek_test')
  })

  it('reports missing agent.json', async () => {
    const empty = new Uint8Array([0x50, 0x4b, 0x05, 0x06, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0])
    const file = new File([empty], 'empty.zip', { type: 'application/zip' })
    const result = await peekAgentZipName(file)
    expect(result.error).toBe('missing agent.json')
  })
})

function crc32(data: Uint8Array): number {
  let c = ~0 >>> 0
  for (let i = 0; i < data.length; i++) {
    c ^= data[i]
    for (let k = 0; k < 8; k++) c = (c >>> 1) ^ (0xedb88320 & -(c & 1))
  }
  return ~c >>> 0
}

function u16(n: number) {
  const b = new Uint8Array(2)
  new DataView(b.buffer).setUint16(0, n, true)
  return b
}

function u32(n: number) {
  const b = new Uint8Array(4)
  new DataView(b.buffer).setUint32(0, n, true)
  return b
}

function concat(...parts: Uint8Array[]) {
  const out = new Uint8Array(parts.reduce((s, p) => s + p.length, 0))
  let o = 0
  for (const p of parts) {
    out.set(p, o)
    o += p.length
  }
  return out
}

function storeZip(files: Record<string, string>): Uint8Array {
  const locals: Uint8Array[] = []
  const centrals: Uint8Array[] = []
  let offset = 0
  for (const [name, text] of Object.entries(files)) {
    const nameB = new TextEncoder().encode(name)
    const data = new TextEncoder().encode(text)
    const crc = crc32(data)
    const local = concat(
      new Uint8Array([0x50, 0x4b, 0x03, 0x04, 0x14, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00]),
      u32(crc),
      u32(data.length),
      u32(data.length),
      u16(nameB.length),
      u16(0),
      nameB,
      data,
    )
    locals.push(local)
    centrals.push(
      concat(
        new Uint8Array([0x50, 0x4b, 0x01, 0x02, 0x14, 0x00, 0x14, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00]),
        u32(crc),
        u32(data.length),
        u32(data.length),
        u16(nameB.length),
        u16(0),
        u16(0),
        u16(0),
        u16(0),
        u32(0),
        u32(offset),
        nameB,
      ),
    )
    offset += local.length
  }
  const localAll = concat(...locals)
  const centralAll = concat(...centrals)
  return concat(
    localAll,
    centralAll,
    new Uint8Array([0x50, 0x4b, 0x05, 0x06, 0x00, 0x00, 0x00, 0x00]),
    u16(locals.length),
    u16(locals.length),
    u32(centralAll.length),
    u32(localAll.length),
    u16(0),
  )
}

describe('peekZipPackage', () => {
  it('prefers project.json project bundle over agent.json', async () => {
    const raw = storeZip({
      'project.json': JSON.stringify({
        kind: 'project-agents',
        schemaVersion: 1,
        agentNames: ['alice', 'bob'],
      }),
      'agent.json': JSON.stringify({ name: 'should-not-win', schemaVersion: 1 }),
    })
    const peek = await peekZipPackage(new File([Uint8Array.from(raw)], 'demo-agents.zip', { type: 'application/zip' }))
    expect(peek).toEqual({ kind: 'project-bundle', agentNames: ['alice', 'bob'] })
  })

  it('ignores project.json with a foreign kind', async () => {
    const raw = storeZip({
      'project.json': JSON.stringify({ kind: 'something-else', agentNames: ['alice'] }),
    })
    const peek = await peekZipPackage(new File([Uint8Array.from(raw)], 'x.zip', { type: 'application/zip' }))
    expect(peek.kind).toBe('unknown')
  })

  it('falls back to root agent.json for single-agent zip', async () => {
    const peek = await peekZipPackage(goExportFile())
    expect(peek).toEqual({ kind: 'agent', name: 'peek_test' })
  })

  it('returns unrecognized when neither manifest exists', async () => {
    const raw = storeZip({ 'readme.txt': 'no manifests' })
    const peek = await peekZipPackage(new File([Uint8Array.from(raw)], 'x.zip', { type: 'application/zip' }))
    expect(peek.kind).toBe('unknown')
  })
})
