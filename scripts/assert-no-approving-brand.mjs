#!/usr/bin/env node
/**
 * Brand clear guard: fail CI when product protocol/storage identifiers
 * reintroduce Approving brand strings outside the allowlist.
 *
 * Allowed (plan constraints):
 * - CHANGELOG.md historical text
 * - github.com/cocofhu/approving* / approving-pages / approving-ai.com
 * - buttons.approving / i18n verb "Approving…"
 * - migrateBrandStorage LEGACY_* tables and approving-* as migration sources
 * - LEGACY_PRODUCT_NAME / BrandLogo stored Approving → display Grasp
 * - comments documenting the old onboarding-dismiss key
 * - node type=approve / Approve node concept
 *
 * Uses a Node walk (not ripgrep) so GitHub-hosted runners without `rg` still pass.
 */
import { readdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

const patterns = [
  String.raw`approving-theme`,
  String.raw`approving-locale`,
  String.raw`approving-sidebar-hidden`,
  String.raw`approving-drafts`,
  String.raw`approving-project-context`,
  String.raw`approving-runs-status-filter`,
  String.raw`approving-onboarding-suppress:`,
  String.raw`approving\.workflowFavorites\.`,
  String.raw`approving\.home\.`,
  String.raw`approving\.gateShareUrl\.`,
  String.raw`application/approving-node`,
  String.raw`/__approving/`,
  String.raw`X-Approving-Doctor-Cleanup`,
  String.raw`approving-sb-`,
  String.raw`approving\.managed`,
  String.raw`approving\.name`,
  String.raw`approving-vcs:`,
  String.raw`approving-mcp-spa-proxy`,
  String.raw`approving-mcp-advertise`,
  String.raw`/tmp/approving-ssh-inject`,
  String.raw`approving-acp-`,
]

const skipDirNames = new Set([
  '.git',
  'node_modules',
  'dist',
  'coverage',
  '.vite',
  'vendor',
])

// Generated build output (gitignored), skipped by repo-relative path: the
// source files are scanned under their own allowlisted paths.
const skipDirPaths = new Set([
  'docs/public', // docs/scripts/build.mjs copies docs/site here
])

const skipFileNames = new Set(['CHANGELOG.md'])

const skipExt = new Set([
  '.png', '.jpg', '.jpeg', '.gif', '.webp', '.ico', '.pdf',
  '.woff', '.woff2', '.ttf', '.eot', '.mp4', '.mp3', '.zip',
  '.gz', '.tgz', '.wasm', '.bin', '.lock',
])

const allowPathSubstrings = [
  'CHANGELOG.md',
  'migrateBrandStorage.ts',
  'migrateBrandStorage.test.ts',
  'useBrandSettings.ts',
  'BrandLogo.test.ts',
  'scripts/assert-no-approving-brand.mjs',
  'web/index.html', // boot script may read legacy once
  'docs/site/js/locale.js', // may read legacy once
  'theme.test.ts',
  'locale.test.ts',
  'useWorkflowFavorites.ts', // legacyFavoritesKeyForUser migration source
]

const allowLineRegexes = [
  /github\.com\/cocofhu\/approving/,
  /approving-pages/,
  /approving-ai\.com/,
  /buttons\.approving/,
  /LEGACY_PRODUCT_NAME/,
  /LEGACY_STORAGE_KEYS/,
  /LEGACY_DRAFT_IDB_NAME/,
  /LEGACY_KEY/,
  /approving-onboarding-dismiss:/,
  /"approving":\s*"Approving/,
  /product_name:\s*'Approving'/,
  /migrate from approving-/,
  /copy stores from approving-drafts/,
  /localStorage\.getItem\('approving-locale'\)/,
  /localStorage\.getItem\("approving-locale"\)/,
  /removeItem\('approving-locale'\)/,
  /removeItem\("approving-locale"\)/,
  /setItem\('approving-theme'/, // migration unit tests
  /setItem\('approving-locale'/,
  /setItem\("approving-locale"/,
  /approving\.workflowFavorites\.alice/, // migration unit tests
  /approving\.gateShareUrl\.r1/,
  /notifications\.prefs/,
  /runTerminalNotifications\.readIds/,
]

const hitRe = new RegExp(patterns.join('|'))

function walk(dir, acc) {
  let entries
  try {
    entries = readdirSync(dir, { withFileTypes: true })
  } catch {
    return
  }
  for (const ent of entries) {
    if (ent.isDirectory()) {
      if (skipDirNames.has(ent.name)) continue
      if (ent.name.startsWith('.') && ent.name !== '.github') continue
      const sub = path.join(dir, ent.name)
      if (skipDirPaths.has(path.relative(root, sub).split(path.sep).join('/'))) continue
      walk(sub, acc)
      continue
    }
    if (skipFileNames.has(ent.name)) continue
    const ext = path.extname(ent.name).toLowerCase()
    if (skipExt.has(ext)) continue
    acc.push(path.join(dir, ent.name))
  }
}

function main() {
  if (root === path.parse(root).root) {
    console.error(`assert-no-approving-brand: refusing to walk filesystem root (${root})`)
    process.exit(2)
  }
  const files = []
  walk(root, files)

  const offenders = []
  for (const abs of files) {
    const rel = path.relative(root, abs).split(path.sep).join('/')
    if (allowPathSubstrings.some((s) => rel.includes(s))) continue
    let text
    try {
      text = readFileSync(abs, 'utf8')
    } catch {
      continue
    }
    if (!hitRe.test(text)) continue
    const lines = text.split('\n')
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i]
      if (!hitRe.test(line)) continue
      const rec = `${rel}:${i + 1}:${line}`
      if (allowLineRegexes.some((re) => re.test(rec))) continue
      offenders.push(rec)
    }
  }

  if (offenders.length) {
    console.error('assert-no-approving-brand: forbidden brand remnants:\n')
    for (const o of offenders.slice(0, 80)) console.error(o)
    if (offenders.length > 80) console.error(`… and ${offenders.length - 80} more`)
    process.exit(1)
  }
  console.log('assert-no-approving-brand: ok')
}

main()
