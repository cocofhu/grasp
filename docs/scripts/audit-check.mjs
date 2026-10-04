#!/usr/bin/env node
/**
 * `npm audit --audit-level=high` with a reviewed, expiring allowlist.
 * Same rules as web/scripts/audit-check.mjs: fail on any high/critical
 * advisory not in audit-allowlist.json, and on allowlist entries past their
 * `expires` date. An expired entry fails even when npm no longer reports it,
 * so a stale exemption cannot linger. Every entry needs id, reason, and expires.
 */
import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

const LEVELS = new Set(['high', 'critical'])
const allowlist = JSON.parse(readFileSync(join(process.cwd(), 'audit-allowlist.json'), 'utf8'))
if (!Array.isArray(allowlist)) {
  console.error('audit-check: audit-allowlist.json must be an array')
  process.exit(1)
}
const today = new Date().toISOString().slice(0, 10)

let failed = false
for (const entry of allowlist) {
  if (!entry || !entry.id || !entry.reason || !entry.expires) {
    console.error(`allowlist entry missing id, reason, or expires: ${JSON.stringify(entry)}`)
    failed = true
    continue
  }
  if (!/^\d{4}-\d{2}-\d{2}$/.test(entry.expires)) {
    console.error(`allowlist expires must be YYYY-MM-DD: ${entry.id}`)
    failed = true
  } else if (entry.expires < today) {
    console.error(`allowlist entry expired on ${entry.expires}: ${entry.id}`)
    failed = true
  }
}
if (failed) process.exit(1)

let raw
try {
  raw = execFileSync('npm', ['audit', '--json'], { encoding: 'utf8', maxBuffer: 64 << 20 })
} catch (e) {
  // npm audit exits non-zero whenever it reports vulnerabilities.
  raw = e.stdout
}
let report
try {
  report = JSON.parse(raw || '')
} catch {
  console.error('audit-check: npm audit returned no JSON report')
  process.exit(1)
}
if (report.error) {
  console.error('audit-check: npm audit failed:', report.error.summary || report.error)
  process.exit(1)
}

const advisories = new Map()
for (const v of Object.values(report.vulnerabilities || {})) {
  for (const via of v.via) {
    if (typeof via !== 'object' || !LEVELS.has(via.severity)) continue
    const id = String(via.url || '').split('/').pop() || String(via.source)
    advisories.set(id, `${via.name} ${via.range}: ${via.title}`)
  }
}

const allowed = new Map(allowlist.map((a) => [a.id, a]))
for (const [id, desc] of advisories) {
  const entry = allowed.get(id)
  if (!entry) {
    console.error(`not allowlisted: ${id} ${desc}`)
    failed = true
  } else {
    console.log(`allowlisted until ${entry.expires}: ${id} ${desc}`)
  }
}
for (const id of allowed.keys()) {
  if (!advisories.has(id)) console.log(`notice: ${id} is no longer reported; remove it from audit-allowlist.json`)
}
if (failed) process.exit(1)
console.log(`ok: no unallowlisted high/critical advisories (${advisories.size} allowlisted)`)
