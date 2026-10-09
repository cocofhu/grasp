#!/usr/bin/env node
/**
 * govulncheck for server, sandbox-gateway/gateway, and sandbox-gateway/sandbox.
 * Called symbols fail the process unless listed in govulncheck-allowlist.json
 * with a reason and an expires date. Expired entries fail even after the
 * finding is gone, so exceptions get revisited. A fixed_version is printed
 * next to the finding; allow it only with a reason and an expiry. The
 * current toolchain is Go 1.26.x with a patch of at least 1.26.9.
 *
 * Does not read Actions secrets. Invoke via govulncheck-check.sh, which pins
 * the scanner binary.
 */
import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

const MODULES = ['server', 'sandbox-gateway/gateway', 'sandbox-gateway/sandbox']
const root = join(import.meta.dirname, '..', '..')
const today = new Date().toISOString().slice(0, 10)
const bin = process.env.GOVULNCHECK_BIN || 'govulncheck'

const allowlist = JSON.parse(readFileSync(join(root, 'govulncheck-allowlist.json'), 'utf8'))
if (!Array.isArray(allowlist)) {
  console.error('govulncheck-check: govulncheck-allowlist.json must be an array')
  process.exit(1)
}

let failed = false
for (const entry of allowlist) {
  if (!entry || !entry.id || !entry.module || !entry.reason || !entry.expires) {
    console.error(`allowlist entry missing id, module, reason, or expires: ${JSON.stringify(entry)}`)
    failed = true
    continue
  }
  if (!MODULES.includes(entry.module)) {
    console.error(`allowlist module must be one of ${MODULES.join(', ')}: ${entry.module}`)
    failed = true
  }
  if (!/^\d{4}-\d{2}-\d{2}$/.test(entry.expires)) {
    console.error(`allowlist expires must be YYYY-MM-DD: ${entry.id}`)
    failed = true
  } else if (entry.expires < today) {
    console.error(`allowlist entry expired on ${entry.expires}: ${entry.module} ${entry.id}`)
    failed = true
  }
}
if (failed) process.exit(1)

const allowed = new Map(allowlist.map((entry) => [`${entry.module}\t${entry.id}`, entry]))
const reported = new Map()

for (const moduleDir of MODULES) {
  const cwd = join(root, moduleDir)
  let raw = ''
  let status = 0
  try {
    raw = execFileSync(bin, ['-json', './...'], {
      cwd,
      encoding: 'utf8',
      maxBuffer: 64 << 20,
      env: { ...process.env, GOPROXY: process.env.GOPROXY || 'https://proxy.golang.org,direct' },
    })
  } catch (err) {
    status = err.status ?? 1
    raw = err.stdout || ''
    if (status !== 3) {
      const detail = (err.stderr || err.message || '').toString().trim()
      console.error(`govulncheck failed in ${moduleDir} (exit ${status})`)
      if (detail) console.error(detail)
      process.exit(1)
    }
  }

  const called = new Map()
  let messages
  try {
    messages = parseJSONValues(raw)
  } catch (err) {
    console.error(`govulncheck-check: non-JSON output from ${moduleDir}: ${err.message}`)
    process.exit(1)
  }
  for (const msg of messages) {
    const finding = msg.finding
    if (!finding?.osv) continue
    const fn = finding.trace?.[0]?.function || ''
    if (!fn) continue
    const prev = called.get(finding.osv)
    const fixed = finding.fixed_version || ''
    if (!prev) {
      called.set(finding.osv, { fixed })
    } else if (!fixed) {
      prev.fixed = ''
    }
  }

  if (status === 3 && called.size === 0) {
    console.error(`govulncheck-check: ${moduleDir} exited 3 without a called-symbol finding`)
    process.exit(1)
  }

  for (const [id, info] of called) {
    reported.set(`${moduleDir}\t${id}`, { moduleDir, id, fixed: info.fixed })
  }
  console.log(`scanned ${moduleDir}: ${called.size} called vulnerabilities`)
}

for (const { moduleDir, id, fixed } of reported.values()) {
  const key = `${moduleDir}\t${id}`
  const entry = allowed.get(key)
  const fixNote = fixed ? ` fixed in ${fixed}` : ' no patched release'
  if (!entry) {
    console.error(`not allowlisted:${fixNote}: ${moduleDir} ${id}`)
    failed = true
    continue
  }
  console.log(`allowlisted until ${entry.expires}: ${moduleDir} ${id}${fixNote} (${entry.reason})`)
}

for (const entry of allowlist) {
  if (!reported.has(`${entry.module}\t${entry.id}`)) {
    console.log(`notice: ${entry.module} ${entry.id} is no longer reported; remove it from govulncheck-allowlist.json`)
  }
}

if (failed) process.exit(1)
console.log('ok: no unallowlisted called vulnerabilities')

// govulncheck -json prints a stream of pretty-printed objects, not one object per line.
function parseJSONValues(raw) {
  const messages = []
  const n = raw.length
  let i = 0
  while (i < n) {
    while (i < n && /\s/.test(raw[i])) i++
    if (i >= n) break
    if (raw[i] !== '{') {
      throw new Error(`expected object at offset ${i}`)
    }
    const start = i
    let depth = 0
    let inStr = false
    let esc = false
    for (; i < n; i++) {
      const c = raw[i]
      if (inStr) {
        if (esc) {
          esc = false
          continue
        }
        if (c === '\\') {
          esc = true
          continue
        }
        if (c === '"') inStr = false
        continue
      }
      if (c === '"') {
        inStr = true
        continue
      }
      if (c === '{') depth++
      else if (c === '}') {
        depth--
        if (depth === 0) {
          i++
          messages.push(JSON.parse(raw.slice(start, i)))
          break
        }
      }
    }
    if (depth !== 0) throw new Error('truncated JSON object')
  }
  return messages
}
