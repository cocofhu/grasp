/** Trigger a browser download of a ZIP blob. */
export function downloadZip(blob: Blob, filename: string) {
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = filename
  a.click()
  URL.revokeObjectURL(a.href)
}

/** Write-identity charset: Unicode letters/digits plus ASCII _/- (Demo「改后」). */
const NAME_RE = /^[\p{L}\p{N}_-]+$/u
const MAX_AGENT_NAME_RUNES = 64
const FULLWIDTH_PUNCT_RE = /[－＿．／＼、，。！？：；（）【】]/

/** NFC-normalize and trim an Agent name (identity key). */
export function normalizeAgentName(name: string): string {
  try {
    return name.normalize('NFC').trim()
  } catch {
    return name.trim()
  }
}

function agentNameRuneCount(name: string): number {
  return Array.from(name).length
}

/**
 * Validate a write-path Agent name (create / rename / import conflict rename).
 * Returns '' when valid, or a stable code: required | invalid.
 */
export function validateAgentName(name: string): string {
  const v = normalizeAgentName(name)
  if (!v) return 'required'
  if (agentNameRuneCount(v) > MAX_AGENT_NAME_RUNES) return 'invalid'
  if (/[\s\u3000]/.test(v)) return 'invalid'
  if (/[./\\]/.test(v) || v === '.' || v === '..') return 'invalid'
  if (FULLWIDTH_PUNCT_RE.test(v)) return 'invalid'
  if (!NAME_RE.test(v)) return 'invalid'
  return ''
}

/** Resolve import target name from ZIP metadata and filename. */
export function resolveImportName(agentJsonName: string | undefined, zipFileName: string): string {
  const fromJson = agentJsonName?.trim()
  if (fromJson) return fromJson
  return zipFileName.replace(/\.zip$/i, '').trim()
}

/** Suggest a non-conflicting rename like `{name}_v2`. */
export function suggestRename(name: string, existing: string[]): string {
  let n = 2
  let candidate = `${name}_v${n}`
  const set = new Set(existing)
  while (set.has(candidate)) {
    n++
    candidate = `${name}_v${n}`
  }
  return candidate
}

export type ZipPeek =
  | { kind: 'project-bundle'; agentNames: string[] }
  | { kind: 'agent'; name?: string }
  | { kind: 'unknown'; error: string }

export const PROJECT_BUNDLE_MANIFEST = 'project.json'
export const PROJECT_BUNDLE_KIND = 'project-agents'

/** Peek a ZIP: project.json (project bundle) first, then root agent.json. */
export async function peekZipPackage(file: File): Promise<ZipPeek> {
  try {
    const buf = new Uint8Array(await file.arrayBuffer())
    const manifestText = await readZipTextEntry(buf, PROJECT_BUNDLE_MANIFEST)
    if (manifestText != null) {
      try {
        const json = JSON.parse(manifestText) as { kind?: string; agentNames?: unknown }
        if (json?.kind === PROJECT_BUNDLE_KIND) {
          const names = Array.isArray(json.agentNames)
            ? json.agentNames.map((n) => String(n).trim()).filter(Boolean)
            : []
          return { kind: 'project-bundle', agentNames: names }
        }
      } catch {
        return { kind: 'unknown', error: 'invalid zip' }
      }
    }
    const agentText = await readZipTextEntry(buf, 'agent.json')
    if (agentText != null) {
      const json = JSON.parse(agentText) as { name?: string }
      return { kind: 'agent', name: typeof json.name === 'string' ? json.name.trim() : undefined }
    }
    return { kind: 'unknown', error: 'unrecognized' }
  } catch {
    return { kind: 'unknown', error: 'invalid zip' }
  }
}

/** Read agent.json name field from a ZIP file (client-side peek for conflict UI). */
export async function peekAgentZipName(file: File): Promise<{ name?: string; error?: string }> {
  const peek = await peekZipPackage(file)
  if (peek.kind === 'agent') return { name: peek.name }
  if (peek.kind === 'unknown' && peek.error === 'invalid zip') return { error: 'invalid zip' }
  return { error: 'missing agent.json' }
}

function readUint16LE(data: Uint8Array, off: number): number {
  return data[off] | (data[off + 1] << 8)
}

function readUint32LE(data: Uint8Array, off: number): number {
  return (
    (data[off] |
      (data[off + 1] << 8) |
      (data[off + 2] << 16) |
      (data[off + 3] << 24)) >>> 0
  )
}

function findEocd(data: Uint8Array): number {
  const minEocd = 22
  const maxComment = 65535
  const start = Math.max(0, data.length - minEocd - maxComment)
  for (let i = data.length - minEocd; i >= start; i--) {
    if (data[i] === 0x50 && data[i + 1] === 0x4b && data[i + 2] === 0x05 && data[i + 3] === 0x06) {
      return i
    }
  }
  return -1
}

interface ZipCdEntry {
  name: string
  method: number
  compSize: number
  localOffset: number
}

function parseCentralDirectory(data: Uint8Array): ZipCdEntry[] {
  const eocd = findEocd(data)
  if (eocd < 0) return []

  const cdSize = readUint32LE(data, eocd + 12)
  const cdOffset = readUint32LE(data, eocd + 16)
  const entries: ZipCdEntry[] = []

  let off = cdOffset
  const cdEnd = cdOffset + cdSize
  while (off + 46 <= cdEnd && off + 46 <= data.length) {
    if (data[off] !== 0x50 || data[off + 1] !== 0x4b || data[off + 2] !== 0x01 || data[off + 3] !== 0x02) {
      break
    }
    const method = readUint16LE(data, off + 10)
    const compSize = readUint32LE(data, off + 20)
    const nameLen = readUint16LE(data, off + 28)
    const extraLen = readUint16LE(data, off + 30)
    const commentLen = readUint16LE(data, off + 32)
    const localOffset = readUint32LE(data, off + 42)

    const nameStart = off + 46
    const nameEnd = nameStart + nameLen
    if (nameEnd > data.length) break
    const name = new TextDecoder().decode(data.subarray(nameStart, nameEnd)).replace(/\\/g, '/')
    entries.push({
      name: name.replace(/^\.\//, ''),
      method,
      compSize,
      localOffset,
    })
    off = nameEnd + extraLen + commentLen
  }
  return entries
}

const INFLATE_TIMEOUT_MS = 3000

async function inflateDeflateRaw(body: Uint8Array): Promise<Uint8Array | null> {
  if (body.length === 0) return new Uint8Array(0)
  try {
    const inflated = (async () => {
      const ds = new DecompressionStream('deflate-raw')
      const writer = ds.writable.getWriter()
      await writer.write(new Uint8Array(body))
      await writer.close()
      return new Uint8Array(await new Response(ds.readable).arrayBuffer())
    })()
    const timeout = new Promise<null>((resolve) => setTimeout(() => resolve(null), INFLATE_TIMEOUT_MS))
    return (await Promise.race([inflated, timeout])) ?? null
  } catch {
    return null
  }
}

async function readEntryData(data: Uint8Array, entry: ZipCdEntry): Promise<Uint8Array | null> {
  const { localOffset, compSize, method } = entry
  if (localOffset + 30 > data.length) return null
  if (data[localOffset] !== 0x50 || data[localOffset + 1] !== 0x4b || data[localOffset + 2] !== 0x03 || data[localOffset + 3] !== 0x04) {
    return null
  }

  const nameLen = readUint16LE(data, localOffset + 26)
  const extraLen = readUint16LE(data, localOffset + 28)
  const dataStart = localOffset + 30 + nameLen + extraLen
  const dataEnd = dataStart + compSize
  if (dataEnd > data.length) return null

  const body = data.subarray(dataStart, dataEnd)
  if (method === 0) return body
  if (method === 8) {
    const inflated = await inflateDeflateRaw(body)
    return inflated
  }
  return null
}

/** Read a text entry via central directory (handles Go archive/zip compSize=0 local headers). */
async function readZipTextEntry(data: Uint8Array, want: string): Promise<string | null> {
  const entry = parseCentralDirectory(data).find((e) => e.name === want)
  if (!entry) return null
  const raw = await readEntryData(data, entry)
  if (raw == null) return null
  return new TextDecoder().decode(raw)
}

/** Parse error body from a failed import response. */
async function parseImportError(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string }
    if (body?.error) return body.error
  } catch {
    // non-JSON
  }
  return `${res.status} import failed`
}
